"use client";

/**
 * JM 在线源 API 客户端(内置服务)
 * 服务已内置在后端,同源 /api/jm/*,无「服务地址」概念。对接 MOBILE_API.md
 * 全部 30 端点。统一响应包装 {code,msg,data}:
 * - code=0 → 返回 data
 * - code=1002/HTTP 401 → 清会话并广播未授权,抛 JmApiError
 * - 其余非 0 → 抛 JmApiError(业务失败 HTTP 仍为 200)
 * - 网络层失败(服务不可达/超时)→ 抛 code=2002 语义的 JmApiError
 */

import { jmApiUrl } from "./config";
import { getJmToken, _fireJmUnauthorized } from "./session";
import {
  JM_ERROR_CODES,
  type JmHealth,
  type JmLoginResult,
  type JmUserInfo,
  type JmCategory,
  type JmComicPage,
  type JmPromoteSection,
  type JmWeekCategory,
  type JmComicItem,
  type JmSearchParams,
  type JmSerializationDay,
  type JmSerializationType,
  type JmComicDetail,
  type JmPhotos,
  type JmCommentPage,
  type JmToggleResult,
  type JmFavoriteFolder,
  type JmHistoryPage,
  type JmHistoryReport,
  type JmSettings,
  type JmSignStatus,
} from "./types";

const DEFAULT_TIMEOUT = 30000;

export class JmApiError extends Error {
  /** JM 错误码(见 types.JM_ERROR_CODES);网络层失败恒为 2002 语义 */
  code: number;
  /** data 附加字段(upstream/captchaRequired/reason 等) */
  data: unknown;
  httpStatus: number;

  constructor(code: number, message: string, options?: { data?: unknown; httpStatus?: number }) {
    super(message);
    this.name = "JmApiError";
    this.code = code;
    this.data = options?.data;
    this.httpStatus = options?.httpStatus ?? 200;
  }
}

export function isJmApiError(err: unknown): err is JmApiError {
  return err instanceof JmApiError;
}

/* ── 请求核心 ── */

interface JmRequestOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  query?: Record<string, string | number | undefined | null>;
  auth?: boolean;
  timeout?: number;
  signal?: AbortSignal;
}

function buildQuery(query?: JmRequestOptions["query"]): string {
  if (!query) return "";
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") continue;
    params.set(key, String(value));
  }
  const qs = params.toString();
  return qs ? `?${qs}` : "";
}

interface JmEnvelope<T> {
  code: number;
  msg: string;
  data: T;
}

async function jmRequest<T>(path: string, options: JmRequestOptions = {}): Promise<T> {
  const { method = "GET", body, query, auth = true, timeout = DEFAULT_TIMEOUT, signal } = options;
  // 契约路径(/api/xxx)→ 同源内置服务 URL(/api/jm/xxx,兼容部署 Base Path)
  const url = jmApiUrl(path) + buildQuery(query);

  const headers: Record<string, string> = {};
  if (auth) {
    const token = getJmToken();
    // 专用头而非 Authorization:Bearer——后端 nowen 鉴权中间件会把 Authorization
    // 当作 API Key 校验且失败不回退 Cookie,会导致登录后所有请求被 401 拦截
    if (token) headers["X-JM-Token"] = token;
  }
  if (body !== undefined) headers["Content-Type"] = "application/json";

  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), timeout);
  const combined = signal ? (AbortSignal.any?.([signal, controller.signal]) ?? controller.signal) : controller.signal;

  let res: Response;
  try {
    res = await fetch(url, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal: combined,
    });
  } catch (err) {
    clearTimeout(timeoutId);
    if (err instanceof DOMException && err.name === "AbortError" && !signal?.aborted) {
      throw new JmApiError(JM_ERROR_CODES.NETWORK, `请求超时(${timeout}ms)`, { httpStatus: 0 });
    }
    throw new JmApiError(JM_ERROR_CODES.NETWORK, "无法连接内置在线源服务,请检查网络后重试", {
      httpStatus: 0,
    });
  }
  clearTimeout(timeoutId);

  // 401:统一会话失效处理
  if (res.status === 401) {
    _fireJmUnauthorized();
    throw new JmApiError(JM_ERROR_CODES.UNAUTHORIZED, "登录已失效,请重新登录", { httpStatus: 401 });
  }

  // 422:参数校验失败(FastAPI 默认结构,非包装体)
  if (res.status === 422) {
    throw new JmApiError(422, "请求参数不合法", { httpStatus: 422 });
  }

  let envelope: JmEnvelope<T>;
  try {
    envelope = (await res.json()) as JmEnvelope<T>;
  } catch {
    throw new JmApiError(JM_ERROR_CODES.INTERNAL, "在线源服务响应格式异常", { httpStatus: res.status });
  }

  if (envelope.code !== JM_ERROR_CODES.OK) {
    if (envelope.code === JM_ERROR_CODES.UNAUTHORIZED) {
      _fireJmUnauthorized();
    }
    throw new JmApiError(envelope.code, envelope.msg || "请求失败", {
      data: (envelope as { data?: unknown }).data,
      httpStatus: res.status,
    });
  }

  return envelope.data;
}

