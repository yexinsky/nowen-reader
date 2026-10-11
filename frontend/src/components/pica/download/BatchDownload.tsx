"use client";

/**
 * 哔咔批量下载 · 列表多选(AOP 装饰层,镜像 JM 版)
 *
 * 用法:把列表包一层 <PicaBatchSelectionProvider comics={items}>…</PicaBatchSelectionProvider>,
 * 卡片通过 context 感知多选模式;操作条内的「下载」直接打开下载对话框
 * (每部漫画一个任务,含全部章节)。
 */

import { createContext, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { CheckSquare, Download, Square, X } from "lucide-react";
import type { PicaComicItem } from "@/lib/pica/types";
import {
  PicaDownloadDialog,
  type PicaDownloadBatchItem,
} from "@/components/pica/download/DownloadDialog";

interface BatchSelectionValue {
  selectMode: boolean;
  isSelected: (bookId: string) => boolean;
  toggle: (bookId: string) => void;
}

const BatchSelectionContext = createContext<BatchSelectionValue | null>(null);

/** 卡片/网格内部消费:返回 null 表示当前不在多选容器内 */
export function usePicaBatchSelection(): BatchSelectionValue | null {
  return useContext(BatchSelectionContext);
}

export function PicaBatchSelectionProvider({
  comics,
  children,
  hint = "多选下载",
}: {
  comics: PicaComicItem[];
  children: ReactNode;
  hint?: string;
}) {
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dialogOpen, setDialogOpen] = useState(false);

  // 列表变化(翻页/筛选/切换 Tab)时丢弃已不在列表中的选择,避免误下
  useEffect(() => {
    setSelected((prev) => {
      if (prev.size === 0) return prev;
      const alive = new Set(comics.map((c) => c.id));
      const next = new Set([...prev].filter((id) => alive.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [comics]);

  const value = useMemo<BatchSelectionValue>(
    () => ({
      selectMode,
      isSelected: (bookId: string) => selected.has(bookId),
      toggle: (bookId: string) =>
        setSelected((prev) => {
          const next = new Set(prev);
          if (next.has(bookId)) next.delete(bookId);
          else next.add(bookId);
          return next;
        }),
    }),
    [selectMode, selected]
  );

  const selectedComics = useMemo<PicaDownloadBatchItem[]>(
    () =>
      comics
        .filter((c) => selected.has(c.id))
        .map((c) => ({ bookId: c.id, title: c.title, author: c.author })),
    [comics, selected]
  );

  const allSelected = comics.length > 0 && selected.size === comics.length;

  const exitSelectMode = () => {
    setSelectMode(false);
    setSelected(new Set());
  };

  return (
    <BatchSelectionContext.Provider value={value}>
      {comics.length > 0 && (
        <div className="mb-2 flex flex-wrap items-center justify-end gap-2">
          {!selectMode ? (
            <button
              type="button"
              onClick={() => setSelectMode(true)}
              className="inline-flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted transition-colors hover:border-accent/50 hover:text-foreground"
            >
              <CheckSquare className="h-3.5 w-3.5" />
              {hint}
            </button>
          ) : (
            <>
              <span className="mr-auto text-xs text-muted">
                已选 <span className="font-medium text-accent">{selected.size}</span> / {comics.length} 部
              </span>
              <button
                type="button"
                onClick={() =>
                  setSelected(allSelected ? new Set() : new Set(comics.map((c) => c.id)))
                }
                className="inline-flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted transition-colors hover:border-accent/50 hover:text-foreground"
              >
                {allSelected ? <Square className="h-3.5 w-3.5" /> : <CheckSquare className="h-3.5 w-3.5" />}
                {allSelected ? "全不选" : "全选本页"}
              </button>
              <button
                type="button"
                disabled={selected.size === 0}
                onClick={() => setDialogOpen(true)}
                className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Download className="h-3.5 w-3.5" />
                批量下载({selected.size})
              </button>
              <button
                type="button"
                onClick={exitSelectMode}
                aria-label="退出多选"
                className="inline-flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted transition-colors hover:text-foreground"
              >
                <X className="h-3.5 w-3.5" />
                退出
              </button>
            </>
          )}
        </div>
      )}

      {children}

      <PicaDownloadDialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        batch={selectedComics}
      />
    </BatchSelectionContext.Provider>
  );
}
