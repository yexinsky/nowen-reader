"use client";

/**
 * 哔咔在线源(PicACG)URL 拼装(内置服务)
 *
 * - 在线源已内置进 Nowen 后端,同源挂载 /api/pica/*
 * - 服务端下发的封面/头像地址(/api/pica/image?fs=...&path=...)经 resolvePicaUrl
 *   叠加部署 Base Path;阅读器图片 URL 由后端直接下发完整代理地址
 */

import { apiPath } from "@/lib/base-path";

/** 内置服务同源前缀(gin 组 /api/pica) */
export const PICA_API_PREFIX = "/api/pica";

/**
 * 契约路径 → 同源内置服务 URL;apiPath 负责叠加部署 Base Path。
 */
export function picaApiUrl(path: string): string {
  const rest = path.startsWith("/api/") ? path.slice(4) : path;
  const normalized = rest.startsWith("/") ? rest : `/${rest}`;
  return apiPath(PICA_API_PREFIX + normalized);
}

/**
 * 将服务端下发的相对地址(coverUrl / avatarUrl)拼为可访问的同源地址。
 * 口径:
 * - 完整 http(s) URL 原样返回(兼容性)
 * - 站内绝对路径(如 /api/pica/image?fs=...)→ 叠加部署 Base Path
 * - 空值返回 ""
 */
export function resolvePicaUrl(pathOrUrl: string | null | undefined): string {
  if (!pathOrUrl) return "";
  if (/^https?:\/\//i.test(pathOrUrl)) return pathOrUrl;
  if (pathOrUrl.startsWith("/")) return apiPath(pathOrUrl);
  return "";
}
