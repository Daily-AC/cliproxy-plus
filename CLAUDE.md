# AI Deployment Guide

This document is for AI coding assistants (Claude Code, Cursor, etc.) helping deploy and develop this codebase.

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

## Deployment

### Prerequisites

- A Linux server (amd64) with public IP
- Go 1.26+ (for building, can cross-compile from another machine)
- Node.js 20+ / npm (for frontend build)
- A reverse proxy (Caddy / Nginx) for HTTPS termination (recommended)

### Directory Layout

Create a working directory on your server (e.g., `/opt/cliproxyapi/`):

```
/opt/cliproxyapi/
├── cliproxyapi          # Binary
├── config.yaml          # Configuration
├── auths/               # Auth credential files (auto-generated)
└── static/
    └── management.html  # Frontend HTML (from frontend/dist/index.html)
```

### Minimal config.yaml

```yaml
api-key:
  - your-api-key-here
auth-dir: ./auths
proxy-url: ''            # HTTPS proxy if needed, empty for direct connection
remote-management:
  allow-remote: true
  disable-control-panel: false
  secret-key: <bcrypt-hash-of-your-management-password>
```

Generate bcrypt hash: `htpasswd -nbBC 10 "" 'your-password' | cut -d: -f2`

### Deploy Steps

```bash
# 1. Build (on your dev machine)
cd backend && GOOS=linux GOARCH=amd64 go build -o /tmp/cliproxyapi-linux ./cmd/server/
cd ../frontend && npm run build

# 2. Upload to server
scp /tmp/cliproxyapi-linux yourserver:/opt/cliproxyapi/cliproxyapi
scp frontend/dist/index.html yourserver:/opt/cliproxyapi/static/management.html

# 3. Start on server
ssh yourserver
chmod +x /opt/cliproxyapi/cliproxyapi
cd /opt/cliproxyapi
nohup ./cliproxyapi server --config ./config.yaml >> /tmp/cliproxyapi.log 2>&1 &
```

### Updating (restart)

```bash
# Kill old process
kill -9 $(lsof -t -i :8317 | head -1)
sleep 2

# Replace binary and restart
cp /tmp/cliproxyapi-new /opt/cliproxyapi/cliproxyapi
chmod +x /opt/cliproxyapi/cliproxyapi
cd /opt/cliproxyapi
nohup ./cliproxyapi server --config ./config.yaml >> /tmp/cliproxyapi.log 2>&1 &
```

### Reverse Proxy (Caddy example)

```
your-domain.com {
    reverse_proxy localhost:8317
}
```

Service runs on port **8317** by default.

### Common Gotchas

1. **Frontend path**: Management HTML must go to `static/management.html` relative to the config directory, NOT the config directory itself. The backend auto-updater may also overwrite this file from upstream releases.
2. **Port busy**: Use `lsof -t -i :8317` to find and `kill -9` the old process. Regular `kill` may not work if the process is stuck in a goroutine.
3. **Go version**: Requires Go 1.26+. If your dev machine has an older version, either upgrade or build directly on the server.
4. **Reverse proxy restart**: If you use Caddy/Nginx, restart it after restarting CLIProxyAPI to avoid stale upstream connections.
5. **Cloudflare**: If using Cloudflare CDN in front, set SSL mode to "Full (strict)" and add a WAF skip rule for your domain to avoid blocking API requests.

### Verification

```bash
# Check if service is running
curl -s http://localhost:8317/v1/models -H "Authorization: Bearer your-api-key" | python3 -m json.tool

# Test chat (Anthropic format)
curl -s http://localhost:8317/v1/messages \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":50,"messages":[{"role":"user","content":"Hi"}]}'

# Check logs
tail -50 /tmp/cliproxyapi.log

# Access management panel
# Open http://localhost:8317/management.html in browser
```

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
