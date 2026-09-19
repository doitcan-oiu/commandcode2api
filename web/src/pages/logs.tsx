import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  Activity,
  ChevronLeft,
  ChevronRight,
  MousePointer2,
  Pause,
  Play,
  RefreshCw,
  Search,
  ShieldCheck,
  X,
} from "lucide-react";
import { api } from "@/lib/api";
import { compact, dateTime, number } from "@/lib/format";
import { useMediaQuery } from "@/lib/use-media-query";
import type { Account, Client, LogsPage, RequestLog } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  EmptyState,
  ErrorState,
  Freshness,
  Loading,
  PageHeading,
} from "@/components/shared";
import { RequestDetails } from "@/components/logs/request-details";

export function LogsPageView() {
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState("all");
  const [model, setModel] = useState("");
  const [draftModel, setDraftModel] = useState("");
  const [accountId, setAccountId] = useState("all");
  const [clientId, setClientId] = useState("all");
  const [selected, setSelected] = useState<RequestLog | null>(null);
  const [live, setLive] = useState(true);
  const compactView = useMediaQuery("(max-width: 1199px)");
  const params = new URLSearchParams({
    page: String(page),
    pageSize: "20",
    ...(status !== "all" ? { status } : {}),
    ...(model ? { model } : {}),
    ...(accountId !== "all" ? { accountId } : {}),
    ...(clientId !== "all" ? { clientId } : {}),
  });
  const logs = useQuery({
    queryKey: ["logs", params.toString()],
    queryFn: () => api<LogsPage>("/logs?" + params),
    placeholderData: keepPreviousData,
    refetchInterval: live ? 15_000 : false,
  });
  const accounts = useQuery({
    queryKey: ["accounts"],
    queryFn: () => api<{ items: Account[] }>("/accounts"),
  });
  const clients = useQuery({
    queryKey: ["clients"],
    queryFn: () => api<{ items: Client[] }>("/clients"),
  });
  const pages = Math.max(1, Math.ceil((logs.data?.total || 0) / 20));
  const resetPage = () => {
    setPage(1);
    setSelected(null);
  };
  const hasFilters =
    status !== "all" || model || accountId !== "all" || clientId !== "all";
  return (
    <>
      <PageHeading
        eyebrow="OBSERVABILITY / REQUESTS"
        title="请求日志"
        description="选择一次调用，检查它的路由、响应与资源消耗。"
        actions={
          <>
            <Button
              variant="outline"
              aria-pressed={live}
              onClick={() => setLive(!live)}
            >
              {live ? <Pause size={14} /> : <Play size={14} />}自动刷新
              {live ? "开启" : "暂停"}
            </Button>
            <Button
              variant="outline"
              disabled={logs.isFetching}
              onClick={() => void logs.refetch()}
            >
              <RefreshCw
                size={15}
                className={logs.isFetching ? "animate-spin" : ""}
              />
              刷新
            </Button>
          </>
        }
      />
      <div className="log-workspace">
        <section className="log-stream workspace-panel">
          <header className="log-stream-heading">
            <h2>
              调用记录<span>{logs.data ? number(logs.data.total) : "—"}</span>
            </h2>
            <Freshness at={logs.dataUpdatedAt} loading={logs.isFetching} />
          </header>
          <div className="log-filters">
            <form
              className="search-input"
              onSubmit={(e) => {
                e.preventDefault();
                setModel(draftModel.trim());
                resetPage();
              }}
            >
              <Search size={15} />
              <Input
                aria-label="按模型筛选日志"
                placeholder="搜索模型，回车筛选…"
                value={draftModel}
                onChange={(e) => setDraftModel(e.target.value)}
              />
            </form>
            <Select
              value={status}
              onValueChange={(v) => {
                setStatus(v);
                resetPage();
              }}
            >
              <SelectTrigger aria-label="请求状态">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="success">成功请求</SelectItem>
                <SelectItem value="error">失败请求</SelectItem>
                <SelectItem value="429">429 限流</SelectItem>
                <SelectItem value="401">401 认证</SelectItem>
                <SelectItem value="503">503 不可用</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={accountId}
              onValueChange={(v) => {
                setAccountId(v);
                resetPage();
              }}
            >
              <SelectTrigger aria-label="上游账户">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部账户</SelectItem>
                {accounts.data?.items.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={clientId}
              onValueChange={(v) => {
                setClientId(v);
                resetPage();
              }}
            >
              <SelectTrigger aria-label="访问令牌">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部令牌</SelectItem>
                {clients.data?.items.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {hasFilters && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setStatus("all");
                  setModel("");
                  setDraftModel("");
                  setAccountId("all");
                  setClientId("all");
                  resetPage();
                }}
              >
                <X size={13} />
                清除筛选
              </Button>
            )}
          </div>
          {logs.isPending ? (
            <div className="panel-loading">
              <Loading rows={3} />
            </div>
          ) : logs.error ? (
            <ErrorState error={logs.error} retry={() => void logs.refetch()} />
          ) : (
            <>
              {logs.data.items.length ? (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>模型 / 协议</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead className="text-right">Tokens</TableHead>
                      <TableHead className="text-right">耗时</TableHead>
                      <TableHead className="text-right">时间</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {logs.data.items.map((log) => (
                      <TableRow
                        key={log.id}
                        data-state={
                          selected?.id === log.id ? "selected" : undefined
                        }
                        className="request-row"
                        onClick={() => setSelected(log)}
                      >
                        <TableCell>
                          <button
                            className="request-select"
                            aria-label={"查看请求 " + log.id}
                            aria-pressed={selected?.id === log.id}
                            onClick={(e) => {
                              e.stopPropagation();
                              setSelected(log);
                            }}
                          >
                            <strong title={log.model}>
                              {log.model || "未知模型"}
                            </strong>
                            <span>
                              {log.protocol} · {log.stream ? "流式" : "非流式"}
                              <i /> {log.clientName || "—"}
                            </span>
                          </button>
                        </TableCell>
                        <TableCell>
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
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {compact(log.inputTokens + log.outputTokens)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums text-muted-foreground">
                          {number(log.latencyMs / 1000, 2)}s
                        </TableCell>
                        <TableCell className="text-right text-muted-foreground">
                          <span title={dateTime(log.createdAt)}>
                            {new Date(log.createdAt).toLocaleTimeString(
                              "zh-CN",
                              { hour12: false },
                            )}
                          </span>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              ) : (
                <EmptyState
                  title="没有符合条件的记录"
                  description="发起 API 调用后，记录会出现在这里。已有记录时可以清除筛选条件。"
                  icon={<Activity />}
                />
              )}
              <div className="pagination">
                <span>每页 20 条</span>
                <div>
                  <span>
                    {page} / {pages}
                  </span>
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label="上一页"
                    disabled={page <= 1 || logs.isPlaceholderData}
                    onClick={() => {
                      setPage((p) => p - 1);
                      setSelected(null);
                    }}
                  >
                    <ChevronLeft size={14} />
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label="下一页"
                    disabled={page >= pages || logs.isPlaceholderData}
                    onClick={() => {
                      setPage((p) => p + 1);
                      setSelected(null);
                    }}
                  >
                    <ChevronRight size={14} />
                  </Button>
                </div>
              </div>
            </>
          )}
        </section>
        {!compactView && (
          <aside className="log-inspector workspace-panel">
            <header className="inspector-heading">
              <h2>请求详情</h2>
              {selected && (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="清除请求选择"
                  onClick={() => setSelected(null)}
                >
                  <X size={14} />
                </Button>
              )}
            </header>
            {selected ? (
              <RequestDetails log={selected} />
            ) : (
              <div className="inspector-placeholder">
                <MousePointer2 size={28} />
                <h3>选择一条请求</h3>
                <p>在这里查看完整调用路径、Token 消耗和错误信息。</p>
                <span>记录仅包含请求元数据</span>
              </div>
            )}
          </aside>
        )}
      </div>
      <p className="privacy-note">
        <ShieldCheck size={13} />
        不保存提示词、对话内容或响应正文。
      </p>
      <Sheet
        open={compactView && !!selected}
        onOpenChange={(open) => !open && setSelected(null)}
      >
        <SheetContent className="detail-sheet">
          <SheetHeader>
            <SheetTitle>请求详情</SheetTitle>
            <SheetDescription>路由与资源消耗</SheetDescription>
          </SheetHeader>
          {selected && <RequestDetails log={selected} />}
        </SheetContent>
      </Sheet>
    </>
  );
}
