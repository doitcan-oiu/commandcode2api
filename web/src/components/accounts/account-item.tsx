import { Clock3, Pencil, RefreshCw, Trash2 } from "lucide-react";
import type { Account } from "@/lib/types";
import {
  accountErrorLabel,
  balance,
  dateTime,
  monthlyWindow,
  number,
  relativeTime,
} from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { ConfirmAction, QuotaBar, StatusBadge } from "@/components/shared";

export function AccountItem({
  account,
  now,
  onDetails,
  onEdit,
  onRefresh,
  onToggle,
  onDelete,
  refreshing,
  refreshDisabled,
  togglePending,
  deletePending,
}: {
  account: Account;
  now: number;
  onDetails: () => void;
  onEdit: () => void;
  onRefresh: () => void;
  onToggle: (enabled: boolean) => void;
  onDelete: () => void;
  refreshing: boolean;
  refreshDisabled: boolean;
  togglePending: boolean;
  deletePending: boolean;
}) {
  const warning =
    account.cooldownUntil > now
      ? `冷却至 ${dateTime(account.cooldownUntil)}`
      : accountErrorLabel(account.lastError);
  return (
    <article
      className="account-item"
      data-disabled={!account.enabled || undefined}
    >
      <header className="account-item-heading">
        <button onClick={onDetails} title={account.label}>
          {account.label}
        </button>
        <StatusBadge status={account.enabled ? account.status : "disabled"} />
      </header>
      <div className="account-item-summary">
        <div>
          <code>{account.keyPreview}</code>
          <span className="plan-label">
            {account.usage?.plan?.name || "套餐待同步"}
          </span>
        </div>
        <div className="credit-value">
          <strong>{number(balance(account.usage), 2)}</strong>
          <span>可用 credits</span>
        </div>
      </div>
      <div className="account-item-quotas">
        <QuotaBar
          label="5 小时"
          window={account.usage?.credits?.fiveHour}
          compact
        />
        <QuotaBar
          label="每周"
          window={account.usage?.credits?.weekly}
          compact
        />
        <QuotaBar
          label="月度"
          window={monthlyWindow(account.usage)}
          estimated
          compact
        />
      </div>
      <dl className="account-item-routing">
        <div>
          <dt>调度权重</dt>
          <dd>{account.weight}</dd>
        </div>
        <div>
          <dt>优先级</dt>
          <dd>{account.priority}</dd>
        </div>
        <div>
          <dt>当前并发</dt>
          <dd>
            {account.inflight}
            <small> / {account.maxConcurrent || "∞"}</small>
          </dd>
        </div>
      </dl>
      {warning && (
        <p className="account-item-warning" title={warning}>
          <Clock3 size={11} />
          <span>{warning}</span>
        </p>
      )}
      <footer className="account-item-footer">
        <div className="account-toggle">
          <Switch
            checked={account.enabled}
            onCheckedChange={onToggle}
            disabled={togglePending}
            aria-label={`${account.enabled ? "停用" : "启用"}账户 ${account.label}`}
          />
          <span title={dateTime(account.lastRefreshAt)}>
            {relativeTime(account.lastRefreshAt)}
          </span>
        </div>
        <div className="account-item-actions">
          <Button
            variant="ghost"
            size="icon"
            aria-label={`同步账户 ${account.label}`}
            title="同步用量"
            onClick={onRefresh}
            disabled={refreshDisabled}
          >
            <RefreshCw size={14} className={refreshing ? "animate-spin" : ""} />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={`编辑账户 ${account.label}`}
            title="编辑账户"
            onClick={onEdit}
          >
            <Pencil size={14} />
          </Button>
          <ConfirmAction
            title={`删除“${account.label}”？`}
            description="账户将从调度池移除，历史请求日志会保留。此操作不可撤销。"
            onConfirm={onDelete}
            pending={deletePending}
            trigger={
              <Button
                variant="ghost"
                size="icon"
                className="destructive-ghost"
                aria-label={`删除账户 ${account.label}`}
                title="删除账户"
              >
                <Trash2 size={14} />
              </Button>
            }
          />
        </div>
      </footer>
    </article>
  );
}
