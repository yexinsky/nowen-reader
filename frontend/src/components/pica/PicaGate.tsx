"use client";

/**
 * 哔咔在线源页面守卫与错误卡(镜像 JmGate)
 * - PicaGate = 登录守卫:未登录 → 重定向 /pica/login(记录来源页,登录后回跳)
 *   哔咔内容接口一律要求登录,守卫覆盖全部内容页
 * - PicaErrorCard:网络/业务错误通用错误卡片(2002 提示配代理)
 */

import { useEffect } from "react";
import { Navigate, useLocation } from "react-router-dom";
import Link from "next/link";
import { AlertTriangle } from "lucide-react";
import { usePicaSession } from "@/lib/pica/session";
import { isPicaApiError, picaProfile } from "@/lib/pica/client";
import { PICA_ERROR_CODES } from "@/lib/pica/types";

/**
 * 主动校验本地会话:进入哔咔区块时用 /auth/profile(实时回源)验证
 * token 是否仍有效。失效 → client 统一清会话+失效提示 → 守卫跳登录。
 * 模块级 60s 节流;网络失败不清理会话(避免瞬断误登出)。
 */
let lastValidatedAt = 0;
let validating = false;
const VALIDATE_INTERVAL = 60_000;

function usePicaSessionValidation(isLoggedIn: boolean) {
  useEffect(() => {
    if (!isLoggedIn || validating) return;
    if (Date.now() - lastValidatedAt < VALIDATE_INTERVAL) return;
    validating = true;
    picaProfile()
      .catch((err) => {
        if (!(isPicaApiError(err) && err.code === PICA_ERROR_CODES.UNAUTHORIZED)) {
          console.warn("[pica] 会话校验失败,保留本地登录态", err);
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
  return <Navigate to={`/pica/login?from=${from}`} replace />;
}

/** 未登录时重定向登录页,已登录渲染 children */
export function PicaAuthGuard({ children }: { children: React.ReactNode }) {
  const { isLoggedIn } = usePicaSession();
  usePicaSessionValidation(isLoggedIn);
  if (!isLoggedIn) return <LoginPrompt />;
  return <>{children}</>;
}

/** 在线源页面统一守卫(登录) */
export function PicaGate({ children }: { children: React.ReactNode }) {
  return <PicaAuthGuard>{children}</PicaAuthGuard>;
}

/** 哔咔服务不可达 / 业务错误的通用错误卡片(列表页复用) */
export function PicaErrorCard({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry?: () => void;
}) {
  const message =
    error instanceof Error ? error.message : "加载失败,请稍后重试";
  const isNetwork = isPicaApiError(error) && error.code === 2002;
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
            上游访问失败时需在设置的「哔咔漫画」中配置代理后重试。
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
            href="/settings?tab=pica"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-card-hover"
          >
            检查设置
          </Link>
        </div>
      </div>
    </div>
  );
}
