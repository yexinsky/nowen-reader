"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown, Search } from "lucide-react";

export interface SearchableSelectOption {
  value: number;
  label: string;
  /** 次要信息(如数量),展示在标签名右侧 */
  hint?: string;
}

const PANEL_MARGIN = 4;
// 搜索行(约 41px)+ 面板边框,用于翻转与最大高度估算
const SEARCH_ROW_HEIGHT = 45;
const LIST_MAX_HEIGHT = 240;

interface PanelPos {
  left: number;
  width: number;
  /** 向上翻转时用 bottom 锚定,否则用 top */
  top?: number;
  bottom?: number;
  listMaxH: number;
}

/**
 * 可搜索下拉(受控):输入关键词过滤选项,支持 ↑/↓/Enter/Esc 键盘操作与点击外部关闭。
 * 面板通过 portal 渲染到 body 并用 fixed 定位,避免被页面的 overflow 容器裁剪;
 * 空间不足时自动向上翻转,滚动/缩放时重新定位。无 UI 库依赖,样式对齐表单原生 select。
 */
export function SearchableSelect({
  value,
  onChange,
  options,
  placeholder,
  searchPlaceholder,
  noMatchText,
  className = "",
}: {
  value: number;
  onChange: (value: number) => void;
  options: SearchableSelectOption[];
  placeholder?: string;
  searchPlaceholder?: string;
  noMatchText?: string;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [highlight, setHighlight] = useState(0);
  const [pos, setPos] = useState<PanelPos | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);

  const selected = options.find((o) => o.value === value);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter((o) => o.label.toLowerCase().includes(q));
  }, [options, query]);

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const vh = window.innerHeight;
    const spaceBelow = vh - rect.bottom - PANEL_MARGIN * 2;
    const spaceAbove = rect.top - PANEL_MARGIN * 2;
    const estH = SEARCH_ROW_HEIGHT + LIST_MAX_HEIGHT;
    const upward = spaceBelow < Math.min(estH, spaceAbove) && spaceAbove > spaceBelow;
    const listMaxH = Math.max(
      120,
      Math.min(LIST_MAX_HEIGHT, (upward ? spaceAbove : spaceBelow) - SEARCH_ROW_HEIGHT)
    );
    setPos((prev) => {
      const next: PanelPos = {
        left: rect.left,
        width: rect.width,
        top: upward ? undefined : rect.bottom + PANEL_MARGIN,
        bottom: upward ? vh - rect.top + PANEL_MARGIN : undefined,
        listMaxH,
      };
      if (
        prev &&
        prev.left === next.left &&
        prev.width === next.width &&
        prev.top === next.top &&
        prev.bottom === next.bottom &&
        prev.listMaxH === next.listMaxH
      ) {
        return prev;
      }
      return next;
    });
  }, []);

  useEffect(() => {
    if (!open) return;
    updatePosition();
    // capture 捕获任意容器的滚动(触发器移动后需要重新定位);面板内部滚动不影响位置
    const onScroll = (e: Event) => {
      if (panelRef.current && e.target instanceof Node && panelRef.current.contains(e.target)) {
        return;
      }
      updatePosition();
    };
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", updatePosition);
    return () => {
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", updatePosition);
    };
  }, [open, updatePosition]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (rootRef.current?.contains(target)) return;
      if (panelRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setHighlight(0);
      inputRef.current?.focus();
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const el = listRef.current?.children[highlight] as HTMLElement | undefined;
    el?.scrollIntoView({ block: "nearest" });
  }, [highlight, open, filtered.length]);

  const moveHighlight = (delta: number) => {
    setHighlight((prev) => {
      if (filtered.length === 0) return 0;
      return (prev + delta + filtered.length) % filtered.length;
    });
  };

  const pick = (v: number) => {
    onChange(v);
    setOpen(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      moveHighlight(1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      moveHighlight(-1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const opt = filtered[highlight];
      if (opt) pick(opt.value);
    } else if (e.key === "Escape") {
      e.preventDefault();
      setOpen(false);
    }
  };

  return (
    <div ref={rootRef} className={`relative ${className}`}>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 rounded-lg border border-border/50 bg-background px-3 py-2 text-left text-sm text-foreground outline-none transition-colors hover:border-border focus:border-accent/50"
      >
        {selected ? (
          <span className="flex min-w-0 items-baseline gap-1.5">
            <span className="truncate">{selected.label}</span>
            {selected.hint && <span className="shrink-0 text-xs text-muted">{selected.hint}</span>}
          </span>
        ) : (
          <span className="min-w-0 truncate text-muted/60">{placeholder}</span>
        )}
        <ChevronDown
          className={`h-3.5 w-3.5 shrink-0 text-muted transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>

      {open &&
        pos &&
        createPortal(
          <div
            ref={panelRef}
            style={{
              position: "fixed",
              left: pos.left,
              width: pos.width,
              top: pos.top,
              bottom: pos.bottom,
              zIndex: 50,
            }}
            className="overflow-hidden rounded-lg border border-border/50 bg-card shadow-lg"
          >
            <div className="flex items-center gap-2 border-b border-border/40 px-3 py-2">
              <Search className="h-3.5 w-3.5 shrink-0 text-muted" />
              <input
                ref={inputRef}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setHighlight(0);
                }}
                onKeyDown={handleKeyDown}
                placeholder={searchPlaceholder}
                className="w-full bg-transparent text-sm text-foreground placeholder-muted/50 outline-none"
              />
            </div>
            <ul
              ref={listRef}
              role="listbox"
              style={{ maxHeight: pos.listMaxH }}
              className="overflow-y-auto py-1"
            >
              {filtered.length === 0 ? (
                <li className="px-3 py-2 text-xs text-muted">{noMatchText}</li>
              ) : (
                filtered.map((o, i) => (
                  <li key={o.value}>
                    <button
                      type="button"
                      role="option"
                      aria-selected={o.value === value}
                      onClick={() => pick(o.value)}
                      onMouseEnter={() => setHighlight(i)}
                      className={`flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-sm transition-colors ${
                        i === highlight ? "bg-accent/10 text-accent" : "text-foreground"
                      }`}
                    >
                      <span className="min-w-0 truncate">{o.label}</span>
                      <span className="flex shrink-0 items-center gap-1.5">
                        {o.hint && <span className="text-xs text-muted">{o.hint}</span>}
                        {o.value === value && <Check className="h-3.5 w-3.5 text-accent" />}
                      </span>
                    </button>
                  </li>
                ))
              )}
            </ul>
          </div>,
          document.body
        )}
    </div>
  );
}
