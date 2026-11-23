"use client";

/**
 * JM 批量下载 · 详情页下载入口
 *
 * 按钮承载下载的**全生命周期**,不再只在有活动任务时另挂一个「下载中 N」：
 * - 无任务            → 「下载」(打开章节/目录选择对话框)
 * - 有活动任务        → 「下载中 42%」/「打包中」,点击直达任务面板看逐章进度
 * - 最近一次成功      → 「已归档」,点击看 zip 路径;并给「重新下载」入口
 * - 最近一次失败      → 「下载失败 · 重试」,点击直接重开对话框;「原因」看错误详情
 *
 * 历史问题:完成/失败后按钮整块消失,用户既看不到归档结果,也找不到重试入口。
 */

import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, CheckCircle2, Download, Loader2 } from "lucide-react";
import { useJmDownloadCenter } from "@/lib/jm/downloads";
import { JmDownloadDialog } from "@/components/jm/download/DownloadDialog";
import { JmDownloadTasksPanel } from "@/components/jm/download/DownloadTasks";
import type { JmDownloadTask, JmEpisode } from "@/lib/jm/types";

const BASE_BTN =
  "relative inline-flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60";

function isActiveTask(t: JmDownloadTask): boolean {
  return t.status === "queued" || t.status === "running" || t.status === "packing";
}

/** 活动任务的进度百分比(打包阶段固定显示文案,不显示数字) */
function progressPercent(t: JmDownloadTask): number {
  if (t.totalImages <= 0) return 0;
  return Math.min(100, Math.floor((t.doneImages / t.totalImages) * 100));
}

export function JmDownloadButton({
  aid,
  title,
  author,
  chapters,
}: {
  aid: string;
  title?: string;
  author?: string;
  chapters?: JmEpisode[];
}) {
  const { tasks } = useJmDownloadCenter();
  const [open, setOpen] = useState(false);
  const [tasksOpen, setTasksOpen] = useState(false);
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  /** 本部漫画的任务(任务列表按创建时间倒序返回,取最新一条做状态) */
  const mine = useMemo(() => tasks.filter((t) => t.aid === aid), [tasks, aid]);
  const active = useMemo(() => mine.find(isActiveTask) ?? null, [mine]);
  const latest = mine[0] ?? null;
  const done = !active && latest?.status === "done" ? latest : null;
  const failed = !active && latest?.status === "failed" ? latest : null;

  let primary: React.ReactNode;
  if (active) {
    const label =
      active.status === "packing" ? "打包中…" : active.status === "queued" ? "排队中…" : `下载中 ${progressPercent(active)}%`;
    primary = (
      <button
        type="button"
        onClick={() => setTasksOpen(true)}
        className={`${BASE_BTN} border-accent/40 bg-accent/10 text-accent hover:opacity-90`}
        title="查看下载进度与逐章明细"
      >
        <Loader2 className="h-4 w-4 animate-spin" />
        {label}
      </button>
    );
  } else if (failed) {
    primary = (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={`${BASE_BTN} border-red-500/40 bg-red-500/10 text-red-400 hover:bg-red-500/15`}
        title={failed.error ? `上次失败:${failed.error}` : "上次下载失败,点击重试"}
      >
        <AlertTriangle className="h-4 w-4" />
        下载失败 · 重试
      </button>
    );
  } else if (done) {
    primary = (
      <button
        type="button"
        onClick={() => setTasksOpen(true)}
        className={`${BASE_BTN} border-emerald-500/40 bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/15`}
        title={done.zipPath ? `已归档:${done.zipPath}` : "已归档到下载目录"}
      >
        <CheckCircle2 className="h-4 w-4" />
        已归档
      </button>
    );
  } else {
    primary = (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={`${BASE_BTN} border-border bg-background text-muted hover:border-accent/50 hover:text-foreground`}
      >
        <Download className="h-4 w-4" />
        下载
      </button>
    );
  }

  return (
    <>
      {primary}

      {/* 已归档后仍可重新下载;失败时也给「原因」入口 */}
      {mounted && done && (
        <button
          type="button"
          onClick={() => setOpen(true)}
          className={`${BASE_BTN} border-border bg-background text-muted hover:border-accent/50 hover:text-foreground`}
          title="重新下载本章/整本"
        >
          <Download className="h-4 w-4" />
          重新下载
        </button>
      )}
      {mounted && failed && (
        <button
          type="button"
          onClick={() => setTasksOpen(true)}
          className={`${BASE_BTN} border-border bg-background text-muted hover:border-accent/50 hover:text-foreground`}
          title={failed.error || "查看失败原因"}
        >
          查看原因
        </button>
      )}

      <JmDownloadDialog
        open={open}
        onClose={() => setOpen(false)}
        aid={aid}
        title={title}
        author={author}
        chapters={chapters}
        onOpenTasks={() => setTasksOpen(true)}
      />
      <JmDownloadTasksPanel
        open={tasksOpen}
        onClose={() => setTasksOpen(false)}
        highlightAid={aid}
      />
    </>
  );
}

export default JmDownloadButton;
