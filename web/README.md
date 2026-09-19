# Gateway 管理界面

React 19 + TypeScript + Vite，使用官方 shadcn/ui（Radix）、Tailwind CSS 4、TanStack Query 和 Recharts。页面与业务代码位于 `src/pages`，官方组件位于 `src/components/ui`。

默认采用深色管理台。`src/index.css` 只保留样式入口和 Tailwind 主题映射；`src/styles/foundation.css` 维护颜色、表单及公共组件，`shell.css` 维护导航、登录和响应式外壳，`workspace.css` 维护业务页面。账户卡片不显示头像，三种额度逐行显示，未知值和估算标记始终保留。

总览集中展示网关指标、请求趋势和账户池状态；账户详细用量在上游账户页面查看。设置分为调度策略、API 接入和账户安全，分别支持 `#settings`、`#connection`、`#security` 地址直接进入。

```sh
npm ci
npm run dev
npm run build
npm run typecheck
npm test
npm run lint
```

开发服务器默认将 `/api` 和 `/v1` 代理至 `http://127.0.0.1:3050`。可使用 `VITE_API_TARGET` 环境变量覆盖，例如 PowerShell：

```powershell
$env:VITE_API_TARGET = 'http://127.0.0.1:3051'
npm run dev
```

生产产物为 `dist/`，由 Go 服务同源提供，运行时无需 Node。端口、管理员凭证和上游密钥均不编译进前端。管理会话使用 HttpOnly Cookie；前端不存储管理员密码，完整访问令牌仅在创建后展示一次。

账户的 5 小时及每周用量来自官方窗口；月度用量由套餐额度与余额估算。未知数据使用 `—`。上游账期汇总与网关本地 24 小时指标分别展示，不混用统计周期。图表使用真实管理 API，无演示数据。

组件通过 `npx shadcn@latest add ...` 从官方 registry 安装，配置见 `components.json`；组件许可证见 `licenses/shadcn-ui.txt`。后续增补可执行 `npx shadcn add <component>`，现有依赖及其版本由 `package-lock.json` 锁定。

官方集成文档：[shadcn Vite](https://ui.shadcn.com/docs/installation/vite)、[Tailwind Vite](https://tailwindcss.com/docs/installation/using-vite)、[Vite](https://vite.dev/guide/)。
