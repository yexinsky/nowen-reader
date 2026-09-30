"use client";

/**
 * JM 批量下载 · 任务面板与浮动入口
 *
 * - JmDownloadTasksPanel:任务列表(进度条/章节明细/取消/移除/归档路径)
 * - JmDownloadFab:右下(或调用方定位)浮动按钮,角标显示活动任务数;
 *   传入 comic 上下文时提供「下载本作 / 下载本章」直达入口
 *
 * 状态来源 lib/jm/downloads.ts(模块级订阅 + 按需轮询),本文件不修改任何现有页面组件。
 */

import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Download,
  Loader2,
  Trash2,
  X,
  XCircle,
} from "lucide-react";
import { cancelJmDownload, removeJmDownload, useJmDownloadCenter } from "@/lib/jm/downloads";
import type { JmDownloadChapter, JmDownloadTask } from "@/lib/jm/types";
import { JmDownloadDialog, type JmDownloadBatchItem } from "@/components/jm/download/DownloadDialog";
import type { JmEpisode } from "@/lib/jm/types";

/* ── 文案与进度口径 ── */

const STATUS_TEXT: Record<string, string> = {
  queued: "排队中",
  running: "下载中",
  packing: "打包中",
  done: "已完成",
  failed: "失败",
  canceled: "已取消",
};

function formatSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return "";
  if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

/** 进度:以章节完成度为主,活动章节内用图片数细分 */
function taskProgress(task: JmDownloadTask): { percent: number; text: string } {
  const total = task.chapters.length;
  const done = task.chapters.filter((c) => c.state === "done").length;
  if (task.status === "done") return { percent: 100, text: `${done}/${total} 章` };
  if (task.status === "failed") return { percent: 100, text: `${done}/${total} 章` };
  if (total === 0) return { percent: 0, text: "准备中" };
  const runningSlices = task.chapters
    .filter((c) => c.state === "running")
    .reduce((acc, c) => acc + (c.total > 0 ? c.done / c.total : 0), 0);
  const percent = Math.min(100, Math.round(((done + runningSlices) / total) * 100));
  return { percent, text: `${done}/${total} 章 · ${task.doneImages} 张` };
}

function chapterLabel(ch: JmDownloadChapter, index: number): string {
  return ch.title || `第${index + 1}话`;
}

function isActive(status: string): boolean {
  return status === "queued" || status === "running" || status === "packing";
}

/* ── 单任务卡片 ── */