/* ── 图片/验证码 URL(非 JSON 端点) ── */

export { resolveJmUrl, jmImageUrl, jmCaptchaUrl } from "./config";

/* ── #1 system ── */

/** 健康检查/连通性探测(无需 JM Bearer;nowen 登录由同源 cookie 保证) */
export function jmHealth(opts?: { signal?: AbortSignal }): Promise<JmHealth> {
  return jmRequest<JmHealth>("/api/health", { auth: false, timeout: 8000, signal: opts?.signal });
}

/* ── #2-#5 auth ── */

/** 登录;需验证码时抛 code=1003(data.captchaRequired) */
export function jmLogin(username: string, password: string, captcha?: string): Promise<JmLoginResult> {
  const body: Record<string, string> = { username, password };
  if (captcha) body.captcha = captcha;
  return jmRequest<JmLoginResult>("/api/auth/login", { method: "POST", body, auth: false });
}

/** 当前用户信息(登录快照,不回源) */
export function jmProfile(): Promise<JmUserInfo> {
  return jmRequest<JmUserInfo>("/api/auth/profile");
}

/** 销毁服务端会话 */
export function jmLogout(): Promise<{ ok: boolean }> {
  return jmRequest<{ ok: boolean }>("/api/auth/logout", { method: "POST" });
}

/* ── #6-#13 comic 浏览 ── */

export function jmCategories(): Promise<JmCategory[]> {
  return jmRequest<JmCategory[]>("/api/categories", { auth: false });
}

/** 首页/全部漫画列表(order=Latest|View) */
export function jmComicsIndex(page: number, order: "Latest" | "View" = "Latest"): Promise<JmComicPage> {
  return jmRequest<JmComicPage>("/api/comics/index", { auth: false, query: { page, order } });
}

/** 首页推荐区块 */
export function jmPromote(): Promise<{ sections: JmPromoteSection[] }> {
  return jmRequest<{ sections: JmPromoteSection[] }>("/api/comics/promote", { auth: false });
}

/** 最近更新 */
export function jmLatest(page: number): Promise<JmComicPage> {
  return jmRequest<JmComicPage>("/api/comics/latest", { auth: false, query: { page } });
}

/** 每周连载(day 1=周一…7=周日) */
export function jmSerialization(
  day: JmSerializationDay,
  type: JmSerializationType,
  page: number
): Promise<JmComicPage & { day: number; type: string }> {
  return jmRequest("/api/comics/serialization", {
    auth: false,
    query: { day, type, page },
  });
}

/** 每周必看期数列表 */
export function jmWeek(): Promise<{ categories: JmWeekCategory[] }> {
  return jmRequest<{ categories: JmWeekCategory[] }>("/api/comics/week", { auth: false });
}

/** 每周必看单期内容(无翻页) */
export function jmWeekFilter(id: string, type: "manga" | "hanman" | "another" = "manga"): Promise<{ list: JmComicItem[] }> {
  return jmRequest<{ list: JmComicItem[] }>("/api/comics/week/filter", {
    auth: false,
    query: { id, type },
  });
}

