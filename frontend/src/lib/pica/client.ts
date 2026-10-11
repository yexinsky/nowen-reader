"use client";

/**
 * 哔咔在线源(PicACG)API 客户端(内置服务)
 * 同源 /api/pica/*,统一响应包装 {code,msg,data}:
 * - code=0 → 返回 data
 * - code=1002/HTTP 401 → 清会话并广播未授权,抛 PicaApiError
 * - 其余非 0 → 抛 PicaApiError(业务失败 HTTP 仍为 200)
 * - 网络层失败 → 抛 code=2002 语义的 PicaApiError
 * - 站点会话失效(HTTP 401 非哔咔包装体)→ 抛 code=1401、不清哔咔会话
 *
 * 哔咔上游对内容接口一律要求登录(实测):除 health/login 外所有端点
 * 均携带 X-Pica-Token。
 */

import { picaApiUrl } from "./config";
import { getPicaToken, _firePicaUnauthorized } from "./session";
import {
  PICA_ERROR_CODES,
  type PicaUserInfo,
  type PicaCategory,
  type PicaComicPage,
  type PicaCollectionSection,
  type PicaComicDetail,
  type PicaEpsPage,
  type PicaPagesResult,
  type PicaCommentPage,
  type PicaHistoryPage,
  type PicaHistoryItem,
  type PicaSettings,
  type PicaDownloadDir,
  type PicaDownloadTask,
} from "./types";

const DEFAULT_TIMEOUT = 30000;

export class PicaApiError extends Error {
  /** 哔咔契约错误码(见 types.PICA_ERROR_CODES);网络层失败恒为 2002 语义 */
  code: number;
  /** data 附加字段(upstream/reason 等) */
  data: unknown;
  httpStatus: number;

  constructor(code: number, message: string, options?: { data?: unknown; httpStatus?: number }) {
    super(message);
    this.name = "PicaApiError";
    this.code = code;
    this.data = options?.data;
    this.httpStatus = options?.httpStatus ?? 200;
  }
}

export function isPicaApiError(err: unknown): err is PicaApiError {
  return err instanceof PicaApiError;
}

/* ── 请求核心 ── */

interface PicaRequestOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  query?: Record<string, string | number | undefined | null>;
  auth?: boolean;
  timeout?: number;
  signal?: AbortSignal;
}

function buildQuery(query?: PicaRequestOptions["query"]): string {
  if (!query) return "";
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") continue;
    params.set(key, String(value));
  }
  const qs = params.toString();
  return qs ? `?${qs}` : "";
}

interface PicaEnvelope<T> {
  code: number;
  msg: string;
  data: T;
}

async function picaRequest<T>(path: string, options: PicaRequestOptions = {}): Promise<T> {
  const { method = "GET", body, query, auth = true, timeout = DEFAULT_TIMEOUT, signal } = options;
  const url = picaApiUrl(path) + buildQuery(query);

  const headers: Record<string, string> = {};
  if (auth) {
    const token = getPicaToken();
    // 专用头而非 Authorization:Bearer——后端 nowen 鉴权中间件会把 Authorization
    // 当作 API Key 校验且失败不回退 Cookie(与 JM 流同款约束)
    if (token) headers["X-Pica-Token"] = token;
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
      throw new PicaApiError(PICA_ERROR_CODES.NETWORK, `请求超时(${timeout}ms)`, { httpStatus: 0 });
    }
    throw new PicaApiError(PICA_ERROR_CODES.NETWORK, "无法连接内置在线源服务,请检查网络后重试", {
      httpStatus: 0,
    });
  }
  clearTimeout(timeoutId);

  // 401:区分哔咔会话失效(code=1002 包装体)与站点会话失效(纯 {"error":...})。
  // 仅哔咔失效才清会话+广播跳 /pica/login;站点失效交由站点登录流程。
  if (res.status === 401) {
    let body: { code?: unknown } | null = null;
    try {
      body = (await res.json()) as { code?: unknown };
    } catch {
      // 保留 null → 走下方站点失效分支
    }
    if (body?.code === PICA_ERROR_CODES.UNAUTHORIZED) {
      _firePicaUnauthorized();
      throw new PicaApiError(PICA_ERROR_CODES.UNAUTHORIZED, "登录已失效,请重新登录", { httpStatus: 401 });
    }
    throw new PicaApiError(1401, "站点登录已失效,请刷新页面重新登录站点", { httpStatus: 401 });
  }

  if (res.status === 422) {
    throw new PicaApiError(422, "请求参数不合法", { httpStatus: 422 });
  }

  let envelope: PicaEnvelope<T>;
  try {
    envelope = (await res.json()) as PicaEnvelope<T>;
  } catch {
    throw new PicaApiError(4000, "在线源服务响应格式异常", { httpStatus: res.status });
  }

  if (envelope.code !== PICA_ERROR_CODES.OK) {
    if (envelope.code === PICA_ERROR_CODES.UNAUTHORIZED) {
      _firePicaUnauthorized();
    }
    throw new PicaApiError(envelope.code, envelope.msg || "请求失败", {
      data: (envelope as { data?: unknown }).data,
      httpStatus: res.status,
    });
  }

  return envelope.data;
}

