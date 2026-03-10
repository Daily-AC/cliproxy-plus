package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	// clientID is the GitHub Copilot VS Code extension OAuth client ID.
	clientID = "Iv1.b507a08c87ecfe98"
	// deviceCodeURL is the endpoint for requesting device codes.
	deviceCodeURL = "https://github.com/login/device/code"
	// tokenURL is the endpoint for exchanging device codes for tokens.
	tokenURL = "https://github.com/login/oauth/access_token"
	// defaultPollInterval is the default interval for polling token endpoint.
	defaultPollInterval = 5 * time.Second
	// maxPollDuration is the maximum time to wait for user authorization.
	maxPollDuration = 15 * time.Minute
)

// GithubCopilotAuth handles GitHub Copilot Device Flow authentication.
type GithubCopilotAuth struct {
	httpClient *http.Client
	cfg        *config.Config
}

// NewGithubCopilotAuth creates a new auth service instance.
func NewGithubCopilotAuth(cfg *config.Config) *GithubCopilotAuth {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}
	return &GithubCopilotAuth{httpClient: client, cfg: cfg}
}

// StartDeviceFlow initiates the GitHub Device Flow authentication.
func (g *GithubCopilotAuth) StartDeviceFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("scope", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("github: failed to create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: device code request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: failed to read device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: device code request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var deviceCode DeviceCodeResponse
	if err = json.Unmarshal(bodyBytes, &deviceCode); err != nil {
		return nil, fmt.Errorf("github: failed to parse device code response: %w", err)
	}

	return &deviceCode, nil
}

// PollForToken polls the token endpoint until the user authorizes or the device code expires.
func (g *GithubCopilotAuth) PollForToken(ctx context.Context, deviceCode *DeviceCodeResponse) (*GithubTokenData, error) {
	if deviceCode == nil {
		return nil, fmt.Errorf("github: device code is nil")
	}

	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < defaultPollInterval {
		interval = defaultPollInterval
	}

	deadline := time.Now().Add(maxPollDuration)
	if deviceCode.ExpiresIn > 0 {
		codeDeadline := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)
		if codeDeadline.Before(deadline) {
			deadline = codeDeadline
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Infof("github-copilot: starting device flow polling, interval=%v, deadline=%v", interval, deadline.Format(time.RFC3339))

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("github: context cancelled: %w", ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("github: device code expired")
			}

			log.Debugf("github-copilot: polling token endpoint...")
			token, pollErr, shouldContinue := g.exchangeDeviceCode(ctx, deviceCode.DeviceCode)
			if token != nil {
				log.Infof("github-copilot: token obtained successfully")
				return token, nil
			}
			if !shouldContinue {
				log.Errorf("github-copilot: polling stopped: %v", pollErr)
				return nil, pollErr
			}
			log.Debugf("github-copilot: authorization still pending, continuing...")
		}
	}
}

// exchangeDeviceCode attempts to exchange the device code for an access token.
func (g *GithubCopilotAuth) exchangeDeviceCode(ctx context.Context, deviceCode string) (*GithubTokenData, error, bool) {
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("device_code", deviceCode)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("github: failed to create token request: %w", err), false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		log.Errorf("github-copilot: token exchange HTTP error: %v", err)
		return nil, fmt.Errorf("github: token request failed: %w", err), false
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: failed to read token response: %w", err), false
	}

	var oauthResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		AccessToken      string `json:"access_token"`
		TokenType        string `json:"token_type"`
		Scope            string `json:"scope"`
	}

	if err = json.Unmarshal(bodyBytes, &oauthResp); err != nil {
		return nil, fmt.Errorf("github: failed to parse token response: %w", err), false
	}

	if oauthResp.Error != "" {
		switch oauthResp.Error {
		case "authorization_pending":
			return nil, nil, true
		case "slow_down":
			return nil, nil, true
		case "expired_token":
			return nil, fmt.Errorf("github: device code expired"), false
		case "access_denied":
			return nil, fmt.Errorf("github: access denied by user"), false
		default:
			return nil, fmt.Errorf("github: OAuth error: %s - %s", oauthResp.Error, oauthResp.ErrorDescription), false
		}
	}

	if oauthResp.AccessToken == "" {
		return nil, fmt.Errorf("github: empty access token in response"), false
	}

	log.Infof("github-copilot: device flow authentication successful")

	return &GithubTokenData{
		AccessToken: oauthResp.AccessToken,
		TokenType:   oauthResp.TokenType,
		Scope:       oauthResp.Scope,
	}, nil, false
}

