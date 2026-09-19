import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ArrowRight,
  ArrowUpRight,
  Clock3,
  Code2,
  Layers3,
  RefreshCw,
  ShieldCheck,
  TriangleAlert,
} from "lucide-react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/lib/api";
import { accountErrorLabel, compact, number, relativeTime } from "@/lib/format";
import type { Account, Overview } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  CopyButton,
  EmptyState,
  ErrorState,
  Freshness,
  Loading,
  PageHeading,
} from "@/components/shared";
export function OverviewPage({
  navigate,
}: {
  navigate: (page: string) => void;
}) {
  const [metric, setMetric] = useState<"requests" | "tokens" | "errors">(
    "requests",
  );
  const overview = useQuery({
    queryKey: ["overview"],
    queryFn: () => api<Overview>("/overview"),
    refetchInterval: 30_000,
  });
  const accounts = useQuery({
    queryKey: ["accounts"],
    queryFn: () => api<{ items: Account[] }>("/accounts"),
    refetchInterval: 30_000,
  });
  const stats = overview.data?.stats;
  const attention = (accounts.data?.items || []).filter(
    (a) =>
      a.enabled &&
      (["cooldown", "invalid", "error", "exhausted"].includes(a.status) ||
        !!a.usage?.failures?.length),
  );
  const metrics = stats
    ? [
        {
          id: "requests" as const,
          label: "请求次数",
          value: number(stats.requests24h),
          unit: "requests",
        },
        {
          id: "tokens" as const,
          label: "Token 用量",
          value: compact(stats.inputTokens24h + stats.outputTokens24h),
          unit: "tokens",
        },
        {
          id: "errors" as const,
          label: "错误请求",
          value: number(
            overview.data?.series.reduce((sum, p) => sum + p.errors, 0),
          ),
          unit: "errors",
        },
      ]
    : [];
  const endpoint = window.location.origin + "/v1";
  return (
    <>
      <PageHeading
        eyebrow="GATEWAY / OVERVIEW"
        title="运行总览"
        description="从流量到资源，掌握网关的每个环节。"
        actions={
          <>
            <Freshness
              at={overview.dataUpdatedAt}
              loading={overview.isFetching}
            />
            <Button
              variant="outline"
              disabled={overview.isFetching}
              onClick={() => {
                void overview.refetch();
                void accounts.refetch();
              }}
            >
              <RefreshCw
                size={15}
                className={overview.isFetching ? "animate-spin" : ""}
              />
              刷新
            </Button>
          </>
        }
      />
      {overview.isPending ? (
        <Loading rows={4} />
      ) : overview.error ? (
        <ErrorState
          error={overview.error}
          retry={() => void overview.refetch()}
        />
      ) : (
        stats && (
          <div className="dashboard-layout">
            <section className="dashboard-traffic workspace-panel">
              <header className="panel-heading">
                <div>
                  <span className="section-kicker">TRAFFIC ANALYTICS</span>
                  <h2>流量分析</h2>
                </div>
                <span className="period-label">
                  <Clock3 size={13} />
                  最近 24 小时
                </span>
              </header>
              <div className="traffic-metrics" aria-label="图表指标">
                {metrics.map((item) => (
                  <button
                    key={item.id}
                    aria-pressed={metric === item.id}
                    onClick={() => setMetric(item.id)}
                  >
                    <span>
                      {item.label}
                      <ArrowUpRight size={13} />
                    </span>
                    <strong>{item.value}</strong>
                    <small>{item.unit}</small>
                  </button>
                ))}
              </div>
              <div className="traffic-chart">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart
                    data={overview.data.series}
                    margin={{ top: 16, right: 18, left: -18, bottom: 0 }}
                  >
                    <defs>
                      <linearGradient
                        id="trafficFill"
                        x1="0"
                        y1="0"
                        x2="0"
                        y2="1"
                      >
                        <stop
                          offset="0%"
                          stopColor="var(--primary)"
                          stopOpacity={0.24}
                        />
                        <stop
                          offset="100%"
                          stopColor="var(--primary)"
                          stopOpacity={0.01}
                        />
                      </linearGradient>
                    </defs>
                    <CartesianGrid
                      stroke="var(--border)"
                      strokeDasharray="3 6"
                      vertical={false}
                    />
                    <XAxis
                      dataKey="timestamp"
                      tickFormatter={(v) =>
                        new Date(v).toLocaleTimeString("zh-CN", {
                          hour: "2-digit",
                          minute: "2-digit",
                          hour12: false,
                        })
                      }
                      minTickGap={50}
                      tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
                      axisLine={false}
                      tickLine={false}
                      dy={8}
                    />
                    <YAxis
                      allowDecimals={false}
                      tickFormatter={(v) => compact(v)}
                      tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
                      axisLine={false}
                      tickLine={false}
                    />
                    <Tooltip
                      labelFormatter={(v) =>
                        new Date(Number(v)).toLocaleString("zh-CN")
                      }
                      contentStyle={{
                        background: "var(--popover)",
                        border: "1px solid var(--border-strong)",
                        borderRadius: 10,
                        fontSize: 12,
                      }}
                      labelStyle={{
                        color: "var(--text-secondary)",
                        marginBottom: 8,
                      }}
                      cursor={{ stroke: "var(--border-strong)" }}
                    />
                    <Area
                      type="monotone"
                      dataKey={metric}
                      name={metrics.find((m) => m.id === metric)?.label}
                      stroke="var(--primary)"
                      strokeWidth={2.5}
                      fill="url(#trafficFill)"
                      isAnimationActive={false}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
              <footer className="traffic-foot">
                <span>
                  <ShieldCheck size={14} />
                  成功率{" "}
                  <b>
                    {stats.requests24h
                      ? number(stats.successRate, 1) + "%"
                      : "—"}
                  </b>
                </span>
                <span>
                  <Clock3 size={14} />
                  平均耗时 <b>{number(stats.averageLatencyMs / 1000, 2)}s</b>
                </span>
                <span>
                  输入 {compact(stats.inputTokens24h)} / 输出{" "}
                  {compact(stats.outputTokens24h)}
                </span>
              </footer>
            </section>
            <section className="dashboard-operations workspace-panel">
              <header className="panel-heading">
                <div>
                  <span className="section-kicker">RESOURCES</span>
                  <h2>资源状态</h2>
                </div>
                <Layers3 size={18} />
              </header>
              <div className="resource-numbers">
                <div>
                  <span>可用账户</span>
                  <strong>
                    {stats.activeAccounts}
                    <small> / {stats.totalAccounts}</small>
                  </strong>
                </div>
                <div>
                  <span>在途请求</span>
                  <strong>{stats.inflight}</strong>
                </div>
                <div>
                  <span>访问令牌</span>
                  <strong>{stats.totalClients}</strong>
                </div>
              </div>
              <div className="attention-heading">
                <h3>需要关注</h3>
                <span>
                  {accounts.isPending || accounts.error
                    ? "—"
                    : attention.length}
                </span>
              </div>
              {accounts.isPending ? (
                <Loading rows={1} />
              ) : accounts.error ? (
                <div className="attention-error">
                  <p>账户状态暂时不可用</p>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void accounts.refetch()}
                  >
                    重新获取
                  </Button>
                </div>
              ) : attention.length ? (
                <div className="attention-list">
                  {attention.slice(0, 3).map((a) => (
                    <div key={a.id}>
                      <TriangleAlert size={14} />
                      <div>
                        <strong title={a.label}>{a.label}</strong>
                        <p>
                          {a.lastError
                            ? accountErrorLabel(a.lastError)
                            : a.status === "cooldown"
                              ? "账户正在冷却"
                              : a.usage?.failures?.length
                                ? "部分用量同步失败"
                                : "账户暂不可用"}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="attention-clear">
                  <ShieldCheck size={23} />
                  <strong>
                    {stats.totalAccounts
                      ? "当前没有待处理的账户"
                      : "账户池等待接入"}
                  </strong>
                  <p>
                    {stats.totalAccounts
                      ? "已连接账户未报告需关注的状态。"
                      : "添加上游账户，开始分配 API 请求。"}
                  </p>
                </div>
              )}
              <Button
                variant="outline"
                className="w-full"
                onClick={() => navigate("accounts")}
              >
                管理账户池
                <ArrowRight size={14} />
              </Button>
            </section>
            <section className="dashboard-activity workspace-panel">
              <header className="panel-heading">
                <div>
                  <span className="section-kicker">ACTIVITY</span>
                  <h2>最近调用</h2>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => navigate("logs")}
                >
                  查看请求日志
                  <ArrowRight size={14} />
                </Button>
              </header>
              {overview.data.recentLogs.length ? (
                <div className="activity-feed">
                  {overview.data.recentLogs.slice(0, 5).map((log) => (
                    <div className="activity-entry" key={log.id}>
                      <span
                        className={
                          "activity-glyph " +
                          (log.status >= 200 && log.status < 400
                            ? "success"
                            : "error")
                        }
                      >
                        <Activity size={16} />
                      </span>
                      <div className="activity-route">
                        <strong title={log.model}>
                          {log.model || "未知模型"}
                        </strong>
                        <span>
                          {log.clientName || "未命名令牌"}
                          <ArrowRight size={10} />
                          {log.accountName || "未分配账户"}
                        </span>
                      </div>
                      <div className="activity-measures">
                        <Badge
                          variant="outline"
                          className={
                            log.status >= 200 && log.status < 400
                              ? "status-good"
                              : "status-error"
                          }
                        >
                          {log.status || "中断"}
                        </Badge>
                        <span>
                          {compact(log.inputTokens + log.outputTokens)} tokens ·{" "}
                          {number(log.latencyMs / 1000, 2)}s
                        </span>
                      </div>
                      <time>{relativeTime(log.createdAt)}</time>
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState
                  title="等待第一条调用"
                  description="请求完成后，这里会显示调用路径、状态和用量。"
                  icon={<Activity />}
                />
              )}
            </section>
            <section className="dashboard-connect workspace-panel">
              <header className="panel-heading">
                <div>
                  <span className="section-kicker">QUICK CONNECT</span>
                  <h2>接入你的客户端</h2>
                </div>
                <Code2 size={18} />
              </header>
              <ol className="connect-steps">
                <li>
                  <span>01</span>
                  <div>
                    <h3>填入网关地址</h3>
                    <p>OpenAI 兼容客户端的 Base URL</p>
                    <div className="connect-endpoint">
                      <code title={endpoint}>{endpoint}</code>
                      <CopyButton value={endpoint} label="复制" />
                    </div>
                  </div>
                </li>
                <li>
                  <span>02</span>
                  <div>
                    <h3>使用独立访问令牌</h3>
                    <p>为应用分配权限与调用配额。</p>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => navigate("clients")}
                    >
                      管理访问令牌
                      <ArrowUpRight size={13} />
                    </Button>
                  </div>
                </li>
                <li>
                  <span>03</span>
                  <div>
                    <h3>选择协议并开始调用</h3>
                    <p>OpenAI · Anthropic · Responses</p>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => navigate("connection")}
                    >
                      查看接入示例
                      <ArrowUpRight size={13} />
                    </Button>
                  </div>
                </li>
              </ol>
            </section>
          </div>
        )
      )}
    </>
  );
}
