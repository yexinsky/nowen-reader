"use client";

/**
 * JM 批量下载 · 启动对话框
 *
 * 职责:选择归档目录(书库管理中的目录 + 内置测试目录,下拉框)与章节范围
 * (默认全部章节),提交后由后端异步下载 → 打包 zip → 落到所选目录。
 * - 单作模式:aid + chapters(可勾选章节);pids 传入时默认只勾选这些章节
 * - 批量模式:batch 每部漫画一个任务(全章节),后端按 JM_DOWNLOAD_TASK_CONCURRENCY 排队
 * - 关闭/卸载作废在途请求;错误文案区分 2001/2002 与 422
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Check, Download, HardDrive, Loader2, X } from "lucide-react";
import { isJmApiError } from "@/lib/jm/client";
import { startJmDownload, useJmDownloadCenter } from "@/lib/jm/downloads";
import type { JmEpisode } from "@/lib/jm/types";

export interface JmDownloadBatchItem {
  aid: string;
  title: string;
  author?: string;
}

export interface JmDownloadDialogProps {
  open: boolean;
  onClose: () => void;
  /** 单作模式 */
  aid?: string;
  title?: string;
  author?: string;
  /** 章节清单(单作模式;缺省则整本下载,不展示章节选择) */
  chapters?: JmEpisode[];
  /** 默认勾选的章节 pid(如阅读页当前章);缺省 = 全部章节 */
  pids?: string[];
  /** 批量模式:每部漫画一个任务 */
  batch?: JmDownloadBatchItem[];
  /** 「查看任务」回调(缺省关闭对话框) */
  onOpenTasks?: () => void;
}

function errText(err: unknown, fallback: string): string {
  if (isJmApiError(err)) {
    if (err.code === 2002) return `${err.message || "网络不可达"}(请检查网络与上游代理设置)`;
    if (err.httpStatus === 422) return err.message || "请求参数不合法";
    return err.message || fallback;
  }
  return err instanceof Error ? err.message : fallback;
}

function formatSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return "";
  if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

