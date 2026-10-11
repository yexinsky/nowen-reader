"use client";

/**
 * 哔咔在线源(PicACG)会话管理
 * - 上游 JWT / 用户信息存 localStorage(设备级;哔咔为无状态 Bearer,服务端零会话)
 * - 模块级状态 + 订阅,useSyncExternalStore 供组件消费
 * - 401/code=1002 时由 client 调用 clearPicaSession + 广播未授权事件
 */

import { useSyncExternalStore, useCallback } from "react";
import type { PicaUserInfo } from "./types";

const TOKEN_KEY = "pica_token";
const USER_KEY = "pica_user_info";

interface SessionState {
  token: string;
  userInfo: PicaUserInfo | null;
}

/* ── 模块级状态 ── */

let state: SessionState = { token: "", userInfo: null };
let initialized = false;
const listeners = new Set<() => void>();
const unauthorizedListeners = new Set<() => void>();

function readStorage(): SessionState {
  if (typeof window === "undefined") return { token: "", userInfo: null };
  const token = localStorage.getItem(TOKEN_KEY) ?? "";
  let userInfo: PicaUserInfo | null = null;
  try {
    const raw = localStorage.getItem(USER_KEY);
    if (raw) userInfo = JSON.parse(raw) as PicaUserInfo;
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
export function setPicaSession(token: string, userInfo: PicaUserInfo): void {
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(USER_KEY, JSON.stringify(userInfo));
  state = { token, userInfo };
  notify();
}

/** 清除会话(登出或 401 失效) */
export function clearPicaSession(): void {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  state = { token: "", userInfo: null };
  notify();
}

/** 获取当前 token(client 注入 X-Pica-Token 用) */
export function getPicaToken(): string {
  ensureInit();
  return state.token;
}

/* ── 401 广播 ── */

/**
 * 注册未授权回调(client 收到 401/1002 时触发,先于 clearPicaSession 通知)。
 * 返回取消注册函数。
 */
export function onPicaUnauthorized(cb: () => void): () => void {
  unauthorizedListeners.add(cb);
  return () => unauthorizedListeners.delete(cb);
}

/** 登录页「会话已失效」提示的一次性标志(sessionStorage,仅真正的 401 失效才写入) */
export const PICA_EXPIRED_HINT_KEY = "pica_expired_hint";

/** 供 client 内部触发,业务代码勿直接调用 */
export function _firePicaUnauthorized(): void {
  clearPicaSession();
  try {
    sessionStorage.setItem(PICA_EXPIRED_HINT_KEY, "1");
  } catch {
    /* 隐私模式等场景静默跳过 */
  }
  unauthorizedListeners.forEach((cb) => cb());
}

/** 读取并清除失效提示标志(登录页用) */
export function consumePicaExpiredHint(): boolean {
  try {
    if (sessionStorage.getItem(PICA_EXPIRED_HINT_KEY) === "1") {
      sessionStorage.removeItem(PICA_EXPIRED_HINT_KEY);
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
export function usePicaSession(): SessionState & { isLoggedIn: boolean } {
  const s = useSyncExternalStore(subscribe, snapshot, () => EMPTY);
  return { ...s, isLoggedIn: s.token !== "" };
}

/** React Hook:登出动作(稳定引用) */
export function usePicaLogout(): () => void {
  return useCallback(() => clearPicaSession(), []);
}
