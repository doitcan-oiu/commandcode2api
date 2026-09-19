import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Copy, KeyRound, Plus, Search, ShieldCheck } from "lucide-react";
import { api, copyText, useAction } from "@/lib/api";
import { splitModels } from "@/lib/format";
import { ClientDetails } from "@/components/clients/client-details";
import type { Client } from "@/lib/types";
import { useNow } from "@/lib/use-now";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  EmptyState,
  ErrorState,
  Field,
  Loading,
  PageHeading,
  PendingButton,
} from "@/components/shared";

function localInputDate(timestamp: number) {
  if (!timestamp) return "";
  const d = new Date(timestamp);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16);
}
function ClientEditor({
  client,
  onClose,
  onCreated,
}: {
  client: Client | null;
  onClose: () => void;
  onCreated: (token: string) => void;
}) {
  const [name, setName] = useState(client?.name || "");
  const [rpm, setRpm] = useState(client?.rpm ?? 60);
  const [concurrent, setConcurrent] = useState(client?.maxConcurrent ?? 5);
  const [requests, setRequests] = useState(client?.maxRequests ?? 0);
  const [tokens, setTokens] = useState(client?.maxTokens ?? 0);
  const [models, setModels] = useState(client?.models.join("\n") || "");
  const [expiresAt, setExpiresAt] = useState(
    localInputDate(client?.expiresAt || 0),
  );
  const save = useAction(
    () =>
      api<{ client: Client; token?: string }>(
        client ? `/clients/${client.id}` : "/clients",
        client ? "PATCH" : "POST",
        {
          name,
          rpm,
          maxConcurrent: concurrent,
          maxRequests: requests,
          maxTokens: tokens,
          models: splitModels(models),
          expiresAt: expiresAt ? new Date(expiresAt).getTime() : 0,
        },
      ),
    client ? "令牌设置已保存" : "访问令牌已创建",
    (data) => {
      onClose();
      if (data.token) onCreated(data.token);
    },
  );
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[540px]">
        <DialogHeader>
          <DialogTitle>{client ? "编辑访问令牌" : "创建访问令牌"}</DialogTitle>
          <DialogDescription>
            为应用分配独立令牌。限制值为 0 时表示不限制。
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field id="client-name" label="令牌名称">
            <Input
              id="client-name"
              placeholder="例如：Cherry Studio / 开发环境"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={100}
              required
            />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field id="client-rpm" label="每分钟请求上限">
              <Input
                id="client-rpm"
                type="number"
                min={0}
                value={rpm}
                onChange={(e) => setRpm(Number(e.target.value))}
                required
              />
            </Field>
            <Field id="client-concurrent" label="最大并发">
              <Input
                id="client-concurrent"
                type="number"
                min={0}
                value={concurrent}
                onChange={(e) => setConcurrent(Number(e.target.value))}
                required
              />
            </Field>
            <Field id="client-requests" label="累计请求配额">
              <Input
                id="client-requests"
                type="number"
                min={0}
                value={requests}
                onChange={(e) => setRequests(Number(e.target.value))}
                required
              />
            </Field>
            <Field id="client-tokens" label="累计 Token 配额">
              <Input
                id="client-tokens"
                type="number"
                min={0}
                value={tokens}
                onChange={(e) => setTokens(Number(e.target.value))}
                required
              />
            </Field>
          </div>
          <Field
            id="client-expiry"
            label="过期时间（可选）"
            hint="使用浏览器本地时间；留空表示永不过期。"
          >
            <Input
              id="client-expiry"
              type="datetime-local"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
            />
          </Field>
          <Field
            id="client-models"
            label="模型白名单"
            hint="留空允许所有模型；每行一个，或用英文逗号分隔。"
          >
            <Textarea
              id="client-models"
              value={models}
              onChange={(e) => setModels(e.target.value)}
              placeholder="deepseek/deepseek-v4-flash"
              rows={2}
            />
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              取消
            </Button>
            <PendingButton type="submit" pending={save.isPending}>
              {client ? "保存修改" : "创建令牌"}
            </PendingButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
export function ClientsPage() {
  const now = useNow();
  const query = useQuery({
    queryKey: ["clients"],
    queryFn: () => api<{ items: Client[] }>("/clients"),
    refetchInterval: 30_000,
  });
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [editor, setEditor] = useState<Client | null | undefined>(undefined);
  const [token, setToken] = useState("");
  const [saved, setSaved] = useState(false);
  const toggle = useAction(
    ({ id, enabled }: { id: string; enabled: boolean }) =>
      api("/clients/" + id, "PATCH", { enabled }),
    "令牌状态已更新",
  );
  const remove = useAction(
    (id: string) => api("/clients/" + id, "DELETE"),
    "令牌已删除",
  );
  const clients = query.data?.items || [];
  const filtered = clients.filter((c) =>
    (c.name + " " + c.tokenPreview)
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  const selected = filtered.find((c) => c.id === selectedId) || filtered[0];
  return (
    <>
      <PageHeading
        eyebrow="APPLICATION ACCESS"
        title="访问令牌"
        description="为每个应用分配独立凭证，集中管理权限、速率与用量。"
        actions={
          <Button onClick={() => setEditor(null)}>
            <Plus size={15} />
            创建令牌
          </Button>
        }
      />
      {query.isPending ? (
        <Loading rows={3} />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : !clients.length ? (
        <div className="workspace-empty">
          <EmptyState
            title="为第一个应用创建令牌"
            description="客户端使用网关令牌调用 API，完整令牌只在创建时显示一次。"
            icon={<KeyRound />}
            action={
              <Button onClick={() => setEditor(null)}>
                <Plus size={15} />
                创建访问令牌
              </Button>
            }
          />
        </div>
      ) : (
        <div className="clients-workspace">
          <aside className="client-directory">
            <div className="directory-heading">
              <h2>应用凭证</h2>
              <span>{clients.length}</span>
            </div>
            <div className="search-input">
              <Search size={15} />
              <Input
                aria-label="搜索令牌"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="搜索名称或令牌…"
              />
            </div>
            <div className="client-directory-list">
              {filtered.map((client) => (
                <button
                  key={client.id}
                  className="client-directory-item"
                  aria-pressed={selected?.id === client.id}
                  onClick={() => setSelectedId(client.id)}
                >
                  <div>
                    <strong title={client.name}>{client.name}</strong>
                    <span
                      className={
                        client.enabled &&
                        (!client.expiresAt || client.expiresAt > now)
                          ? "live-dot"
                          : "live-dot muted"
                      }
                    />
                  </div>
                  <code>{client.tokenPreview}</code>
                  <small>
                    {!client.enabled
                      ? "已停用"
                      : client.expiresAt && client.expiresAt <= now
                        ? "已过期"
                        : "已启用"}
                    <span>{client.inflight} 个并发</span>
                  </small>
                </button>
              ))}
            </div>
            <p className="directory-note">
              <ShieldCheck size={14} />
              上游密钥不会暴露给客户端
            </p>
          </aside>
          {selected ? (
            <ClientDetails
              client={selected}
              now={now}
              onEdit={() => setEditor(selected)}
              onToggle={(enabled) =>
                toggle.mutate({ id: selected.id, enabled })
              }
              onDelete={() => remove.mutate(selected.id)}
              togglePending={toggle.isPending}
              deletePending={remove.isPending}
            />
          ) : (
            <div className="workspace-empty">
              <EmptyState
                title="没有匹配的令牌"
                description="尝试其他名称，或清除搜索条件。"
                icon={<Search />}
              />
            </div>
          )}
        </div>
      )}
      {editor !== undefined && (
        <ClientEditor
          client={editor}
          onClose={() => setEditor(undefined)}
          onCreated={(value) => {
            setSaved(false);
            setToken(value);
          }}
        />
      )}
      <Dialog
        open={!!token}
        onOpenChange={(open) => {
          if (!open && saved) setToken("");
        }}
      >
        <DialogContent
          onInteractOutside={(e) => e.preventDefault()}
          onEscapeKeyDown={(e) => {
            if (!saved) e.preventDefault();
          }}
          showCloseButton={false}
        >
          <DialogHeader>
            <DialogTitle>访问令牌已创建</DialogTitle>
            <DialogDescription>
              请立即复制并妥善保存。关闭后将无法再次查看完整令牌。
            </DialogDescription>
          </DialogHeader>
          <div className="token-reveal">
            <code>{token}</code>
            <Button
              variant="outline"
              className="w-full"
              onClick={() => void copyText(token)}
            >
              <Copy size={15} />
              复制访问令牌
            </Button>
          </div>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input
              className="accent-emerald-400"
              type="checkbox"
              checked={saved}
              onChange={(e) => setSaved(e.target.checked)}
            />
            我已保存这枚令牌
          </label>
          <DialogFooter>
            <Button disabled={!saved} onClick={() => setToken("")}>
              <Check size={15} />
              完成
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
