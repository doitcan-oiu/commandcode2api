# Command Code Gateway (Go)

A self-hosted API gateway for a single administrator and team clients. Pool multiple Command Code accounts, track balances and usage windows, and issue separate client tokens through a React, shadcn/ui, and Tailwind CSS management interface. The Go service exposes OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages APIs.

[中文说明](README_zh.md) · [Upstream maintenance](docs/UPSTREAM.md)

The runtime is one Go executable plus built static web assets. SQLite stores accounts, quotas, settings, and request metadata. Node.js/npm is needed to build the frontend and run development tests, not to run the deployed service.

## Build and first start

Use Go 1.26+ and Node.js 24 with npm. From the repository root:

```bash
npm --prefix web ci
npm --prefix web run build
go run ./cmd/commandcode-proxy
```

Open `http://localhost:3050`. On a new data directory, create the administrator username and password directly in the setup screen; no setup token is required. Passwords must contain 8–1024 characters. Setup closes after the first administrator is created and cannot replace an existing account, including after a restart.

Alternatively, set `CC_ADMIN_PASSWORD` before the first start to initialize username `admin`. This variable initializes an empty installation only; it does not reset an existing administrator password. Change passwords through the authenticated management interface.

After signing in:

1. Add upstream Command Code `user_...` keys under Accounts and refresh their usage. Configure labels, weights, priorities, allowed models, and account concurrency as needed.
2. Create a downstream access token under Access tokens. Save the returned `ccg_...` value when shown; the full token is returned only on creation. Replace a lost token by creating another and disabling/deleting the old one.
3. Configure your API client with this downstream token and the gateway base URL.

To build a binary:

```bash
go build -trimpath -o bin/commandcode-proxy ./cmd/commandcode-proxy
./bin/commandcode-proxy
```

On Windows, use `-o bin/commandcode-proxy.exe` and run `.\bin\commandcode-proxy.exe`. Keep `web/dist` with the deployment or set `CC_WEB_DIR` to its location. The service reads `configs/config.json` relative to its working directory; use `-config /absolute/path/config.json` for another file. Environment variables override the file.

## Client requests

In gateway mode, clients use issued `ccg_...` tokens. Upstream account credentials stay on the server.

```bash
curl http://localhost:3050/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer ccg_YOUR_ACCESS_TOKEN' \
  -d '{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"Hello"}],"stream":true}'
```

For Windows PowerShell, use `Invoke-RestMethod` to avoid native-command JSON quoting problems:

```powershell
$token = Read-Host "Gateway access token"
$body = @{
    model = "deepseek/deepseek-v4-flash"
    messages = @(@{ role = "user"; content = "Hello" })
} | ConvertTo-Json -Depth 10

Invoke-RestMethod -Uri "http://127.0.0.1:3050/v1/chat/completions" `
    -Method Post -Headers @{ Authorization = "Bearer $token" } `
    -ContentType "application/json" -Body $body
```

| Endpoint | Purpose |
| --- | --- |
| `GET /` | Management interface in gateway mode |
| `/api/admin/*` | Administrator session and management API |
| `POST /v1/chat/completions` | OpenAI Chat Completions, streaming and non-streaming |
| `POST /v1/responses` | OpenAI Responses, streaming and non-streaming |
| `POST /v1/messages` | Anthropic Messages, streaming and non-streaming |
| `GET /v1/models` | Client-filtered model list from an available upstream account |
| `GET /health` | Local process health, without credentials |

OpenAI-compatible clients use `http://localhost:3050/v1`; Anthropic-compatible clients use `http://localhost:3050`. Both `Authorization: Bearer ...` and `x-api-key` are supported. Administrator sessions are separate from client tokens.

## Account pooling and usage

The pool filters disabled, invalid, cooling, model-incompatible, and concurrency-limited accounts, then uses the highest available account priority. Within that priority group:

| Strategy | Behavior |
| --- | --- |
| `quota_aware` (default) | Weighted distribution adjusted by the remaining fraction of known five-hour and weekly windows |
| `weighted_round_robin` | Smooth weighted round-robin distribution |
| `least_inflight` | Prefer the lowest active-request load relative to account weight |

Optional session affinity keeps eligible requests for a session on the same account. Unavailable accounts can be bypassed; affinity does not guarantee an upstream session survives a failover.

Authentication failures mark accounts invalid. Quota/rate-limit responses trigger cooldowns; temporary upstream errors receive shorter cooldowns. Retry settings limit account switches before a response starts. Streams that have already started are not replayed against another account. If no account is available, the service returns a structured error.

The usage client reads `/alpha/whoami`, `/alpha/billing/credits`, `/alpha/billing/subscriptions`, and `/alpha/usage/summary`. The UI displays identity, plan, balances, five-hour/weekly windows, resets, and cumulative usage. Automatic refresh defaults to 300 seconds, with individual and full-pool manual refreshes.

