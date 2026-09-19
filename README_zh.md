# Command Code Gateway（Go 版）

面向单管理员和团队客户端的 API 聚合服务：将多个 Command Code 账户组成账号池，查看余额与用量窗口，给不同应用分发独立访问令牌，并提供 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 兼容接口。

前端使用 React、shadcn/ui、Tailwind CSS；后端为 Go HTTP 服务和纯 Go SQLite。部署时运行 Go 程序并提供已构建的静态资源，运行时不需要 Node.js、npm 或 Command Code CLI。

[English](README.md) · [上游维护记录](docs/UPSTREAM.md)

## 本地构建与首次启动

需要 Go 1.26+、Node.js 24 和 npm。在仓库根目录执行：

```bash
npm --prefix web ci
npm --prefix web run build
go run ./cmd/commandcode-proxy
```

打开 `http://localhost:3050`。首次使用新数据目录时，直接在初始化页面设置管理员用户名和 8–1024 个字符的密码，无需 setup token。创建成功后关闭初始化入口，重复提交或重启程序都不会覆盖已有管理员。

也可以在首次启动前设置 `CC_ADMIN_PASSWORD`，以用户名 `admin` 自动初始化。它只对尚未初始化的数据库生效，不会覆盖已有管理员密码；已有密码请登录后在设置中修改。

登录后的步骤：

1. 在“上游账户”中添加 Command Code `user_...` 密钥并刷新用量，按需配置名称、权重、优先级、并发上限和模型白名单。
2. 在“访问令牌”中创建给客户端使用的 `ccg_...` 令牌。完整令牌只在创建时显示一次，请立即保存；丢失后创建新令牌并停用或删除旧令牌。
3. 在聊天应用或代码中填写网关地址和 `ccg_...` 令牌。

编译成可执行文件：

```bash
go build -trimpath -o bin/commandcode-proxy ./cmd/commandcode-proxy
./bin/commandcode-proxy
```

Windows 使用：

```powershell
go build -trimpath -o bin/commandcode-proxy.exe ./cmd/commandcode-proxy
.\bin\commandcode-proxy.exe
```

程序按当前工作目录读取 `configs/config.json`，也可用 `-config /path/to/config.json` 指定配置；环境变量优先。二进制部署时保留构建好的 `web/dist`，或用 `CC_WEB_DIR` 指定静态资源目录。只执行 `go build` 不会自动构建前端。

## API 接入

默认聚合模式使用下游 `ccg_...` 令牌，上游 `user_...` 密钥只保存在服务端。

| 地址 | 用途 |
| --- | --- |
| `/` | 管理界面 |
| `/api/admin/*` | 管理员会话和管理接口 |
| `POST /v1/chat/completions` | OpenAI Chat Completions，支持流式和非流式 |
| `POST /v1/responses` | OpenAI Responses，支持流式和非流式 |
| `POST /v1/messages` | Anthropic Messages，支持流式和非流式 |
| `GET /v1/models` | 从可用上游账户查询模型列表，并按客户端模型白名单过滤 |
| `GET /health` | 无需认证的本地健康检查 |

OpenAI 客户端的 Base URL 为 `http://localhost:3050/v1`，Anthropic 客户端使用 `http://localhost:3050`。API 支持 `Authorization: Bearer ...` 或 `x-api-key`，管理员登录会话与客户端令牌分开。

```bash
curl http://localhost:3050/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer ccg_YOUR_ACCESS_TOKEN' \
  -d '{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

Windows PowerShell 建议使用下面的方式，避免 `curl.exe` 参数中的 JSON 双引号被处理：

```powershell
$token = Read-Host "粘贴网关访问令牌"
$body = @{
    model = "deepseek/deepseek-v4-flash"
    messages = @(@{ role = "user"; content = "你好" })
} | ConvertTo-Json -Depth 10

Invoke-RestMethod -Uri "http://127.0.0.1:3050/v1/chat/completions" `
    -Method Post -Headers @{ Authorization = "Bearer $token" } `
    -ContentType "application/json; charset=utf-8" -Body ([System.Text.Encoding]::UTF8.GetBytes($body))
