# Command Code Proxy (Go)

A standalone Go proxy exposing OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages interfaces for Command Code. The service uses only the Go standard library and does not require Node.js, npm, or the Command Code CLI at runtime.

[中文说明](README_zh.md)

## Build and run

Requires Go 1.26 or newer:

```bash
go build -trimpath -o bin/commandcode-proxy ./cmd/commandcode-proxy
./bin/commandcode-proxy
```

On Windows:

```powershell
go build -trimpath -o bin/commandcode-proxy.exe ./cmd/commandcode-proxy
.\bin\commandcode-proxy.exe
```

The default address is `http://localhost:3050`. Run the commands above from the repository root: the proxy reads `configs/config.json` relative to the current working directory. Use `-config /path/to/config.json` to select another file, including an existing root-level configuration. Environment variables override the file. Each request must pass a Command Code key in `Authorization: Bearer user_...` or `x-api-key: user_...`. The legacy `apiKey` configuration field is accepted but unused, matching the previous request-authentication behavior.

```bash
curl http://localhost:3050/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer user_YOUR_KEY' \
  -d '{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"Hello"}],"stream":true}'
```

## Docker

The Dockerfile copies the local `go.mod` and Go sources, then runs `go build` in a Go builder image. The final `scratch` image contains the statically linked binary, CA certificates, and default configuration. It runs as UID/GID `65532:65532` and uses the binary's `-healthcheck` mode; no shell, Node.js, npm install, or remote source checkout is needed in the runtime image.

```bash
docker build -t commandcode-proxy:local .
docker run -d --name cc-proxy -p 3050:3050 commandcode-proxy:local
```

The npm version notification is enabled by default, matching the original project. To disable it, add `-e CC_CHECK_PROTOCOL_DRIFT=false` before the image name. For a custom configuration, add `--mount type=bind,source=/absolute/path/config.json,target=/app/configs/config.json,readonly`. Keep credentials in the runtime configuration instead of baking them into an image. A configured log file must be writable by UID 65532; stdout is the default.

Compose builds from the local source and mounts `./configs/config.json` read-only:

```bash
docker compose up -d --build
docker compose logs -f
```

Compose accepts `PROXY_PORT` for the host port and `CC_CHECK_PROTOCOL_DRIFT` for version notifications. The container port remains 3050. Rebuild after updating Go sources.

For a multi-platform image, substitute your registry and tag:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -t YOUR_REGISTRY/commandcode-proxy:latest --push .
```

The publish workflow builds the repository source for both platforms and pushes to `ghcr.io/<owner>/<repository>` on the `release` branch or a `v*` tag. `release` updates on the release branch; `latest` updates on either trigger.

## npm metadata checks and protocol version

The former JavaScript implementation did not dynamically install or execute an npm package. It fetched `https://registry.npmjs.org/command-code/latest` at startup and every 24 hours to inspect the `version` field. The Go implementation preserves this notification behavior, with a 10-second request timeout.

- The implemented protocol version stays fixed at **1.53.1**, including `x-command-code-version` and lifecycle metadata.
- A different npm version produces a warning. It never downloads package archives, executes CLI code, or automatically changes the protocol version.
- Set `CC_CHECK_PROTOCOL_DRIFT=false` or `"checkProtocolDrift": false` in the configuration to disable the registry request completely. A failed check does not prevent the service from starting.
- Registry checks do not use `CC_UPSTREAM_PROXY`; that setting applies to Command Code upstream traffic.
- Dynamic model discovery is separate: `CC_USE_PROVIDER_MODELS` controls fetching `/provider/v1/models` from the Command Code API.

Updating compatibility still requires reviewing the upstream protocol and changing this project's Go code.

The upstream repository, ported baseline, file mapping, and synchronization process are recorded in [docs/UPSTREAM.md](docs/UPSTREAM.md). [AGENTS.md](AGENTS.md) points new sessions to these maintenance instructions.

## Configuration

