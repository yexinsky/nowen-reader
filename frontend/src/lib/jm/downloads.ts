"use client";

/**
 * JM 批量下载 · 模块级状态(对齐 lib/jm/session.ts 的 useSyncExternalStore 口径)
 *
 * - 任务列表与目录候选集中在此:详情页/阅读页/列表页任意入口共用同一份状态,
 *   无需在页面树里挂 Provider
 * - 轮询按需启停:有组件订阅且有活动任务时 1.5s/次,空闲时 15s/次,无订阅即停
 * - 入口:useJmDownloadCenter()(订阅状态)、startDownload()(建任务)
 */

import { useEffect, useSyncExternalStore } from "react";
import {
  isJmApiError,
  jmDownloadCancel,
  jmDownloadDirs,
  jmDownloadRemove,
  jmDownloadStart,
  jmDownloadTasks,
} from "./client";
import type { JmDownloadDir, JmDownloadStartParams, JmDownloadTask } from "./types";

interface DownloadState {
  dirs: JmDownloadDir[];
  /** 后端内置测试目录(不入库,首次使用的安全落点) */
  testDir: string;
  tasks: JmDownloadTask[];
  loaded: boolean;
  error: string | null;
}

const EMPTY: DownloadState = { dirs: [], testDir: "", tasks: [], loaded: false, error: null };

let state: DownloadState = EMPTY;
const listeners = new Set<() => void>();

function setState(patch: Partial<DownloadState>) {
  state = { ...state, ...patch };
  listeners.forEach((l) => l());
}

function subscribeStore(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot(): DownloadState {
  return state;
}

function getServerSnapshot(): DownloadState {
  return EMPTY;
}

/* ── 轮询 ── */

const ACTIVE_POLL_MS = 1500;
const IDLE_POLL_MS = 15000;

let timer: ReturnType<typeof setTimeout> | null = null;
let subscribers = 0;

function hasActiveTask(): boolean {
  return state.tasks.some((t) => t.status === "queued" || t.status === "running" || t.status === "packing");
}

async function refreshTasks(): Promise<void> {
  try {
    const data = await jmDownloadTasks();
    setState({ tasks: data.list ?? [], error: null });
  } catch (err) {
    setState({ error: errText(err, "任务列表加载失败") });
  }
}

async function tick(): Promise<void> {
  timer = null;
  await refreshTasks();
  schedule();
}

function schedule(): void {
  if (timer !== null || subscribers <= 0) return;
  timer = setTimeout(() => {
    void tick();
  }, hasActiveTask() ? ACTIVE_POLL_MS : IDLE_POLL_MS);
}

function stopPolling(): void {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
}

/** 立即拉取一次(供动作后刷新) */
export function refreshJmDownloads(): Promise<void> {
  return refreshTasks();
}

/** 拉取目录候选(带缓存:已加载且需刷新时可传 force) */
export async function loadJmDownloadDirs(force = false): Promise<void> {
  if (state.dirs.length > 0 && !force) return;
  try {
    const data = await jmDownloadDirs();
    setState({ dirs: data.dirs ?? [], testDir: data.testDir ?? "" });
  } catch (err) {
    setState({ error: errText(err, "下载目录加载失败") });
  }
}

/** 建下载任务;成功后立即刷新任务列表 */
export async function startJmDownload(params: JmDownloadStartParams): Promise<JmDownloadTask> {
  const task = await jmDownloadStart(params);
  await refreshTasks();
  schedule();
  return task;
}

export async function cancelJmDownload(id: string): Promise<void> {
  await jmDownloadCancel(id);
  await refreshTasks();
}

export async function removeJmDownload(id: string): Promise<void> {
  await jmDownloadRemove(id);
  await refreshTasks();
}

function errText(err: unknown, fallback: string): string {
  if (isJmApiError(err)) {
    if (err.code === 2002) return `${err.message || "网络不可达"}(请检查网络与上游代理设置)`;
    return err.message || fallback;
  }
  return err instanceof Error ? err.message : fallback;
}

/* ── React 绑定 ── */

export interface JmDownloadCenter {
  dirs: JmDownloadDir[];
  testDir: string;
  tasks: JmDownloadTask[];
  loaded: boolean;
  error: string | null;
  /** 活动任务数(queued/running/packing) */
  activeCount: number;
  defaultDir: JmDownloadDir | null;
}

/**
 * 订阅下载任务状态并驱动轮询(组件挂载即启用,卸载后无订阅者自动停)。
 */
export function useJmDownloadCenter(): JmDownloadCenter {
  const s = useSyncExternalStore(subscribeStore, getSnapshot, getServerSnapshot);

  useEffect(() => {
    subscribers += 1;
    void loadJmDownloadDirs();
    if (state.loaded) {
      schedule();
    } else {
      void refreshTasks().then(() => setState({ loaded: true }));
      schedule();
    }
    return () => {
      subscribers -= 1;
      if (subscribers <= 0) {
        subscribers = 0;
        stopPolling();
      }
    };
  }, []);

  const activeCount = s.tasks.filter(
    (t) => t.status === "queued" || t.status === "running" || t.status === "packing"
  ).length;

  return {
    dirs: s.dirs,
    testDir: s.testDir,
    tasks: s.tasks,
    loaded: s.loaded,
    error: s.error,
    activeCount,
    defaultDir: s.dirs.find((d) => d.isDefault) ?? s.dirs[0] ?? null,
  };
}
