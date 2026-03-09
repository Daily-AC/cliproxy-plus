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
	anyRouterBaseURL    = "https://anyrouter.top"
	anyRouterMaxRetries = 2
	anyRouterRetryDelay = 500 * time.Millisecond

	// ccIdentifier is the Claude Code identity string used during system prompt reconstruction.
	ccIdentifier = "You are Claude Code, Anthropic's official CLI for Claude."
)

// AnyRouterExecutor handles requests to AnyRouter with custom request transformation.
// AnyRouter requires specific request format transformations to pass validation.
type AnyRouterExecutor struct {
	cfg *config.Config
}

// NewAnyRouterExecutor creates a new AnyRouter executor.
func NewAnyRouterExecutor(cfg *config.Config) *AnyRouterExecutor {
	return &AnyRouterExecutor{cfg: cfg}
}

func (e *AnyRouterExecutor) Identifier() string { return "anyrouter" }

// Execute handles non-streaming AnyRouter requests.
func (e *AnyRouterExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	apiKey, _ := anyRouterCreds(auth)
	if apiKey == "" {
		return resp, fmt.Errorf("anyrouter: missing api key")
	}

	reporter := newUsageReporter(ctx, e.Identifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	to := sdktranslator.FromString("claude")
	stream := from != to
	body := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, stream)
	body, _ = sjson.SetBytes(body, "model", baseModel)

	body, err = thinking.ApplyThinking(body, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return resp, err
	}

	// Apply AnyRouter-specific transformations
	body = transformRequestForAnyRouter(body)

	url := fmt.Sprintf("%s/v1/messages?beta=true", anyRouterBaseURL)

	var lastErr error
	for attempt := 0; attempt <= anyRouterMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return resp, ctx.Err()
			case <-time.After(anyRouterRetryDelay):
			}
		}

		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if reqErr != nil {
			return resp, reqErr
		}
		applyAnyRouterHeaders(httpReq, apiKey)

		var authID, authLabel, authType, authValue string
		if auth != nil {
			authID = auth.ID
			authLabel = auth.Label
			authType, authValue = auth.AccountInfo()
		}
		recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
			URL:       url,
			Method:    http.MethodPost,
			Headers:   httpReq.Header.Clone(),
			Body:      body,
			Provider:  e.Identifier(),
			AuthID:    authID,
			AuthLabel: authLabel,
			AuthType:  authType,
			AuthValue: authValue,
		})

		httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
		httpResp, doErr := httpClient.Do(httpReq)
		if doErr != nil {
			recordAPIResponseError(ctx, e.cfg, doErr)
			lastErr = doErr
			continue
		}
		recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			b, _ := io.ReadAll(httpResp.Body)
			_ = httpResp.Body.Close()
			appendAPIResponseChunk(ctx, e.cfg, b)

			// Retry on 500 with "invalid claude code request"
			if httpResp.StatusCode == 500 && strings.Contains(string(b), "invalid claude code request") {
				log.Debugf("anyrouter: retrying due to 'invalid claude code request' (attempt %d/%d)", attempt+1, anyRouterMaxRetries+1)
				lastErr = statusErr{code: httpResp.StatusCode, msg: string(b)}
				continue
			}

			return resp, statusErr{code: httpResp.StatusCode, msg: string(b)}
		}

		data, readErr := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		if readErr != nil {
			recordAPIResponseError(ctx, e.cfg, readErr)
			return resp, readErr
		}
		appendAPIResponseChunk(ctx, e.cfg, data)
		reporter.publish(ctx, parseClaudeUsage(data))

		var param any
		out := sdktranslator.TranslateNonStream(
			ctx,
			to,
			from,
			req.Model,
			opts.OriginalRequest,
			body,
			data,
			&param,
		)
		resp.Body = out
		resp.Headers = httpResp.Header.Clone()
		return resp, nil
	}

	if lastErr != nil {
		return resp, lastErr
	}
	return resp, fmt.Errorf("anyrouter: request failed after retries")
}