/* ── health / auth ── */

export function picaHealth(opts?: { signal?: AbortSignal }): Promise<{ status: string; version: string }> {
  return picaRequest("/api/health", { auth: false, timeout: 8000, signal: opts?.signal });
}

/** 登录(哔咔邮箱/用户名 + 密码;无验证码) */
export function picaLogin(email: string, password: string): Promise<{ token: string; userInfo: PicaUserInfo | null }> {
  return picaRequest<{ token: string; userInfo: PicaUserInfo | null }>("/api/auth/login", {
    method: "POST",
    body: { email, password },
    auth: false,
  });
}

/** 实时用户资料(上游 users/profile) */
export function picaProfile(): Promise<PicaUserInfo> {
  return picaRequest<PicaUserInfo>("/api/auth/profile");
}

/** 登出(哔咔无服务端会话,前端丢弃 token) */
export function picaLogout(): Promise<{ ok: boolean }> {
  return picaRequest<{ ok: boolean }>("/api/auth/logout", { method: "POST" });
}

/* ── 内容端点(哔咔要求登录,全部携带 token) ── */

export function picaCategories(): Promise<{ list: PicaCategory[]; total: number }> {
  return picaRequest("/api/categories");
}

/** 分类浏览(s ∈ dd 新→旧 / da 旧→新 / ld 爱心 / vd 观看) */
export function picaComics(category: string, sort: "dd" | "da" | "ld" | "vd", page: number): Promise<PicaComicPage> {
  return picaRequest("/api/comics", { query: { c: category, s: sort, page } });
}

/** 高级搜索(categories 逗号分隔分类名,可空) */
export function picaSearch(
  keyword: string,
  page: number,
  opts?: { categories?: string[]; sort?: "dd" | "da" | "ld" | "vd" }
): Promise<PicaComicPage> {
  return picaRequest("/api/search", {
    query: {
      keyword,
      page,
      sort: opts?.sort || "",
      categories: opts?.categories?.join(",") || "",
    },
  });
}

export function picaKeywords(): Promise<{ list: string[] }> {
  return picaRequest("/api/keywords");
}

/** 随机本子 */
export function picaRandom(): Promise<{ list: import("./types").PicaComicItem[] }> {
  return picaRequest("/api/random");
}

/** 排行榜(tt ∈ H24 日榜 / D7 周榜 / D30 月榜) */
export function picaLeaderboard(tt: "H24" | "D7" | "D30"): Promise<{ list: import("./types").PicaComicItem[] }> {
  return picaRequest("/api/leaderboard", { query: { tt } });
}

/** 神魔推荐 */
export function picaCollections(): Promise<{ list: PicaCollectionSection[] }> {
  return picaRequest("/api/collections");
}

