# CLIProxyAPI Plus — AI 部署指南

本文档供 AI 编程助手（Claude Code、Cursor 等）阅读，指导完成从零到运行的完整部署。用户只需提供服务器 SSH 访问和少量凭证信息，其余步骤由 AI 助手自主完成。

## 快速开始

如果用户还没有克隆仓库，先执行：

```bash
git clone https://github.com/Daily-AC/cliproxy-plus.git
cd cliproxy-plus
```

然后按下方流程部署。

---

## 部署流程总览

```
1. 克隆仓库 → 2. 收集信息 → 3. 编译 → 4. 上传 → 5. 配置 → 6. 启动 → 7. 反向代理 → 8. 添加 Provider 凭证 → 9. 验证
```

---

## 第一步：向用户收集信息

部署前需要向用户确认以下信息。**只问一次，一起问**：

| 信息 | 说明 | 示例 |
|------|------|------|
| **服务器 SSH** | 怎么连服务器 | `ssh myserver` 或 `ssh root@1.2.3.4` |
| **安装目录** | 二进制和配置放哪里 | `/opt/cliproxyapi/`（默认推荐） |
| **API Key** | 客户端访问此代理的密钥（自定义） | `my-proxy-key-2026` |
| **管理面板密码** | 登录 Web 管理面板的密码 | `my-admin-password` |
| **是否需要 HTTPS** | 是否配反向代理 + 域名 | 域名 / 仅 IP 直连 |
| **是否需要出站代理** | 服务器能否直连 Anthropic/OpenAI API | 能直连则留空，不能则填代理地址 |

可选（后续通过管理面板配置也行）：
- AnyRouter 签到信息（user-id、session-id、webhook-url）
- 各 Provider 的 OAuth 凭证

---

## 第二步：服务器环境准备

### 系统要求

- Linux amd64（Ubuntu/Debian/CentOS 均可）
- 开放端口 8317（或反向代理端口 80/443）

### 编译环境

二进制可在**任意机器**上交叉编译，不要求服务器上有 Go/Node。

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.26+ | 编译后端 |
| Node.js | 20+ | 编译前端 |
| npm | 随 Node | 前端依赖 |

如果本地 Go 版本不够，可以在服务器上安装 Go 并编译：
```bash
# 在服务器上安装 Go 1.26（如果需要）
wget https://go.dev/dl/go1.26.0.linux-amd64.tar.gz
rm -rf /usr/local/go && tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

---

## 第三步：编译

```bash
# 后端（交叉编译为 Linux amd64）
cd backend
GOOS=linux GOARCH=amd64 go build -o /tmp/cliproxyapi-linux ./cmd/server/

# 前端（输出单文件 HTML，约 2.2MB）
cd ../frontend
npm install    # 首次需要
npm run build  # 输出 dist/index.html
```

---

## 第四步：上传和目录结构

```bash
# 假设 SSH 别名为 myserver，安装目录为 /opt/cliproxyapi/
ssh myserver 'mkdir -p /opt/cliproxyapi/auths /opt/cliproxyapi/static'

scp /tmp/cliproxyapi-linux myserver:/opt/cliproxyapi/cliproxyapi
scp frontend/dist/index.html myserver:/opt/cliproxyapi/static/management.html
```

最终目录结构：
```
/opt/cliproxyapi/
├── cliproxyapi              # 可执行文件
├── config.yaml              # 配置文件
├── auths/                   # 凭证文件（运行时自动生成）
└── static/
    └── management.html      # 管理面板前端（⚠️ 必须在 static/ 子目录下）
```

> **⚠️ 关键**：`management.html` 必须放在 `static/management.html`，不能放在 config.yaml 同级目录。

---

## 第五步：生成配置文件

在服务器上生成 `config.yaml`：

```bash
# 生成管理面板密码的 bcrypt hash
# 方法 1：htpasswd
htpasswd -nbBC 10 "" 'your-password' | cut -d: -f2

# 方法 2：Python（如果没有 htpasswd）
python3 -c "import bcrypt; print(bcrypt.hashpw(b'your-password', bcrypt.gensalt(10)).decode())"
```

### 最小配置

```yaml
api-key:
  - your-api-key-here          # 客户端用这个 key 访问代理

auth-dir: ./auths              # 凭证文件目录

proxy-url: ''                  # 出站代理（直连留空，否则填 http://proxy:port）

remote-management:
  allow-remote: true
  disable-control-panel: false
  secret-key: '<上面生成的 bcrypt hash>'