function TaskCard({ task, onCancel, onRemove }: { task: JmDownloadTask; onCancel: () => void; onRemove: () => void }) {
  const [expanded, setExpanded] = useState(false);
  const { percent, text } = taskProgress(task);
  const failedChapters = task.chapters.filter((c) => c.state === "failed");

  const statusTone =
    task.status === "done"
      ? "text-emerald-500"
      : task.status === "failed"
        ? "text-red-400"
        : task.status === "canceled"
          ? "text-muted"
          : "text-accent";

  return (
    <div className="rounded-lg border border-border/60 bg-background p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-foreground" title={task.title || task.aid}>
            {task.title || `JM${task.aid}`}
          </p>
          <p className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[11px] text-muted">
            <span className={`font-medium ${statusTone}`}>{STATUS_TEXT[task.status] ?? task.status}</span>
            <span>{text}</span>
            {task.status === "done" && task.zipSize && <span>{formatSize(task.zipSize)}</span>}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {isActive(task.status) ? (
            <button
              type="button"
              onClick={onCancel}
              className="rounded-md border border-border px-2 py-1 text-[11px] text-muted transition-colors hover:border-red-500/50 hover:text-red-400"
            >
              取消
            </button>
          ) : (
            <button
              type="button"
              onClick={onRemove}
              aria-label="移除记录"
              className="rounded-md border border-border p-1.5 text-muted transition-colors hover:border-red-500/50 hover:text-red-400"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          )}
          {task.chapters.length > 0 && (
            <button
              type="button"
              onClick={() => setExpanded((v) => !v)}
              aria-label={expanded ? "收起章节" : "展开章节"}
              className="rounded-md border border-border p-1.5 text-muted transition-colors hover:text-foreground"
            >
              {expanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
            </button>
          )}
        </div>
      </div>

      {/* 进度条 */}
      <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted/15">
        <div
          className={`h-full rounded-full transition-all ${
            task.status === "failed" ? "bg-red-500/70" : task.status === "canceled" ? "bg-muted" : "bg-accent"
          }`}
          style={{ width: `${percent}%` }}
        />
      </div>

      {/* 结果/错误/警告 */}
      {task.status === "done" && task.zipPath && (
        <p className="mt-2 break-all text-[11px] text-muted">
          <CheckCircle2 className="mr-1 inline h-3 w-3 text-emerald-500" />
          已归档:{task.zipPath}
        </p>
      )}
      {task.status === "failed" && task.error && (
        <p className="mt-2 break-all text-[11px] text-red-400">
          <XCircle className="mr-1 inline h-3 w-3" />
          {task.error}
        </p>
      )}
      {task.status === "done" && task.warning && (
        <p className="mt-2 break-all text-[11px] text-amber-500">
          <AlertTriangle className="mr-1 inline h-3 w-3" />
          {task.warning}
        </p>
      )}

      {/* 章节明细 */}
      {expanded && (
        <div className="mt-2 max-h-44 space-y-0.5 overflow-y-auto rounded-md border border-border/40 bg-card p-1.5">
          {task.chapters.map((ch, i) => (
            <div
              key={`${ch.pid}-${i}`}
              className="flex items-center justify-between gap-2 rounded px-1.5 py-1 text-[11px]"
            >
              <span
                className={`min-w-0 truncate ${
                  ch.state === "failed" ? "text-red-400" : "text-foreground/80"
                }`}
                title={chapterLabel(ch, i)}
              >
                {chapterLabel(ch, i)}
              </span>
              <span className="shrink-0 text-muted">
                {ch.state === "done"
                  ? `${ch.total}P`
                  : ch.state === "running"
                    ? `${ch.done}/${ch.total}`
                    : ch.state === "failed"
                      ? "失败"
                      : "待下载"}
              </span>
            </div>
          ))}
        </div>
      )}
      {!expanded && failedChapters.length > 0 && (
        <p className="mt-1 text-[11px] text-red-400">失败章节:{failedChapters.length} 个</p>
      )}
    </div>
  );
}

/* ── 任务面板 ── */

export function JmDownloadTasksPanel({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { tasks, activeCount } = useJmDownloadCenter();

  const sorted = useMemo(
    () => [...tasks].sort((a, b) => Number(isActive(b.status)) - Number(isActive(a.status))),
    [tasks]
  );

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[70] flex items-end justify-center bg-black/50 backdrop-blur-[2px] sm:items-center"
      onClick={onClose}
    >
      <div
        className="flex max-h-[88vh] w-full max-w-lg flex-col overflow-hidden rounded-t-2xl border border-border bg-card shadow-2xl sm:rounded-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between gap-2 border-b border-border/60 px-4 py-3">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <Download className="h-4 w-4 text-accent" />
            下载任务
            {activeCount > 0 && <span className="text-xs font-normal text-muted">{activeCount} 进行中</span>}
          </h3>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭"
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted transition-colors hover:bg-background hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-4 py-3">
          {sorted.length === 0 ? (
            <p className="py-10 text-center text-sm text-muted">暂无下载任务</p>
          ) : (
            sorted.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                onCancel={() => void cancelJmDownload(task.id)}
                onRemove={() => void removeJmDownload(task.id)}
              />
            ))
          )}
        </div>
      </div>
    </div>
  );
}

/* ── 页头按钮 ── */

/**
 * 下载任务入口按钮:放在 PageHeader 的 actions 中(与 JmBackButton 同款样式),
 * 角标展示进行中的任务数;点击打开任务面板查看下载队列。
 */
export function JmDownloadTasksButton() {
  const { activeCount } = useJmDownloadCenter();
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="下载任务"
        title="查看下载队列"
        className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border bg-card px-3 text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground"
      >
        <Download className="h-4 w-4" />
        下载任务
        {activeCount > 0 && (
          <span className="ml-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-semibold text-white">
            {activeCount > 99 ? "99+" : activeCount}
          </span>
        )}
      </button>
      <JmDownloadTasksPanel open={open} onClose={() => setOpen(false)} />
    </>
  );
}

