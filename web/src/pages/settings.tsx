import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Cable,
  Code2,
  Copy,
  LockKeyhole,
  Save,
  Settings2,
  ShieldCheck,
} from "lucide-react";
import { api, copyText, useAction } from "@/lib/api";
import type { Settings } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  CopyButton,
  ErrorState,
  Loading,
  PageHeading,
  PendingButton,
} from "@/components/shared";

function SettingRow({
  label,
  hint,
  id,
  children,
}: {
  label: string;
  hint?: string;
  id?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="setting-row">
      <div>
        <label htmlFor={id}>{label}</label>
        {hint && <p>{hint}</p>}
      </div>
      <div className="setting-control">{children}</div>
    </div>
  );
}
function RoutingSettings({ initial }: { initial: Settings }) {
  const [values, setValues] = useState(initial);
  const update = (key: keyof Settings, value: unknown) =>
    setValues((prev) => ({ ...prev, [key]: value }));
  const save = useAction(
    () => api<Settings>("/settings", "PATCH", values),
    "系统设置已保存",
    setValues,
  );
  return (
    <form
      className="configuration-form"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <section className="configuration-section">
        <div className="configuration-heading">
          <span>01</span>
          <div>
            <h2>请求路由</h2>
            <p>决定账户选择方式与会话分配行为。</p>
          </div>
        </div>
        <SettingRow
          label="负载均衡策略"
          hint="在相同优先级内选择账户；自动跳过不可用账户。"
        >
          <Select
            value={values.strategy}
            onValueChange={(v) => update("strategy", v)}
          >
            <SelectTrigger aria-label="负载均衡策略">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="quota_aware">额度感知</SelectItem>
              <SelectItem value="weighted_round_robin">加权轮询</SelectItem>
              <SelectItem value="least_inflight">最少并发</SelectItem>
            </SelectContent>
          </Select>
        </SettingRow>
        <SettingRow
          id="session-affinity"
          label="会话亲和性"
          hint="同一会话优先使用原账户，不可用时自动切换。"
        >
          <Switch
            id="session-affinity"
            checked={values.sessionAffinity}
            onCheckedChange={(v) => update("sessionAffinity", v)}
          />
        </SettingRow>
      </section>
      <section className="configuration-section">
        <div className="configuration-heading">
          <span>02</span>
          <div>
            <h2>同步与恢复</h2>
            <p>保持用量同步，控制上游异常时的恢复行为。</p>
          </div>
        </div>
        <SettingRow
          id="setting-refresh"
          label="用量同步间隔"
          hint="自动查询账户用量，范围 30–86400 秒。"
        >
          <div className="input-unit">
            <Input
              id="setting-refresh"
              type="number"
              required
              min={30}
              max={86400}
              value={values.refreshIntervalSeconds}
              onChange={(e) =>
                update("refreshIntervalSeconds", Number(e.target.value))
              }
            />
            <span>秒</span>
          </div>
        </SettingRow>
        <SettingRow
          id="setting-retry"
          label="最大重试次数"
          hint="仅在尚未输出响应时可切换账户，范围 0–5。"
        >
          <div className="input-unit">
            <Input
              id="setting-retry"
              type="number"
              required
              min={0}
              max={5}
              value={values.maxRetries}
              onChange={(e) => update("maxRetries", Number(e.target.value))}
            />
            <span>次</span>
          </div>
        </SettingRow>
        <SettingRow
          id="setting-cooldown"
          label="限流冷却时间"
          hint="上游未给出恢复时间时的默认等待时长。"
        >
          <div className="input-unit">
            <Input
              id="setting-cooldown"
              type="number"
              required
              min={1}
              max={86400}
              value={values.cooldownSeconds}
              onChange={(e) =>
                update("cooldownSeconds", Number(e.target.value))
              }
            />
            <span>秒</span>
          </div>
        </SettingRow>
      </section>
      <section className="configuration-section">
        <div className="configuration-heading">
          <span>03</span>
          <div>
            <h2>日志保留</h2>
            <p>自动清理过期请求元数据。</p>
          </div>
        </div>
        <SettingRow
          id="setting-retention"
          label="保留时长"
          hint="范围 1–365 天，不保存请求或响应正文。"
        >
          <div className="input-unit">
            <Input
              id="setting-retention"
              type="number"
              required
              min={1}
              max={365}
              value={values.logRetentionDays}
              onChange={(e) =>
                update("logRetentionDays", Number(e.target.value))
              }
            />
            <span>天</span>
          </div>
        </SettingRow>
      </section>
      <div className="settings-savebar">
        <span>保存后，后续请求将使用新的配置。</span>
        <PendingButton type="submit" pending={save.isPending}>
          <Save size={15} />
          保存设置
        </PendingButton>
      </div>
    </form>
  );
}
function PasswordSettings() {
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const change = useAction(
    () => {
      if (password !== confirm) throw new Error("两次输入的新密码不一致");
      return api("/password", "POST", {
        currentPassword: current,
        newPassword: password,
      });
    },
    "密码已更新，其他管理会话已退出",
    () => {
      setCurrent("");
      setPassword("");
      setConfirm("");
    },
  );
  return (
    <form
      className="configuration-form"
      onSubmit={(e) => {
        e.preventDefault();
        change.mutate();
      }}
    >
      <section className="configuration-section">
        <div className="configuration-heading">
          <span>
            <LockKeyhole size={18} />
          </span>
          <div>
            <h2>更新管理员密码</h2>
            <p>修改后，其他管理会话将自动退出。</p>
          </div>
        </div>
        <SettingRow
          id="old-password"
          label="当前密码"
          hint="验证你对管理账户的访问权限。"
        >
          <Input
            id="old-password"
            type="password"
            autoComplete="current-password"
            required
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </SettingRow>
        <SettingRow
          id="new-password"
          label="新密码"
          hint="至少 8 个字符，建议使用独立的长密码。"
        >
          <Input
            id="new-password"
            type="password"
            autoComplete="new-password"
            required
            minLength={8}
            maxLength={1024}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </SettingRow>
        <SettingRow
          id="confirm-password"
          label="确认新密码"
          hint="再次输入新的管理员密码。"
        >
          <Input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            required
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
          />
        </SettingRow>
      </section>
      <div className="settings-savebar">
        <span>
          <ShieldCheck size={14} />
          密码仅用于管理控制台登录
        </span>
        <PendingButton type="submit" pending={change.isPending}>
          更新密码
        </PendingButton>
      </div>
    </form>
  );
}
function ConnectionGuide() {
  const origin = window.location.origin;
  const curl = `curl "${origin}/v1/chat/completions" \\\n  -H "Authorization: Bearer YOUR_GATEWAY_TOKEN" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}'`;
  const powershell = `$apiKey = Read-Host "输入网关访问令牌"\n$body = @{\n  model = "deepseek/deepseek-v4-flash"\n  messages = @(@{ role = "user"; content = "hi" })\n} | ConvertTo-Json -Depth 10\nInvoke-RestMethod -Uri "${origin}/v1/chat/completions" -Method Post -Headers @{ Authorization = "Bearer $apiKey" } -ContentType "application/json" -Body $body`;
  return (
    <div className="connection-guide">
      <section className="configuration-section">
        <div className="configuration-heading">
          <span>01</span>
          <div>
            <h2>选择接入地址</h2>
            <p>根据客户端使用的协议填写 Base URL。</p>
          </div>
        </div>
        <div className="endpoint-grid">
          <div className="endpoint-row">
            <div>
              <small>OpenAI / Responses</small>
              <code>{origin}/v1</code>
            </div>
            <CopyButton value={origin + "/v1"} />
          </div>
          <div className="endpoint-row">
            <div>
              <small>Anthropic</small>
              <code>{origin}</code>
            </div>
            <CopyButton value={origin} />
          </div>
        </div>
      </section>
      <section className="configuration-section">
        <div className="configuration-heading">
          <span>02</span>
          <div>
            <h2>配置访问凭证</h2>
            <p>每个应用可以使用独立的令牌和模型权限。</p>
          </div>
        </div>
        <p className="connection-hint">
          <ShieldCheck size={17} />
          <span>
            在「访问令牌」中创建凭证，填入客户端的 API
            Key。模型名使用上游完整名称，例如{" "}
            <code>deepseek/deepseek-v4-flash</code>。Responses
            当前为无状态调用。
          </span>
        </p>
      </section>
      <section className="configuration-section">
        <div className="configuration-heading">
          <span>03</span>
          <div>
            <h2>发起第一次调用</h2>
            <p>复制示例，并填写你创建的网关访问令牌。</p>
          </div>
        </div>
        <Tabs defaultValue="powershell">
          <div className="flex items-center justify-between mb-4">
            <TabsList>
              <TabsTrigger value="powershell">PowerShell</TabsTrigger>
              <TabsTrigger value="curl">cURL</TabsTrigger>
            </TabsList>
            <Code2 size={17} className="text-muted-foreground" />
          </div>
          {[
            { id: "powershell", code: powershell },
            { id: "curl", code: curl },
          ].map((example) => (
            <TabsContent key={example.id} value={example.id}>
              <div className="code-block">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => void copyText(example.code)}
                >
                  <Copy size={13} />
                  复制
                </Button>
                <pre>
                  <code>{example.code}</code>
                </pre>
              </div>
            </TabsContent>
          ))}
        </Tabs>
      </section>
    </div>
  );
}