```

### 完整配置（含 AnyRouter）

```yaml
api-key:
  - your-api-key-here

auth-dir: ./auths
proxy-url: ''

remote-management:
  allow-remote: true
  disable-control-panel: false
  secret-key: '<bcrypt hash>'

anyrouter-api-key:
  - api-key: 'your-anyrouter-api-key'
    label: '主力'
    enabled: true
    check-in:
      enabled: true
      user-id: '12345'               # 见下方"如何获取"
      session-id: 'your-session-id'   # 见下方"如何获取"
      webhook-url: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx'  # 可选
```

---

## 第六步：启动服务

```bash
ssh myserver
chmod +x /opt/cliproxyapi/cliproxyapi
cd /opt/cliproxyapi
nohup ./cliproxyapi server --config ./config.yaml >> /tmp/cliproxyapi.log 2>&1 &
```

验证是否启动：
```bash
curl -s http://localhost:8317/v1/models -H "Authorization: Bearer your-api-key" | head -20
```

---

## 第七步：反向代理（可选但推荐）

### Caddy（推荐，自动 HTTPS）

```bash
# 安装 Caddy
apt install -y caddy   # Debian/Ubuntu
# 或 yum install caddy  # CentOS
```

编辑 `/etc/caddy/Caddyfile`：
```
your-domain.com {
    reverse_proxy localhost:8317
}
```

```bash
systemctl restart caddy
```

### Nginx

```nginx
server {
    listen 443 ssl;
    server_name your-domain.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:8317;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;               # 流式响应必须关闭缓冲
        proxy_read_timeout 300s;           # LLM 响应可能很慢
    }
}
```

### Cloudflare CDN

如果域名走 Cloudflare：
- SSL 模式设为 **Full (strict)**
- 添加 **WAF 跳过规则**：对该域名跳过所有托管规则（否则会拦截 API 请求）

---

## 第八步：添加 Provider 凭证

服务启动后，打开管理面板：`http://服务器IP:8317/management.html`（或你的域名）

### 支持的 Provider 及认证方式

| Provider | 认证方式 | 说明 |
|----------|----------|------|
| **Claude (Anthropic)** | 管理面板 OAuth | 点击认证 → 跳转登录 → 自动获取 token |
| **Codex (OpenAI)** | 管理面板 OAuth | 同上 |
| **Gemini CLI** | 管理面板 OAuth | 同上 |
| **Kimi** | 管理面板 OAuth | 同上 |
| **Antigravity** | 管理面板 OAuth | 同上 |
| **GitHub Copilot** | 管理面板 Device Flow | 点击认证 → 复制设备码 → 去 GitHub 输入 → 等待完成 |
| **AnyRouter** | config.yaml 手动配置 | 需要 API key，在配置文件中填写 |

大部分 Provider 通过管理面板的 **OAuth 认证** 页面一键完成，无需手动操作。

---

## 凭证获取指南（需用户提供的信息）

### AnyRouter user-id 和 session-id

这两个值需要从浏览器获取：

