// Package checkin provides automated sign-in functionality for third-party API providers.
package checkin

import (
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
	log "github.com/sirupsen/logrus"
)

const (
	anyRouterBaseURL  = "https://anyrouter.top"
	anyRouterSignIn   = anyRouterBaseURL + "/api/user/sign_in"
	anyRouterUserSelf = anyRouterBaseURL + "/api/user/self"
)

// AnyRouterCheckInManager handles scheduled and manual check-ins for AnyRouter accounts.
type AnyRouterCheckInManager struct {
	mu     sync.Mutex
	cfg    *config.Config
	cancel context.CancelFunc
}

// NewAnyRouterCheckInManager creates and starts a new check-in manager.
func NewAnyRouterCheckInManager(cfg *config.Config) *AnyRouterCheckInManager {
	m := &AnyRouterCheckInManager{cfg: cfg}
	m.startScheduler()
	return m
}

// SetConfig updates the configuration (e.g., after hot-reload).
func (m *AnyRouterCheckInManager) SetConfig(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// Stop cancels the background scheduler.
func (m *AnyRouterCheckInManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

// startScheduler launches a goroutine that triggers check-in daily at 00:05 UTC.
func (m *AnyRouterCheckInManager) startScheduler() {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	go func() {
		for {
			now := time.Now().UTC()
			// Calculate next 00:05 UTC
			next := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, time.UTC)
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			delay := next.Sub(now)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
				m.checkInAll(ctx)
			}
		}
	}()
}

// checkInAll performs check-in for all configured AnyRouter keys that have check-in enabled.
func (m *AnyRouterCheckInManager) checkInAll(ctx context.Context) {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	if cfg == nil {
		return
	}

	for i := range cfg.AnyRouterKey {
		entry := cfg.AnyRouterKey[i]
		if !entry.CheckIn.Enabled {
			continue
		}
		if entry.CheckIn.UserID == "" || entry.CheckIn.SessionID == "" {
			log.Warnf("anyrouter check-in: skipping key %d, missing user-id or session-id", i)
			continue
		}

		result, err := CheckIn(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if err != nil {
			log.Errorf("anyrouter check-in failed for key %d: %v", i, err)
			if entry.CheckIn.WebhookURL != "" {
				_ = sendWebhook(ctx, entry.CheckIn.WebhookURL, fmt.Sprintf("AnyRouter check-in failed: %v", err))
			}
			continue
		}

		msg := fmt.Sprintf("AnyRouter check-in success! Message: %s", result)

		// Query balance after check-in
		balance, balErr := QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if balErr == nil {
			msg = fmt.Sprintf("AnyRouter check-in success! Balance: %.2f", balance)
		}

		log.Infof("anyrouter check-in key %d: %s", i, msg)
		if entry.CheckIn.WebhookURL != "" {
			_ = sendWebhook(ctx, entry.CheckIn.WebhookURL, msg)
		}
	}
}

// ManualCheckIn performs an immediate check-in for all configured AnyRouter keys.
// Returns a summary of results for each key.
func (m *AnyRouterCheckInManager) ManualCheckIn(ctx context.Context) []CheckInResult {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	if cfg == nil {
		return nil
	}

	var results []CheckInResult
	for i := range cfg.AnyRouterKey {
		entry := cfg.AnyRouterKey[i]
		if !entry.CheckIn.Enabled {
			continue
		}
		if entry.CheckIn.UserID == "" || entry.CheckIn.SessionID == "" {
			results = append(results, CheckInResult{
				Index: i,
				Error: "missing user-id or session-id",
			})
			continue
		}

		msg, err := CheckIn(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if err != nil {
			results = append(results, CheckInResult{
				Index: i,
				Error: err.Error(),
			})
			continue
		}

		r := CheckInResult{
			Index:   i,
			Message: msg,
		}

		balance, balErr := QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if balErr == nil {
			r.Balance = balance
		}

		results = append(results, r)
	}
	return results
}

// QueryAllBalances queries balance for all configured AnyRouter keys with check-in enabled.
func (m *AnyRouterCheckInManager) QueryAllBalances(ctx context.Context) []BalanceResult {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	if cfg == nil {
		return nil
	}

	var results []BalanceResult
	for i := range cfg.AnyRouterKey {
		entry := cfg.AnyRouterKey[i]
		if entry.CheckIn.UserID == "" || entry.CheckIn.SessionID == "" {
			continue
		}

		balance, err := QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if err != nil {
			results = append(results, BalanceResult{
				Index: i,
				Error: err.Error(),
			})
			continue
		}

		results = append(results, BalanceResult{
			Index:   i,
			Balance: balance,
		})
	}
	return results
}

// CheckInResult represents the result of a single check-in operation.
type CheckInResult struct {
	Index   int     `json:"index"`
	Message string  `json:"message,omitempty"`
	Balance float64 `json:"balance,omitempty"`
	Error   string  `json:"error,omitempty"`
}

// BalanceResult represents the result of a balance query.
type BalanceResult struct {
	Index   int     `json:"index"`
	Balance float64 `json:"balance,omitempty"`
	Error   string  `json:"error,omitempty"`
}

// CheckIn performs a single sign-in request to AnyRouter.
func CheckIn(ctx context.Context, userID, sessionID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anyRouterSignIn, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Cookie", fmt.Sprintf("session=%s", sessionID))
	req.Header.Set("New-Api-User", userID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return string(body), nil
	}

	if !result.Success {
		return "", fmt.Errorf("check-in failed: %s", result.Message)
	}
	return result.Message, nil
}

// QueryBalance queries the current user balance from AnyRouter.
func QueryBalance(ctx context.Context, userID, sessionID string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, anyRouterUserSelf, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Cookie", fmt.Sprintf("session=%s", sessionID))
	req.Header.Set("New-Api-User", userID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Quota float64 `json:"quota"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.Success {
		return 0, fmt.Errorf("query failed")
	}
	// Quota is typically in cents or similar units; divide by 500000 to get dollar-like value
	return result.Data.Quota / 500000.0, nil
}

// sendWebhook sends a notification to a Feishu webhook URL.
func sendWebhook(ctx context.Context, webhookURL, message string) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}
	payload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": message,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook failed with status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