Unknown numeric values remain unknown instead of becoming zero. Individual endpoint failures preserve other successful report sections. `limited` alone does not mean exhausted: scheduling uses explicit exceeded flags, known window consumption, and known balances. An expired window can be probed again. When exhaustion has no known reset time, a bounded cooldown allows refresh and recovery.

Monthly plan allowances inferred from the reference project's community table are labeled estimates and do not determine request admission. Upstream balances and summaries may cover different periods; these are not a local billing ledger. The reference project's MIT notice is retained in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## Client limits and request records

Each client token supports enable/disable, expiration, allowed models, requests per minute, maximum concurrency, cumulative request allowance, and cumulative token allowance. Zero disables the corresponding `rpm`, `maxConcurrent`, `maxRequests`, or `maxTokens` limit.

`maxRequests` counts accepted requests when authorization reserves them, including requests that subsequently fail. `maxTokens` is checked against the recorded total and settled using reported input/output usage when each request finishes. It is not an upfront token reservation: one request or several in-flight requests can exceed the remaining allowance, and missing upstream usage cannot be precisely counted. These are local service quotas, not prepaid financial balances. Limits do not automatically reset each billing month.

The dashboard and request records show status, selected account/client, model, protocol, latency, attempts, and reported token usage. Logs store metadata rather than prompt/response bodies or full credentials. Retention defaults to 30 days with an additional 100,000-record cap. The application has one administrator account; team API clients use tokens without separate dashboard logins or roles.

## Docker and persistent data

The Dockerfile builds the local checkout using `COPY`: a Node stage runs `npm ci` and `npm run build`, a Go stage runs `go build`, and the final `scratch` image contains the executable, frontend assets, configuration, CA certificates, and dependency notices under `/app/licenses`. It runs as UID/GID `65532:65532`, with no Node.js, shell, or C runtime. A writable `/tmp` supports SQLite temporary files; its `-healthcheck` command probes `/health`.

```bash
docker compose up -d --build
docker compose logs -f
```

Compose mounts `configs/config.json` read-only and the named volume `gateway-data` at `/app/data`. Open the management page to create the initial administrator. `PROXY_PORT` sets the host port; `CC_CHECK_PROTOCOL_DRIFT=false` disables the npm notification. Rebuild after changing Go or frontend source.

Without Compose:

```bash
docker build -t commandcode-gateway:local .
docker run -d --name cc-gateway -p 3050:3050 \
  -v commandcode-gateway-data:/app/data \
  commandcode-gateway:local
docker logs cc-gateway
```

For a bind-mounted data directory, UID/GID 65532 must be able to write it. Keep credentials out of images. Put a TLS reverse proxy in front of an internet-accessible installation; preserve the original Host/Origin and forward `X-Forwarded-Proto: https` for secure session cookies. Disable SSE response buffering and allow long streaming read timeouts.

### Storage and backup

`CC_DATA_DIR` contains `gateway.db` (SQLite) and `master.key`. Upstream keys are encrypted with AES-256-GCM before database storage. Client token hashes and administrator password hashes are stored instead of plaintext secrets. Account metadata and request records are also in the database; encrypting upstream keys is not whole-database encryption.

Back up the data directory as one unit, including **both the database and `master.key`**. Stop the service before a filesystem copy so the database and any WAL files form a consistent snapshot. Restore the directory before starting the replacement instance. Losing `master.key` makes stored upstream credentials unrecoverable; the service refuses to generate a replacement for an existing database. Anyone with both files can decrypt upstream credentials, so protect backups as secrets. Do not share one data directory between running replicas: deployment is a single process with local SQLite and in-memory sessions/rate-limit state.