1. 登录 [anyrouter.top](https://anyrouter.top)
2. 打开浏览器开发者工具（F12）→ Network 标签
3. 刷新页面，找到任意 API 请求
4. 从请求头中获取：
   - `New-Api-User: 12345` → 这就是 **user-id**（5 位数字）
   - `Cookie: session=xxxxxx` → `session=` 后面的值就是 **session-id**

### 飞书 Webhook URL（签到通知，可选）

1. 飞书 → 目标群 → 设置 → 群机器人 → 添加机器人 → 自定义机器人
2. 复制 Webhook 地址：`https://open.feishu.cn/open-apis/bot/v2/hook/xxxx`

### GitHub Copilot

无需提前获取凭证。在管理面板 OAuth 页面点击 GitHub Copilot → 页面显示设备码 → 去 [github.com/login/device](https://github.com/login/device) 输入 → 等待页面显示成功。

> **注意**：GitHub 账号需要有活跃的 Copilot 订阅。

---

## 更新部署

```bash
# 本地重新编译
cd backend && GOOS=linux GOARCH=amd64 go build -o /tmp/cliproxyapi-linux ./cmd/server/
cd ../frontend && npm run build

# 上传
scp /tmp/cliproxyapi-linux myserver:/tmp/cliproxyapi-new
scp frontend/dist/index.html myserver:/opt/cliproxyapi/static/management.html

# 服务器上重启
ssh myserver
kill -9 $(lsof -t -i :8317 | head -1)
sleep 2
cp /tmp/cliproxyapi-new /opt/cliproxyapi/cliproxyapi
chmod +x /opt/cliproxyapi/cliproxyapi
cd /opt/cliproxyapi
nohup ./cliproxyapi server --config ./config.yaml >> /tmp/cliproxyapi.log 2>&1 &

# 如果用了 Caddy/Nginx，也重启一下
systemctl restart caddy  # 或 systemctl restart nginx
```

---

## 验证清单

```bash
# 1. 服务是否运行
lsof -i :8317

# 2. 模型列表是否返回
curl -s http://localhost:8317/v1/models -H "Authorization: Bearer your-api-key" | python3 -m json.tool

# 3. 管理面板是否可访问
curl -s -o /dev/null -w "%{http_code}" http://localhost:8317/management.html

# 4. API 请求测试（Anthropic 格式）
curl -s http://localhost:8317/v1/messages \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":50,"messages":[{"role":"user","content":"Hi"}]}'

# 5. 查看日志
tail -50 /tmp/cliproxyapi.log
```

---

## 常见问题

| 问题 | 原因和解决方案 |
|------|----------------|
| 管理面板空白 | `management.html` 放错位置。必须在 `static/management.html`，不是 config 同级目录 |
| 端口被占用 | `kill -9 $(lsof -t -i :8317 \| head -1)` 强杀。普通 `kill` 可能杀不掉卡死的 goroutine |
| Go 编译失败 | 需要 Go 1.26+。用 `go version` 检查，不够则升级或在服务器上编译 |
| Caddy 挂了 | 杀 CLIProxyAPI 进程时有时会连带杀掉 Caddy。重启服务后务必 `systemctl restart caddy` |
| Cloudflare 521 | 后端没运行或 Caddy 没启动。检查 `lsof -i :8317` 和 `systemctl status caddy` |
| API 返回 403 | Cloudflare WAF 拦截。给域名添加跳过规则 |
| Claude API 连接失败 | 服务器 IP 可能被 Anthropic 地域封锁。尝试设置 `proxy-url` 走代理 |
| zh-CN.json 编译报错 | 中文 JSON 文件中混入了 Unicode 智能引号（`""`）。用 `python3 -c "import json; json.load(open('path'))"` 验证 |

---

## 代码架构（开发参考）

### 项目结构

- `backend/` — Go 1.26 模块（`github.com/router-for-me/CLIProxyAPI/v6`）
- `frontend/` — React 19 + TypeScript + Vite，输出单文件 `dist/index.html`
- 后端和前端独立编译，无 monorepo 工具

### 后端关键文件

| 文件 | 用途 |
|------|------|
| `cmd/server/main.go` | 入口 |
| `internal/api/server.go` | 路由注册 |
| `internal/api/handlers/management/auth_files.go` | 管理 API（OAuth、配额、凭证文件） |
| `internal/runtime/executor/*.go` | 各 Provider 的请求执行器 |
| `internal/auth/*/` | 各 Provider 的 OAuth 流程 |
| `internal/checkin/anyrouter.go` | AnyRouter 自动签到 + 飞书通知 |
| `sdk/cliproxy/service.go` | 核心服务：执行器注册、模型注册 |
| `sdk/cliproxy/auth/selector.go` | 凭证选择（轮询、模型别名路由） |

### 添加新 Provider

1. `internal/auth/<provider>/` — OAuth 流程 + token 存储
2. `internal/runtime/executor/<provider>_executor.go` — 实现执行器接口
3. `internal/api/handlers/management/auth_files.go` — 添加 handler
4. `internal/api/server.go` — 注册路由
5. `sdk/cliproxy/service.go` — 注册执行器
6. `frontend/src/pages/OAuthPage.tsx` — PROVIDERS 数组添加条目

### 模型别名系统（三层）

1. **调度层** `selector.go`：`canonicalModelKey()` — 别名 → 标准名（凭证选择时）
2. **注册层** `service.go`：`applyGlobalModelAliases()` — 别名出现在 `/v1/models` 列表
3. **执行层** `anyrouter_executor.go`：翻译为上游 API 接受的模型名

### 前端关键模式

- **单文件输出**：`vite-plugin-singlefile` 内联所有资源到一个 HTML
- **API 客户端**：`services/api/client.ts`（Axios 单例，自动解包 `response.data`）
- **状态管理**：Zustand stores
- **国际化**：`en.json` + `zh-CN.json`（编辑中文 JSON 后用 Python `json.load()` 验证）
