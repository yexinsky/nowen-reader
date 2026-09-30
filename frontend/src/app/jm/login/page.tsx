"use client";

/**
 * JM 在线源 · 登录页(PRD docs/PRD_JM_SOURCE.md M2)
 * 接口:MOBILE_API.md #2 captcha、#3 login
 *
 * 在线源已内置为同源服务,不存在「服务地址未配置」态,直接渲染登录表单。
 * 流程:提交 → jmLogin;1003 显示验证码行(点击图片刷新);1001 凭据错误;
 * 2002 提示检查网络;成功 → setJmSession → 回跳 ?from= 指定路径(缺省 /jm)。
 * 已登录进入本页直接跳转 /jm。
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
  ShieldCheck,
  User,
} from "lucide-react";
import { jmCaptchaUrl } from "@/lib/jm/config";
import { isJmApiError, jmLogin } from "@/lib/jm/client";
import { setJmSession, useJmSession, consumeJmExpiredHint } from "@/lib/jm/session";

interface LoginError {
  msg: string;
  /** 2002 网络失败:附网络检查引导 */
  network?: boolean;
}

export default function JmLoginPage() {
  const { isLoggedIn } = useJmSession();
  const [searchParams] = useSearchParams();
  const from = searchParams.get("from");

  const [username, setUsername] = useState(() => {
    // 记住上次登录用户名(对齐 JM 官方前端 jm_last_user)
    try {
      return localStorage.getItem("jm_last_user") ?? "";
    } catch {
      return "";
    }
  });
  // 「登录已失效」提示仅在真正发生过 401 失效时显示一次
  // (首次访问/主动退出不显示;读取即消费,避免残留)
  const [expiredHint] = useState(() => consumeJmExpiredHint());
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [captcha, setCaptcha] = useState("");
  const [captchaRequired, setCaptchaRequired] = useState(false);
  const [captchaBust, setCaptchaBust] = useState(() => Date.now());
  const [captchaLoadError, setCaptchaLoadError] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<LoginError | null>(null);

  const refreshCaptcha = () => {
    setCaptcha("");
    setCaptchaBust(Date.now());
    setCaptchaLoadError(false);
  };

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;
    const name = username.trim();
    if (!name || !password) {
      setError({ msg: "请输入用户名和密码" });
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const result = await jmLogin(
        name,
        password,
        captchaRequired ? captcha.trim() || undefined : undefined
      );
      setJmSession(result.token, result.userInfo);
      try {
        localStorage.setItem("jm_last_user", name);
      } catch {
        /* 隐私模式等场景下静默跳过 */
      }
      // isLoggedIn 渲染分支接管:跳转 from(站内路径)或 /jm
    } catch (err) {
      if (isJmApiError(err)) {
        switch (err.code) {
          case 1001:
            setError({ msg: "用户名或密码错误" });
            break;
          case 1003:
            setCaptchaRequired(true);
            setError({ msg: err.message || "需要验证码,请输入后重试" });
            break;
          case 2002:
            setError({ msg: err.message || "无法连接在线源服务", network: true });
            break;
          default:
            setError({ msg: err.message || "登录失败,请稍后重试" });
        }
        // 验证码已展示(或本次要求验证码)时自动换一张,避免旧验证码已失效
        if (captchaRequired || err.code === 1003) refreshCaptcha();
      } else {
        setError({ msg: "登录失败,请稍后重试" });
      }
    } finally {
      setSubmitting(false);
    }
  };

  // 已登录:直接跳转来源页(仅接受站内路径)或在线首页
  if (isLoggedIn) {
    const target = from && from.startsWith("/") && !from.startsWith("//") ? from : "/jm";
    return <Navigate to={target} replace />;
  }

  const pageHeader = (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-border px-3 sm:px-4">
      <button
        type="button"
        onClick={() => window.history.back()}
        aria-label="返回"
        className="flex h-10 w-10 items-center justify-center rounded-lg text-muted transition-colors hover:bg-card hover:text-foreground"
      >
        <ArrowLeft className="h-5 w-5" />
      </button>
      <span className="text-sm font-medium text-muted">在线漫画源</span>
      <Link
        to="/settings?tab=jm-source"
        aria-label="在线源设置"
        className="flex h-10 w-10 items-center justify-center rounded-lg text-muted transition-colors hover:bg-card hover:text-foreground"
      >
        <SettingsIcon className="h-5 w-5" />
      </Link>
    </header>
  );

  const inputCls =
    "h-11 w-full rounded-lg border border-border bg-card pl-10 pr-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted/60 focus:border-accent";

  return (
    <div className="flex min-h-screen flex-col bg-background text-foreground">
      {pageHeader}
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
              <h1 className="text-lg font-semibold text-foreground">登录在线漫画源</h1>
              <p className="mt-1 text-xs text-muted">
                使用 JM 账号登录,会话有效期 7 天;JM 服务重启后需重新登录。
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
                      to="/settings?tab=jm-source"
                      className="underline transition-opacity hover:opacity-80"
                    >
                      查看在线源服务状态
                    </Link>
                  </p>
                )}
              </div>
            )}

            <form onSubmit={handleSubmit} className="space-y-4" noValidate>
              <div>
                <label
                  htmlFor="jm-login-username"
                  className="mb-1.5 block text-sm font-medium text-foreground"
                >
                  用户名
                </label>
                <div className="relative">
                  <User className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/60" />
                  <input
                    id="jm-login-username"
                    type="text"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    placeholder="JM 账号用户名"
                    autoComplete="username"
                    spellCheck={false}
                    className={inputCls}
                  />
                </div>
              </div>

              <div>
                <label
                  htmlFor="jm-login-password"
                  className="mb-1.5 block text-sm font-medium text-foreground"
                >
                  密码
                </label>
                <div className="relative">
                  <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/60" />
                  <input
                    id="jm-login-password"
                    type={showPassword ? "text" : "password"}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="JM 账号密码"
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

              {captchaRequired && (
                <div>
                  <label
                    htmlFor="jm-login-captcha"
                    className="mb-1.5 block text-sm font-medium text-foreground"
                  >
                    验证码
                  </label>
                  <div className="flex items-center gap-2">
                    <div className="relative min-w-0 flex-1">
                      <ShieldCheck className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/60" />
                      <input
                        id="jm-login-captcha"
                        type="text"
                        value={captcha}
                        onChange={(e) => setCaptcha(e.target.value)}
                        placeholder="请输入验证码"
                        autoComplete="off"
                        maxLength={16}
                        spellCheck={false}
                        className={inputCls}
                      />
                    </div>
                    <button
                      type="button"
                      onClick={refreshCaptcha}
                      title="看不清?点击刷新验证码"
                      className="shrink-0 overflow-hidden rounded-lg border border-border transition-opacity hover:opacity-80"
                    >
                      <img
                        src={jmCaptchaUrl(captchaBust)}
                        alt="验证码,点击刷新"
                        className="h-11 w-28 bg-card object-cover"
                        onError={() => setCaptchaLoadError(true)}
                        onLoad={() => setCaptchaLoadError(false)}
                      />
                    </button>
                  </div>
                  {captchaLoadError && (
                    <p className="mt-1.5 text-xs text-muted">
                      验证码加载失败,请检查服务连接后点击图片重试。
                    </p>
                  )}
                </div>
              )}

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
