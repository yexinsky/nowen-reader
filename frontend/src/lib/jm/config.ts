"use client";

/**
 * JM 在线源 URL 拼装(内置服务)
 *
 * - 在线源已内置进 Nowen 后端,同源挂载 /api/jm/*,不再有「服务地址(jm_base_url)」概念
 * - MOBILE_API.md 契约路径(/api/health、/api/image?...)经 jmApiUrl 映射到内置服务
 * - 服务端下发的封面/头像/阅读器图片/验证码地址均在此拼装
 */

import { apiPath } from "@/lib/base-path";

/** 内置服务同源前缀(gin 组 /api/jm) */
export const JM_API_PREFIX = "/api/jm";

/**
 * 契约路径 → 同源内置服务 URL。
 * 契约端点在服务端去掉了 /api 段挂载(如 /api/health → /api/jm/health,
 * /api/image?path=... → /api/jm/image?path=...);apiPath 负责叠加部署 Base Path。
 */
export function jmApiUrl(path: string): string {
  const rest = path.startsWith("/api/") ? path.slice(4) : path;
  const normalized = rest.startsWith("/") ? rest : `/${rest}`;
  return apiPath(JM_API_PREFIX + normalized);
}

/* ── URL 拼装 ── */

/**
 * 将服务端下发的相对地址(coverUrl / avatarUrl)拼为可访问的同源地址。
 * 口径:
 * - 完整 http(s) URL 原样返回(兼容性)
 * - 站内绝对路径(如 /api/image?path=...)→ 前缀换为 /api/jm/image?...
 * - 裸路径(如 media/albums/{aid}_3x4.jpg)→ 包装为 /api/jm/image?path=...&scramble=0
 * - 空值返回 ""
 */
export function resolveJmUrl(pathOrUrl: string | null | undefined): string {
  if (!pathOrUrl) return "";
  if (/^https?:\/\//i.test(pathOrUrl)) return pathOrUrl;
  if (pathOrUrl.startsWith("/")) return jmApiUrl(pathOrUrl);
  return jmApiUrl(`/api/image?path=${encodeURIComponent(pathOrUrl)}&scramble=0`);
}

/**
 * 阅读器图片 URL(#16 GET /api/image)。
 * path 为 /api/photos 返回的 images[].path(不接受前导斜杠,原样透传)。
 */
export function jmImageUrl(path: string, scramble: string, aid: string): string {
  const params = new URLSearchParams({
    path,
    scramble: scramble || "0",
    aid: aid || "",
  });
  return jmApiUrl(`/api/image?${params.toString()}`);
}

/** 验证码图片 URL(#2 GET /api/auth/captcha,bust 加时间戳防缓存) */
export function jmCaptchaUrl(bust?: number): string {
  const url = jmApiUrl("/api/auth/captcha");
  return bust ? `${url}?r=${bust}` : url;
}
