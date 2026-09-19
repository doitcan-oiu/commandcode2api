import { ArrowDown, KeyRound, Layers3 } from "lucide-react";
import type { RequestLog } from "@/lib/types";
import { dateTime, number } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { CopyButton } from "@/components/shared";
export function RequestDetails({ log }: { log: RequestLog }) {
  return (
    <div className="request-inspection">
      <div className="inspection-status">
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
          {log.stream ? "流式响应" : "非流式响应"} · {log.protocol}
        </span>
      </div>
      <h3 className="inspection-model">{log.model || "未知模型"}</h3>
      <time>{dateTime(log.createdAt)}</time>
      <div className="inspection-route">
        <div>
          <KeyRound size={15} />
          <span>
            <small>客户端令牌</small>
            <strong>{log.clientName || "—"}</strong>
          </span>
        </div>
        <ArrowDown className="route-arrow" size={14} />
        <div>
          <Layers3 size={15} />
          <span>
            <small>上游账户</small>
            <strong>{log.accountName || "未分配"}</strong>
          </span>
        </div>
      </div>
      <div className="inspection-metrics">
        <div>
          <span>输入 Tokens</span>
          <strong>{number(log.inputTokens)}</strong>
        </div>
        <div>
          <span>输出 Tokens</span>
          <strong>{number(log.outputTokens)}</strong>
        </div>
        <div>
          <span>请求耗时</span>
          <strong>
            {number(log.latencyMs / 1000, 2)}
            <small> s</small>
          </strong>
        </div>
        <div>
          <span>尝试次数</span>
          <strong>{log.attempts}</strong>
        </div>
      </div>
      {log.error && (
        <div className="inspection-error">
          <strong>错误信息</strong>
          <p>{log.error}</p>
        </div>
      )}
      <div className="inspection-id">
        <span>请求 ID</span>
        <code>{log.id}</code>
        <CopyButton value={log.id} label="复制 ID" />
      </div>
    </div>
  );
}
