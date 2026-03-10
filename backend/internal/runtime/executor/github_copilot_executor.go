package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/sjson"
)

const (
	copilotTokenURL     = "https://api.github.com/copilot_internal/v2/token"
	copilotAPIBaseURL   = "https://api.githubcopilot.com"
	copilotTokenRefresh = 5 * time.Minute // refresh token 5 min before expiry
)

// GithubCopilotExecutor handles requests to GitHub Copilot API.
// It manages Copilot API token exchange (GitHub OAuth → Copilot token) and
// forwards requests in OpenAI chat completions format.
type GithubCopilotExecutor struct {
	cfg *config.Config

	mu          sync.Mutex
	tokenCache  map[string]*copilotTokenEntry // keyed by github access token
}

type copilotTokenEntry struct {
	token     string
	expiresAt time.Time
}

// NewGithubCopilotExecutor creates a new GitHub Copilot executor.
func NewGithubCopilotExecutor(cfg *config.Config) *GithubCopilotExecutor {
	return &GithubCopilotExecutor{
		cfg:        cfg,
		tokenCache: make(map[string]*copilotTokenEntry),
	}
}

func (e *GithubCopilotExecutor) Identifier() string { return "github-copilot" }

// getCopilotToken exchanges a GitHub OAuth token for a short-lived Copilot API token.
func (e *GithubCopilotExecutor) getCopilotToken(ctx context.Context, auth *cliproxyauth.Auth) (string, error) {
	githubToken := e.resolveGithubToken(auth)
	if githubToken == "" {
		return "", fmt.Errorf("github-copilot: missing github access token")
	}

	e.mu.Lock()
	if entry, ok := e.tokenCache[githubToken]; ok && time.Now().Before(entry.expiresAt.Add(-copilotTokenRefresh)) {
		token := entry.token
		e.mu.Unlock()
		return token, nil
	}
	e.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("github-copilot: failed to create token request: %w", err)
	}
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.95.0")
	req.Header.Set("Editor-Plugin-Version", "copilot/1.0.0")

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github-copilot: token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("github-copilot: failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github-copilot: token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err = json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("github-copilot: failed to parse token response: %w", err)
	}

	if tokenResp.Token == "" {
		return "", fmt.Errorf("github-copilot: empty copilot token in response")
	}

	expiresAt := time.Unix(tokenResp.ExpiresAt, 0)
	log.Debugf("github-copilot: obtained API token, expires at %s", expiresAt.Format(time.RFC3339))

	e.mu.Lock()
	e.tokenCache[githubToken] = &copilotTokenEntry{
		token:     tokenResp.Token,
		expiresAt: expiresAt,
	}
	e.mu.Unlock()

	return tokenResp.Token, nil
}

func (e *GithubCopilotExecutor) resolveGithubToken(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	// Try attributes first (set during synthesis), then metadata (from JSON file)
	if auth.Attributes != nil {
		if t := strings.TrimSpace(auth.Attributes["api_key"]); t != "" {
			return t
		}
	}
	if auth.Metadata != nil {
		if t, ok := auth.Metadata["access_token"].(string); ok && t != "" {
			return t
		}
	}
	return ""
}

// Execute handles non-streaming GitHub Copilot requests.
func (e *GithubCopilotExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := newUsageReporter(ctx, e.Identifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	copilotToken, err := e.getCopilotToken(ctx, auth)
	if err != nil {
		return resp, err
	}

	from := opts.SourceFormat
	to := sdktranslator.FromString("openai")
	body := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, false)
	body, _ = sjson.SetBytes(body, "model", baseModel)
	body, _ = sjson.SetBytes(body, "stream", false)

	url := copilotAPIBaseURL + "/chat/completions"

	httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if reqErr != nil {
		return resp, reqErr
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+copilotToken)
	httpReq.Header.Set("Editor-Version", "vscode/1.95.0")
	httpReq.Header.Set("Editor-Plugin-Version", "copilot/1.0.0")
	httpReq.Header.Set("Copilot-Integration-Id", "vscode-chat")

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL: url, Method: http.MethodPost, Headers: httpReq.Header.Clone(),
		Body: body, Provider: e.Identifier(),
		AuthID: authID, AuthLabel: authLabel, AuthType: authType, AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, doErr := httpClient.Do(httpReq)
	if doErr != nil {
		recordAPIResponseError(ctx, e.cfg, doErr)
		return resp, doErr
	}
	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		appendAPIResponseChunk(ctx, e.cfg, b)
		return resp, statusErr{code: httpResp.StatusCode, msg: string(b)}
	}

	data, readErr := io.ReadAll(httpResp.Body)
	_ = httpResp.Body.Close()
	if readErr != nil {
		recordAPIResponseError(ctx, e.cfg, readErr)
		return resp, readErr
	}
	appendAPIResponseChunk(ctx, e.cfg, data)
	reporter.publish(ctx, parseOpenAIUsage(data))

	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, from, req.Model, opts.OriginalRequest, body, data, &param)
	resp.Payload = []byte(out)
	resp.Headers = httpResp.Header.Clone()
	return resp, nil
}

