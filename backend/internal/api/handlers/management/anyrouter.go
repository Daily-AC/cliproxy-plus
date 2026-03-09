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

// PostAnyRouterCheckIn triggers a manual check-in for all configured AnyRouter keys.
func (h *Handler) PostAnyRouterCheckIn(c *gin.Context) {
	if h.cfg == nil || len(h.cfg.AnyRouterKey) == 0 {
		c.JSON(200, gin.H{"results": []checkin.CheckInResult{}})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	var results []checkin.CheckInResult
	for i := range h.cfg.AnyRouterKey {
		entry := h.cfg.AnyRouterKey[i]
		if !entry.CheckIn.Enabled || entry.CheckIn.UserID == "" || entry.CheckIn.SessionID == "" {
			continue
		}

		msg, err := checkin.CheckIn(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if err != nil {
			results = append(results, checkin.CheckInResult{
				Index: i,
				Error: err.Error(),
			})
			continue
		}

		r := checkin.CheckInResult{
			Index:   i,
			Message: msg,
		}

		balance, balErr := checkin.QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if balErr == nil {
			r.Balance = balance
		}

		results = append(results, r)
	}

	c.JSON(200, gin.H{"results": results})
}

// GetAnyRouterBalance queries balance for all configured AnyRouter keys.
func (h *Handler) GetAnyRouterBalance(c *gin.Context) {
	if h.cfg == nil || len(h.cfg.AnyRouterKey) == 0 {
		c.JSON(200, gin.H{"results": []checkin.BalanceResult{}})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	var results []checkin.BalanceResult
	for i := range h.cfg.AnyRouterKey {
		entry := h.cfg.AnyRouterKey[i]
		if entry.CheckIn.UserID == "" || entry.CheckIn.SessionID == "" {
			continue
		}

		balance, err := checkin.QueryBalance(ctx, entry.CheckIn.UserID, entry.CheckIn.SessionID)
		if err != nil {
			results = append(results, checkin.BalanceResult{
				Index: i,
				Error: err.Error(),
			})
			continue
		}

		results = append(results, checkin.BalanceResult{
			Index:   i,
			Balance: balance,
		})
	}

	c.JSON(200, gin.H{"results": results})
}

func normalizeAnyRouterKey(entry *config.AnyRouterKey) {
	if entry == nil {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.CheckIn.UserID = strings.TrimSpace(entry.CheckIn.UserID)
	entry.CheckIn.SessionID = strings.TrimSpace(entry.CheckIn.SessionID)
	entry.CheckIn.WebhookURL = strings.TrimSpace(entry.CheckIn.WebhookURL)
}
