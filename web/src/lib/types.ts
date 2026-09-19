export interface Session {
  authenticated: boolean;
  setupRequired: boolean;
  username: string;
}
export interface UsageWindow {
  used: number | null;
  cap: number | null;
  exceeded: boolean;
  resetAt: number;
}
export interface UsageReport {
  account: { id: string; name: string; userName: string } | null;
  credits: {
    monthlyCredits: number | null;
    purchasedCredits: number | null;
    freeCredits: number | null;
    limited: boolean;
    exceeded: string;
    belowThreshold: boolean;
    creditThreshold: number | null;
    fiveHour: UsageWindow | null;
    weekly: UsageWindow | null;
  } | null;
  plan: {
    planId: string;
    name: string;
    status: string;
    monthlyCredits: number | null;
    currentPeriodEnd: number;
    currentPeriodStart: number;
    cancelAtPeriodEnd: boolean;
    estimated: boolean;
  } | null;
  usage: {
    totalCount: number | null;
    totalCost: number | null;
    averageCost: number | null;
    successRate: number | null;
    completedCount: number | null;
    failedCount: number | null;
    totalTokensIn: number | null;
    totalTokensOut: number | null;
    totalCredits: number | null;
    periodBasis: string;
  } | null;
  failures: string[];
}
export interface Account {
  id: string;
  label: string;
  keyPreview: string;
  enabled: boolean;
  weight: number;
  priority: number;
  maxConcurrent: number;
  models: string[];
  status: string;
  cooldownUntil: number;
  lastError: string;
  inflight: number;
  usage: UsageReport | null;
  lastRefreshAt: number;
  createdAt: number;
  updatedAt: number;
}
export interface Client {
  id: string;
  name: string;
  tokenPreview: string;
  enabled: boolean;
  rpm: number;
  maxConcurrent: number;
  maxRequests: number;
  maxTokens: number;
  models: string[];
  expiresAt: number;
  usedRequests: number;
  usedTokens: number;
  inflight: number;
  lastUsedAt: number;
  createdAt: number;
}
export interface RequestLog {
  id: string;
  model: string;
  protocol: string;
  accountId: string;
  accountName: string;
  clientId: string;
  clientName: string;
  status: number;
  stream: boolean;
  inputTokens: number;
  outputTokens: number;
  latencyMs: number;
  attempts: number;
  error: string;
  createdAt: number;
}
export interface Stats {
  totalAccounts: number;
  activeAccounts: number;
  totalClients: number;
  inflight: number;
  requests24h: number;
  successRate: number;
  inputTokens24h: number;
  outputTokens24h: number;
  averageLatencyMs: number;
}
export interface Overview {
  stats: Stats;
  series: {
    timestamp: number;
    requests: number;
    errors: number;
    tokens: number;
  }[];
  recentLogs: RequestLog[];
}
export interface Settings {
  strategy: "quota_aware" | "weighted_round_robin" | "least_inflight";
  maxRetries: number;
  refreshIntervalSeconds: number;
  cooldownSeconds: number;
  logRetentionDays: number;
  sessionAffinity: boolean;
}
export interface LogsPage {
  items: RequestLog[];
  total: number;
  page: number;
  pageSize: number;
}