export function SettingsPage({
  tab,
  onTabChange,
}: {
  tab: string;
  onTabChange: (tab: string) => void;
}) {
  const query = useQuery({
    queryKey: ["settings"],
    queryFn: () => api<Settings>("/settings"),
  });
  const sections = [
    {
      id: "routing",
      label: "调度与恢复",
      detail: "账户分配与自动同步",
      icon: Settings2,
    },
    {
      id: "connection",
      label: "API 接入",
      detail: "地址、协议与调用示例",
      icon: Cable,
    },
    {
      id: "security",
      label: "账户安全",
      detail: "管理员登录密码",
      icon: LockKeyhole,
    },
  ];
  return (
    <>
      <PageHeading
        eyebrow="WORKSPACE / SETTINGS"
        title="系统设置"
        description="配置你的网关，让它适应团队的使用方式。"
      />
      <div className="settings-workspace">
        <nav className="settings-navigation" aria-label="设置分类">
          {sections.map((item) => (
            <button
              key={item.id}
              aria-current={tab === item.id ? "page" : undefined}
              onClick={() => onTabChange(item.id)}
            >
              <item.icon size={18} />
              <span>
                <strong>{item.label}</strong>
                <small>{item.detail}</small>
              </span>
            </button>
          ))}
          <div className="settings-nav-note">
            <ShieldCheck size={17} />
            <p>配置和凭据保存在当前网关实例中。</p>
          </div>
        </nav>
        <div className="settings-content">
          {tab === "routing" ? (
            query.isPending ? (
              <Loading rows={3} />
            ) : query.error ? (
              <ErrorState
                error={query.error}
                retry={() => void query.refetch()}
              />
            ) : (
              <RoutingSettings initial={query.data} />
            )
          ) : tab === "connection" ? (
            <ConnectionGuide />
          ) : (
            <PasswordSettings />
          )}
        </div>
      </div>
    </>
  );
}
