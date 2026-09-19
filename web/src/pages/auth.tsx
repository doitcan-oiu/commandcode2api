import { useState } from "react";
import {
  ArrowRight,
  Eye,
  EyeOff,
  KeyRound,
  ShieldCheck,
  Layers3,
  Workflow,
} from "lucide-react";
import { useMutation } from "@tanstack/react-query";
import { api, queryClient } from "@/lib/api";
import { Brand, Field, PendingButton } from "@/components/shared";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";

export function AuthPage({ setup }: { setup: boolean }) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const mutation = useMutation({
    mutationFn: () => {
      if (setup && password !== confirm)
        throw new Error("两次输入的密码不一致");
      return api(setup ? "/setup" : "/login", "POST", {
        username,
        password,
      });
    },
    onSuccess: () => {
      setPassword("");
      setConfirm("");
      queryClient.removeQueries({
        predicate: (query) => query.queryKey[0] !== "session",
      });
      void queryClient.invalidateQueries({ queryKey: ["session"] });
    },
  });
  return (
    <div className="auth-page">
      <header className="auth-header">
        <Brand />
        <span>私有 API 工作空间</span>
      </header>
      <main className="auth-stage">
        <aside className="auth-overview">
          <span className="section-kicker">YOUR PRIVATE GATEWAY</span>
          <h1>
            所有账户，
            <br />
            一个 API 入口。
          </h1>
          <p>在自己的基础设施上，管理额度、分配访问、追踪调用。</p>
          <div className="auth-topology" aria-hidden="true">
            <div>
              <Layers3 size={22} />
              <span>账户池</span>
            </div>
            <i />
            <div>
              <Workflow size={24} />
              <span>CommandCode</span>
            </div>
            <i />
            <div>
              <KeyRound size={21} />
              <span>你的应用</span>
            </div>
          </div>
          <ol className="auth-capabilities">
            <li>
              <b>01</b>
              <span>
                <strong>统一管理账户</strong>
                <small>汇集多个上游 Key 与各自的使用额度</small>
              </span>
            </li>
            <li>
              <b>02</b>
              <span>
                <strong>独立控制访问</strong>
                <small>按应用分发令牌、设置权限与配额</small>
              </span>
            </li>
            <li>
              <b>03</b>
              <span>
                <strong>随时了解运行情况</strong>
                <small>查看请求路径、用量与异常状态</small>
              </span>
            </li>
          </ol>
        </aside>
        <section className="auth-form-area">
          <div className="auth-form-wrap">
            <div className="auth-icon">
              {setup ? <KeyRound size={24} /> : <ShieldCheck size={24} />}
            </div>
            <h2>{setup ? "初始化你的网关" : "登录管理控制台"}</h2>
            <p className="auth-subtitle">
              {setup
                ? "创建管理员账户，开始管理你的 API 资源。"
                : "继续查看账户用量与网关运行情况。"}
            </p>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                mutation.mutate();
              }}
              className="space-y-5"
            >
              <Field id="username" label="管理员用户名">
                <Input
                  id="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="admin"
                  required
                  autoComplete="username"
                />
              </Field>
              <Field
                id="password"
                label="密码"
                hint={
                  setup ? "至少 8 个字符，建议使用独立的长密码。" : undefined
                }
              >
                <div className="password-field">
                  <Input
                    id="password"
                    type={showPassword ? "text" : "password"}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    minLength={setup ? 8 : 1}
                    required
                    autoComplete={setup ? "new-password" : "current-password"}
                    placeholder={setup ? "创建管理员密码" : "输入管理员密码"}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={showPassword ? "隐藏密码" : "显示密码"}
                    onClick={() => setShowPassword(!showPassword)}
                  >
                    {showPassword ? <EyeOff size={15} /> : <Eye size={15} />}
                  </Button>
                </div>
              </Field>
              {setup && (
                <Field id="confirm" label="确认密码">
                  <Input
                    id="confirm"
                    type="password"
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    required
                    autoComplete="new-password"
                    placeholder="再次输入密码"
                  />
                </Field>
              )}
              {mutation.error && (
                <div className="form-error" role="alert">
                  {mutation.error.message}
                </div>
              )}
              <PendingButton
                className="w-full h-11"
                pending={mutation.isPending}
                type="submit"
              >
                {setup ? "创建账户并进入" : "登录控制台"}
                <ArrowRight size={16} />
              </PendingButton>
            </form>
            <div className="auth-tip">
              <ShieldCheck size={14} />
              <span>凭据仅用于连接你自己的网关服务。</span>
            </div>
          </div>
        </section>
      </main>
      <footer className="auth-form-foot">
        <span>CommandCode Gateway</span>
        <span>OpenAI · Anthropic · Responses</span>
      </footer>
    </div>
  );
}
