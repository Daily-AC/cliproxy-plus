# AI Deployment Guide

This document is for AI coding assistants (Claude Code, Cursor, etc.) working on this codebase.

## Project Overview

CLIProxyAPI Plus is a Go backend + React frontend project. The backend proxies LLM API requests across multiple providers with credential rotation. The frontend is a single-page management panel compiled to one HTML file.

## Repository Structure

- `backend/` — Go 1.26 module (`github.com/router-for-me/CLIProxyAPI/v6`)
- `frontend/` — React 19 + TypeScript + Vite, outputs `dist/index.html`
- No monorepo tooling; build backend and frontend independently

## Build Commands

```bash
# Backend (from backend/)
go build -o cliproxyapi ./cmd/server/

# Cross-compile for Linux amd64 (from backend/)
GOOS=linux GOARCH=amd64 go build -o /tmp/cliproxyapi-linux ./cmd/server/

# Frontend (from frontend/)
npm install    # first time only
npm run build  # outputs dist/index.html (~2.2MB single file)
```

## Deployment to LD Server

The production server is at `ssh ld` (86.53.183.23). Service runs on port 8317 behind Caddy reverse proxy + Cloudflare CDN at `https://cliproxy.qmledmq.cn`.

### Deploy Steps

```bash
# 1. Build
cd backend && GOOS=linux GOARCH=amd64 go build -o /tmp/cliproxyapi-linux ./cmd/server/
cd ../frontend && npm run build

# 2. Upload
scp /tmp/cliproxyapi-linux ld:/tmp/cliproxyapi-new
scp frontend/dist/index.html ld:/root/cliproxyapi/static/management.html

# 3. Restart (on LD)
ssh ld
kill -9 $(lsof -t -i :8317 | head -1)
sleep 2
cp /tmp/cliproxyapi-new /root/cliproxyapi/cliproxyapi
chmod +x /root/cliproxyapi/cliproxyapi
cd /root/cliproxyapi
nohup ./cliproxyapi server --config ./config.yaml >> /tmp/cliproxyapi.log 2>&1 &
systemctl restart caddy
```

### Important Paths on LD

| Path | Description |
|------|-------------|
| `/root/cliproxyapi/cliproxyapi` | Binary |
| `/root/cliproxyapi/config.yaml` | Config |
| `/root/cliproxyapi/auths/` | Auth credential files |
| `/root/cliproxyapi/static/management.html` | Frontend HTML (**not** `/root/cliproxyapi/management.html`) |
| `/tmp/cliproxyapi.log` | Runtime logs |

### Common Gotchas

1. **Frontend path**: Management HTML must go to `static/management.html` relative to config dir, NOT the config dir itself. The backend auto-updater may also overwrite this file.
2. **Caddy dies on restart**: When killing the CLIProxyAPI process, Caddy sometimes stops too. Always run `systemctl restart caddy` after restarting the service.
3. **Port busy**: Use `lsof -t -i :8317` to find and `kill -9` the old process. `kill` without `-9` may not work if the process is stuck in a goroutine.
4. **Go version**: Requires Go 1.26+. The local machine may have an older version; cross-compile on a machine with the right Go version, or build on LD directly.

## Code Architecture

### Backend Key Files

| File | Purpose |
|------|---------|
| `cmd/server/main.go` | Entry point |
| `internal/api/server.go` | Route registration |
| `internal/api/handlers/management/auth_files.go` | Management API handlers (OAuth, quota, auth files) |
| `internal/runtime/executor/*.go` | Per-provider request executors |
| `internal/auth/*/` | OAuth flows per provider |
| `internal/registry/model_definitions_static_data.go` | Static model metadata |
| `sdk/cliproxy/service.go` | Core service: executor registration, model registration, auth synthesis |
| `sdk/cliproxy/auth/selector.go` | Credential selection (round-robin, fill-first, model aliases) |

### Adding a New Provider

1. **Auth package**: Create `internal/auth/<provider>/` with OAuth flow + token storage
2. **Executor**: Create `internal/runtime/executor/<provider>_executor.go` implementing the executor interface
3. **Handler**: Add management handler in `auth_files.go` (e.g., `RequestXxxToken()`)
4. **Route**: Register in `server.go` under the `mgmt` group
5. **Service**: Register executor in `service.go` switch on provider name
6. **Frontend**: Add provider to `OAuthPage.tsx` PROVIDERS array, create quota section if needed

### Model Alias System

Three layers handle model aliases:

1. **Scheduler** (`selector.go`): `globalModelAliases` in `canonicalModelKey()` — routes alias to canonical model during credential selection
2. **Registry** (`service.go`): `globalModelAliasesForRegistry` in `applyGlobalModelAliases()` — registers alias in `/v1/models` listing
3. **Executor** (e.g., `anyrouter_executor.go`): `anyRouterModelAliases` — translates alias to upstream model name in API requests

### Frontend Key Patterns

- **Single-file output**: `vite-plugin-singlefile` inlines all assets into one HTML
- **API client**: `services/api/client.ts` — Axios singleton, auto-unwraps `response.data`
- **State**: Zustand stores in `stores/`
- **i18n**: `en.json` + `zh-CN.json` — **be careful with zh-CN.json encoding**: smart quotes (Unicode `"` `"`) in JSON cause TypeScript build errors; always validate with `python3 -c "import json; json.load(open('path'))"` after editing
- **Quota sections**: Each provider gets a component in `components/quota/`, registered in `QuotaPage.tsx`

## Testing

```bash
# Verify API is running
curl -s https://cliproxy.qmledmq.cn/v1/models -H "Authorization: Bearer xiaoxi-cliproxy-key-2026" | python3 -m json.tool

# Test specific model
curl -s https://cliproxy.qmledmq.cn/v1/messages \
  -H "Authorization: Bearer xiaoxi-cliproxy-key-2026" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-haiku-4-5","max_tokens":50,"messages":[{"role":"user","content":"Hi"}]}'

# Check logs
ssh ld 'tail -50 /tmp/cliproxyapi.log'
```

## Cloudflare / DNS

- Domain: `qmledmq.cn` (Cloudflare DNS)
- `cliproxy.qmledmq.cn` → LD (86.53.183.23), proxied through Cloudflare
- Caddy handles HTTPS termination on LD, Cloudflare in Full (strict) mode
