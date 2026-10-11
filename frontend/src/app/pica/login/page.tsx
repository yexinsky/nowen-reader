"use client";

/**
 * 哔咔在线源 · 登录页
 * 接口:POST /api/pica/auth/login(哔咔邮箱/用户名 + 密码;无验证码)
 * 流程:提交 → picaLogin;1001 凭据错误;2002 提示检查网络;
 * 成功 → setPicaSession → 回跳 ?from= 指定路径(缺省 /pica)。
 * 已登录进入本页直接跳转 /pica。
 */

import { useState, type FormEvent } from "react";
import { Link, Navigate, useSearchParams } from "react-router-dom";
import {
  AlertTriangle,
  ArrowLeft,
  Eye,
  EyeOff,
  Loader2,
  Lock,
  LogIn,
  Settings as SettingsIcon,
  User,
} from "lucide-react";
import { isPicaApiError, picaLogin } from "@/lib/pica/client";
import { setPicaSession, usePicaSession, consumePicaExpiredHint } from "@/lib/pica/session";

interface LoginError {
  msg: string;
  /** 2002 网络失败:附网络检查引导 */
  network?: boolean;
}

export default function PicaLoginPage() {
  const { isLoggedIn } = usePicaSession();
  const [searchParams] = useSearchParams();
  const from = searchParams.get("from");

  const [username, setUsername] = useState(() => {
    try {
      return localStorage.getItem("pica_last_user") ?? "";
    } catch {
      return "";
    }
  });
  const [expiredHint] = useState(() => consumePicaExpiredHint());
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<LoginError | null>(null);

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;
    const name = username.trim();
    if (!name || !password) {
      setError({ msg: "请输入账号和密码" });
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const result = await picaLogin(name, password);
      setPicaSession(result.token, result.userInfo ?? {
        email: name, name, level: 0, exp: 0, gender: "", title: "",
        avatarUrl: null, isPunched: false, createdAt: "",
      });
      try {
        localStorage.setItem("pica_last_user", name);
      } catch {
        /* 隐私模式等场景下静默跳过 */
      }
      // isLoggedIn 渲染分支接管:跳转 from(站内路径)或 /pica
    } catch (err) {
      if (isPicaApiError(err)) {
        switch (err.code) {
          case 1001:
            setError({ msg: "账号或密码错误" });
            break;
          case 2002:
            setError({ msg: err.message || "无法连接在线源服务", network: true });
            break;
          default:
            setError({ msg: err.message || "登录失败,请稍后重试" });
        }
      } else {
        setError({ msg: "登录失败,请稍后重试" });
      }
    } finally {
      setSubmitting(false);
    }
  };

  // 已登录:直接跳转来源页(仅接受站内路径)或哔咔首页
  if (isLoggedIn) {
    const target = from && from.startsWith("/") && !from.startsWith("//") ? from : "/pica";
    return <Navigate to={target} replace />;
  }

  const inputCls =
    "h-11 w-full rounded-lg border border-border bg-card pl-10 pr-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted/60 focus:border-accent";

  return (
    <div className="flex min-h-screen flex-col bg-background text-foreground">
      <header className="flex h-14 shrink-0 items-center justify-between border-b border-border px-3 sm:px-4">
        <button
          type="button"
          onClick={() => window.history.back()}
          aria-label="返回"
          className="flex h-10 w-10 items-center justify-center rounded-lg text-muted transition-colors hover:bg-card hover:text-foreground"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <span className="text-sm font-medium text-muted">哔咔漫画</span>
        <Link
          to="/settings?tab=pica"
          aria-label="在线源设置"
          className="flex h-10 w-10 items-center justify-center rounded-lg text-muted transition-colors hover:bg-card hover:text-foreground"
        >
          <SettingsIcon className="h-5 w-5" />
        </Link>
      </header>
      <main className="flex flex-1 items-center justify-center px-4 py-10">
        <div className="w-full max-w-md space-y-4">
          {from && expiredHint && (
            <div
              className="flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-500"
              role="status"
            >
              <AlertTriangle className="h-4 w-4 shrink-0" />
              登录已失效,请重新登录
            </div>
          )}

          <div className="rounded-xl border border-border bg-card p-6 sm:p-8">
            <div className="mb-6 text-center">
              <span className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-accent/10 text-accent">
                <LogIn className="h-6 w-6" />
              </span>
              <h1 className="text-lg font-semibold text-foreground">登录哔咔漫画</h1>
              <p className="mt-1 text-xs text-muted">
                使用哔咔账号(邮箱/用户名)登录;token 保存于本机,失效后需重新登录。
              </p>
            </div>

            {error && (
              <div
                className="mb-4 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-500"
                role="alert"
              >
                <p>{error.msg}</p>
                {error.network && (
                  <p className="mt-1 text-xs text-red-500/80">
                    请检查网络连接,
                    <Link
                      to="/settings?tab=pica"
                      className="underline transition-opacity hover:opacity-80"
                    >
                      查看哔咔源设置
                    </Link>
                  </p>
                )}
              </div>
            )}

            <form onSubmit={handleSubmit} className="space-y-4" noValidate>
              <div>
                <label
                  htmlFor="pica-login-username"
                  className="mb-1.5 block text-sm font-medium text-foreground"
                >
                  账号
                </label>
                <div className="relative">
                  <User className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/60" />
                  <input
                    id="pica-login-username"
                    type="text"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    placeholder="哔咔邮箱或用户名"
                    autoComplete="username"
                    spellCheck={false}
                    className={inputCls}
                  />
                </div>
              </div>

              <div>
                <label
                  htmlFor="pica-login-password"
                  className="mb-1.5 block text-sm font-medium text-foreground"
                >
                  密码
                </label>
                <div className="relative">
                  <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/60" />
                  <input
                    id="pica-login-password"
                    type={showPassword ? "text" : "password"}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="哔咔账号密码"
                    autoComplete="current-password"
                    className={`${inputCls} pr-10`}
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword((v) => !v)}
                    aria-label={showPassword ? "隐藏密码" : "显示密码"}
                    className="absolute right-1 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-md text-muted transition-colors hover:bg-card-hover hover:text-foreground"
                  >
                    {showPassword ? (
                      <EyeOff className="h-4 w-4" />
                    ) : (
                      <Eye className="h-4 w-4" />
                    )}
                  </button>
                </div>
              </div>

              <button
                type="submit"
                disabled={submitting}
                className="inline-flex h-11 w-full items-center justify-center gap-2 rounded-lg bg-accent text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {submitting ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    登录中…
                  </>
                ) : (
                  <>
                    <LogIn className="h-4 w-4" />
                    登录
                  </>
                )}
              </button>
            </form>
          </div>
        </div>
      </main>
    </div>
  );
}
