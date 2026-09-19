import { Suspense, lazy, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ChevronDown,
  Code2,
  KeyRound,
  Layers3,
  LayoutDashboard,
  LogOut,
  Settings2,
  ShieldCheck,
  UserRound,
} from "lucide-react";
import { api, queryClient, useAction } from "@/lib/api";
import type { Session } from "@/lib/types";
import { Brand, ErrorState, Loading } from "@/components/shared";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { AuthPage } from "@/pages/auth";

const OverviewPage = lazy(() =>
  import("@/pages/overview").then((m) => ({ default: m.OverviewPage })),
);
const AccountsPage = lazy(() =>
  import("@/pages/accounts").then((m) => ({ default: m.AccountsPage })),
);
const ClientsPage = lazy(() =>
  import("@/pages/clients").then((m) => ({ default: m.ClientsPage })),
);
const LogsPage = lazy(() =>
  import("@/pages/logs").then((m) => ({ default: m.LogsPageView })),
);
const SettingsPage = lazy(() =>
  import("@/pages/settings").then((m) => ({ default: m.SettingsPage })),
);
const navigation = [
  { id: "overview", label: "运行总览", icon: LayoutDashboard },
  { id: "accounts", label: "上游账户", icon: Layers3 },
  { id: "clients", label: "访问令牌", icon: KeyRound },
  { id: "logs", label: "请求日志", icon: Activity },
  { id: "settings", label: "系统设置", icon: Settings2 },
];
function currentPage() {
  const hash = window.location.hash.slice(1);
  return [...navigation.map((n) => n.id), "connection", "security"].includes(
    hash,
  )
    ? hash
    : "overview";
}
export default function App() {
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api<Session>("/session"),
    retry: 1,
    staleTime: 60_000,
  });
  const [page, setPage] = useState(currentPage);
  useEffect(() => {
    const sync = () => {
      setPage(currentPage());
      window.scrollTo({ top: 0, behavior: "instant" });
    };
    window.addEventListener("hashchange", sync);
    return () => window.removeEventListener("hashchange", sync);
  }, []);
  const navigate = (next: string) => {
    setPage(next);
    window.location.hash = next;
    window.scrollTo({ top: 0, behavior: "instant" });
  };
  const logout = useAction(
    () => api("/logout", "POST"),
    "已退出登录",
    () => {
      queryClient.clear();
      window.location.hash = "";
      window.location.reload();
    },
  );
  if (session.isPending)
    return (
      <div className="boot-screen">
        <Brand />
        <div className="w-60 mt-10">
          <Loading rows={1} />
        </div>
        <p>正在连接管理服务…</p>
      </div>
    );
  if (session.error)
    return (
      <div className="boot-screen">
        <Brand />
        <ErrorState
          error={session.error}
          retry={() => void session.refetch()}
        />
      </div>
    );
  if (!session.data.authenticated)
    return <AuthPage setup={session.data.setupRequired} />;
  const active = ["connection", "security"].includes(page) ? "settings" : page;
  return (
    <div className="console-shell">
      <header className="console-header">
        <div className="console-header-inner">
          <a
            className="console-brand"
            href="#overview"
            aria-label="CommandCode 运行总览"
          >
            <Brand />
          </a>
          <nav className="console-nav" aria-label="主导航">
            {navigation.map((item) => (
              <a
                key={item.id}
                href={"#" + item.id}
                aria-current={active === item.id ? "page" : undefined}
              >
                <item.icon size={16} />
                <span>{item.label}</span>
              </a>
            ))}
          </nav>
          <div className="console-tools">
            <span className="connection-indicator" title="管理会话已连接">
              <span />
              已连接
            </span>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button className="admin-trigger" variant="ghost">
                  <UserRound size={16} />
                  <span>{session.data.username || "admin"}</span>
                  <ChevronDown size={13} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuItem onClick={() => navigate("connection")}>
                  <Code2 size={15} />
                  API 接入指南
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => navigate("security")}>
                  <ShieldCheck size={15} />
                  账户安全
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  disabled={logout.isPending}
                  onClick={() => logout.mutate()}
                >
                  <LogOut size={15} />
                  退出登录
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>
      <main className="console-main">
        <Suspense fallback={<Loading rows={4} />}>
          {page === "overview" ? (
            <OverviewPage navigate={navigate} />
          ) : page === "accounts" ? (
            <AccountsPage />
          ) : page === "clients" ? (
            <ClientsPage />
          ) : page === "logs" ? (
            <LogsPage />
          ) : (
            <SettingsPage
              tab={
                page === "connection"
                  ? "connection"
                  : page === "security"
                    ? "security"
                    : "routing"
              }
              onTabChange={(tab) =>
                navigate(tab === "routing" ? "settings" : tab)
              }
            />
          )}
        </Suspense>
      </main>
      <footer className="console-footer">
        <span>COMMANDCODE / GATEWAY</span>
        <span>
          <ShieldCheck size={12} /> 自托管 · 密钥加密存储
        </span>
      </footer>
    </div>
  );
}