export function picaComicDetail(id: string): Promise<PicaComicDetail> {
  return picaRequest(`/api/comics/${encodeURIComponent(id)}`);
}

export function picaEps(id: string, page = 1): Promise<PicaEpsPage> {
  return picaRequest(`/api/comics/${encodeURIComponent(id)}/eps`, { query: { page } });
}

/** 章节图片列表(order 为章节序号,每页约 40 张) */
export function picaEpsPages(id: string, order: number, page = 1): Promise<PicaPagesResult> {
  return picaRequest(`/api/comics/${encodeURIComponent(id)}/eps/${order}/pages`, { query: { page } });
}

export function picaComments(id: string, page = 1): Promise<PicaCommentPage> {
  return picaRequest(`/api/comics/${encodeURIComponent(id)}/comments`, { query: { page } });
}

/** 相关推荐 */
export function picaRecommendation(id: string): Promise<{ list: import("./types").PicaComicItem[] }> {
  return picaRequest(`/api/comics/${encodeURIComponent(id)}/recommendation`);
}

/* ── 账号互动 ── */

/** 收藏列表(s ∈ dd 新→旧 / da 旧→新) */
export function picaFavorites(sort: "dd" | "da" = "da", page = 1): Promise<PicaComicPage> {
  return picaRequest("/api/favourites", { query: { s: sort, page } });
}

/** 收藏/取消收藏(切换式;返回 action=favourite|un_favourite) */
export function picaToggleFavorite(id: string): Promise<{ action: string }> {
  return picaRequest(`/api/comics/${encodeURIComponent(id)}/favourite`, { method: "POST" });
}

/** 爱心点赞(切换式;返回 action=like|unlike) */
export function picaToggleLike(id: string): Promise<{ action: string }> {
  return picaRequest(`/api/comics/${encodeURIComponent(id)}/like`, { method: "POST" });
}

/** 每日打卡 */
export function picaPunchIn(): Promise<{ ok: boolean }> {
  return picaRequest("/api/punch-in", { method: "POST" });
}

/* ── 设置(仅需站点登录) ── */

export function picaGetSettings(): Promise<PicaSettings> {
  return picaRequest("/api/settings", { auth: false });
}

export function picaSaveSettings(patch: Partial<PicaSettings>): Promise<PicaSettings> {
  return picaRequest("/api/settings", { method: "PUT", body: patch, auth: false });
}

/* ── 阅读历史 ── */

export function picaHistoryList(page = 1): Promise<PicaHistoryPage> {
  return picaRequest("/api/history", { query: { page } });
}

export function picaHistoryReport(item: {
  id: string;
  title: string;
  coverUrl: string;
  order: number;
  epTitle?: string | null;
  imageIndex?: number;
}): Promise<{ ok: boolean }> {
  return picaRequest("/api/history", { method: "POST", body: item });
}

export function picaHistoryClear(): Promise<{ ok: boolean }> {
  return picaRequest("/api/history", { method: "DELETE" });
}

/* ── 批量下载(私有扩展) ── */

export function picaDownloadDirs(): Promise<{ dirs: PicaDownloadDir[]; testDir: string }> {
  return picaRequest("/api/downloads/dirs");
}

export function picaDownloadList(): Promise<{ list: PicaDownloadTask[]; tempRoot: string }> {
  return picaRequest("/api/downloads");
}

export function picaDownloadStart(params: {
  bookId: string;
  title?: string;
  author?: string;
  orders?: number[];
  destDir: string;
}): Promise<PicaDownloadTask> {
  return picaRequest("/api/downloads", { method: "POST", body: params });
}

export function picaDownloadCancel(id: string): Promise<{ ok: boolean }> {
  return picaRequest(`/api/downloads/${encodeURIComponent(id)}/cancel`, { method: "POST" });
}

export function picaDownloadRemove(id: string): Promise<{ ok: boolean }> {
  return picaRequest(`/api/downloads/${encodeURIComponent(id)}`, { method: "DELETE" });
}
