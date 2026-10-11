"use client";

/**
 * 哔咔批量下载 · 详情页下载入口(镜像 JM 版)
 */

import { useEffect, useState } from "react";
import { Download, Loader2 } from "lucide-react";
import { usePicaDownloadCenter } from "@/lib/pica/downloads";
import { PicaDownloadDialog } from "@/components/pica/download/DownloadDialog";
import { PicaDownloadTasksPanel } from "@/components/pica/download/DownloadTasks";
import type { PicaEpisode } from "@/lib/pica/types";

export function PicaDownloadButton({
  bookId,
  title,
  author,
  episodes,
}: {
  bookId: string;
  title?: string;
  author?: string;
  episodes?: PicaEpisode[];
}) {
  const { activeCount } = usePicaDownloadCenter();
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

      <PicaDownloadDialog
        open={open}
        onClose={() => setOpen(false)}
        bookId={bookId}
        title={title}
        author={author}
        episodes={episodes}
        onOpenTasks={() => setTasksOpen(true)}
      />
      <PicaDownloadTasksPanel open={tasksOpen} onClose={() => setTasksOpen(false)} />
    </>
  );
}

export default PicaDownloadButton;
