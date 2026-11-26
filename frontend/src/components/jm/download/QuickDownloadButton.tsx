"use client";

/**
 * JM 在线源 · 列表行「快速下载」按钮
 *
 * 列表页(阅读历史等)每一行一个入口,点一下就能整本下载,
 * 不必先点进详情页再点「下载」—— 列表场景不需要选章节,所以省掉章节勾选。
 *
 * 状态口径与详情页 JmDownloadButton 一致(同一份 useJmDownloadCenter 数据):
 * - 无任务   → 描边图标按钮,点击打开下载对话框(整本)
 * - 进行中   → accent 色 + 转圈,点击打开任务面板看进度
 * - 已归档   → emerald 勾,点击打开任务面板
 * - 失败     → 红点感叹号,点击重开对话框重试
 */

import { useMemo, useState } from "react";
import { AlertTriangle, CheckCircle2, Download, Loader2 } from "lucide-react";
import { useJmDownloadCenter } from "@/lib/jm/downloads";
import { JmDownloadDialog } from "@/components/jm/download/DownloadDialog";
import { JmDownloadTasksPanel } from "@/components/jm/download/DownloadTasks";
import type { JmDownloadTask } from "@/lib/jm/types";

function isActiveTask(t: JmDownloadTask): boolean {
  return t.status === "queued" || t.status === "running" || t.status === "packing";
}

export function JmQuickDownloadButton({
  aid,
  title,
  author,
}: {
  aid: string;
  title?: string;
  author?: string;
}) {
  const { tasks } = useJmDownloadCenter();
  const [open, setOpen] = useState(false);
  const [tasksOpen, setTasksOpen] = useState(false);

  const mine = useMemo(() => tasks.filter((t) => t.aid === aid), [tasks, aid]);
  const active = useMemo(() => mine.find(isActiveTask) ?? null, [mine]);
  const latest = mine[0] ?? null;
  const done = !active && latest?.status === "done";
  const failed = !active && latest?.status === "failed";

  let icon = <Download className="h-4 w-4" />;
  let tone = "border-border bg-background text-muted hover:border-accent/50 hover:text-foreground";
  let label = "下载整本";
  if (active) {
    icon = <Loader2 className="h-4 w-4 animate-spin" />;
    tone = "border-accent/40 bg-accent/10 text-accent";
    label = active.status === "packing" ? "打包中" : active.status === "queued" ? "排队中" : "下载中";
  } else if (failed) {
    icon = <AlertTriangle className="h-4 w-4" />;
    tone = "border-red-500/40 bg-red-500/10 text-red-400 hover:bg-red-500/15";
    label = latest?.error ? `下载失败:${latest.error}` : "下载失败,点击重试";
  } else if (done) {
    icon = <CheckCircle2 className="h-4 w-4" />;
    tone = "border-emerald-500/40 bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/15";
    label = latest?.zipPath ? `已归档:${latest.zipPath}` : "已归档";
  }

  return (
    <>
      <button
        type="button"
        title={label}
        aria-label={label}
        onClick={(e) => {
          // 行本身是 Link:别让点按钮顺带跳进详情页
          e.preventDefault();
          e.stopPropagation();
          if (active || done) setTasksOpen(true);
          else setOpen(true);
        }}
        className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border transition-colors ${tone}`}
      >
        {icon}
      </button>

      <JmDownloadDialog
        open={open}
        onClose={() => setOpen(false)}
        aid={aid}
        title={title}
        author={author}
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

export default JmQuickDownloadButton;
