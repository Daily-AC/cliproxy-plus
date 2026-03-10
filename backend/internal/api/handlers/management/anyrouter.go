package management

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/checkin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// GetAnyRouterKeys returns the configured AnyRouter API keys.
func (h *Handler) GetAnyRouterKeys(c *gin.Context) {
	c.JSON(200, gin.H{"anyrouter-api-key": h.cfg.AnyRouterKey})
}

// PutAnyRouterKeys replaces all AnyRouter API keys.
func (h *Handler) PutAnyRouterKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.AnyRouterKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.AnyRouterKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	for i := range arr {
		normalizeAnyRouterKey(&arr[i])
	}
	h.cfg.AnyRouterKey = arr
	h.cfg.SanitizeAnyRouterKeys()
	h.persist(c)
}

// DeleteAnyRouterKey removes an AnyRouter API key by api-key value or index.
func (h *Handler) DeleteAnyRouterKey(c *gin.Context) {
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		out := make([]config.AnyRouterKey, 0, len(h.cfg.AnyRouterKey))
		for _, v := range h.cfg.AnyRouterKey {
			if v.APIKey != val {
				out = append(out, v)
			}
		}
		if len(out) != len(h.cfg.AnyRouterKey) {
			h.cfg.AnyRouterKey = out
			h.cfg.SanitizeAnyRouterKeys()
			h.persist(c)
		} else {
			c.JSON(404, gin.H{"error": "item not found"})
		}
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && idx >= 0 && idx < len(h.cfg.AnyRouterKey) {
			h.cfg.AnyRouterKey = append(h.cfg.AnyRouterKey[:idx], h.cfg.AnyRouterKey[idx+1:]...)
			h.cfg.SanitizeAnyRouterKeys()
			h.persist(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}

// PostAnyRouterCheckIn triggers a manual check-in for a single AnyRouter key by index.
func (h *Handler) PostAnyRouterCheckIn(c *gin.Context) {
	if h.cfg == nil || len(h.cfg.AnyRouterKey) == 0 {
		c.JSON(200, gin.H{"status": "error", "error": "no anyrouter keys configured"})
		return
	}
	var body struct {
		Index int `json:"index"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		body.Index = 0
	}
	if body.Index < 0 || body.Index >= len(h.cfg.AnyRouterKey) {
		c.JSON(400, gin.H{"status": "error", "error": "invalid index"})
		return
	}
	entry := h.cfg.AnyRouterKey[body.Index]
	if !entry.CheckIn.Enabled || entry.CheckIn.UserID == "" || entry.CheckIn.SessionID == "" {
		c.JSON(200, gin.H{"status": "error", "error": "check-in not configured for this key"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// Query balance BEFORE check-in
	balanceBefore := -1.0
	if b, err := checkin.QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID); err == nil {
		balanceBefore = b
	}

	msg, err := checkin.CheckIn(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
	if err != nil {
		if entry.CheckIn.WebhookURL != "" {
			go checkin.SendWebhookCard(context.Background(), entry.CheckIn.WebhookURL, &checkin.CheckInCardData{
				Success:       false,
				Label:         entry.Label,
				Remark:        err.Error(),
				BalanceBefore: balanceBefore,
				Manual:        true,
			})
		}
		c.JSON(200, gin.H{"status": "error", "error": err.Error()})
		return
	}

	// Query balance AFTER check-in
	resp := gin.H{"status": "ok", "message": msg}
	balanceAfter := -1.0
	if b, err := checkin.QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID); err == nil {
		balanceAfter = b
		resp["balance"] = b
	}

	if entry.CheckIn.WebhookURL != "" {
		go checkin.SendWebhookCard(context.Background(), entry.CheckIn.WebhookURL, &checkin.CheckInCardData{
			Success:       true,
			Label:         entry.Label,
			Remark:        msg,
			BalanceBefore: balanceBefore,
			BalanceAfter:  balanceAfter,
			Manual:        true,
		})
	}
	c.JSON(200, resp)
}

// GetAnyRouterBalance queries balance for a single AnyRouter key by index.
func (h *Handler) GetAnyRouterBalance(c *gin.Context) {
	if h.cfg == nil || len(h.cfg.AnyRouterKey) == 0 {
		c.JSON(200, gin.H{"status": "error", "error": "no anyrouter keys configured"})
		return
	}
	var idx int
	if idxStr := c.Query("index"); idxStr != "" {
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil {
			idx = 0
		}
	}
	if idx < 0 || idx >= len(h.cfg.AnyRouterKey) {
		c.JSON(400, gin.H{"status": "error", "error": "invalid index"})
		return
	}
	entry := h.cfg.AnyRouterKey[idx]
	if entry.CheckIn.UserID == "" || entry.CheckIn.SessionID == "" {
		c.JSON(200, gin.H{"status": "error", "error": "missing user-id or session-id"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	balance, err := checkin.QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
	if err != nil {
		c.JSON(200, gin.H{"status": "error", "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "ok", "balance": balance})
}

func normalizeAnyRouterKey(entry *config.AnyRouterKey) {
	if entry == nil {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.Label = strings.TrimSpace(entry.Label)
	entry.CheckIn.UserID = strings.TrimSpace(entry.CheckIn.UserID)
	entry.CheckIn.SessionID = strings.TrimSpace(entry.CheckIn.SessionID)
	entry.CheckIn.WebhookURL = strings.TrimSpace(entry.CheckIn.WebhookURL)
}
