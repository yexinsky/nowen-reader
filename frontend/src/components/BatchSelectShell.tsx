"use client";

/**
 * 通用批量选择壳(列表页复用)
 *
 * 定位:与 jm/download/BatchDownload.tsx、pica/download/BatchDownload.tsx 互补 ——
 * 那两个是「卡片网格」专用(卡片通过 context 感知多选,点封面即选中,还需要封面勾选角标);
 * 本组件面向**没有卡片网格的列表**(典型是阅读历史那种一行一条的列表),
 * 由调用方决定每一行怎么画选中态,因此用 render-prop 而不是 context。
 *
 * 为什么用 fixed 操作条:页面链路上 AppShell 与路由容器都带 `overflow-x-hidden`,
 * 会把祖先变成滚动容器,`position: sticky` 的吸附基准失效 —— 列表一滚,
 * 含「批量下载」的操作条就跟着滚出可视区。详见 components/BatchActionBar.tsx。
 */

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Check, CheckSquare, Download, Square, X } from "lucide-react";
import { BatchActionBar, BatchActionBarSpacer } from "@/components/BatchActionBar";

export interface BatchSelectItem {
  /** 唯一标识(下载对话框按它取漫画) */
  id: string;
  title: string;
  author?: string;
}

export interface BatchSelectState {
  selectMode: boolean;
  isSelected: (id: string) => boolean;
  toggle: (id: string) => void;
}

export function BatchSelectShell({
  items,
  children,
  onDownload,
  hint = "多选下载",
}: {
  /** 本页可用于批量下载的全部条目 */
  items: BatchSelectItem[];
  /** 行渲染;在 selectMode 下调用方应让整行点击 = toggle 而不是跳转 */
  children: (state: BatchSelectState) => ReactNode;
  /** 点「下载(N)」:拿到被选中的条目,由调用方打开自己的下载对话框 */
  onDownload: (picked: BatchSelectItem[]) => void;
  hint?: string;
}) {
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // 列表变化(翻页/清空/刷新)时丢弃已不在列表中的选择,避免误下已消失的条目
  useEffect(() => {
    setSelected((prev) => {
      if (prev.size === 0) return prev;
      const alive = new Set(items.map((it) => it.id));
      const next = new Set([...prev].filter((id) => alive.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [items]);

  const state = useMemo<BatchSelectState>(
    () => ({
      selectMode,
      isSelected: (id: string) => selected.has(id),
      toggle: (id: string) =>
        setSelected((prev) => {
          const next = new Set(prev);
          if (next.has(id)) next.delete(id);
          else next.add(id);
          return next;
        }),
    }),
    [selectMode, selected]
  );

  const allSelected = items.length > 0 && selected.size === items.length;
  const exit = () => {
    setSelectMode(false);
    setSelected(new Set());
  };

  return (
    <>
      {/* 未进入多选:列表顶部一个紧凑入口 */}
      {items.length > 0 && !selectMode && (
        <div className="mb-2 flex flex-wrap items-center justify-end gap-2">
          <button
            type="button"
            onClick={() => setSelectMode(true)}
            className="inline-flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <CheckSquare className="h-3.5 w-3.5" />
            {hint}
          </button>
        </div>
      )}

      {children(state)}

      {selectMode && (
        <>
          <BatchActionBarSpacer />
          <BatchActionBar>
            {/* 单行不换行:手机 390px 下四个按钮换行会把操作条顶成两行,压住列表 */}
            <span className="shrink-0 text-xs text-muted">
              已选 <span className="font-medium text-accent">{selected.size}</span>/{items.length}
            </span>
            <button
              type="button"
              onClick={() =>
                setSelected(allSelected ? new Set() : new Set(items.map((it) => it.id)))
              }
              className="ml-auto inline-flex shrink-0 items-center gap-1 rounded-lg border border-border px-2 py-1.5 text-xs text-muted transition-colors hover:border-accent/50 hover:text-foreground"
            >
              {allSelected ? <Square className="h-3.5 w-3.5" /> : <CheckSquare className="h-3.5 w-3.5" />}
              <span className="hidden sm:inline">{allSelected ? "全不选" : "全选本页"}</span>
              <span className="sm:hidden">{allSelected ? "全不选" : "全选"}</span>
            </button>
            <button
              type="button"
              disabled={selected.size === 0}
              onClick={() => onDownload(items.filter((it) => selected.has(it.id)))}
              className="inline-flex shrink-0 items-center gap-1 rounded-lg bg-accent px-2.5 py-1.5 text-xs font-medium text-white shadow-sm transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Download className="h-3.5 w-3.5" />
              下载({selected.size})
            </button>
            <button
              type="button"
              onClick={exit}
              aria-label="退出多选"
              className="inline-flex shrink-0 items-center justify-center rounded-lg border border-border px-2 py-1.5 text-xs text-muted transition-colors hover:text-foreground"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </BatchActionBar>
        </>
      )}
    </>
  );
}

/** 多选模式下每行左侧的勾选圆点(列表行复用,统一观感) */
export function BatchSelectMark({ checked }: { checked: boolean }) {
  return (
    <span
      aria-hidden
      className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-md border transition-colors ${
        checked ? "border-accent bg-accent text-white" : "border-border bg-background text-transparent"
      }`}
    >
      <Check className="h-3.5 w-3.5" />
    </span>
  );
}
