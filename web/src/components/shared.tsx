import {
  Terminal,
  AlertCircle,
  CircleDashed,
  Clock3,
  Copy,
  Database,
  LoaderCircle,
} from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "./ui/button";
import { Badge } from "./ui/badge";
import { Skeleton } from "./ui/skeleton";
import { Progress } from "./ui/progress";
import { Label } from "./ui/label";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "./ui/alert-dialog";
import { copyText } from "@/lib/api";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { dateTime, number, statusLabel, usagePercent } from "@/lib/format";
import type { UsageWindow } from "@/lib/types";
import { cn } from "@/lib/utils";

export function Brand({ compact: small = false }: { compact?: boolean }) {
  return (
    <div className="brand">
      <div className="brand-mark">
        <Terminal size={21} strokeWidth={2.2} />
      </div>
      {!small && (
        <div>
          <strong>
            Command<span>Code</span>
          </strong>
          <small>GATEWAY</small>
        </div>
      )}
    </div>
  );
}
export function PageHeading({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <div className="page-heading">
      <div>
        <span className="page-category">{eyebrow}</span>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      <div className="page-actions">{actions}</div>
    </div>
  );
}
export function Loading({ rows = 3 }: { rows?: number }) {
  return (
    <div className="grid gap-4" role="status" aria-label="正在加载">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-28 w-full rounded-xl" />
      ))}
    </div>
  );
}
export function ErrorState({
  error,
  retry,
}: {
  error: Error;
  retry?: () => void;
}) {
  return (
    <div className="empty-state error-state" role="alert">
      <div className="empty-icon">
        <AlertCircle />
      </div>
      <h3>暂时无法获取数据</h3>
      <p>{error.message}</p>
      {retry && (
        <Button variant="outline" onClick={retry}>
          重新加载
        </Button>
      )}
    </div>
  );
}
export function EmptyState({
  title,
  description,
  action,
  icon = <Database />,
}: {
  title: string;
  description: string;
  action?: ReactNode;
  icon?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <div className="empty-icon">{icon}</div>
      <h3>{title}</h3>
      <p>{description}</p>
      {action}
    </div>
  );
}
export function Field({
  label,
  hint,
  children,
  id,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
  id?: string;
}) {
  return (
    <div className="form-field">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {hint && <p>{hint}</p>}
    </div>
  );
}
export function PendingButton({
  pending,
  children,
  ...props
}: React.ComponentProps<typeof Button> & { pending: boolean }) {
  return (
    <Button {...props} disabled={pending || props.disabled}>
      {pending && <LoaderCircle className="animate-spin" size={16} />}
      {children}
    </Button>
  );
}
export function StatusBadge({ status }: { status: string }) {
  const healthy = ["active", "healthy", "ready", "available"].includes(status);
  const warning = ["cooldown", "exhausted", "quota_exhausted"].includes(status);
  return (
    <Badge
      variant="outline"
      className={cn(
        "status-badge",
        healthy
          ? "status-good"
          : warning
            ? "status-warning"
            : ["invalid", "error"].includes(status)
              ? "status-error"
              : "status-muted",
      )}
    >
      <span className="status-dot" />
      {statusLabel(status)}
    </Badge>
  );
}
export function QuotaBar({
  label,
  window,
  estimated = false,
  compact: dense = false,
}: {
  label: string;
  window?: UsageWindow | null;
  estimated?: boolean;
  compact?: boolean;
}) {
  const percent = usagePercent(window);
  if (dense) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="quota-row quota-compact" tabIndex={0}>
            <div className="quota-compact-heading">
              <div className="quota-compact-label">
                {label}
                {estimated && <span className="estimate-tag">估算</span>}
              </div>
              <span className="quota-compact-amount">
                {number(window?.used, 2)} / {number(window?.cap, 2)}
              </span>
              <strong>
                {percent === null ? "—" : `${number(percent, 1)}%`}
                <small>已用</small>
              </strong>
            </div>
            <Progress
              aria-label={`${label}已用比例`}
              value={percent}
              className={
                percent === null
                  ? "quota-unknown"
                  : percent >= 90
                    ? "quota-danger"
                    : percent >= 70
                      ? "quota-warning"
                      : ""
              }
            />
          </div>
        </TooltipTrigger>
        <TooltipContent sideOffset={6}>
          <p>
            {label}
            {estimated ? "（估算）" : ""}：已用 {number(window?.used, 2)} /{" "}
            {number(window?.cap, 2)} credits
          </p>
          <p>
            {window?.resetAt
              ? `${dateTime(window.resetAt)} 重置`
              : "重置时间未知"}
          </p>
        </TooltipContent>
      </Tooltip>
    );
  }
  return (
    <div className="quota-row">
      <div className="flex items-center justify-between gap-2">
        <span>
          {label}
          {estimated && <span className="estimate-tag">估算</span>}
        </span>
        <span className="tabular-nums text-foreground">
          {percent === null ? "—" : `${number(percent, 1)}%`}
          <span className="text-muted-foreground ml-1 text-xs">已用</span>
        </span>
      </div>
      <Progress
        aria-label={`${label}已用比例`}
        value={percent}
        className={cn(
          "h-1.5",
          percent === null
            ? "quota-unknown"
            : percent >= 90
              ? "quota-danger"
              : percent >= 70
                ? "quota-warning"
                : "",
        )}
      />
      <div className="flex justify-between text-[11px] text-muted-foreground">
        <span>
          {number(window?.used, 2)} / {number(window?.cap, 2)} credits
        </span>
        <span>
          {window?.resetAt
            ? `${dateTime(window.resetAt)} 重置`
            : "重置时间未知"}
        </span>
      </div>
    </div>
  );
}
export function CopyButton({
  value,
  label = "复制",
}: {
  value: string;
  label?: string;
}) {
  return (
    <Button variant="outline" size="sm" onClick={() => void copyText(value)}>
      <Copy size={14} />
      {label}
    </Button>
  );
}
export function ConfirmAction({
  trigger,
  title,
  description,
  onConfirm,
  pending,
  confirm = "确认删除",
}: {
  trigger: ReactNode;
  title: string;
  description: string;
  onConfirm: () => void;
  pending?: boolean;
  confirm?: string;
}) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>{trigger}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-white hover:bg-destructive/90"
            disabled={pending}
            onClick={onConfirm}
          >
            {confirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
export function Freshness({ at, loading }: { at: number; loading?: boolean }) {
  return (
    <span className="freshness">
      {loading ? (
        <CircleDashed size={13} className="animate-spin" />
      ) : (
        <Clock3 size={13} />
      )}
      {loading ? "同步中" : at ? `${dateTime(at)} 更新` : "等待首次同步"}
    </span>
  );
}
