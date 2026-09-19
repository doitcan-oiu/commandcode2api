import type { Account, UsageReport, UsageWindow } from "./types";

export const number = (value: number | null | undefined, digits = 0) =>
  value == null || !Number.isFinite(value)
    ? "—"
    : value.toLocaleString("zh-CN", { maximumFractionDigits: digits });
export const compact = (value: number | null | undefined) =>
  value == null || !Number.isFinite(value)
    ? "—"
    : Intl.NumberFormat("en", {
        notation: "compact",
        maximumFractionDigits: 1,
      }).format(value);
export const dateTime = (value: number | null | undefined) =>
  value
    ? new Date(value).toLocaleString("zh-CN", {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      })
    : "—";
export const relativeTime = (value: number | null | undefined) => {
  if (!value) return "尚未同步";
  const seconds = Math.max(0, (Date.now() - value) / 1000);
  return seconds < 60
    ? "刚刚更新"
    : seconds < 3600
      ? `${Math.floor(seconds / 60)} 分钟前`
      : `${Math.floor(seconds / 3600)} 小时前`;
};
export function usagePercent(window?: UsageWindow | null): number | null {
  if (window?.used == null || window.cap == null || window.cap <= 0)
    return null;
  return Math.min(100, Math.max(0, (window.used / window.cap) * 100));
}
export function monthlyWindow(report?: UsageReport | null): UsageWindow | null {
  const cap = report?.plan?.monthlyCredits,
    remaining = report?.credits?.monthlyCredits;
  if (cap == null || remaining == null || cap <= 0) return null;
  return {
    used: Math.max(0, cap - remaining),
    cap,
    exceeded: remaining <= 0,
    resetAt: report?.plan?.currentPeriodEnd ?? 0,
  };
}
export function balance(report?: UsageReport | null) {
  const c = report?.credits;
  return !c ||
    c.monthlyCredits == null ||
    c.purchasedCredits == null ||
    c.freeCredits == null
    ? null
    : c.monthlyCredits + c.purchasedCredits + c.freeCredits;
}
export const splitModels = (value: string) => [
  ...new Set(
    value
      .split(/[,\n]/)
      .map((s) => s.trim())
      .filter(Boolean),
  ),
];
export const statusLabels: Record<string, string> = {
  active: "可用",
  healthy: "可用",
  ready: "可用",
  available: "可用",
  disabled: "已停用",
  cooldown: "冷却中",
  exhausted: "额度耗尽",
  quota_exhausted: "额度耗尽",
  invalid: "密钥无效",
  error: "异常",
  unknown: "待同步",
};
export const statusLabel = (value: string) => statusLabels[value] || value;

const errorLabels: Record<string, string> = {
  five_hour_exhausted: "5 小时窗口额度已用尽，等待重置",
  weekly_exhausted: "每周窗口额度已用尽，等待重置",
  quota_exhausted: "上游额度已用尽，等待恢复",
  balance_exhausted: "账户余额已用尽",
  "Upstream authentication rejected": "上游认证失败，请检查 API Key",
  "Upstream quota or rate limit reached": "上游额度或速率受限，暂时冷却",
  "Upstream temporarily unavailable": "上游暂时不可用，正在自动恢复",
  "Usage refresh failed; retrying on next refresh":
    "用量同步失败，将在下次同步时重试",
  "Some usage endpoints are temporarily unavailable":
    "部分上游用量数据暂时无法获取",
};
export const accountErrorLabel = (value: string) => errorLabels[value] || value;

export function isQuotaExhausted(
  account: Pick<Account, "status" | "lastError" | "enabled">,
) {
  return (
    account.enabled &&
    account.status === "cooldown" &&
    [
      "five_hour_exhausted",
      "weekly_exhausted",
      "quota_exhausted",
      "balance_exhausted",
    ].includes(account.lastError)
  );
}