// ExecuteStream handles streaming GitHub Copilot requests.
func (e *GithubCopilotExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	reporter := newUsageReporter(ctx, e.Identifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	copilotToken, err := e.getCopilotToken(ctx, auth)
	if err != nil {
		return nil, err
	}

	from := opts.SourceFormat
	to := sdktranslator.FromString("openai")
	body := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, true)
	body, _ = sjson.SetBytes(body, "model", baseModel)
	body, _ = sjson.SetBytes(body, "stream", true)
	bodyForTranslation := body

	url := copilotAPIBaseURL + "/chat/completions"

	httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if reqErr != nil {
		return nil, reqErr
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+copilotToken)
	httpReq.Header.Set("Editor-Version", "vscode/1.95.0")
	httpReq.Header.Set("Editor-Plugin-Version", "copilot/1.0.0")
	httpReq.Header.Set("Copilot-Integration-Id", "vscode-chat")

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL: url, Method: http.MethodPost, Headers: httpReq.Header.Clone(),
		Body: body, Provider: e.Identifier(),
		AuthID: authID, AuthLabel: authLabel, AuthType: authType, AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, doErr := httpClient.Do(httpReq)
	if doErr != nil {
		recordAPIResponseError(ctx, e.cfg, doErr)
		return nil, doErr
	}
	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		b, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		appendAPIResponseChunk(ctx, e.cfg, b)
		return nil, statusErr{code: httpResp.StatusCode, msg: string(b)}
	}

	decodedBody, decErr := decodeResponseBody(httpResp.Body, httpResp.Header.Get("Content-Encoding"))
	if decErr != nil {
		recordAPIResponseError(ctx, e.cfg, decErr)
		_ = httpResp.Body.Close()
		return nil, decErr
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := decodedBody.Close(); errClose != nil {
				log.Errorf("github-copilot: response body close error: %v", errClose)
			}
		}()

		if from == to {
			// OpenAI → OpenAI: forward SSE stream directly
			scanner := bufio.NewScanner(decodedBody)
			scanner.Buffer(nil, 52_428_800)
			for scanner.Scan() {
				line := scanner.Bytes()
				appendAPIResponseChunk(ctx, e.cfg, line)
				if detail, ok := parseOpenAIStreamUsage(line); ok {
					reporter.publish(ctx, detail)
				}
				cloned := make([]byte, len(line)+1)
				copy(cloned, line)
				cloned[len(line)] = '\n'
				out <- cliproxyexecutor.StreamChunk{Payload: cloned}
			}
			if errScan := scanner.Err(); errScan != nil {
				recordAPIResponseError(ctx, e.cfg, errScan)
				reporter.publishFailure(ctx)
				out <- cliproxyexecutor.StreamChunk{Err: errScan}
			}
			return
		}

		// Translate stream between formats
		scanner := bufio.NewScanner(decodedBody)
		scanner.Buffer(nil, 52_428_800)
		var param any
		for scanner.Scan() {
			line := scanner.Bytes()
			appendAPIResponseChunk(ctx, e.cfg, line)
			if detail, ok := parseOpenAIStreamUsage(line); ok {
				reporter.publish(ctx, detail)
			}
			chunks := sdktranslator.TranslateStream(
				ctx, to, from, req.Model, opts.OriginalRequest,
				bodyForTranslation, bytes.Clone(line), &param,
			)
			for i := range chunks {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunks[i])}
			}
		}
		if errScan := scanner.Err(); errScan != nil {
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}

// Refresh is a no-op. Copilot token refresh happens in getCopilotToken.
func (e *GithubCopilotExecutor) Refresh(_ context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}

// CountTokens is not supported.
func (e *GithubCopilotExecutor) CountTokens(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, statusErr{code: http.StatusNotImplemented, msg: "count_tokens not supported for github-copilot"}
}

// HttpRequest injects Copilot credentials and executes the HTTP request.
func (e *GithubCopilotExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("github-copilot executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	copilotToken, err := e.getCopilotToken(ctx, auth)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+copilotToken)
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}
