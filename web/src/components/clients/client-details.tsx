import { KeyRound, Pencil, ShieldCheck, Trash2 } from "lucide-react";
import type { Client } from "@/lib/types";
import { dateTime, number } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { Switch } from "@/components/ui/switch";
import { ConfirmAction } from "@/components/shared";

function ClientQuota({
  label,
  used,
  limit,
  unit,
}: {
  label: string;
  used: number;
  limit: number;
  unit: string;
}) {
  const percent = limit > 0 ? (used / limit) * 100 : null;
  return (
    <div className="client-quota">
      <div>
        <span>{label}</span>
        <small>
          {percent === null ? "不限制" : `${number(percent, 1)}% 已用`}
        </small>
      </div>
      <strong>
        {number(used)}
        <span>{unit}</span>
      </strong>
      {percent !== null ? (
        <Progress
          aria-label={label}
          value={Math.min(percent, 100)}
          className={percent >= 100 ? "quota-danger" : ""}
        />
      ) : (
        <div className="unlimited-line" />
      )}
      <p>
        {limit ? `累计上限 ${number(limit)} ${unit}` : "未设置累计配额上限"}
      </p>
    </div>
  );
}
export function ClientDetails({
  client,
  now,
  onEdit,
  onToggle,
  onDelete,
  togglePending,
  deletePending,
}: {
  client: Client;
  now: number;
  onEdit: () => void;
  onToggle: (enabled: boolean) => void;
  onDelete: () => void;
  togglePending: boolean;
  deletePending: boolean;
}) {
  const expired = !!client.expiresAt && client.expiresAt <= now;
  return (
    <article className="client-detail">
      <header className="client-detail-heading">
        <div className="client-detail-symbol">
          <KeyRound size={24} />
        </div>
        <div>
          <span className="section-kicker">ACCESS TOKEN</span>
          <h2>{client.name}</h2>
          <code>{client.tokenPreview}</code>
        </div>
        <Button variant="outline" onClick={onEdit}>
          <Pencil size={14} />
          编辑配置
        </Button>
      </header>
      <div className="client-access-state">
        <div>
          <span
            className={
              client.enabled && !expired ? "live-dot" : "live-dot muted"
            }
          />
          <strong>
            {!client.enabled
              ? "令牌已停用"
              : expired
                ? "令牌已过期"
                : "允许客户端访问"}
          </strong>
          <span>
            {expired ? "更新有效期后可继续使用" : "调用将分配至可用的上游账户"}
          </span>
        </div>
        <Switch
          checked={client.enabled}
          onCheckedChange={onToggle}
          disabled={togglePending}
          aria-label={`${client.enabled ? "停用" : "启用"}令牌 ${client.name}`}
        />
      </div>
      <section className="client-detail-section">
        <div className="section-caption">
          <h3>累计用量</h3>
          <span>按已记录的网关请求统计</span>
        </div>
        <div className="client-quota-grid">
          <ClientQuota
            label="请求配额"
            used={client.usedRequests}
            limit={client.maxRequests}
            unit="次"
          />
          <ClientQuota
            label="Token 配额"
            used={client.usedTokens}
            limit={client.maxTokens}
            unit="tokens"
          />
        </div>
      </section>
      <section className="client-detail-section">
        <div className="section-caption">
          <h3>调用限制</h3>
        </div>
        <dl className="client-limits">
          <div>
            <dt>每分钟请求</dt>
            <dd>
              {client.rpm || "不限"}
              <small>{client.rpm ? " RPM" : ""}</small>
            </dd>
          </div>
          <div>
            <dt>当前并发 / 上限</dt>
            <dd>
              {client.inflight}
              <small> / {client.maxConcurrent || "不限"}</small>
            </dd>
          </div>
          <div>
            <dt>有效期至</dt>
            <dd className="date-value">
              {client.expiresAt ? dateTime(client.expiresAt) : "长期有效"}
            </dd>
          </div>
        </dl>
      </section>
      <section className="client-detail-section">
        <div className="section-caption">
          <h3>模型权限</h3>
          <span>
            {client.models.length
              ? `${client.models.length} 个指定模型`
              : "可使用上游提供的所有模型"}
          </span>
        </div>
        <div className="model-permissions">
          {client.models.length ? (
            client.models.map((model) => (
              <Badge variant="secondary" key={model}>
                {model}
              </Badge>
            ))
          ) : (
            <span>
              <ShieldCheck size={16} />
              全部模型
            </span>
          )}
        </div>
      </section>
      <footer className="client-detail-footer">
        <div>
          <span>创建于 {dateTime(client.createdAt)}</span>
          <span>
            最近调用{" "}
            {client.lastUsedAt ? dateTime(client.lastUsedAt) : "尚无记录"}
          </span>
        </div>
        <ConfirmAction
          title={`删除“${client.name}”？`}
          description="删除后客户端将失去访问权限，历史请求日志会保留。"
          onConfirm={onDelete}
          pending={deletePending}
          trigger={
            <Button variant="ghost" size="sm" className="destructive-ghost">
              <Trash2 size={14} />
              删除令牌
            </Button>
          }
        />
      </footer>
    </article>
  );
}
