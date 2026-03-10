// Package checkin provides automated sign-in functionality for third-party API providers.
package checkin

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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

		// Query balance BEFORE check-in
		balanceBefore := -1.0
		if b, err := QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID); err == nil {
			balanceBefore = b
		}

		result, err := CheckIn(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if err != nil {
			log.Errorf("anyrouter check-in failed for key %d: %v", i, err)
			if entry.CheckIn.WebhookURL != "" {
				_ = SendWebhookCard(ctx, entry.CheckIn.WebhookURL, &CheckInCardData{
					Success:       false,
					Label:         entry.Label,
					Remark:        err.Error(),
					BalanceBefore: balanceBefore,
				})
			}
			continue
		}

		// Query balance AFTER check-in
		balanceAfter := -1.0
		if b, err := QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID); err == nil {
			balanceAfter = b
		}

		log.Infof("anyrouter check-in key %d: success, before=%.2f after=%.2f msg=%s", i, balanceBefore, balanceAfter, result)
		if entry.CheckIn.WebhookURL != "" {
			_ = SendWebhookCard(ctx, entry.CheckIn.WebhookURL, &CheckInCardData{
				Success:       true,
				Label:         entry.Label,
				Remark:        result,
				BalanceBefore: balanceBefore,
				BalanceAfter:  balanceAfter,
			})
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

// acw_sc__v2 WAF challenge solver
var (
	acwArg1Re      = regexp.MustCompile(`var arg1='([0-9A-Fa-f]{40})'`)
	acwPermutation = []int{15, 35, 29, 24, 33, 16, 1, 38, 10, 9, 19, 31, 40, 27, 22, 23, 25, 13, 6, 11, 39, 18, 20, 8, 14, 21, 32, 26, 2, 30, 7, 4, 17, 5, 3, 28, 34, 37, 12, 36}
	acwKey         = "3000176000856006061501533003690027800375"
)

func solveACWChallenge(body []byte) (string, bool) {
	m := acwArg1Re.FindSubmatch(body)
	if m == nil {
		return "", false
	}
	arg1 := string(m[1])
	reordered := make([]byte, len(acwPermutation))
	for i := 0; i < len(arg1) && i < len(acwPermutation); i++ {
		for j := 0; j < len(acwPermutation); j++ {
			if acwPermutation[j] == i+1 {
				reordered[j] = arg1[i]
			}
		}
	}
	u := string(reordered)
	minLen := len(u)
	if len(acwKey) < minLen {
		minLen = len(acwKey)
	}
	var result strings.Builder
	for i := 0; i+1 < minLen; i += 2 {
		uByte, err1 := hex.DecodeString(u[i : i+2])
		kByte, err2 := hex.DecodeString(acwKey[i : i+2])
		if err1 != nil || err2 != nil {
			continue
		}
		result.WriteString(fmt.Sprintf("%02x", uByte[0]^kByte[0]))
	}
	return result.String(), true
}

func doWithACW(ctx context.Context, method, url string, userID, sessionID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Cookie", fmt.Sprintf("session=%s", sessionID))
	req.Header.Set("New-Api-User", userID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	acwCookie, isChallenge := solveACWChallenge(respBody)
	if !isChallenge {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
		}
		return respBody, nil
	}

	log.Debugf("anyrouter: solved acw_sc__v2 challenge, retrying")
	req2, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create retry request: %w", err)
	}
	req2.Header.Set("Cookie", fmt.Sprintf("session=%s; acw_sc__v2=%s", sessionID, acwCookie))
	req2.Header.Set("New-Api-User", userID)

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("retry request failed: %w", err)
	}
	defer resp2.Body.Close()
	respBody2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read retry response: %w", err)
	}
	if resp2.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp2.StatusCode, string(respBody2))
	}
	return respBody2, nil
}

