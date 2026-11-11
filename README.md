# Model Gateway

参考 [new-api](https://github.com/QuantumNous/new-api)（渠道/密钥/日志/管理台）与 [LiteLLM](https://github.com/BerriAI/litellm)（模型路由/回退/虚拟密钥/用量记录）实现的模型网关：用一个 OpenAI 兼容入口统一管理多个上游渠道，配套 React 管理台。

## 已实现能力

- **OpenAI 兼容网关**：`GET /v1/models`、`POST /v1/chat/completions`（含 SSE 流式透传）
- **多渠道路由**：按模型匹配渠道，优先级 + 权重选择，上游 5xx/连接失败时切换到下一渠道
- **模型映射**：渠道级「请求模型 → 上游模型」映射，便于统一对外模型名（如 `gpt-4o-mini` → 上游实际模型）
- **渠道管理**：OpenAI 兼容上游、模型列表、Base URL、API Key（脱敏展示）、启停、优先级/权重、一键连通性测试
- **密钥管理**：`sk-` 令牌（仅创建时返回一次明文，数据库存 SHA-256）、可选总 Token 额度、用量统计
- **请求日志**：模型、渠道、耗时、Prompt/Completion/总 Token、状态码、是否流式
- **管理台**：登录、概览（含近 7 日请求趋势）、渠道 CRUD/测试、密钥 CRUD、日志筛选与分页、管理员改密（React 18 + Ant Design 5）
- **部署**：单二进制 + `web/dist` 静态资源；提供 Dockerfile / docker-compose

## 快速开始

要求：Go 1.25+、Node 20+、pnpm。

```bash
# 1) 构建前端（产物在 web/dist，Go 服务从磁盘提供）
cd web && pnpm install && pnpm build && cd ..

# 2) 启动网关（默认 :8080，数据库 ./data/gateway.db）
go run ./cmd/server
```

打开 <http://localhost:8080>，默认管理员 `admin / admin123`（首次启动写入数据库，请通过环境变量修改）。

前端开发模式（热更新，`/api` 与 `/v1` 代理到 :8080）：

```bash
cd web && pnpm dev    # http://localhost:5173
```

### Docker

```bash
docker compose up --build   # http://localhost:8080，数据持久化到 Docker 卷 gateway-data
```

也可以不使用 Docker Compose：

```bash
docker build -t model-gateway:local .
docker run --rm -p 8080:8080 -v gateway-data:/app/data \
  -e ADMIN_PASSWORD=change-me model-gateway:local
```

### 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | HTTP 监听端口 |
| `DATABASE_PATH` | `./data/gateway.db` | SQLite 路径，目录自动创建 |
| `ADMIN_USERNAME` | `admin` | 管理员用户名 |
| `ADMIN_PASSWORD` | `admin123` | 首次启动时写入的管理员密码 |
| `JWT_SECRET` | 随机生成并持久化 | 管理台 JWT 签名密钥 |
| `WEB_DIST` | `web/dist` | 前端构建产物目录 |

## 使用示例

```bash
# 登录管理台，拿到 admin JWT
TOKEN=$(curl -s -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | jq -r .token)

# 新建上游渠道（任意 OpenAI 兼容服务）；model_mapping 可选
curl -s -X POST localhost:8080/api/channels \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"my-upstream","base_url":"https://api.example.com","api_key":"upstream-key","models":["gpt-4o-mini"],"model_mapping":{"gpt-4o-mini":"upstream-model-id"},"priority":1,"weight":1,"status":1}'

# 渠道连通性测试（请求 {base_url}/v1/models）
curl -s -X POST localhost:8080/api/channels/1/test \
  -H "Authorization: Bearer $TOKEN"

# 修改管理员密码
curl -s -X PUT localhost:8080/api/auth/password \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"old_password":"admin123","new_password":"change-me"}'

# 新建网关密钥，响应里的 key 只出现这一次
curl -s -X POST localhost:8080/api/tokens \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"local-dev","quota":0}'   # quota=0 表示不限

# 通过网关调用（流式去掉 "stream":true 或改用 -N 观察 SSE）
curl -N localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-..." -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"你好"}]}'
```

## 验证

```bash
# Go：单元 + 集成测试（httptest mock 上游，不依赖真实厂商 API）
go vet ./... && go test ./...

# 前端：TypeScript 严格检查 + 生产构建
cd web && pnpm build
```

Go 集成测试覆盖：管理员登录/鉴权与改密、渠道与密钥 CRUD（API Key 不泄漏）、模型映射改写上游请求、渠道连通性测试成功/失败、`/v1/models` 聚合、非流式与流式代理及用量落库、上游 5xx 自动切换、无效密钥与额度耗尽、7 日趋势统计。

## 目录

```
cmd/server/       入口与优雅退出
internal/config/  环境变量配置
internal/store/   SQLite schema 与仓储（channels/tokens/logs/settings）
internal/auth/    JWT、bcrypt、API Key 哈希
internal/gateway/ 渠道选择、上游转发、SSE 与 usage 解析
internal/api/     管理 API、OpenAI 兼容 API、SPA 静态资源、集成测试
web/              React 管理台（Vite + TS + Ant Design）
docs/DESIGN.md    设计、接口契约与验收标准
```

## 已知限制（MVP 范围外）

- 仅适配 OpenAI 协议上游；Anthropic/Gemini 原生格式、RAG/Embedding、图像与视频接口未实现。
- Token 用量取自上游返回的 `usage`；上游流式响应未返回 usage 时记为 0，不做本地分词估算。
- 单节点 SQLite 部署，无 Redis/分布式限流/多副本一致性。
- 上游渠道 API Key 以明文存储在本地 SQLite（令牌密钥为 SHA-256）；生产环境需配合磁盘与数据库访问控制。
- 无多租户、计费/支付、OAuth、审计等企业能力。
- 未实现限流、IP 白名单和 CORS 配置；管理台与管理 API 依赖 JWT，网关依赖 `sk-` 令牌。
- 模型映射只改写发往上游的请求 `model`；上游响应（含流式 chunk）中的 `model` 字段保持上游原值。
- 管理员改密后，已签发的 JWT 在过期前仍然有效（未实现 token 吊销）。
- 渠道连通性测试固定请求 `{base_url}/v1/models`，要求上游实现该端点。
