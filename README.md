# CLIProxyAPI Plus

CLIProxyAPI Plus is a customized fork of [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) — an API proxy that provides unified OpenAI/Claude/Gemini-compatible endpoints with multi-credential rotation, quota management, and a web-based management panel.

## Added Features (over upstream)

- **AnyRouter Provider** — Built-in transform proxy for AnyRouter API, auto check-in with Feishu webhook notifications, balance query
- **GitHub Copilot** — Device Flow OAuth authentication, Copilot API token exchange, dedicated executor
- **Haiku Alias** — `claude-haiku-4-5` routes transparently to `claude-haiku-4-5-20251001`
- **Management Panel Enhancements** — AnyRouter multi-key management, GitHub Copilot quota display, device code UI for OAuth

## Architecture

```
backend/              Go 1.26 API server
├── cmd/server/       Entry point
├── internal/
│   ├── api/          Gin HTTP handlers + routes
│   ├── auth/         OAuth providers (github/, kimi/, codex/, etc.)
│   ├── checkin/      AnyRouter auto check-in
│   ├── config/       YAML config parsing
│   ├── registry/     Model registry + static definitions
│   ├── runtime/      Request executors (anthropic, openai, copilot, anyrouter, etc.)
│   └── watcher/      Auth file synthesizer
└── sdk/              Core SDK (auth selector, service, translator)

frontend/             React 19 + Vite management panel
├── src/
│   ├── components/   UI components (quota/, providers/, ui/)
│   ├── pages/        Route pages (QuotaPage, OAuthPage, etc.)
│   ├── services/api/ API client layer
│   └── i18n/         en.json / zh-CN.json
└── dist/index.html   Single-file build output (embedded by backend)
```

## Quick Start

### Prerequisites

- Go 1.26+
- Node.js 20+ / npm

### Build

```bash
# Backend
cd backend
go build -o cliproxyapi ./cmd/server/

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o cliproxyapi-linux ./cmd/server/

# Frontend (outputs single-file dist/index.html)
cd frontend
npm install
npm run build
```

### Deploy

1. Copy the binary and `config.yaml` to the server
2. Copy `frontend/dist/index.html` to `<config-dir>/static/management.html`
3. Create an `auths/` directory for credential files
4. Run:

```bash
./cliproxyapi server --config ./config.yaml
```

### Config Example (minimal)

```yaml
api-key:
  - your-api-key
auth-dir: ./auths
proxy-url: ''
remote-management:
  allow-remote: true
  disable-control-panel: false
  secret-key: <bcrypt-hash>
```

## Key Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /management.html` | Web management panel |
| `POST /v1/chat/completions` | OpenAI-compatible chat |
| `POST /v1/messages` | Anthropic-compatible messages |
| `GET /v1/models` | List available models |
| `GET /v0/management/github-copilot-auth-url` | Start GitHub Copilot OAuth |
| `GET /v0/management/github-copilot-quota` | Query Copilot subscription status |

## License

See upstream [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) for license terms.
