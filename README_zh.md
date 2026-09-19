# Command Code Proxy（Go 版）

将 Command Code 转为 OpenAI Chat Completions、OpenAI Responses 和 Anthropic Messages 兼容接口的独立 Go 代理。服务仅使用 Go 标准库，运行时不需要 Node.js、npm 或 Command Code CLI。

[English](README.md)

## 构建和运行

需要 Go 1.26 或更高版本：

```bash
go build -trimpath -o bin/commandcode-proxy ./cmd/commandcode-proxy
./bin/commandcode-proxy
```

Windows：

```powershell
go build -trimpath -o bin/commandcode-proxy.exe ./cmd/commandcode-proxy
.\bin\commandcode-proxy.exe
```

默认地址为 `http://localhost:3050`。请在仓库根目录运行以上命令：程序读取相对于当前工作目录的 `configs/config.json`，也可用 `-config /path/to/config.json` 指定其他配置文件，包括原先放在根目录的配置。环境变量覆盖文件配置。每次请求必须通过 `Authorization: Bearer user_...` 或 `x-api-key: user_...` 传入 Command Code Key。旧配置字段 `apiKey` 可以保留，但不会用于请求认证，这与原实现的实际行为一致。

```bash
curl http://localhost:3050/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer user_YOUR_KEY' \
  -d '{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

## Docker 部署

Dockerfile 先 `COPY` 本地 `go.mod` 和 Go 源码，再在 Go 构建阶段执行 `go build`。最终使用 `scratch` 镜像，只包含静态二进制、CA 证书和默认配置。以 UID/GID `65532:65532` 运行，使用二进制自身的 `-healthcheck` 探活；运行镜像无需 shell、Node.js、npm install 或远程拉取源码。

```bash
docker build -t commandcode-proxy:local .
docker run -d --name cc-proxy -p 3050:3050 commandcode-proxy:local
```

与原项目一样，默认开启 npm 版本更新提醒；需要关闭时，在镜像名之前追加 `-e CC_CHECK_PROTOCOL_DRIFT=false`。自定义配置可追加 `--mount type=bind,source=/绝对路径/config.json,target=/app/configs/config.json,readonly`。建议运行时挂载含凭据的配置，避免把 Key 打入镜像。默认日志输出到 stdout；指定日志文件时，需要确保 UID 65532 对该路径有写权限。

Compose 从本地源码构建，并只读挂载 `./configs/config.json`：

```bash
docker compose up -d --build
docker compose logs -f
```

通过 `PROXY_PORT` 设置宿主机端口，通过 `CC_CHECK_PROTOCOL_DRIFT` 控制版本提醒；容器内端口固定为 3050。更新 Go 源码后需要重新构建。

多架构构建时替换仓库地址和标签：

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -t YOUR_REGISTRY/commandcode-proxy:latest --push .
```

发布工作流在 `release` 分支或 `v*` tag 上构建这两种架构，并推送到 `ghcr.io/<owner>/<repository>`。`release` 标签跟随发布分支，`latest` 随发布分支和版本 tag 更新。

## 动态 npm 检测与协议版本

原 JavaScript 实现没有动态安装或执行 npm 包。它在启动时及之后每 24 小时访问 `https://registry.npmjs.org/command-code/latest`，只读取其中的 `version`。Go 版本保留此更新提醒，请求超时为 10 秒。

- 实际实现的协议版本固定为 **1.53.1**，`x-command-code-version` 和生命周期元数据都使用这个值。
- npm 版本不同时仅记录告警，不下载包压缩文件、不执行 CLI，也不会自动修改协议版本。
- 设置 `CC_CHECK_PROTOCOL_DRIFT=false`，或在配置文件中设置 `"checkProtocolDrift": false`，即可完全关闭 registry 请求。检查失败不影响服务启动。
- npm 检测不经过 `CC_UPSTREAM_PROXY`，该配置只用于 Command Code 上游请求。
- 动态模型列表是另一项功能：`CC_USE_PROVIDER_MODELS` 控制是否从 Command Code API 的 `/provider/v1/models` 获取模型。

上游协议变化时，仍需检查协议并修改本项目的 Go 实现。

本 fork 的上游地址、迁移基线及后续同步流程见 [docs/UPSTREAM.md](docs/UPSTREAM.md)。新会话可从仓库内的 [AGENTS.md](AGENTS.md) 读取维护约定，再拉取上游并比较差异。

## 配置