// CreateTokenStorage creates storage from token data.
func (g *GithubCopilotAuth) CreateTokenStorage(tokenData *GithubTokenData) *GithubCopilotTokenStorage {
	return &GithubCopilotTokenStorage{
		AccessToken: tokenData.AccessToken,
		TokenType:   tokenData.TokenType,
		Scope:       tokenData.Scope,
		Type:        "github-copilot",
	}
}

// FetchUserLogin queries the GitHub API for the authenticated user's login name.
func (g *GithubCopilotAuth) FetchUserLogin(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", fmt.Errorf("github: failed to create user request: %w", err)
	}
	req.Header.Set("Authorization", "token "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: user request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("github: failed to read user response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: user request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var user struct {
		Login string `json:"login"`
	}
	if err = json.Unmarshal(bodyBytes, &user); err != nil {
		return "", fmt.Errorf("github: failed to parse user response: %w", err)
	}

	return user.Login, nil
}

// CopilotModelInfo describes a model available through GitHub Copilot.
type CopilotModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	IsDefault     bool   `json:"is_default,omitempty"`
	ModelPicker   bool   `json:"model_picker_enabled,omitempty"`
	Capabilities  string `json:"capabilities,omitempty"`
	RateLimit     string `json:"rate_limit,omitempty"`
	PremiumFactor int    `json:"premium_factor,omitempty"`
}

// CheckCopilotAccess verifies a GitHub token has Copilot access.
// Returns: Copilot API token, list of available models, error.
func CheckCopilotAccess(ctx context.Context, cfg *config.Config, githubToken string) (string, []CopilotModelInfo, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}

	// Step 1: Exchange GitHub token for Copilot API token
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.95.0")
	req.Header.Set("Editor-Plugin-Version", "copilot/1.0.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err = json.Unmarshal(body, &tokenResp); err != nil || tokenResp.Token == "" {
		return "", nil, fmt.Errorf("invalid token response")
	}

	// Step 2: Fetch available models from Copilot API
	modelsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.githubcopilot.com/models", nil)
	if err != nil {
		return tokenResp.Token, nil, nil
	}
	modelsReq.Header.Set("Authorization", "Bearer "+tokenResp.Token)
	modelsReq.Header.Set("Editor-Version", "vscode/1.95.0")
	modelsReq.Header.Set("Editor-Plugin-Version", "copilot/1.0.0")
	modelsReq.Header.Set("Copilot-Integration-Id", "vscode-chat")
	modelsReq.Header.Set("Accept", "application/json")

	modelsResp, err := client.Do(modelsReq)
	if err != nil {
		return tokenResp.Token, nil, nil
	}
	defer modelsResp.Body.Close()

	modelsBody, _ := io.ReadAll(modelsResp.Body)
	if modelsResp.StatusCode != http.StatusOK {
		log.Warnf("github-copilot: models endpoint returned %d: %s", modelsResp.StatusCode, string(modelsBody))
		return tokenResp.Token, nil, nil
	}

	var modelsData struct {
		Data []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Version      string `json:"version"`
			IsDefault    bool   `json:"is_default"`
			ModelPicker  bool   `json:"model_picker_enabled"`
			Capabilities struct {
				Type string `json:"type"`
			} `json:"capabilities"`
			Policy struct {
				Type  string `json:"type"`
				Terms string `json:"terms"`
			} `json:"policy"`
		} `json:"data"`
	}

	if err = json.Unmarshal(modelsBody, &modelsData); err != nil {
		// Try alternate format: just an array
		var arr []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			IsDefault   bool   `json:"is_default"`
			ModelPicker bool   `json:"model_picker_enabled"`
		}
		if err2 := json.Unmarshal(modelsBody, &arr); err2 != nil {
			log.Warnf("github-copilot: failed to parse models response: %v", err)
			return tokenResp.Token, nil, nil
		}
		models := make([]CopilotModelInfo, len(arr))
		for i, m := range arr {
			models[i] = CopilotModelInfo{
				ID:          m.ID,
				Name:        m.Name,
				IsDefault:   m.IsDefault,
				ModelPicker: m.ModelPicker,
			}
		}
		return tokenResp.Token, models, nil
	}

	models := make([]CopilotModelInfo, len(modelsData.Data))
	for i, m := range modelsData.Data {
		models[i] = CopilotModelInfo{
			ID:          m.ID,
			Name:        m.Name,
			IsDefault:   m.IsDefault,
			ModelPicker: m.ModelPicker,
			Capabilities: m.Capabilities.Type,
		}
	}
	return tokenResp.Token, models, nil
}