The existing JSON configuration keys remain supported. The shipped `configs/config.json` uses port 3050 and leaves `apiKey` empty. The file also supports `modelRefreshIntervalMs` (default `300000`).

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `PORT` / `HOST` | `3050` / `0.0.0.0` | Listen port and address |
| `CC_API_BASE` | `https://api.commandcode.ai` | Command Code upstream URL |
| `CC_UPSTREAM_PROXY` | empty | Explicit HTTP proxy for upstream requests; HTTPS targets use CONNECT |
| `LOG_FILE` | empty | Optional log file; otherwise stdout |
| `LOG_LEVEL` | `info` | Log verbosity: `info`, `warn`, or `error` |
| `CC_CHECK_PROTOCOL_DRIFT` | `true` | Check npm metadata and warn about protocol drift |
| `CC_USE_PROVIDER_MODELS` | `true` | Dynamically retrieve provider models |
| `CMD_ZDR` | off | Set `1` for ZDR-only routing |
| `CC_CLI_MODE` | `agent` | Upstream envelope mode |
| `CC_CLI_SESSION_MODE` | `interactive` | Lifecycle metadata mode |
| `CC_FINGERPRINT_SALT` | empty | Salt for deterministic per-key device fingerprints |
| `CC_DEVICE_PROJECT_DIR` | built-in Windows project path | Device profile project directory |
| `CC_EMPTY_SYSTEM_PLACEHOLDER` | `true` | Insert a space when there is no system prompt; `false` disables |
| `CC_MAX_BODY_MB` | `100` | Maximum request body size in MiB; larger bodies receive 413 |
| `CC_MAX_INFLIGHT` | `0` | Global in-flight limit; 0 disables, excess requests receive 503 |
| `CC_STREAM_IDLE_MS` | `30000` | Streaming upstream read idle timeout |
| `CC_NONSTREAM_IDLE_MS` | `90000` | Non-streaming upstream read idle timeout |
| `CC_CLIENT_DRAIN_TIMEOUT_MS` | `0` | Optional timeout for blocked downstream writes; 0 disables |
| `CC_KEEPALIVE_TIMEOUT_MS` | `65000` | HTTP keep-alive idle timeout |

`PROJECT_SLUG` / `projectSlug` are retained as configuration inputs for compatibility; the protocol's project header is derived from the device project directory to match `config.workingDir`.

## API and protocol behavior

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions, streaming and non-streaming |
| `POST /v1/responses` | OpenAI Responses, streaming and non-streaming |
| `POST /v1/messages` | Anthropic Messages, streaming and non-streaming |
| `GET /v1/models` | OpenAI-style model list |
| `GET /health` | Local health check, no API key required |
| `GET /` | Alias for the local health check |

Point an OpenAI-compatible client at `http://localhost:3050/v1`; Anthropic-compatible clients use `http://localhost:3050` as their base URL. The proxy translates text, reasoning, tools, tool results, images, usage, and finish events to each interface. Upstream generation requests use the CLI envelope with `config`, `memory`, `taste`, `skills`, `permissionMode`, `threadId`, `mode`, `promptCache`, and `params`.

Each key has an independent session. Device fingerprints are derived deterministically from the key and configured salt, so the same key retains the same device identity across restarts and instances. Fingerprint registration and lifecycle metadata use the same device profile and fixed protocol version. Request logs avoid API key values.

Responses is stateless: `previous_response_id` and `store=true` return 400; send the full history each turn. Its text, reasoning, and function-call conversion follows the original implementation; `input_image` and file input are not yet supported. Use Chat Completions or Messages for images. Messages thinking signatures retain the original synthetic format for display compatibility and cannot be used for official Anthropic signature verification.

Invalid JSON, missing/invalid credentials, oversized bodies, upstream errors, empty output, interrupted streams, and concurrency limits return structured errors. A stream ending without a `finish` event is an error. Context/output limits and `pause_turn` finish reasons are preserved rather than reported as successful `stop` events. Once SSE headers are sent, errors are delivered in the stream.

## Deployment notes

Upstream idle timers reset when new data arrives; they are not total request deadlines. Increase `CC_STREAM_IDLE_MS` for models that spend a long time reasoning before returning a chunk. A slow client applies backpressure to upstream reads. `CC_CLIENT_DRAIN_TIMEOUT_MS` can bound time spent on blocked downstream writes.

Set both request-size and concurrency limits for the deployment's available memory, and configure the reverse proxy accordingly. The old Node.js RSS measurements do not describe this Go implementation. Keep the reverse proxy's upstream keep-alive timeout below `CC_KEEPALIVE_TIMEOUT_MS`, turn off response buffering for SSE, and allow a read timeout longer than the proxy's upstream idle timer. `/health` and `/` remain available when the in-flight limit is reached.

## Development and verification

```text
cmd/commandcode-proxy/  Command entry point and flags
internal/config/       Configuration loading and its Go tests
internal/proxy/        Proxy implementation and its Go tests
configs/config.json    Default runtime configuration
tests/integration/     Optional Node.js HTTP regression suite
docs/UPSTREAM.md       Upstream baseline and maintenance process
.github/workflows/     Verification and image publishing
```

The root keeps module metadata, Docker deployment files, READMEs, and maintenance instructions. The replaced JavaScript server is available in Git history; Node.js files under `tests/integration` are retained as development tests.

```bash
go test ./...
go vet ./...
go test -race ./...
```

The race detector needs a supported platform and C compiler. The retained black-box HTTP regression suite uses Node.js only as a development test runner and mock upstream; it starts the compiled Go binary and makes no Command Code or npm registry requests:

```bash
go build -o bin/commandcode-proxy ./cmd/commandcode-proxy
node --test tests/integration/*.test.mjs
```

On Windows, build `bin/commandcode-proxy.exe`. Set `CC_TEST_BINARY` to test another executable. No `npm install` is required. CI checks Go tests, the race detector, these regressions, and Docker build/start/health behavior.

This project is unofficial and is not affiliated with Command Code. Use it in accordance with the upstream service's terms.