// ExecuteStream handles streaming AnyRouter requests.
func (e *AnyRouterExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName

	apiKey, _ := anyRouterCreds(auth)
	if apiKey == "" {
		return nil, fmt.Errorf("anyrouter: missing api key")
	}

	reporter := newUsageReporter(ctx, e.Identifier(), baseModel, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	to := sdktranslator.FromString("claude")
	body := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, true)
	body, _ = sjson.SetBytes(body, "model", baseModel)

	body, err = thinking.ApplyThinking(body, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return nil, err
	}

	// Apply AnyRouter-specific transformations
	body = transformRequestForAnyRouter(body)
	bodyForTranslation := body

	url := fmt.Sprintf("%s/v1/messages?beta=true", anyRouterBaseURL)

	var httpResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= anyRouterMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(anyRouterRetryDelay):
			}
		}

		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if reqErr != nil {
			return nil, reqErr
		}
		applyAnyRouterHeaders(httpReq, apiKey)

		var authID, authLabel, authType, authValue string
		if auth != nil {
			authID = auth.ID
			authLabel = auth.Label
			authType, authValue = auth.AccountInfo()
		}
		recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
			URL:       url,
			Method:    http.MethodPost,
			Headers:   httpReq.Header.Clone(),
			Body:      body,
			Provider:  e.Identifier(),
			AuthID:    authID,
			AuthLabel: authLabel,
			AuthType:  authType,
			AuthValue: authValue,
		})

		httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
		resp, doErr := httpClient.Do(httpReq)
		if doErr != nil {
			recordAPIResponseError(ctx, e.cfg, doErr)
			lastErr = doErr
			continue
		}
		recordAPIResponseMetadata(ctx, e.cfg, resp.StatusCode, resp.Header.Clone())

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			appendAPIResponseChunk(ctx, e.cfg, b)

			if resp.StatusCode == 500 && strings.Contains(string(b), "invalid claude code request") {
				log.Debugf("anyrouter: retrying due to 'invalid claude code request' (attempt %d/%d)", attempt+1, anyRouterMaxRetries+1)
				lastErr = statusErr{code: resp.StatusCode, msg: string(b)}
				continue
			}

			return nil, statusErr{code: resp.StatusCode, msg: string(b)}
		}

		httpResp = resp
		break
	}

	if httpResp == nil {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("anyrouter: request failed after retries")
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
				log.Errorf("response body close error: %v", errClose)
			}
		}()

		if from == to {
			// Claude -> Claude: forward SSE stream directly, filtering keep-alive lines
			scanner := bufio.NewScanner(decodedBody)
			scanner.Buffer(nil, 52_428_800)
			for scanner.Scan() {
				line := scanner.Bytes()
				// Filter out SSE keep-alive comments
				if bytes.Equal(bytes.TrimSpace(line), []byte(": keep-alive")) {
					continue
				}
				appendAPIResponseChunk(ctx, e.cfg, line)
				if detail, ok := parseClaudeStreamUsage(line); ok {
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

		// For other formats, use translation with keep-alive filtering
		scanner := bufio.NewScanner(decodedBody)
		scanner.Buffer(nil, 52_428_800)
		var param any
		for scanner.Scan() {
			line := scanner.Bytes()
			if bytes.Equal(bytes.TrimSpace(line), []byte(": keep-alive")) {
				continue
			}
			appendAPIResponseChunk(ctx, e.cfg, line)
			if detail, ok := parseClaudeStreamUsage(line); ok {
				reporter.publish(ctx, detail)
			}
			chunks := sdktranslator.TranslateStream(
				ctx,
				to,
				from,
				req.Model,
				opts.OriginalRequest,
				bodyForTranslation,
				bytes.Clone(line),
				&param,
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

// Refresh is a no-op for API key auth.
func (e *AnyRouterExecutor) Refresh(_ context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return auth, nil
}

// CountTokens is not supported by AnyRouter.
func (e *AnyRouterExecutor) CountTokens(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, statusErr{code: http.StatusNotImplemented, msg: "count_tokens not supported for anyrouter"}
}

// HttpRequest injects AnyRouter credentials and executes the HTTP request.
func (e *AnyRouterExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("anyrouter executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	apiKey, _ := anyRouterCreds(auth)
	if apiKey != "" {
		httpReq.Header.Set("X-Api-Key", apiKey)
	}
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// anyRouterCreds extracts AnyRouter credentials from an auth entry.
func anyRouterCreds(a *cliproxyauth.Auth) (apiKey, baseURL string) {
	if a == nil {
		return "", ""
	}
	if a.Attributes != nil {
		apiKey = a.Attributes["api_key"]
		baseURL = a.Attributes["base_url"]
	}
	if baseURL == "" {
		baseURL = anyRouterBaseURL
	}
	return
}

// applyAnyRouterHeaders sets the required headers for AnyRouter requests.
func applyAnyRouterHeaders(r *http.Request, apiKey string) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Anthropic-Version", "2023-06-01")
	r.Header.Set("Anthropic-Beta", "claude-code-20250219")
	r.Header.Set("X-Api-Key", apiKey)
}

// transformRequestForAnyRouter applies AnyRouter-specific request transformations:
// 1. Simplifies system blocks (removes billing headers, Agent SDK mentions, CC identifier)
// 2. Simplifies tool input_schema to {"type":"object"}
func transformRequestForAnyRouter(body []byte) []byte {
	body = transformAnyRouterSystem(body)
	body = transformAnyRouterTools(body)
	return body
}

// transformAnyRouterSystem restructures the system array for AnyRouter validation.
// It filters out specific blocks and reconstructs as [CC_ID, merged_text].
func transformAnyRouterSystem(body []byte) []byte {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}

	systemRaw, ok := raw["system"]
	if !ok {
		return body
	}

	var systemBlocks []map[string]json.RawMessage
	if err := json.Unmarshal(systemRaw, &systemBlocks); err != nil {
		// system might be a string, leave as-is
		return body
	}

	var textParts []string
	for _, block := range systemBlocks {
		typeRaw, hasType := block["type"]
		if !hasType {
			continue
		}
		var blockType string
		if err := json.Unmarshal(typeRaw, &blockType); err != nil || blockType != "text" {
			continue
		}
		textRaw, hasText := block["text"]
		if !hasText {
			continue
		}
		var text string
		if err := json.Unmarshal(textRaw, &text); err != nil {
			continue
		}

		// Skip billing header blocks
		if strings.HasPrefix(text, "x-anthropic-billing-header") {
			continue
		}
		// Skip Agent SDK blocks
		if strings.Contains(text, "Claude Agent SDK") {
			continue
		}
		// Skip CC identifier (will be re-added)
		if text == ccIdentifier {
			continue
		}

		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			textParts = append(textParts, trimmed)
		}
	}

	// Rebuild system as [CC_ID, merged_text]
	newSystem := []map[string]interface{}{
		{"type": "text", "text": ccIdentifier},
	}
	if len(textParts) > 0 {
		merged := strings.Join(textParts, "\n\n")
		newSystem = append(newSystem, map[string]interface{}{"type": "text", "text": merged})
	}

	newSystemJSON, err := json.Marshal(newSystem)
	if err != nil {
		return body
	}

	raw["system"] = newSystemJSON
	result, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return result
}

// transformAnyRouterTools simplifies tool schemas for AnyRouter validation.
// Each tool keeps name, description, and cache_control, but input_schema becomes {"type":"object"}.
func transformAnyRouterTools(body []byte) []byte {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}

	toolsRaw, ok := raw["tools"]
	if !ok {
		return body
	}

	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(toolsRaw, &tools); err != nil {
		return body
	}

	simplifiedSchema := json.RawMessage(`{"type":"object"}`)
	newTools := make([]map[string]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		newTool := map[string]json.RawMessage{
			"input_schema": simplifiedSchema,
		}
		if name, ok := tool["name"]; ok {
			newTool["name"] = name
		}
		if desc, ok := tool["description"]; ok {
			newTool["description"] = desc
		}
		if cc, ok := tool["cache_control"]; ok {
			newTool["cache_control"] = cc
		}
		newTools = append(newTools, newTool)
	}

	newToolsJSON, err := json.Marshal(newTools)
	if err != nil {
		return body
	}

	raw["tools"] = newToolsJSON
	result, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return result
}
