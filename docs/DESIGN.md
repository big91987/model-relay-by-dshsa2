# Model Gateway MVP 设计

参考对象：new-api（Go + React，渠道/令牌/日志/管理台）与 LiteLLM（模型别名 → 部署列表、路由/回退、虚拟密钥、用量记录）。

## 目标

在空仓库中交付一个可本地运行、可验证的模型网关 MVP：

1. OpenAI 兼容网关：`GET /v1/models`、`POST /v1/chat/completions`（含 SSE 流式）。
2. 多渠道路由：按模型选择渠道，失败切换，支持优先级/权重。
3. 密钥与额度：`sk-` 令牌、可选用量额度、用量统计。
4. 请求日志：模型、渠道、耗时、token 用量、状态码、是否流式。
5. React 管理台：登录、概览、渠道、密钥、日志。
6. 测试不依赖真实厂商 API：测试内置 mock 上游。

## 技术选型

- 后端：Go 1.25，标准库 `net/http`（1.22+ 路由模式），SQLite（`modernc.org/sqlite`，纯 Go），JWT（`golang-jwt/jwt/v5`），bcrypt（`golang.org/x/crypto`）。
- 前端：Vite + React 18 + TypeScript + Ant Design 5 + React Router。
- 单进程部署：Go 服务同时提供 `/api/*`、`/v1/*` 与 `web/dist` 静态资源。

## 目录

```
cmd/server/            # 入口
internal/config/       # 环境变量配置
internal/store/        # SQLite schema 与仓储
internal/auth/         # 管理端 JWT、API token 哈希
internal/gateway/      # 渠道路由、上游转发、SSE、用量解析
internal/api/          # 管理 API 与 OpenAI 兼容 API
internal/webui/        # 静态资源服务
web/                   # React 管理台
docs/                  # 本设计
```

## 数据模型（SQLite）

```sql
channels(id, name UNIQUE, type DEFAULT 'openai', base_url, api_key,
         models TEXT/*JSON array*/, priority INT DEFAULT 0, weight INT DEFAULT 1,
         status INT DEFAULT 1, created_at, updated_at)
tokens(id, name, key_hash UNIQUE, key_prefix, quota INT DEFAULT 0/*0=不限*/,
       used_quota INT DEFAULT 0, status INT DEFAULT 1, created_at, updated_at, last_used_at)
logs(id, token_id, token_name, channel_id, channel_name, model, prompt_tokens,
     completion_tokens, total_tokens, status_code, latency_ms, stream, error, created_at)
settings(key TEXT PRIMARY KEY, value TEXT)  -- 管理员密码哈希、JWT secret
```

初始化：管理员用户名固定 `admin`，密码取 `ADMIN_PASSWORD`（默认 `admin123`），bcrypt 存 `settings`；JWT secret 取 `JWT_SECRET`，否则随机生成并持久化。

## 管理 API（Bearer JWT）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/login` | `{username,password}` → `{token}` |
| GET | `/api/auth/me` | 当前管理员 |
| GET | `/api/dashboard` | 渠道/密钥/请求/token 统计 + 最近日志 |
| GET/POST | `/api/channels` | 列表（分页、搜索）/ 新建 |
| PUT/DELETE | `/api/channels/{id}` | 更新/删除 |
| GET/POST | `/api/tokens` | 列表 / 新建（返回一次性明文 key） |
| PUT/DELETE | `/api/tokens/{id}` | 更新（名称/状态/额度）/ 删除 |
| GET | `/api/logs` | 分页 + 模型/状态过滤 |
| GET | `/api/models` | 启用渠道聚合出的模型 |

列表统一 `{items:[], total, page, page_size}`。

## 网关 API（Bearer `sk-...`）

- `GET /v1/models` → `{object:"list", data:[{id, object:"model", owned_by:"gateway"}]}`
- `POST /v1/chat/completions`：
  - 校验 token（哈希查找、状态、额度）；
  - 按请求 `model` 过滤启用渠道（models 含该模型或 `*`），按 priority 降序后按 weight 加权随机；
  - 转发到 `{base_url}/v1/chat/completions`，替换上游 `Authorization: Bearer {channel.api_key}`；
  - 连接错误或上游 5xx 时换下一个渠道（最多 3 次；已开始输出 SSE 后不重试）；
  - 流式：逐块转发并 flush，尽量从 SSE `usage` 字段解析用量；非流式：从响应 JSON 解析 `usage`；
  - 写 `logs`，并在 token 上累加 `used_quota`（若设置额度）。
- 错误统一 OpenAI 形状：`{"error":{"message","type","code"}}`。
- 额度耗尽 → 429 `insufficient_quota`；token 无效 → 401 `invalid_api_key`。

## 前端页面

- `/login` 登录；未登录跳转。
- `/` 概览：统计卡片 + 最近日志。
- `/channels` 渠道表格 + 新建/编辑弹窗 + 启停/删除。
- `/tokens` 密钥表格 + 新建（弹窗展示一次性完整 key）+ 启停/编辑/删除。
- `/logs` 日志表格（模型过滤、分页）。
- 统一布局：侧边栏 + 顶栏（管理员、退出）。

## 验收标准

1. `go build ./...`、`go test ./...` 通过。
2. `cd web && pnpm install && pnpm build` 通过（TypeScript 无错误）。
3. Go 集成测试（httptest mock 上游）覆盖：
   - 管理员登录与未授权拒绝；
   - 渠道/令牌创建，`/v1/models` 聚合；
   - 非流式补全代理 + 日志写入；
   - 流式补全 SSE 透传；
   - 上游 5xx 切换渠道；
   - 无效 token / 额度耗尽错误。
4. 浏览器冒烟（Playwright）：登录 → 创建渠道 → 创建令牌 → 概览/日志可见；页面无 console error。
5. README 给出运行命令；不依赖真实外部 API 即可复现。

## 第二增量（已完成）

- **模型映射**：`channels.model_mapping`（JSON object，请求模型 → 上游模型）；网关转发前改写请求体 `model`，未命中映射时透传原模型。
- **渠道连通性测试**：`POST /api/channels/{id}/test`，请求 `{base_url}/v1/models`，返回 `{ok,status_code,latency_ms,models,error}`；管理台行内「测试」按钮展示结果。
- **管理员改密**：`PUT /api/auth/password`（旧密码校验 + 新密码至少 6 位 + bcrypt 持久化）；管理台顶栏「修改密码」弹窗。
- **7 日趋势**：`GET /api/dashboard` 增加 `daily:[{date,requests,tokens}]`（补齐缺失日期），概览页用柱状图展示。
- **部署**：多阶段 `Dockerfile`、`docker-compose.yml` 与 `.dockerignore`；容器内 `WEB_DIST=/app/web/dist`、数据卷 `/app/data`。

新增验收点：

- 映射渠道收到对外模型名时，上游实际收到映射后的模型名；
- 渠道测试对 200/500 上游分别返回 `ok:true/false`，并包含状态码与耗时；
- 旧密码错误被拒绝，改密后旧密码登录失败、新密码登录成功；
- dashboard `daily` 固定 7 个点且今日统计正确；
- 管理台可完成创建映射、点击测试、修改密码并用新密码重新登录，console 无错误。

## 明确不做（MVP 外）

- 用户注册/多租户、计费与支付、OAuth；
- 非 OpenAI 协议适配（Anthropic/Gemini 原生格式）、Midjourney/Suno；
- 分布式部署、Redis 缓存、限流中间件；
- 完整 token 计数（仅采用上游返回的 usage；缺失时记 0 并在 README 说明）。
