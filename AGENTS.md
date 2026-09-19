# 项目维护约定

这是 `MAXeaglet/commandcode-proxy` 的 Go 移植 fork；用户要求按原项目的上游协议和行为维护。先读 [docs/UPSTREAM.md](docs/UPSTREAM.md)，其中记录了上游地址、已移植基线、文件映射和同步流程。不能假定新会话继承了之前的聊天内容，也不能把旧的检查结果当作上游最新状态。

- 后端保持 Go 服务，使用标准库 HTTP 和纯 Go SQLite 驱动；运行时不依赖 Node 或 Command Code CLI。用户已明确要求 React、shadcn/ui、Tailwind CSS 管理界面，Node/npm 用于前端构建及开发测试。Docker 保留多阶段 `COPY` 本地源码后分别构建前端和 `go build` 的方式，最终镜像只包含 Go 程序、静态资源、配置和证书。
- 当前协议版本固定为 `1.53.1`，这来自原项目。npm 元数据检查默认开启，启动时及每 24 小时执行，仅提醒版本漂移；不得据 npm 最新版本自动修改请求头、下载或执行包。若上游真正调整了协议，需同时移植相关实现并验证。
- 目录约定：`cmd/commandcode-proxy` 为命令入口，`internal/config` 管理配置，`internal/proxy` 实现协议代理，`internal/gateway` 管理账号池、管理员认证、下游令牌、配额、SQLite 和请求记录，`internal/usage` 归一上游用量，`internal/webui` 托管静态文件，`web` 存放 React 前端。Go 测试与源码同目录；默认配置在 `configs/config.json`，Node 黑盒回归在 `tests/integration`，维护文档在 `docs`。不要把业务源码重新堆在根目录。
- 默认是单管理员、面向团队客户端的聚合服务：上游 `user_...` 密钥留在服务端，下游用 `ccg_...` 令牌；`CC_GATEWAY_ENABLED=false` 保留原有透传认证兼容模式。用户已明确要求首次管理员初始化不使用 setup token：未初始化时直接设置用户名和至少 8 位密码，也可用 `CC_ADMIN_PASSWORD` 初始化；创建后关闭初始化入口，重复或并发初始化不得覆盖已有管理员。管理接口、下游令牌和上游密钥是三个不同认证边界。
- 持久化目录由 `CC_DATA_DIR` 指定，含 SQLite 数据库和用于上游密钥 AES-GCM 加密的 `master.key`；备份恢复必须保留二者。客户端令牌只存摘要，管理员密码只存密码哈希，列表和请求日志不得泄露完整密钥、token、prompt、response。真实凭据、数据目录、构建输出及用户提供的 `demo` 参考目录不提交 Git。
- 上游用量未知值保留为未知，不能伪造成 0；`limited` 不等于已封禁，按实际余额和窗口超额状态处理冷却，过期窗口允许重新尝试。社区套餐额度推算需标记 estimated，不能据估算额度拒绝请求。部分端点失败保留成功部分及失败说明。账号失效、限流、临时故障与禁用状态须区分；流已开始后不得自动重播请求。
- 下游 `maxRequests` 按接受并预留的请求数计数，包括随后失败的请求；`maxTokens` 按完成时上游返回的 usage 结算，单个或并发在途请求可能超出额度，缺失 usage 不得伪造精确计数。不得将该配额机制宣称为严格预扣费或完整计费系统。
- 用户询问上游更新或要求同步时，按 `docs/UPSTREAM.md` 拉取并比较已移植基线与上游提交，给出具体差异；无联网或拉取失败时明确无法确定最新状态。
- 上游是 JavaScript，本仓库是 Go。将上游的行为变化移植到对应 Go 文件，保留本地改动；不要用直接覆盖文件、强制重置或盲目合并代替迁移。
- 同步完成后更新 `docs/UPSTREAM.md` 的已移植基线和处理记录；仅检查过、尚未移植的提交不能计入已移植基线。
- 逻辑变更运行 `go test ./...`、`go vet ./...`；前端修改运行 `npm --prefix web ci`、`npm --prefix web run build` 和相关前端测试。必要时通过 `go build -o bin/commandcode-proxy ./cmd/commandcode-proxy` 构建二进制并运行 `node --test tests/integration/*.test.mjs`（Windows 二进制加 `.exe`）。有条件时运行 `go test -race ./...` 和 Docker 构建探活。只能报告实际执行过的验证；CI 已配置和实际通过须区分。