## Configuration

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `PORT` / `HOST` | `3050` / `0.0.0.0` | Listen port and address |
| `CC_GATEWAY_ENABLED` | `true` | Account pooling and management UI |
| `CC_DATA_DIR` | `data` | Persistent SQLite/encryption-key directory |
| `CC_WEB_DIR` | `web/dist` | Built frontend assets |
| `CC_ADMIN_PASSWORD` | unset | Initial password for username `admin`; otherwise create the administrator in the web UI |
| `CC_API_BASE` | `https://api.commandcode.ai` | Upstream generation/usage API URL |
| `CC_UPSTREAM_PROXY` | unset | Explicit HTTP proxy for Command Code requests; HTTPS uses CONNECT |
| `LOG_FILE` / `LOG_LEVEL` | stdout / `info` | Log file and `info`, `warn`, or `error` verbosity |
| `CC_CHECK_PROTOCOL_DRIFT` | `true` | Warn if npm CLI version differs from fixed protocol |
| `CC_USE_PROVIDER_MODELS` | `true` | Retrieve provider models dynamically |
| `CMD_ZDR` | off | Set `1` for ZDR-only routing |
| `CC_CLI_MODE` / `CC_CLI_SESSION_MODE` | `agent` / `interactive` | Upstream envelope/lifecycle modes |
| `CC_FINGERPRINT_SALT` | empty | Deterministic per-key fingerprint salt |
| `CC_DEVICE_PROJECT_DIR` | built-in Windows path | Device profile project directory |
| `CC_EMPTY_SYSTEM_PLACEHOLDER` | `true` | Insert a space for empty system prompts |
| `CC_MAX_BODY_MB` | `100` | Request body MiB limit; excess receives 413 |
| `CC_MAX_INFLIGHT` | `0` | Global in-flight limit; 0 disables; excess receives 503 |
| `CC_STREAM_IDLE_MS` | `30000` | Streaming upstream read-idle timeout |
| `CC_NONSTREAM_IDLE_MS` | `90000` | Non-streaming upstream read-idle timeout |
| `CC_CLIENT_DRAIN_TIMEOUT_MS` | `0` | Downstream write timeout; 0 disables |
| `CC_KEEPALIVE_TIMEOUT_MS` | `65000` | HTTP keep-alive idle timeout |

Pool strategy, retries, refresh interval, cooldown, affinity, and log retention are persisted in SQLite and managed in the UI. JSON configuration also accepts `gatewayEnabled`, `dataDir`, `webDir`, and `modelRefreshIntervalMs` (provider-model cache interval; default `300000`). The legacy `apiKey` file field is unused. `PROJECT_SLUG` / `projectSlug` remain accepted, while the protocol project header is derived from the device project directory.

### Compatibility mode

Set `CC_GATEWAY_ENABLED=false` for the previous stateless proxy behavior. API callers then supply their own upstream `user_...` keys; management UI/APIs and pooled client quotas are disabled, and `/` is again a health-check alias. `/health` exists in both modes. Upstream session, fingerprint, and protocol conversion behavior remains available.

## Protocol compatibility and npm checks

This remains a Go port of `MAXeaglet/commandcode-proxy`. The Command Code protocol is fixed at **1.53.1**, including headers and lifecycle metadata. The original project queried `https://registry.npmjs.org/command-code/latest` at startup and every 24 hours; the Go service preserves that notification with a 10-second timeout.

A differing npm version only produces a warning. The runtime does not download package archives, execute the CLI, or automatically change protocol behavior. `CC_CHECK_PROTOCOL_DRIFT=false` disables the registry request; a failed check does not prevent startup. Registry checks do not use `CC_UPSTREAM_PROXY`. Frontend `npm ci` during development/Docker builds is separate from this runtime metadata check.

Each upstream key has an independent session and deterministic device fingerprint. Responses is stateless: `previous_response_id` and `store=true` return 400; provide full history each turn. Responses image/file input is unsupported; use Chat Completions or Messages for images. Messages thinking signatures retain the original synthetic format and cannot verify official Anthropic signatures.

Idle timeouts reset when upstream data arrives; they are not total request deadlines. A stream ending without a finish event is an error; after SSE starts, errors are sent in the stream. `/health` checks the local process, not upstream credentials, quota, or remote availability.

Upstream changes must be reviewed and ported into Go. [docs/UPSTREAM.md](docs/UPSTREAM.md) records the actual ported baseline and sync procedure. npm metadata does not establish whether the upstream Git repository has been synchronized.

## Development and verification

```text
cmd/commandcode-proxy/  Program entry point and flags
internal/config/       File/environment configuration
internal/proxy/        Protocol conversion, upstream transport, routing
internal/gateway/      Admin API, account pool, quotas, SQLite storage
internal/usage/        Defensive account/credit/usage normalization
internal/webui/        Static frontend serving
web/                   React, shadcn/ui, Tailwind CSS and frontend tests
configs/config.json    Default runtime configuration
tests/integration/     Node.js HTTP regression suite with local mocks
docs/UPSTREAM.md       Upstream baseline and maintenance process
.github/workflows/     Verification and image publishing
```

```bash
npm --prefix web ci
npm --prefix web run build
go test ./...
go vet ./...
go build -o bin/commandcode-proxy ./cmd/commandcode-proxy
node --test tests/integration/*.test.mjs
```

Use `bin/commandcode-proxy.exe` on Windows. The black-box tests use local mock servers and do not need real credentials. `go test -race ./...` additionally requires a supported platform and C compiler. Docker runtime checks require a working Docker engine; configuring a CI check does not mean it has run locally.

This project is unofficial and is not affiliated with Command Code.