export function JmDownloadDialog({
  open,
  onClose,
  aid,
  title,
  author,
  chapters,
  pids,
  batch,
  onOpenTasks,
}: JmDownloadDialogProps) {
  const { dirs, defaultDir } = useJmDownloadCenter();
  const isBatch = !!batch && batch.length > 0;

  const [destDir, setDestDir] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<{ count: number; zipNames: string[] } | null>(null);
  const reqRef = useRef({ cancelled: false });

  // 卸载作废在途请求
  useEffect(() => {
    const holder = reqRef.current;
    return () => {
      holder.cancelled = true;
    };
  }, []);

  const episodeList = useMemo<JmEpisode[]>(
    () => [...(chapters ?? [])].sort((a, b) => a.order - b.order),
    [chapters]
  );

  // 打开时重置状态:目录取默认项,章节默认全选(有 pids 则只选 pids)
  useEffect(() => {
    if (!open) return;
    reqRef.current.cancelled = false;
    setError(null);
    setDone(null);
    setBusy(false);
    setDestDir((prev) => prev || defaultDir?.path || "");
    if (episodeList.length > 0) {
      if (pids && pids.length > 0) setSelected(new Set(pids));
      else setSelected(new Set(episodeList.map((ep) => ep.pid)));
    } else {
      setSelected(new Set());
    }
  }, [open, defaultDir?.path, episodeList, pids, reqRef]);

  // 默认目录后到时补齐
  useEffect(() => {
    if (!open || destDir) return;
    if (defaultDir?.path) setDestDir(defaultDir.path);
  }, [open, destDir, defaultDir?.path]);

  const toggleEpisode = useCallback((pid: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(pid)) next.delete(pid);
      else next.add(pid);
      return next;
    });
  }, []);

  const allSelected = episodeList.length > 0 && selected.size === episodeList.length;

  const handleStart = useCallback(async () => {
    if (busy) return;
    if (!destDir) {
      setError("请选择下载目录");
      return;
    }
    const targets: JmDownloadBatchItem[] = isBatch
      ? batch!
      : [{ aid: aid ?? "", title: title ?? "", author }];
    if (!isBatch && !targets[0].aid && selected.size === 0) {
      setError("缺少可下载的章节");
      return;
    }
    if (!isBatch && episodeList.length > 0 && selected.size === 0) {
      setError("请至少选择一个章节");
      return;
    }

    setBusy(true);
    setError(null);
    const created: string[] = [];
    try {
      for (const item of targets) {
        if (reqRef.current.cancelled) return;
        const task = await startJmDownload({
          aid: item.aid,
          title: item.title,
          author: item.author,
          pids: isBatch
            ? undefined
            : episodeList.length > 0
              ? episodeList.filter((ep) => selected.has(ep.pid)).map((ep) => ep.pid)
              : undefined,
          destDir,
        });
        created.push(task.title || task.aid);
      }
      if (reqRef.current.cancelled) return;
      setDone({ count: created.length, zipNames: created });
    } catch (err) {
      if (!reqRef.current.cancelled) setError(errText(err, "创建下载任务失败"));
    } finally {
      if (!reqRef.current.cancelled) setBusy(false);
    }
  }, [aid, author, batch, busy, destDir, episodeList, isBatch, reqRef, selected, title]);

  if (!open) return null;

  const dialogTitle = isBatch
    ? `批量下载 ${batch!.length} 部漫画`
    : `下载${title ? `《${title}》` : "漫画"}`;

  return (
    <div
      className="fixed inset-0 z-[70] flex items-end justify-center bg-black/50 backdrop-blur-[2px] sm:items-center"
      onClick={onClose}
    >
      <div
        className="flex max-h-[88vh] w-full max-w-lg flex-col overflow-hidden rounded-t-2xl border border-border bg-card shadow-2xl sm:rounded-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center justify-between gap-2 border-b border-border/60 px-4 py-3">
          <h3 className="min-w-0 truncate text-sm font-semibold text-foreground" title={dialogTitle}>
            {dialogTitle}
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

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
          {done ? (
            <div className="py-6 text-center">
              <span className="mx-auto mb-3 flex h-11 w-11 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-500">
                <Check className="h-5 w-5" />
              </span>
              <p className="text-sm font-medium text-foreground">
                已创建 {done.count} 个下载任务
              </p>
              <p className="mt-1.5 break-all px-2 text-xs text-muted">
                下载完成后将打包为 zip 归档到 {destDir}
              </p>
            </div>
          ) : (
            <>
              {/* 归档目录(下拉框:书库管理中的目录 + 内置测试目录) */}
              <div>
                <label className="flex items-center gap-1.5 text-xs font-medium text-muted">
                  <HardDrive className="h-3.5 w-3.5" />
                  下载目录
                </label>
                <select
                  value={destDir}
                  onChange={(e) => setDestDir(e.target.value)}
                  className="mt-1.5 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground outline-none transition-colors focus:border-accent"
                >
                  {dirs.length === 0 && <option value="">(未获取到书库目录)</option>}
                  {dirs.map((dir) => (
                    <option key={dir.path} value={dir.path} disabled={!dir.canManage}>
                      {dir.label} · {dir.path}
                      {dir.kind === "test" ? "(不入库)" : ""}
                      {dir.canManage ? "" : "(无权限)"}
                    </option>
                  ))}
                </select>
                <p className="mt-1.5 text-[11px] leading-relaxed text-muted">
                  目录来自「设置 · 书库管理」;打包完成后自动清理下载临时文件夹,
                  同名 zip 不会覆盖已有文件。
                </p>
              </div>

              {/* 章节选择(单作模式且已知章节清单) */}
              {!isBatch && episodeList.length > 0 && (
                <div className="mt-4">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs font-medium text-muted">
                      章节 {selected.size}/{episodeList.length}
                    </span>
                    <button
                      type="button"
                      onClick={() =>
                        setSelected(
                          allSelected ? new Set() : new Set(episodeList.map((ep) => ep.pid))
                        )
                      }
                      className="rounded-lg border border-border px-2 py-1 text-[11px] text-muted transition-colors hover:border-accent/50 hover:text-foreground"
                    >
                      {allSelected ? "全不选" : "全选"}
                    </button>
                  </div>
                  <div className="mt-2 max-h-56 space-y-1 overflow-y-auto rounded-lg border border-border/60 bg-background p-1.5">
                    {episodeList.map((ep) => {
                      const checked = selected.has(ep.pid);
                      return (
                        <label
                          key={`${ep.pid}-${ep.order}`}
                          className={`flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors ${
                            checked ? "bg-accent/10 text-accent" : "text-foreground/90 hover:bg-card-hover"
                          }`}
                        >
                          <input
                            type="checkbox"
                            className="sr-only"
                            checked={checked}
                            onChange={() => toggleEpisode(ep.pid)}
                          />
                          <span
                            className={`flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
                              checked ? "border-accent bg-accent text-white" : "border-border"
                            }`}
                          >
                            {checked && <Check className="h-3 w-3" />}
                          </span>
                          <span className="min-w-0 truncate">{ep.title || `第${ep.order}话`}</span>
                        </label>
                      );
                    })}
                  </div>
                </div>
              )}

              {isBatch && (
                <div className="mt-4 rounded-lg border border-border/60 bg-background p-3">
                  <p className="text-[11px] leading-relaxed text-muted">
                    批量下载将逐部创建任务(包含每部漫画的全部章节),服务端按序排队执行;
                    可在任务面板查看进度或取消。
                  </p>
                  <div className="mt-2 max-h-32 space-y-0.5 overflow-y-auto">
                    {batch!.slice(0, 50).map((c) => (
                      <p key={c.aid} className="truncate text-xs text-foreground/80">
                        {c.title || `JM${c.aid}`}
                      </p>
                    ))}
                    {batch!.length > 50 && (
                      <p className="text-xs text-muted">… 等 {batch!.length} 部</p>
                    )}
                  </div>
                </div>
              )}

              {error && (
                <p className="mt-3 flex items-start gap-1.5 break-all text-xs text-red-400">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  {error}
                </p>
              )}
            </>
          )}
        </div>

        {/* 底部操作 */}
        <div className="flex items-center justify-end gap-2 border-t border-border/60 px-4 py-3">
          {done ? (
            <>
              <button
                type="button"
                onClick={onClose}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground"
              >
                关闭
              </button>
              <button
                type="button"
                onClick={() => {
                  onClose();
                  onOpenTasks?.();
                }}
                className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
              >
                <Download className="h-4 w-4" />
                查看任务
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={onClose}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleStart}
                disabled={busy}
                className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                {isBatch ? `开始下载 ${batch!.length} 部` : "开始下载"}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

export { formatSize };
