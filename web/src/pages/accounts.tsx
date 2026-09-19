import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Download,
  Layers3,
  LayoutGrid,
  List,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
} from "lucide-react";
import { api, useAction } from "@/lib/api";
import {
  accountErrorLabel,
  balance,
  dateTime,
  monthlyWindow,
  number,
  splitModels,
  isQuotaExhausted,
} from "@/lib/format";
import type { Account } from "@/lib/types";
import { useNow } from "@/lib/use-now";
import { Button } from "@/components/ui/button";
import { AccountItem } from "@/components/accounts/account-item";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  EmptyState,
  ErrorState,
  Field,
  Loading,
  PageHeading,
  PendingButton,
  QuotaBar,
  StatusBadge,
} from "@/components/shared";

function AccountEditor({
  account,
  onClose,
}: {
  account: Account | null;
  onClose: () => void;
}) {
  const [label, setLabel] = useState(account?.label || "");
  const [key, setKey] = useState("");
  const [weight, setWeight] = useState(account?.weight ?? 1);
  const [priority, setPriority] = useState(account?.priority ?? 0);
  const [maxConcurrent, setMaxConcurrent] = useState(
    account?.maxConcurrent ?? 5,
  );
  const [models, setModels] = useState(account?.models.join("\n") || "");
  const save = useAction(
    () =>
      api(
        account ? `/accounts/${account.id}` : "/accounts",
        account ? "PATCH" : "POST",
        {
          label,
          weight,
          priority,
          maxConcurrent,
          models: splitModels(models),
          ...(!account ? { key: key.trim() } : {}),
        },
      ),
    account ? "账户设置已保存" : "账户已添加",
    onClose,
  );
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[540px]">
        <DialogHeader>
          <DialogTitle>{account ? "编辑上游账户" : "添加上游账户"}</DialogTitle>
          <DialogDescription>
            {account
              ? "调整调度参数与模型范围。"
              : "接入 Command Code API Key，由网关统一调度。"}
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
          className="space-y-5"
        >
          <Field id="account-label" label="账户名称">
            <Input
              id="account-label"
              placeholder="例如：主账户 / 团队 Pro"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              required
              maxLength={100}
            />
          </Field>
          {!account && (
            <Field
              id="account-key"
              label="Command Code API Key"
              hint="密钥将加密保存在服务端，创建后仅显示脱敏值。"
            >
              <Input
                id="account-key"
                type="password"
                placeholder="user_…"
                autoComplete="off"
                value={key}
                onChange={(e) => setKey(e.target.value)}
                required
              />
            </Field>
          )}
          <div className="grid grid-cols-3 gap-4">
            <Field
              id="account-weight"
              label="调度权重"
              hint="权重越高，分配越多"
            >
              <Input
                id="account-weight"
                type="number"
                min={1}
                max={1000}
                value={weight}
                onChange={(e) => setWeight(Number(e.target.value))}
                required
              />
            </Field>
            <Field id="account-priority" label="优先级" hint="数字越大越优先">
              <Input
                id="account-priority"
                type="number"
                min={-100000}
                max={100000}
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
                required
              />
            </Field>
            <Field id="account-concurrent" label="最大并发" hint="0 表示不限制">
              <Input
                id="account-concurrent"
                type="number"
                min={0}
                max={10000}
                value={maxConcurrent}
                onChange={(e) => setMaxConcurrent(Number(e.target.value))}
                required
              />
            </Field>
          </div>
          <Field
            id="account-models"
            label="允许的模型"
            hint="留空允许所有模型；每行一个，或用英文逗号分隔。"
          >
            <Textarea
              id="account-models"
              value={models}
              onChange={(e) => setModels(e.target.value)}
              placeholder="deepseek/deepseek-v4-flash"
              rows={3}
            />
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              取消
            </Button>
            <PendingButton type="submit" pending={save.isPending}>
              {account ? "保存修改" : "添加账户"}
            </PendingButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
function ImportAccounts({ onClose }: { onClose: () => void }) {
  const [keys, setKeys] = useState("");
  const [prefix, setPrefix] = useState("");
  const [result, setResult] = useState<{
    created: number;
    skipped: number;
    errors: string[];
  } | null>(null);
  const save = useAction(
    () =>
      api<{ created: number; skipped: number; errors: string[] }>(
        "/accounts/import",
        "POST",
        { keys, labelPrefix: prefix },
      ),
    "批量导入已完成",
    (data) => {
      setResult(data);
      setKeys("");
    },
  );
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>批量导入账户</DialogTitle>
          <DialogDescription>
            每行一个 API Key，已存在的密钥会自动跳过。
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-5"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field id="import-prefix" label="名称前缀（可选）">
            <Input
              id="import-prefix"
              value={prefix}
              onChange={(e) => setPrefix(e.target.value)}
              placeholder="例如：团队账户"
            />
          </Field>
          <Field id="import-keys" label="API Keys">
            <Textarea
              id="import-keys"
              className="font-mono text-xs"
              value={keys}
              onChange={(e) => setKeys(e.target.value)}
              placeholder={"user_…\nuser_…"}
              rows={7}
              required
              autoComplete="off"
              spellCheck={false}
            />
          </Field>
          {result && (
            <div className="import-result" role="status">
              <strong>
                已导入 {result.created} 个 · 跳过 {result.skipped} 个
              </strong>
              {result.errors?.map((message, index) => (
                <p key={index} className="text-destructive mt-2">
                  {message}
                </p>
              ))}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" type="button" onClick={onClose}>
              关闭
            </Button>
            <PendingButton type="submit" pending={save.isPending}>
              <Download size={15} />
              开始导入
            </PendingButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
function AccountDetails({
  account,
  onClose,
}: {
  account: Account;
  onClose: () => void;
}) {
  const now = useNow();
  const report = account.usage;
  const items = [
    ["账户名称", report?.account?.name || account.label],
    ["用户名", report?.account?.userName || "—"],
    ["套餐", report?.plan?.name || "—"],
    ["套餐状态", report?.plan?.status || "—"],
    ["到期时间", dateTime(report?.plan?.currentPeriodEnd)],
    ["月度余额", `${number(report?.credits?.monthlyCredits, 2)} credits`],
    ["购买余额", `${number(report?.credits?.purchasedCredits, 2)} credits`],
    ["免费余额", `${number(report?.credits?.freeCredits, 2)} credits`],
    ["累计请求", number(report?.usage?.totalCount)],
    [
      "累计 Tokens",
      report?.usage?.totalTokensIn == null ||
      report?.usage?.totalTokensOut == null
        ? "—"
        : number(report.usage.totalTokensIn + report.usage.totalTokensOut),
    ],
    [
      "累计费用",
      report?.usage?.totalCost == null
        ? "—"
        : `$${number(report.usage.totalCost, 4)}`,
    ],
    ["使用统计周期", report?.usage?.periodBasis || "—"],
    ["创建时间", dateTime(account.createdAt)],
    ["最近同步", dateTime(account.lastRefreshAt)],
  ];
  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="detail-sheet">
        <SheetHeader>
          <SheetTitle>{account.label}</SheetTitle>
          <SheetDescription>账户用量、余额和调度状态</SheetDescription>
        </SheetHeader>
        <div className="sheet-body">
          <div className="flex justify-between items-center">
            <code className="text-xs text-muted-foreground">
              {account.keyPreview}
            </code>
            <StatusBadge status={account.status} />
          </div>
          <div className="detail-quotas">
            <QuotaBar label="5 小时窗口" window={report?.credits?.fiveHour} />
            <QuotaBar label="每周窗口" window={report?.credits?.weekly} />
            <QuotaBar
              label="月度额度"
              window={monthlyWindow(report)}
              estimated
            />
          </div>
          <p className="text-xs text-muted-foreground leading-5">
            月度比例由套餐额度与月度余额估算，不参与账户熔断；5
            小时与每周窗口以官方用量数据为准。
          </p>
          <dl className="detail-list">
            {items.map(([label, value]) => (
              <div key={label}>
                <dt>{label}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
          {account.cooldownUntil > now && (
            <div className="notice-warning">
              冷却至 {dateTime(account.cooldownUntil)}
            </div>
          )}
          {account.lastError && (
            <div className="notice-warning break-words">
              {accountErrorLabel(account.lastError)}
            </div>
          )}
          {!!report?.failures?.length && (
            <div className="notice-warning">
              <strong>部分数据同步失败</strong>
              {report.failures.map((message, index) => (
                <p className="mt-2 break-words" key={index}>
                  {message}
                </p>
              ))}
            </div>
          )}
          <div className="space-y-2">
            <h3 className="text-sm font-medium">模型范围</h3>
            <div className="flex flex-wrap gap-2">
              {account.models.length ? (
                account.models.map((model) => (
                  <Badge
                    variant="secondary"
                    key={model}
                    className="break-all whitespace-normal"
                  >
                    {model}
                  </Badge>
                ))
              ) : (
                <span className="text-sm text-muted-foreground">
                  允许所有模型
                </span>
              )}
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
export function AccountsPage() {
  const now = useNow();
  const query = useQuery({
    queryKey: ["accounts"],
    queryFn: () => api<{ items: Account[] }>("/accounts"),
    refetchInterval: 30_000,
  });
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("all");
  const [sort, setSort] = useState("priority");
  const [layout, setLayout] = useState("grid");
  const [editor, setEditor] = useState<Account | null | undefined>(undefined);
  const [importing, setImporting] = useState(false);
  const [details, setDetails] = useState<string | null>(null);
  const refresh = useAction(
    (id: string) =>
      api(id ? "/accounts/" + id + "/refresh" : "/accounts/refresh", "POST"),
    "用量同步完成",
  );
  const toggle = useAction(
    ({ id, enabled }: { id: string; enabled: boolean }) =>
      api("/accounts/" + id, "PATCH", { enabled }),
    "账户状态已更新",
  );
  const remove = useAction(
    (id: string) => api("/accounts/" + id, "DELETE"),
    "账户已删除",
  );
  const accounts = query.data?.items || [];
  const available = (a: Account) =>
    a.enabled && ["active", "healthy", "ready", "available"].includes(a.status);
  const matches = (a: Account, filter: string) =>
    filter === "all" ||
    (filter === "active"
      ? available(a)
      : filter === "disabled"
        ? !a.enabled
        : filter === "exhausted"
          ? isQuotaExhausted(a)
          : a.enabled && a.status === filter);
  const groups = [
    { id: "all", label: "全部账户" },
    { id: "active", label: "可用" },
    { id: "cooldown", label: "冷却中" },
    { id: "exhausted", label: "额度耗尽" },
    { id: "invalid", label: "密钥无效" },
    { id: "disabled", label: "已停用" },
  ];
  const filtered = accounts
    .filter(
      (a) =>
        matches(a, status) &&
        (a.label + " " + a.keyPreview)
          .toLowerCase()
          .includes(search.toLowerCase()),
    )
    .sort((a, b) =>
      sort === "name"
        ? a.label.localeCompare(b.label)
        : sort === "balance"
          ? (balance(b.usage) ?? -Infinity) - (balance(a.usage) ?? -Infinity)
          : b.priority - a.priority || a.label.localeCompare(b.label),
    );
  const selected = accounts.find((a) => a.id === details);
  return (
    <>
      <PageHeading
        eyebrow="RESOURCE POOL"
        title="上游账户"
        description="管理账户池，查看额度并调整请求分配。"
        actions={
          <>
            <PendingButton
              variant="outline"
              pending={refresh.isPending && refresh.variables === ""}
              onClick={() => refresh.mutate("")}
              disabled={!accounts.length || refresh.isPending}
            >
              <RefreshCw size={15} />
              同步全部
            </PendingButton>
            <Button variant="outline" onClick={() => setImporting(true)}>
              <Download size={15} />
              批量导入
            </Button>
            <Button onClick={() => setEditor(null)}>
              <Plus size={15} />
              添加账户
            </Button>
          </>
        }
      />
      <div className="resource-workspace">
        <aside className="resource-filters">
          <div className="filter-heading">账户状态</div>
          <div className="filter-options" aria-label="账户状态筛选">
            {groups.map((group) => (
              <Button
                key={group.id}
                variant="ghost"
                className="filter-option"
                aria-pressed={status === group.id}
                onClick={() => setStatus(group.id)}
              >
                <span className={"filter-dot " + group.id} />
                <span>{group.label}</span>
                <b>
                  {query.isPending || query.error
                    ? "—"
                    : accounts.filter((a) => matches(a, group.id)).length}
                </b>
              </Button>
            ))}
          </div>
          <div className="filter-note">
            <ShieldCheck size={17} />
            <strong>密钥留在服务端</strong>
            <p>上游密钥加密保存。为应用创建访问令牌，即可共享账户池。</p>
          </div>
        </aside>
        <section className="resource-content">
          <div className="resource-toolbar">
            <div className="search-input">
              <Search size={16} />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                aria-label="搜索账户"
                placeholder="搜索账户名称或密钥…"
              />
            </div>
            <Select value={sort} onValueChange={setSort}>
              <SelectTrigger aria-label="账户排序" className="w-[140px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="priority">优先级排序</SelectItem>
                <SelectItem value="name">账户名称排序</SelectItem>
                <SelectItem value="balance">可用余额排序</SelectItem>
              </SelectContent>
            </Select>
            <Tabs
              value={layout}
              onValueChange={setLayout}
              className="view-switch"
            >
              <TabsList aria-label="账户展示方式">
                <TabsTrigger
                  value="grid"
                  aria-label="卡片视图"
                  title="卡片视图"
                >
                  <LayoutGrid size={16} />
                </TabsTrigger>
                <TabsTrigger
                  value="list"
                  aria-label="列表视图"
                  title="列表视图"
                >
                  <List size={16} />
                </TabsTrigger>
              </TabsList>
            </Tabs>
            <span className="result-count">{filtered.length} 个账户</span>
          </div>
          {query.isPending ? (
            <Loading />
          ) : query.error ? (
            <ErrorState
              error={query.error}
              retry={() => void query.refetch()}
            />
          ) : !accounts.length ? (
            <div className="workspace-empty">
              <EmptyState
                title="接入你的第一个账户"
                description="添加 Command Code API Key，网关会自动查询额度并加入调度池。"
                icon={<Layers3 />}
                action={
                  <Button onClick={() => setEditor(null)}>
                    <Plus size={15} />
                    添加账户
                  </Button>
                }
              />
            </div>
          ) : !filtered.length ? (
            <EmptyState
              title="没有匹配的账户"
              description="更换搜索条件，或查看其他账户状态。"
              icon={<Search />}
              action={
                <Button
                  variant="outline"
                  onClick={() => {
                    setSearch("");
                    setStatus("all");
                  }}
                >
                  清除筛选
                </Button>
              }
            />
          ) : (
            <div className={"account-collection " + layout}>
              {filtered.map((account) => (
                <AccountItem
                  key={account.id}
                  account={account}
                  now={now}
                  onDetails={() => setDetails(account.id)}
                  onEdit={() => setEditor(account)}
                  onRefresh={() => refresh.mutate(account.id)}
                  onToggle={(enabled) =>
                    toggle.mutate({ id: account.id, enabled })
                  }
                  onDelete={() => remove.mutate(account.id)}
                  refreshing={
                    refresh.isPending && refresh.variables === account.id
                  }
                  refreshDisabled={refresh.isPending}
                  togglePending={toggle.isPending}
                  deletePending={remove.isPending}
                />
              ))}
            </div>
          )}
        </section>
      </div>
      {editor !== undefined && (
        <AccountEditor account={editor} onClose={() => setEditor(undefined)} />
      )}
      {importing && <ImportAccounts onClose={() => setImporting(false)} />}
      {selected && (
        <AccountDetails account={selected} onClose={() => setDetails(null)} />
      )}
    </>
  );
}
