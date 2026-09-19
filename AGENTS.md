# 项目维护约定

这是 `MAXeaglet/commandcode-proxy` 的 Go 移植 fork；用户要求按原项目的上游协议和行为维护。先读 [docs/UPSTREAM.md](docs/UPSTREAM.md)，其中记录了上游地址、已移植基线、文件映射和同步流程。不能假定新会话继承了之前的聊天内容，也不能把旧的检查结果当作上游最新状态。

- 保留 Go 标准库服务和 Docker 多阶段 `COPY` 源码后 `go build` 的部署方式。Node 仅用于可选的黑盒回归测试。
- 当前协议版本固定为 `1.53.1`，这来自原项目。npm 元数据检查默认开启，启动时及每 24 小时执行，仅提醒版本漂移；不得据 npm 最新版本自动修改请求头、下载或执行包。若上游真正调整了协议，需同时移植相关实现并验证。
- 目录约定：`cmd/commandcode-proxy` 为命令入口，`internal/config` 管理配置，`internal/proxy` 实现代理；Go 测试与源码同目录。默认配置放在 `configs/config.json`，Node 黑盒回归放在 `tests/integration`，维护文档放在 `docs`。不要把业务源码重新堆在根目录。
- 用户询问上游更新或要求同步时，按 `docs/UPSTREAM.md` 拉取并比较已移植基线与上游提交，给出具体差异；无联网或拉取失败时明确无法确定最新状态。
- 上游是 JavaScript，本仓库是 Go。将上游的行为变化移植到对应 Go 文件，保留本地改动；不要用直接覆盖文件、强制重置或盲目合并代替迁移。
- 同步完成后更新 `docs/UPSTREAM.md` 的已移植基线和处理记录；仅检查过、尚未移植的提交不能计入已移植基线。
- 逻辑变更运行 `go test ./...`、`go vet ./...`，必要时通过 `go build -o bin/commandcode-proxy ./cmd/commandcode-proxy` 构建二进制并运行 `node --test tests/integration/*.test.mjs`（Windows 二进制加 `.exe`）。Linux CI 还运行 `go test -race ./...` 和 Docker 构建探活。只能报告实际执行过的验证。