```

## 调度、额度与日志

账号池先过滤停用、鉴权失效、冷却中、模型不匹配及达到并发上限的账户，然后从最高可用优先级的账户中选择：

| 策略 | 行为 |
| --- | --- |
| `quota_aware`，默认 | 在权重基础上结合已知 5 小时/周窗口的剩余比例分配请求 |
| `weighted_round_robin` | 平滑加权轮询 |
| `least_inflight` | 按权重比较当前并发负载，优先选择负载较低的账户 |

可启用会话亲和性，让同一会话优先继续使用原账户；账户不可用时允许切换。上游鉴权失败标记为失效，额度/速率限制进入冷却，临时上游故障采用较短冷却。设置中的重试次数限制响应开始前的账户切换；已经开始输出的流不会自动重播。没有可用账户时返回结构化错误。

用量同步参考 `commandcode-usage` 项目，查询 `/alpha/whoami`、`/alpha/billing/credits`、`/alpha/billing/subscriptions`、`/alpha/usage/summary`，展示身份、套餐、余额、5 小时/周窗口、重置时间及累计调用。默认每 300 秒同步，也支持单账户或全部刷新。参考项目许可保留在 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

未知额度不会显示为 0；部分端点失败时保留其余成功结果。`limited` 不等于已经耗尽，是否冷却依据明确的超额标记、实际窗口和已知余额；窗口到期后可重新尝试。没有明确重置时间的限制采用有界冷却。由社区套餐表推算的月额度会标记“估算”，不作为拒绝请求的依据；上游余额与累计统计可能使用不同时间范围。

每个访问令牌可设置启停、过期时间、模型白名单、RPM、并发上限、累计请求配额和累计 Token 配额；`rpm`、`maxConcurrent`、`maxRequests`、`maxTokens` 为 0 时表示相应限制关闭。

- 请求配额在接受并预留请求时计数，随后失败的请求仍计数。
- Token 配额按完成时上游报告的输入/输出 usage 结算，不是请求前预扣。单个请求或并发在途请求可能超过剩余额度；上游没有返回 usage 时无法精确计数。
- 累计配额不会按账期自动重置，也不等于预充值或商业计费余额。

请求记录包括客户端、账户、模型、协议、状态、耗时、尝试次数及报告的 Token 数，不保存 prompt/response 正文或完整凭据。日志默认保留 30 天，并设有 100,000 条上限。当前提供一个管理员账户；团队成员通过客户端令牌调用 API，没有独立后台登录或管理员角色体系。

## Docker 部署

Dockerfile 使用本地源码 `COPY` 构建：Node 阶段执行 `npm ci` 和前端构建，Go 阶段执行 `go build`，最终 `scratch` 镜像包含程序、静态资源、配置、CA 证书和 `/app/licenses` 下的依赖许可。运行用户为 `65532:65532`，带 `/health` 探活与供 SQLite 使用的可写 `/tmp`，不需要 Node 或 C 运行库。

```bash
docker compose up -d --build
docker compose logs -f
```

Compose 将配置文件只读挂载到容器，并将 `gateway-data` 命名卷挂载到 `/app/data`。首次打开管理页面创建管理员即可。`PROXY_PORT` 可改变主机端口；`CC_CHECK_PROTOCOL_DRIFT=false` 可关闭 npm 版本提醒。Go 或前端源代码改变后需重新构建。

也可直接执行：

```bash
docker build -t commandcode-gateway:local .
docker run -d --name cc-gateway -p 3050:3050 \
  -v commandcode-gateway-data:/app/data \
  commandcode-gateway:local