/* ── 浮动入口 ── */

export interface JmDownloadFabProps {
  /** 单作上下文:提供「下载本作 / 下载本章」入口 */
  comic?: {
    aid: string;
    title: string;
    author?: string;
    chapters?: JmEpisode[];
    /** 当前章节(阅读页):提供「下载本章」 */
    currentPid?: string;
    currentTitle?: string;
  };
  /** 批量上下文:提供「下载已选中 N 部」入口 */
  batch?: JmDownloadBatchItem[];
  /** 定位类名(缺省左下角,避开阅读页右下角的章节导航) */
  className?: string;
  label?: string;
}

export function JmDownloadFab({ comic, batch, className, label }: JmDownloadFabProps) {
  const { activeCount } = useJmDownloadCenter();
  const [menuOpen, setMenuOpen] = useState(false);
  const [dialog, setDialog] = useState<null | { pids?: string[]; batch?: JmDownloadBatchItem[] }>(null);
  const [tasksOpen, setTasksOpen] = useState(false);

  // 长按/展开后点击外部收起
  useEffect(() => {
    if (!menuOpen) return;
    const close = () => setMenuOpen(false);
    window.addEventListener("click", close);
    return () => window.removeEventListener("click", close);
  }, [menuOpen]);

  const hasActions = !!comic || (batch && batch.length > 0);
  const batchActive = !!batch && batch.length > 0;

  return (
    <>
      <button
        type="button"
        aria-label={label || "下载"}
        onClick={(e) => {
          e.stopPropagation();
          if (hasActions) setMenuOpen((v) => !v);
          else setTasksOpen(true);
        }}
        className={
          className ??
          "fixed bottom-5 left-4 z-30 flex h-11 w-11 items-center justify-center rounded-full border border-border bg-card/90 text-foreground shadow-lg backdrop-blur-xl transition-transform active:scale-95"
        }
        title={label || "批量下载"}
      >
        <Download className="h-5 w-5" />
        {activeCount > 0 && (
          <span className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-1 text-[10px] font-semibold text-white">
            {activeCount > 99 ? "99+" : activeCount}
          </span>
        )}
      </button>

      {/* 动作菜单 */}
      {menuOpen && (
        <div
          className="fixed bottom-20 left-4 z-[65] w-56 overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
          onClick={(e) => e.stopPropagation()}
        >
          {comic && (
            <>
              <button
                type="button"
                onClick={() => {
                  setMenuOpen(false);
                  setDialog({});
                }}
                className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm text-foreground transition-colors hover:bg-card-hover"
              >
                <Download className="h-4 w-4 text-accent" />
                下载整本(全部章节)
              </button>
              {comic.currentPid && (
                <button
                  type="button"
                  onClick={() => {
                    setMenuOpen(false);
                    setDialog({ pids: [comic.currentPid!] });
                  }}
                  className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm text-foreground transition-colors hover:bg-card-hover"
                >
                  <Download className="h-4 w-4 text-accent" />
                  下载本章{comic.currentTitle ? `:${comic.currentTitle.slice(0, 10)}` : ""}
                </button>
              )}
            </>
          )}
          {batchActive && (
            <button
              type="button"
              onClick={() => {
                setMenuOpen(false);
                setDialog({ batch });
              }}
              className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm text-foreground transition-colors hover:bg-card-hover"
            >
              <Download className="h-4 w-4 text-accent" />
              下载已选 {batch!.length} 部
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              setMenuOpen(false);
              setTasksOpen(true);
            }}
            className="flex w-full items-center gap-2 border-t border-border/60 px-3 py-2.5 text-left text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground"
          >
            <Loader2 className="h-4 w-4" />
            下载任务{activeCount > 0 ? `(${activeCount})` : ""}
          </button>
        </div>
      )}

      <JmDownloadDialog
        open={dialog !== null}
        onClose={() => setDialog(null)}
        aid={comic?.aid}
        title={comic?.title}
        author={comic?.author}
        chapters={comic?.chapters}
        pids={dialog?.pids}
        batch={dialog?.batch}
        onOpenTasks={() => setTasksOpen(true)}
      />
      <JmDownloadTasksPanel open={tasksOpen} onClose={() => setTasksOpen(false)} />
    </>
  );
}
