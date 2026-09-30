"use client";

/**
 * JM 在线源会话管理
 * - token / 用户信息存 localStorage(设备级)
 * - 模块级状态 + 订阅,useSyncExternalStore 供组件消费
 * - 401/code=1002 时由 client 调用 clearSession + 广播未授权事件
 *
 * 注意:JM 服务会话为进程内存表(TTL 7 天,服务重启即失效),
 * 登录失效属常态,前端统一走 onUnauthorized 处理。
 */

import { useSyncExternalStore, useCallback } from "react";
import type { JmUserInfo } from "./types";

const TOKEN_KEY = "jm_token";
const USER_KEY = "jm_user_info";

interface SessionState {
  token: string;
  userInfo: JmUserInfo | null;
}

/* ── 模块级状态 ── */

let state: SessionState = { token: "", userInfo: null };
let initialized = false;
const listeners = new Set<() => void>();
const unauthorizedListeners = new Set<() => void>();

function readStorage(): SessionState {
  if (typeof window === "undefined") return { token: "", userInfo: null };
  const token = localStorage.getItem(TOKEN_KEY) ?? "";
  let userInfo: JmUserInfo | null = null;
  try {
    const raw = localStorage.getItem(USER_KEY);
    if (raw) userInfo = JSON.parse(raw) as JmUserInfo;
  } catch {
    userInfo = null;
  }
  return { token, userInfo: token ? userInfo : null };
}

function ensureInit() {
  if (!initialized && typeof window !== "undefined") {
    state = readStorage();
    initialized = true;
  }
}

function notify() {
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === TOKEN_KEY || e.key === USER_KEY) {
      state = readStorage();
      notify();
    }
  });
}

/* ── 会话操作 ── */

/** 保存登录态(登录成功后调用) */
export function setJmSession(token: string, userInfo: JmUserInfo): void {
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(USER_KEY, JSON.stringify(userInfo));
  state = { token, userInfo };
  notify();
}

/** 清除会话(登出或 401 失效) */
export function clearJmSession(): void {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  state = { token: "", userInfo: null };
  notify();
}

/** 获取当前 token(client 注入 Authorization 用) */
export function getJmToken(): string {
  ensureInit();
  return state.token;
}

/* ── 401 广播 ── */

/**
 * 注册未授权回调(client 收到 401/1002 时触发,先于 clearSession 通知)。
 * 返回取消注册函数。全局 401 处理(JmGate / Toast)注册一次即可。
 */
export function onJmUnauthorized(cb: () => void): () => void {
  unauthorizedListeners.add(cb);
  return () => unauthorizedListeners.delete(cb);
}

/** 登录页「会话已失效」提示的一次性标志(sessionStorage,仅真正的 401 失效才写入) */
export const JM_EXPIRED_HINT_KEY = "jm_expired_hint";

/** 供 client 内部触发,业务代码勿直接调用 */
export function _fireJmUnauthorized(): void {
  clearJmSession();
  // 标记「刚刚发生过会话失效」,登录页据此显示一次性提示;
  // 首次访问/主动退出不走这里,登录页不会误显「已失效」
  try {
    sessionStorage.setItem(JM_EXPIRED_HINT_KEY, "1");
  } catch {
    /* 隐私模式等场景静默跳过 */
  }
  unauthorizedListeners.forEach((cb) => cb());
}

/** 读取并清除失效提示标志(登录页用) */
export function consumeJmExpiredHint(): boolean {
  try {
    if (sessionStorage.getItem(JM_EXPIRED_HINT_KEY) === "1") {
      sessionStorage.removeItem(JM_EXPIRED_HINT_KEY);
      return true;
    }
  } catch {
    /* ignore */
  }
  return false;
}

/* ── React 绑定 ── */

function snapshot(): SessionState {
  ensureInit();
  return state;
}

const EMPTY: SessionState = { token: "", userInfo: null };

/** React Hook:当前会话 { token, userInfo, isLoggedIn } */
export function useJmSession(): SessionState & { isLoggedIn: boolean } {
  const s = useSyncExternalStore(subscribe, snapshot, () => EMPTY);
  return { ...s, isLoggedIn: s.token !== "" };
}

/** React Hook:登出动作(稳定引用) */
export function useJmLogout(): () => void {
  return useCallback(() => clearJmSession(), []);
}