docker logs cc-gateway
```

使用主机目录挂载时，确保 UID/GID 65532 能写入。公网部署使用 HTTPS 反向代理，保留原始 Host/Origin，并转发 `X-Forwarded-Proto: https`；关闭 SSE 缓冲，给流式请求留出足够的读取超时。

### 持久化与备份

数据目录包含 SQLite 数据库 `gateway.db` 和加密密钥 `master.key`。上游密钥在写入数据库前使用 AES-256-GCM 加密；下游令牌只存摘要，管理员密码只存密码哈希。账号元数据及用量仍保存在数据库中，这不是整个数据库的透明加密。

**备份与恢复必须同时保留数据库和 `master.key`。** 建议停止程序后整体复制数据目录，包含存在的 SQLite WAL 文件，恢复完再启动。丢失 `master.key` 无法解密旧上游凭据；程序会拒绝给已有数据库自动生成替代密钥。同时拿到数据库和密钥的人可以解密凭据，备份应按密钥文件保护。

当前采用单进程 SQLite 部署，管理员会话和 RPM 窗口状态存于进程内存。不要让多个运行实例共用同一数据目录。

## 配置与兼容模式

| 环境变量 | 默认 | 用途 |
| --- | --- | --- |
| `PORT` / `HOST` | `3050` / `0.0.0.0` | 监听地址 |
| `CC_GATEWAY_ENABLED` | `true` | 启用聚合及管理界面 |
| `CC_DATA_DIR` | `data` | 数据库和加密密钥目录 |
| `CC_WEB_DIR` | `web/dist` | 前端构建目录 |
| `CC_ADMIN_PASSWORD` | 未设置 | 首次初始化 `admin` 密码；未设置则在管理页面创建管理员 |
| `CC_API_BASE` | `https://api.commandcode.ai` | 生成与用量查询的上游地址 |
| `CC_UPSTREAM_PROXY` | 未设置 | Command Code 请求的 HTTP 代理；HTTPS 使用 CONNECT |
| `CC_CHECK_PROTOCOL_DRIFT` | `true` | npm 元数据版本提醒 |
| `CC_USE_PROVIDER_MODELS` | `true` | 动态查询上游模型 |
| `CC_MAX_BODY_MB` | `100` | 请求体大小上限 MiB |
| `CC_MAX_INFLIGHT` | `0` | 全局并发上限，0 为不限制 |
| `CC_STREAM_IDLE_MS` / `CC_NONSTREAM_IDLE_MS` | `30000` / `90000` | 流式/非流式上游读取空闲超时 |

调度策略、重试、同步间隔、冷却、会话亲和性和日志保留在管理界面设置并持久化到数据库。完整环境变量见 [English README](README.md#configuration)。旧 `apiKey` 配置字段仍不参与认证。

设置 `CC_GATEWAY_ENABLED=false` 可恢复原来的无状态代理：每次请求直接携带上游 `user_...` 密钥，管理界面、管理接口、账号池和下游令牌配额关闭，`/` 恢复健康检查别名。`/health` 在两种模式下都可用。

## 协议与 npm 检测

上游协议固定 **1.53.1**，来自原项目，不能通过 npm 最新版本自动升级。服务默认在启动及每 24 小时查询一次 `https://registry.npmjs.org/command-code/latest`，超时 10 秒；不同版本只产生告警，不下载包、不执行 CLI、不自动修改请求头。查询失败不影响服务启动，`CC_CHECK_PROTOCOL_DRIFT=false` 可关闭。该元数据查询不使用 `CC_UPSTREAM_PROXY`。

前端构建时的 `npm ci` 与运行时版本提醒是两件事。GitHub 上游更新需按 [docs/UPSTREAM.md](docs/UPSTREAM.md) 拉取和比较，再移植到 Go；npm 元数据不能证明已经同步上游。

Responses 保持无状态，`store=true` 和 `previous_response_id` 返回 400，客户端每次提供完整历史；Responses 不支持图片/文件输入，图片可用 Chat Completions 或 Messages。Messages 思考签名沿用原项目的合成格式，不能用来验证官方 Anthropic 签名。读取超时按空闲时间计算；缺少结束事件的流会报告错误，已输出 SSE 后通过流内错误返回。`/health` 只检查本地进程，不检查上游密钥和余额。

## 开发与检查

```text
cmd/commandcode-proxy/  程序入口
internal/config/       配置
internal/proxy/        协议转换与代理
internal/gateway/      管理 API、调度、配额、SQLite
internal/usage/        用量查询与归一
internal/webui/        静态资源托管
web/                   React / shadcn/ui / Tailwind CSS
tests/integration/     Node 本地 mock 黑盒测试
docs/UPSTREAM.md       上游维护记录
```

```bash
npm --prefix web ci
npm --prefix web run build
go test ./...
go vet ./...
go build -o bin/commandcode-proxy ./cmd/commandcode-proxy
node --test tests/integration/*.test.mjs
```

Windows 二进制加 `.exe`。测试使用本地模拟端点，无需真实上游密钥。`go test -race ./...` 需要支持的平台和 C 编译器，容器验证需要 Docker 引擎；配置了 CI 不代表已经在本机完成这些检查。

本项目为非官方项目，与 Command Code 无隶属关系。