/** 搜索/分类/排行(keyword 与 mainCategory 二选一路径,路由规则由服务端处理) */
export function jmSearch(params: JmSearchParams): Promise<JmComicPage> {
  return jmRequest<JmComicPage>("/api/comics/search", { auth: false, query: { ...params } });
}

/* ── #14-#16 详情/章节/图片 ── */

export function jmComicDetail(aid: string): Promise<JmComicDetail> {
  return jmRequest<JmComicDetail>(`/api/comics/${encodeURIComponent(aid)}`, { auth: false });
}

export function jmPhotos(pid: string): Promise<JmPhotos> {
  return jmRequest<JmPhotos>(`/api/photos/${encodeURIComponent(pid)}`, { auth: false });
}

/* ── #17-#19 评论/点赞 ── */

export function jmComments(aid: string, page: number): Promise<JmCommentPage> {
  return jmRequest<JmCommentPage>(`/api/comics/${encodeURIComponent(aid)}/comments`, {
    query: { page },
  });
}

export function jmAddComment(aid: string, content: string): Promise<{ ok: boolean }> {
  return jmRequest(`/api/comics/${encodeURIComponent(aid)}/comments`, {
    method: "POST",
    body: { content },
  });
}

/** 点赞(切换语义,以响应 liked 为准;null 表示无法判定,宜回查详情) */
export function jmLike(aid: string): Promise<JmToggleResult> {
  return jmRequest<JmToggleResult>(`/api/comics/${encodeURIComponent(aid)}/like`, { method: "POST" });
}

/* ── #20-#23 收藏 ── */

export function jmFavoriteFolders(): Promise<{ folders: JmFavoriteFolder[]; total: number }> {
  return jmRequest("/api/favorites/folders");
}

export function jmFavorites(folderId: string = "0", page: number = 1): Promise<JmComicPage> {
  return jmRequest<JmComicPage>("/api/favorites", { query: { folderId, page } });
}

/** 添加收藏(live 为切换语义,结果以 favorited 为准) */
export function jmAddFavorite(aid: string, folderId?: string): Promise<JmToggleResult> {
  return jmRequest<JmToggleResult>("/api/favorites", { method: "POST", body: { aid, folderId } });
}

/** 取消收藏(与 #22 同一上游端点,切换语义) */
export function jmRemoveFavorite(aid: string): Promise<JmToggleResult> {
  return jmRequest<JmToggleResult>(`/api/favorites/${encodeURIComponent(aid)}`, { method: "DELETE" });
}

/* ── #24-#26 阅读历史 ── */

export function jmHistoryList(page: number = 1): Promise<JmHistoryPage> {
  return jmRequest<JmHistoryPage>("/api/history", { query: { page } });
}

/** 上报阅读进度(幂等键 aid+pid;阅读中防抖调用) */
export function jmReportHistory(report: JmHistoryReport): Promise<{ ok: boolean }> {
  return jmRequest("/api/history", { method: "POST", body: report });
}

/** 清空全部历史 */
export function jmClearHistory(): Promise<{ ok: boolean }> {
  return jmRequest<{ ok: boolean }>("/api/history", { method: "DELETE" });
}

/* ── #27-#28 设置 ── */

export function jmGetSettings(): Promise<JmSettings> {
  return jmRequest<JmSettings>("/api/settings");
}

/** 写服务端设置(proxy 空串 = 清除直连;变更即时生效并保持登录态) */
export function jmPutSettings(patch: { proxy?: string; imageQuality?: JmSettings["imageQuality"] }): Promise<JmSettings> {
  return jmRequest<JmSettings>("/api/settings", { method: "PUT", body: patch });
}

/* ── #29-#30 签到 ── */

export function jmSignStatus(): Promise<JmSignStatus> {
  return jmRequest<JmSignStatus>("/api/user/sign");
}

/** 执行签到(服务端先查后签,幂等);msg 为"签到成功"/"今日已签到"等 */
export function jmSign(): Promise<{ ok: boolean; msg: string }> {
  return jmRequest<{ ok: boolean; msg: string }>("/api/user/sign", { method: "POST" });
}
