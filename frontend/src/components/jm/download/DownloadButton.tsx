"use client";

/**
 * JM 批量下载 · 详情页下载入口
 *
 * 「下载」按钮(打开章节选择 + 目录选择对话框;活动任务数显示角标),
 * 有任务进行中时并排显示「下载中 N」快捷入口直达任务面板。
 * 供 /jm/comic/{aid} 操作条使用;不修改任何现有组件。
 */

import { useEffect, useState } from "react";
import { Download, Loader2 } from "lucide-react";
import { useJmDownloadCenter } from "@/lib/jm/downloads";
import { JmDownloadDialog } from "@/components/jm/download/DownloadDialog";
import { JmDownloadTasksPanel } from "@/components/jm/download/DownloadTasks";
import type { JmEpisode } from "@/lib/jm/types";

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
  const { activeCount } = useJmDownloadCenter();
  const [open, setOpen] = useState(false);
  const [tasksOpen, setTasksOpen] = useState(false);
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="relative inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-4 py-2 text-sm font-medium text-muted transition-colors hover:border-accent/50 hover:text-foreground"
      >
        <Download className="h-4 w-4" />
        下载
      </button>

      {mounted && activeCount > 0 && (
        <button
          type="button"
          onClick={() => setTasksOpen(true)}
          className="inline-flex items-center gap-1.5 rounded-lg border border-accent/40 bg-accent/10 px-3 py-2 text-sm font-medium text-accent transition-opacity hover:opacity-90"
          title="查看下载任务"
        >
          <Loader2 className="h-4 w-4 animate-spin" />
          下载中 {activeCount}
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
      <JmDownloadTasksPanel open={tasksOpen} onClose={() => setTasksOpen(false)} />
    </>
  );
}

export default JmDownloadButton;
