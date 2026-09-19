# 上游追踪与 Go 迁移记录

本文件随仓库版本管理，为新会话提供可核实的维护上下文。它记录某次检查的结果，不会自动获知未来的上游更新。

## 来源与基线

| 项目 | 值 |
| --- | --- |
| 当前 fork | https://github.com/doitcan-oiu/commandcode2api |
| GitHub 确认的上游 | https://github.com/MAXeaglet/commandcode-proxy |
| 上游分支 | `master` |
| Go 迁移起点 | `cce214d1db9d15c36ea1b59c1b0fb996834d323a` |
| 已移植上游基线 | `cce214d1db9d15c36ea1b59c1b0fb996834d323a` |
| 上次检查日期 | 2026-09-19（Asia/Shanghai） |
| 上次查到的上游 HEAD | `cce214d1db9d15c36ea1b59c1b0fb996834d323a` |
| 上次检查结果 | 与迁移起点一致，无新增提交 |

基线提交：[cce214d](https://github.com/MAXeaglet/commandcode-proxy/commit/cce214d1db9d15c36ea1b59c1b0fb996834d323a)，标题为 `fix: 上游代理跟进 —— 日志脱敏、启动校验、空 body 状态（#32 后续）`。

文档里的上游基线不是本 fork 的 Go 提交号。Go 迁移及后续修改的提交记录见本仓库 Git 历史；实际提交、推送状态以 `git status` 和远程分支状态为准。

## 协议版本与 npm 查询

迁移前的 `proxy.mjs` 明确写着：

```javascript
const CC_PROTOCOL_VERSION = '1.53.1';
let CC_VERSION = CC_PROTOCOL_VERSION;
```

`checkProtocolDrift()` 在启动及每 24 小时 GET `https://registry.npmjs.org/command-code/latest`，超时 10 秒。版本不同只记录告警，不修改 `CC_VERSION`，不安装、下载压缩包或执行 CLI。

Go 对应 `internal/config/config.go` 的协议版本常量与 `internal/proxy/upstream.go` 的 `checkProtocolDrift`，默认行为相同。额外提供 `CC_CHECK_PROTOCOL_DRIFT=false` 供主动关闭检查。npm 的 CLI 版本检查与 GitHub 上游仓库更新是两件事：前者不能告诉我们上游代理修改了什么。

## 下次如何检查更新

先读本文件里的“已移植上游基线”，再检查 `git status --short` 和 `git remote -v`。当前本地已配置 `upstream`；重新克隆时 Git 不会携带额外 remote，若缺少它，添加：

```bash
git remote add upstream https://github.com/MAXeaglet/commandcode-proxy.git
```

若同名 remote 已存在，先核对 URL；不要覆盖用户的其他远程配置。然后执行：

```bash
git fetch upstream master --no-tags
git rev-parse upstream/master
git log --oneline cce214d1db9d15c36ea1b59c1b0fb996834d323a..upstream/master
git diff --stat cce214d1db9d15c36ea1b59c1b0fb996834d323a upstream/master
git diff cce214d1db9d15c36ea1b59c1b0fb996834d323a upstream/master -- proxy.mjs config.json test Dockerfile docker-compose.yml .github
```

这些命令中的旧 SHA 应始终替换成最新记录的“已移植上游基线”。命令中的 `proxy.mjs`、`config.json`、`test` 是上游路径，不要改成下表中的 Go 版路径。比较的是两个上游版本，不是 Go 工作区与 JavaScript 上游的整库差异。若上游重写历史或变更默认分支，先核实共同祖先和分支，不盲目推进基线。

检查时报告新增提交和实际行为差异。用户要求同步时，逐项移植、验证，并记录每项处理结果；只读检查不自动合并或推送。GitHub 的 Sync fork 不能替代 JavaScript 到 Go 的语义迁移。

## 原实现与 Go 文件映射

| 上游文件或内容 | Go 版对应位置 |
| --- | --- |
| `proxy.mjs`：配置、环境变量、协议版本 | `internal/config/config.go` |
| `proxy.mjs`：设备档案和确定性指纹 | `internal/proxy/fingerprint.go` |
| `proxy.mjs`：session、初始化、请求头、HTTP 代理、动态模型、npm 漂移告警 | `internal/proxy/upstream.go` |
| `proxy.mjs`：`buildCcRequest`、Anthropic / Responses 输入转换 | `internal/proxy/request.go` |
| `proxy.mjs`：三协议 JSON / SSE 输出、错误及 usage 映射 | `internal/proxy/response.go` |
| `proxy.mjs`：HTTP 路由、body / 并发上限、超时与取消 | `internal/proxy/server.go` |
| `proxy.mjs`：共享 JSON 值转换和 ID 生成 | `internal/proxy/common.go` |
| `proxy.mjs`：启动、信号退出和容器探活 | `cmd/commandcode-proxy/main.go`、`internal/proxy/runtime.go` |
| `config.json` | `configs/config.json` |
| `test/*.test.mjs` | `tests/integration/*.test.mjs`；Go 单元测试与实现同目录 |

原 JavaScript 源码可通过 `git show <上游基线>:proxy.mjs` 查看，无须还原到 Go 工作区。

## Go 版已有的本地差异

- 运行时只使用 Go 标准库；多阶段 Docker 构建产出静态二进制，非 root 运行并内置探活。
- npm 检查新增关闭开关，默认仍开启；增加 `-config` 路径参数。目录整理后默认读取相对于当前工作目录的 `configs/config.json`，原根目录配置可显式用 `-config ./config.json` 加载。这个默认路径变更属于本 fork 的本地调整。
- Go 入口、配置、代理实现分别放在 `cmd/commandcode-proxy`、`internal/config`、`internal/proxy`；黑盒测试在 `tests/integration`。已替代的 JavaScript 服务代码不在工作区保留，可从 Git 历史检查原实现。
- 同 Key 初始化合并并发请求，动态模型缓存按 Key 隔离；禁止上游重定向，避免认证跨站转发。
- 保留尾行没有换行符的 NDJSON；读空闲超时按实际读取计时，客户端取消释放上游和并发名额。
- 保留空 Anthropic system 块的缓存标记；实际有内容但缺 usage 时不会仅因此误报空响应。
- 输出成功结束事件延迟至确认完成；错误流不会补成功的 `[DONE]`。
- Responses 保持无状态，显式拒绝 `store=true` / `previous_response_id`；图片和文件输入仍未支持。Messages 思考签名保持原合成格式。

同步时应逐项判断上游变更与这些本地差异的关系，不能仅因文件不同就认定缺少上游修复。

## 验证与后续记录

初次迁移已通过 Go 单元/集成测试、`go vet`、26 项 Node 黑盒回归，以及 Linux amd64/arm64 交叉编译。三种请求转换与原 JavaScript 对照了 450 份组合输入；空 system 缓存块的已知修复单独测试。设备指纹用原实现完整 fixture 验证。

本机缺少 Docker 与 C 编译器，因此 Docker 运行和竞态检测当时未在本地执行；CI 已配置相应步骤，不能把“配置了 CI”当作 CI 已通过。

后续同步记录应包含：日期、检查到的上游 SHA、各提交已移植/无需适用/待处理的理由、实际验证结果。只有前面的上游变更都处理完毕后，才能推进“已移植上游基线”。
