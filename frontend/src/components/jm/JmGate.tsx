"use client";

/**
 * JM 在线源页面守卫
 * - JmGate = 登录守卫:未登录 → 重定向 /jm/login(记录来源页,登录后回跳)
 *
 * 在线源已内置为同源服务,「服务地址未配置」态不复存在,页面只保留登录守卫。
 * 全局 401:client 收到 401/1002 已清会话;已挂载页面会在下一渲染
 * 由 useJmSession 感知并触发此处重定向。
 */

import { useEffect } from "react";
import { Navigate, useLocation } from "react-router-dom";
import Link from "next/link";
import { AlertTriangle } from "lucide-react";
import { useJmSession } from "@/lib/jm/session";
import { isJmApiError, jmProfile } from "@/lib/jm/client";
import { JM_ERROR_CODES } from "@/lib/jm/types";

/**
 * 主动校验本地会话:进入 JM 区块时用 /auth/profile(纯本地快照,零外呼)验证
 * token 是否仍有效。过期 → client 统一清会话+失效提示 → 守卫跳登录,
 * 不再等到收藏等账号操作 401 才暴露。模块级 60s 节流(多页面共用一次);
 * 网络失败不清理会话(避免瞬断误登出)。
 */
let lastValidatedAt = 0;
let validating = false;
const VALIDATE_INTERVAL = 60_000;

function useJmSessionValidation(isLoggedIn: boolean) {
  useEffect(() => {
    if (!isLoggedIn || validating) return;
    if (Date.now() - lastValidatedAt < VALIDATE_INTERVAL) return;
    validating = true;
    jmProfile()
      .catch((err) => {
        // JM 失效(1002)已由 client 统一处理(清会话+广播+失效提示);其余失败保留登录态并记日志
        if (!(isJmApiError(err) && err.code === JM_ERROR_CODES.UNAUTHORIZED)) {
          console.warn("[jm] 会话校验失败,保留本地登录态", err);
        }
      })
      .finally(() => {
        validating = false;
        lastValidatedAt = Date.now();
      });
  }, [isLoggedIn]);
}

function LoginPrompt() {
  const location = useLocation();
  const from = encodeURIComponent(location.pathname + location.search);
  return <Navigate to={`/jm/login?from=${from}`} replace />;
}

/** 未登录时重定向登录页,已登录渲染 children */
export function JmAuthGuard({ children }: { children: React.ReactNode }) {
  const { isLoggedIn } = useJmSession();
  useJmSessionValidation(isLoggedIn);
  if (!isLoggedIn) return <LoginPrompt />;
  return <>{children}</>;
}

/** 在线源页面统一守卫(登录) */
export function JmGate({ children }: { children: React.ReactNode }) {
  return <JmAuthGuard>{children}</JmAuthGuard>;
}

/** JM 服务不可达 / 业务错误的通用错误卡片(列表页复用) */
export function JmErrorCard({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry?: () => void;
}) {
  const message =
    error instanceof Error ? error.message : "加载失败,请稍后重试";
  const isNetwork =
    error instanceof Error && "code" in error && (error as { code: number }).code === 2002;
  return (
    <div className="flex min-h-[40vh] items-center justify-center px-4">
      <div className="w-full max-w-md rounded-xl border border-border bg-card p-8 text-center">
        <span className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-amber-500/10 text-amber-500">
          <AlertTriangle className="h-6 w-6" />
        </span>
        <h2 className="text-lg font-semibold text-foreground">
          {isNetwork ? "在线源服务不可达" : "加载失败"}
        </h2>
        <p className="mt-2 break-all text-sm text-muted">{message}</p>
        {isNetwork && (
          <p className="mt-2 text-xs text-muted">
            Live 模式下需在设置的「在线漫画源」中配置上游代理后方可访问内容。
          </p>
        )}
        <div className="mt-6 flex items-center justify-center gap-3">
          {onRetry && (
            <button
              onClick={onRetry}
              className="inline-flex items-center justify-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
            >
              重试
            </button>
          )}
          <Link
            href="/settings?tab=jm-source"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-card-hover"
          >
            检查设置
          </Link>
        </div>
      </div>
    </div>
  );
}
