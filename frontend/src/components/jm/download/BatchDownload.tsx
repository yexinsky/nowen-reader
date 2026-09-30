"use client";

/**
 * JM 批量下载 · 列表多选(AOP 装饰层)
 *
 * 用法:把列表包一层 <JmBatchSelectionProvider comics={items}>…</JmBatchSelectionProvider>,
 * 网格/卡片通过 context 感知多选模式,无需改动原有 props 签名:
 * - 未进入多选:卡片点击照常跳详情页
 * - 进入多选:点击封面切换选中,右上角显示勾选标记,块内出现操作条(全选/下载/退出)
 * - 操作条内的「下载」直接打开下载对话框(每部漫画一个任务,含全部章节)
 */

import { createContext, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { CheckSquare, Download, Square, X } from "lucide-react";
import type { JmComicItem } from "@/lib/jm/types";
import { JmDownloadDialog, type JmDownloadBatchItem } from "@/components/jm/download/DownloadDialog";

interface BatchSelectionValue {
  selectMode: boolean;
  isSelected: (aid: string) => boolean;
  toggle: (aid: string) => void;
}

const BatchSelectionContext = createContext<BatchSelectionValue | null>(null);

/** 卡片/网格内部消费:返回 null 表示当前不在多选容器内 */
export function useJmBatchSelection(): BatchSelectionValue | null {
  return useContext(BatchSelectionContext);
}

export function JmBatchSelectionProvider({
  comics,
  children,
  hint = "多选下载",
}: {
  comics: JmComicItem[];
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
      const alive = new Set(comics.map((c) => c.aid));
      const next = new Set([...prev].filter((aid) => alive.has(aid)));
      return next.size === prev.size ? prev : next;
    });
  }, [comics]);

  const value = useMemo<BatchSelectionValue>(
    () => ({
      selectMode,
      isSelected: (aid: string) => selected.has(aid),
      toggle: (aid: string) =>
        setSelected((prev) => {
          const next = new Set(prev);
          if (next.has(aid)) next.delete(aid);
          else next.add(aid);
          return next;
        }),
    }),
    [selectMode, selected]
  );

  const selectedComics = useMemo<JmDownloadBatchItem[]>(
    () =>
      comics
        .filter((c) => selected.has(c.aid))
        .map((c) => ({ aid: c.aid, title: c.title, author: c.author })),
    [comics, selected]
  );

  const allSelected = comics.length > 0 && selected.size === comics.length;

  const exitSelectMode = () => {
    setSelectMode(false);
    setSelected(new Set());
  };

  return (
    <BatchSelectionContext.Provider value={value}>
      {/* 多选开关:未进入多选时仅一个紧凑按钮;进入后变为操作条 */}
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
                  setSelected(allSelected ? new Set() : new Set(comics.map((c) => c.aid)))
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

      <JmDownloadDialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        batch={selectedComics}
      />
    </BatchSelectionContext.Provider>
  );
}