沿用现有 JSON 配置字段。仓库内 `configs/config.json` 的端口为 3050，`apiKey` 默认留空。文件还支持 `modelRefreshIntervalMs`（默认 `300000`）。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` / `HOST` | `3050` / `0.0.0.0` | 监听端口与地址 |
| `CC_API_BASE` | `https://api.commandcode.ai` | Command Code 上游地址 |
| `CC_UPSTREAM_PROXY` | 空 | 显式指定上游 HTTP 代理；HTTPS 目标使用 CONNECT |
| `LOG_FILE` | 空 | 可选日志文件；默认使用 stdout |
| `LOG_LEVEL` | `info` | 日志等级：`info`、`warn` 或 `error` |
| `CC_CHECK_PROTOCOL_DRIFT` | `true` | 查询 npm 元数据并提醒协议版本漂移 |
| `CC_USE_PROVIDER_MODELS` | `true` | 动态获取模型列表 |
| `CMD_ZDR` | 关闭 | 设置 `1` 启用 ZDR-only 路由 |
| `CC_CLI_MODE` | `agent` | 上游请求信封 mode |
| `CC_CLI_SESSION_MODE` | `interactive` | 生命周期元数据 mode |
| `CC_FINGERPRINT_SALT` | 空 | 每个 Key 的确定性设备指纹盐值 |
| `CC_DEVICE_PROJECT_DIR` | 内置 Windows 项目路径 | 设备档案的项目目录 |
| `CC_EMPTY_SYSTEM_PLACEHOLDER` | `true` | 没有 system 时插入空格；设为 `false` 关闭 |
| `CC_MAX_BODY_MB` | `100` | 单请求体上限，单位 MiB；超限返回 413 |
| `CC_MAX_INFLIGHT` | `0` | 全局在途上限；0 表示不限，超限返回 503 |
| `CC_STREAM_IDLE_MS` | `30000` | 流式上游读取空闲超时 |
| `CC_NONSTREAM_IDLE_MS` | `90000` | 非流式上游读取空闲超时 |
| `CC_CLIENT_DRAIN_TIMEOUT_MS` | `0` | 下游写入阻塞超时；0 表示关闭 |
| `CC_KEEPALIVE_TIMEOUT_MS` | `65000` | HTTP keep-alive 空闲超时 |

为兼容现有配置，仍接受 `PROJECT_SLUG` / `projectSlug`；协议中的项目请求头实际从设备项目目录派生，与 `config.workingDir` 保持一致。

## API 与协议行为

| 端点 | 用途 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions，支持流式和非流式 |
| `POST /v1/responses` | OpenAI Responses，支持流式和非流式 |
| `POST /v1/messages` | Anthropic Messages，支持流式和非流式 |
| `GET /v1/models` | OpenAI 格式模型列表 |
| `GET /health` | 本地探活，不需要 API Key |
| `GET /` | 本地探活的别名 |

OpenAI 兼容客户端的 Base URL 设为 `http://localhost:3050/v1`；Anthropic 兼容客户端设为 `http://localhost:3050`。代理转换文本、思考、工具调用与结果、图片、用量和结束事件。生成请求使用 CLI 信封字段：`config`、`memory`、`taste`、`skills`、`permissionMode`、`threadId`、`mode`、`promptCache`、`params`。

每个 Key 使用独立 session。设备指纹由 Key 和配置的盐值确定性生成，同一 Key 在重启和多实例间保留相同设备身份。指纹注册与生命周期声明共用设备档案和固定协议版本。请求日志避免记录 API Key 值。

Responses 接口保持无状态：`previous_response_id` 和 `store=true` 返回 400，每轮需发送完整历史。该接口沿用原实现的文本、推理和函数调用转换，尚未支持 `input_image` / 文件输入；图片请使用 Chat Completions 或 Messages 接口。Messages 思考块中的签名沿用原实现的合成值，用于显示兼容，不能用作 Anthropic 官方签名验证。

非法 JSON、缺失或无效凭据、请求体超限、上游错误、零输出、流中断和并发超限返回结构化错误。没有 `finish` 事件就结束的流会作为错误处理；上下文/输出限制和 `pause_turn` 会保留对应的结束原因。一旦 SSE 响应头已发送，错误通过流事件返回。

## 部署注意事项

上游空闲超时在收到数据后重置，不限制整个请求的总时长。推理模型长时间没有返回数据时，可调大 `CC_STREAM_IDLE_MS`。慢客户端会对上游读取施加背压；需要限制下游写入阻塞时间时可设置 `CC_CLIENT_DRAIN_TIMEOUT_MS`。

应按机器可用内存同时设置单请求体和并发上限，并在反向代理配置相应限制。原 Node.js 实现的 RSS 测量值不能用于推算 Go 版内存。反代的上游 keep-alive 超时应小于 `CC_KEEPALIVE_TIMEOUT_MS`，SSE 应关闭响应缓冲，读取超时应大于代理的上游空闲超时。达到在途上限时，`/health` 和 `/` 仍可访问。

## 开发与验证

```text
cmd/commandcode-proxy/  命令入口和启动参数
internal/config/       配置加载及对应 Go 测试
internal/proxy/        代理实现及对应 Go 测试
configs/config.json    默认运行配置
tests/integration/     可选的 Node.js HTTP 回归测试
docs/UPSTREAM.md       上游基线和维护流程
.github/workflows/     验证和镜像发布
```

根目录保留模块信息、Docker 部署文件、README 和维护入口。被替代的 JavaScript 服务代码可从 Git 历史查看；`tests/integration` 中的 Node.js 文件仍用于开发回归测试。

```bash
go test ./...
go vet ./...
go test -race ./...
```

竞态检测需要受支持的平台和 C 编译器。保留的 HTTP 黑盒回归使用 Node.js 作为开发测试运行器和 mock 上游，实际启动的是编译后的 Go 二进制，不访问 Command Code 或 npm registry：

```bash
go build -o bin/commandcode-proxy ./cmd/commandcode-proxy
node --test tests/integration/*.test.mjs
```

Windows 下编译目标改为 `bin/commandcode-proxy.exe`。可设置 `CC_TEST_BINARY` 指定其他可执行文件。无需 `npm install`。CI 检查 Go 测试、竞态检测、黑盒回归，以及 Docker 构建、启动和探活。

本项目为非官方实现，与 Command Code 无关联。使用时应遵守上游服务条款。