// CheckIn performs a single sign-in request to AnyRouter.
func CheckIn(ctx context.Context, userID, sessionID string) (string, error) {
	body, err := doWithACW(ctx, http.MethodPost, anyRouterSignIn, userID, sessionID)
	if err != nil {
		return "", err
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
	body, err := doWithACW(ctx, http.MethodGet, anyRouterUserSelf, userID, sessionID)
	if err != nil {
		return 0, err
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

// CheckInCardData holds the data for building a Feishu check-in card.
type CheckInCardData struct {
	Success       bool
	Label         string  // key label (e.g. "主力")
	Remark        string  // check-in API message or error
	BalanceBefore float64 // -1 means unknown
	BalanceAfter  float64 // -1 means unknown
	Manual        bool    // true if manually triggered
}

// SendWebhookCard sends a Feishu interactive card notification.
func SendWebhookCard(ctx context.Context, webhookURL string, d *CheckInCardData) error {
	if strings.TrimSpace(webhookURL) == "" {
		return nil
	}

	cst := time.FixedZone("CST", 8*3600)
	now := time.Now().In(cst).Format("2006-01-02 15:04:05")

	triggerType := "⏰ 自动签到"
	if d.Manual {
		triggerType = "🖱️ 手动签到"
	}

	statusIcon := "✅"
	statusText := "签到成功"
	headerColor := "green"
	if !d.Success {
		statusIcon = "❌"
		statusText = "签到失败"
		headerColor = "red"
	}

	label := d.Label
	if label == "" {
		label = "default"
	}

	// Build card body in Lark Markdown
	var body strings.Builder

	// Time + trigger type
	body.WriteString(fmt.Sprintf("**⏱️ 时间：**%s (UTC+8)\n", now))
	body.WriteString(fmt.Sprintf("**🏷️ 账号：**%s\n", label))
	body.WriteString(fmt.Sprintf("**📌 触发：**%s\n", triggerType))

	// Build elements array
	elements := []map[string]interface{}{
		{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": body.String()}},
		{"tag": "hr"},
	}

	if d.Success {
		var balanceSection strings.Builder

		// Before
		balanceSection.WriteString("**📍 签到前**\n")
		if d.BalanceBefore >= 0 {
			balanceSection.WriteString(fmt.Sprintf("　　💰 余额: **$%.2f**\n", d.BalanceBefore))
		} else {
			balanceSection.WriteString("　　💰 余额: *查询失败*\n")
		}

		// After
		balanceSection.WriteString("**📍 签到后**\n")
		if d.BalanceAfter >= 0 {
			balanceSection.WriteString(fmt.Sprintf("　　💰 余额: **$%.2f**\n", d.BalanceAfter))
		} else {
			balanceSection.WriteString("　　💰 余额: *查询失败*\n")
		}

		elements = append(elements,
			map[string]interface{}{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": balanceSection.String()}},
			map[string]interface{}{"tag": "hr"},
		)

		// Change summary
		var note string
		if d.BalanceBefore >= 0 && d.BalanceAfter >= 0 {
			diff := d.BalanceAfter - d.BalanceBefore
			if diff > 0.001 {
				note = fmt.Sprintf("🎉 余额变化: **+$%.2f**", diff)
			} else if diff < -0.001 {
				note = fmt.Sprintf("📉 余额变化: **-$%.2f**", -diff)
			} else {
				note = "ℹ️ 今日已签到，余额无变化"
			}
		} else {
			note = fmt.Sprintf("%s %s", statusIcon, statusText)
		}

		if d.Remark != "" {
			note += fmt.Sprintf("\n📝 备注: %s", d.Remark)
		}

		elements = append(elements,
			map[string]interface{}{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": note}},
		)
	} else {
		// Failure card
		errMsg := fmt.Sprintf("%s **%s**\n📝 错误: %s", statusIcon, statusText, d.Remark)
		elements = append(elements,
			map[string]interface{}{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": errMsg}},
		)
	}

	card := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title":    map[string]string{"tag": "plain_text", "content": "AnyRouter 签到通知"},
				"template": headerColor,
			},
			"elements": elements,
		},
	}

	data, err := json.Marshal(card)
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

// SendWebhook sends a simple text notification to a Feishu webhook URL (legacy).
func SendWebhook(ctx context.Context, webhookURL, message string) error {
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
