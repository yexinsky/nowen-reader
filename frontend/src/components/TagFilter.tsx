"use client";

import { apiPath } from "@/lib/base-path";
import { useState, useCallback, useMemo, useRef } from "react";
import { Tag, ChevronRight, Languages, Search, X } from "lucide-react";
import { useTranslation, useLocale } from "@/lib/i18n";

interface TagFilterProps {
  allTags: string[];
  selectedTags: string[];
  onTagToggle: (tag: string) => void;
  onClearAll: () => void;
  onTagsTranslated?: () => void;
}

const tagColorMap: Record<string, string> = {
  Action: "border-red-500/30 text-red-400 hover:bg-red-500/10",
  Romance: "border-pink-500/30 text-pink-400 hover:bg-pink-500/10",
  Comedy: "border-amber-500/30 text-amber-400 hover:bg-amber-500/10",
  Fantasy: "border-indigo-500/30 text-indigo-400 hover:bg-indigo-500/10",
  Horror: "border-purple-500/30 text-purple-400 hover:bg-purple-500/10",
  "Sci-Fi": "border-cyan-500/30 text-cyan-400 hover:bg-cyan-500/10",
  Drama: "border-green-500/30 text-green-400 hover:bg-green-500/10",
  "Slice of Life": "border-orange-500/30 text-orange-400 hover:bg-orange-500/10",
  Adventure: "border-blue-500/30 text-blue-400 hover:bg-blue-500/10",
  Mystery: "border-rose-500/30 text-rose-400 hover:bg-rose-500/10",
};

const tagActiveColorMap: Record<string, string> = {
  Action: "bg-red-500/20 border-red-500/50 text-red-600 dark:text-red-300",
  Romance: "bg-pink-500/20 border-pink-500/50 text-pink-600 dark:text-pink-300",
  Comedy: "bg-amber-500/20 border-amber-500/50 text-amber-600 dark:text-amber-300",
  Fantasy: "bg-indigo-500/20 border-indigo-500/50 text-indigo-600 dark:text-indigo-300",
  Horror: "bg-purple-500/20 border-purple-500/50 text-purple-600 dark:text-purple-300",
  "Sci-Fi": "bg-cyan-500/20 border-cyan-500/50 text-cyan-600 dark:text-cyan-300",
  Drama: "bg-green-500/20 border-green-500/50 text-green-600 dark:text-green-300",
  "Slice of Life": "bg-orange-500/20 border-orange-500/50 text-orange-600 dark:text-orange-300",
  Adventure: "bg-blue-500/20 border-blue-500/50 text-blue-600 dark:text-blue-300",
  Mystery: "bg-rose-500/20 border-rose-500/50 text-rose-600 dark:text-rose-300",
};

// 超过该数量的标签默认折叠，展开后进入大面板模式
const FOLD_THRESHOLD = 10;
// 标签总数超过该值时，面板内显示搜索框
const SEARCH_THRESHOLD = 20;
// 面板内最多渲染的标签数（其余靠搜索过滤，避免几千个按钮卡顿）
const RENDER_LIMIT = 600;

export default function TagFilter({
  allTags,
  selectedTags,
  onTagToggle,
  onClearAll,
  onTagsTranslated,
}: TagFilterProps) {
  const t = useTranslation();
  const { locale } = useLocale();
  const [translating, setTranslating] = useState(false);
  const [collapsed, setCollapsed] = useState(true);
  const [query, setQuery] = useState("");
  const searchInputRef = useRef<HTMLInputElement>(null);

  const expanded = !collapsed;
  const showSearch = allTags.length > SEARCH_THRESHOLD;

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      if (!prev) setQuery("");
      return !prev;
    });
  };

  const handleTranslate = useCallback(async () => {
    if (translating) return;
    setTranslating(true);
    try {
      const res = await fetch(apiPath("/api/tags/translate"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ targetLang: locale }),
      });
      if (res.ok) {
        const data = await res.json();
        if (data.success) {
          // renamed > 0 表示有标签被实际重命名，需要清除选中状态（因为旧标签名已不存在）
          if (data.renamed > 0) {
            onClearAll();
          }
          // 无论是否有实际重命名，都刷新标签列表以确保显示最新数据
          onTagsTranslated?.();
        }
      }
    } catch {
      // ignore
    } finally {
      setTranslating(false);
    }
  }, [translating, locale, onClearAll, onTagsTranslated]);

  // 面板内的标签列表：已选置顶 + 按关键词过滤 + 渲染数量上限
  const { restTags, hiddenCount } = useMemo(() => {
    const selectedSet = new Set(selectedTags);
    const q = query.trim().toLowerCase();
    const matches = (tag: string) => !q || tag.toLowerCase().includes(q);
    const rest = allTags.filter((tag) => !selectedSet.has(tag) && matches(tag));
    return {
      restTags: rest.slice(0, RENDER_LIMIT),
      hiddenCount: Math.max(0, rest.length - RENDER_LIMIT),
    };
  }, [allTags, selectedTags, query]);

  const renderTagButton = (tag: string, active: boolean) => (
    <button
      key={tag}
      onClick={() => onTagToggle(tag)}
      className={`shrink-0 rounded-lg border px-3 py-1.5 text-xs font-medium transition-all duration-200 whitespace-nowrap ${
        active
          ? tagActiveColorMap[tag] || "bg-accent/15 border-accent/40 text-accent"
          : tagColorMap[tag] || "border-border/60 text-muted hover:text-foreground hover:border-border"
      }`}
    >
      {active ? `${tag} ✓` : tag}
    </button>
  );

  return (
    <div className="relative">
      <div className={`flex gap-2 flex-col sm:flex-row ${expanded ? "sm:items-start" : "sm:items-center"}`}>
        {/* Label + Translate + Fold Toggle */}
        <div className="flex items-center gap-1.5 shrink-0 pt-0.5">
          <div className="flex items-center gap-1.5 text-muted">
            <Tag className="h-3.5 w-3.5" />
            <span className="text-xs font-medium whitespace-nowrap">{t.tagFilter.label}</span>
          </div>
          {allTags.length > 0 && (
            <button
              onClick={handleTranslate}
              disabled={translating}
              className="flex h-6 items-center gap-1 rounded-md border border-border/40 bg-card/50 px-1.5 text-[10px] font-medium text-muted transition-all hover:text-foreground hover:border-border disabled:opacity-50 disabled:pointer-events-none"
              title={t.tagFilter.translate}
            >
              <Languages className="h-3 w-3" />
              <span>{translating ? t.tagFilter.translating : t.tagFilter.translate}</span>
            </button>
          )}
          {/* 折叠/展开切换 */}
          {allTags.length > FOLD_THRESHOLD && (
            <button
              onClick={toggleCollapsed}
              className="flex h-6 items-center gap-0.5 rounded-md border border-border/40 bg-card/50 px-1.5 text-[10px] font-medium text-muted transition-all hover:text-foreground hover:border-border"
            >
              <ChevronRight className={`h-3 w-3 transition-transform ${collapsed ? "" : "rotate-90"}`} />
              <span>{collapsed ? `${allTags.length}` : t.common.collapse}</span>
            </button>
          )}
        </div>

        {allTags.length === 0 ? (
          /* 无标签时：显示空状态提示 */
          <span className="text-[11px] text-muted/60 italic">
              {t.tagFilter.empty || "暂无标签"}
            </span>
        ) : collapsed && allTags.length > FOLD_THRESHOLD ? (
          /* 折叠时：仅显示已选中标签 + 数量提示 */
          <div className="flex items-center gap-2 flex-wrap">
            {selectedTags.length > 0 ? (
              selectedTags.map((tag) => (
                <button
                  key={tag}
                  onClick={() => onTagToggle(tag)}
                  className={`shrink-0 rounded-lg border px-3 py-1.5 text-xs font-medium transition-all duration-200 whitespace-nowrap ${
                    tagActiveColorMap[tag] || "bg-accent/15 border-accent/40 text-accent"
                  }`}
                >
                  {tag} ×
                </button>
              ))
            ) : (
              <span className="text-xs text-muted">
                {allTags.length} {t.tagFilter.label}
              </span>
            )}
          </div>
        ) : (
          /* 展开面板：多行换行 + 搜索过滤 + 大高度滚动区，适配大量标签 */
          <div className="flex-1 min-w-0 rounded-xl border border-border/40 bg-card/30 p-2.5 space-y-2">
            {/* 面板头部：全部 / 搜索 / （移动端收起） */}
            <div className="flex items-center gap-2 flex-wrap">
              <button
                onClick={onClearAll}
                className={`shrink-0 rounded-lg border px-3 py-1.5 text-xs font-medium transition-all duration-200 ${
                  selectedTags.length === 0
                    ? "bg-accent/20 border-accent/50 text-accent"
                    : "border-border/60 text-muted hover:text-foreground hover:border-border"
                }`}
              >
                {t.common.all}
              </button>

              {showSearch && (
                <div className="relative min-w-0 flex-1 sm:max-w-xs">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted/60" />
                  <input
                    ref={searchInputRef}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder={t.tagFilter.searchPlaceholder}
                    className="h-8 w-full rounded-lg border border-border/40 bg-background/60 pl-8 pr-7 text-xs text-foreground placeholder:text-muted/50 outline-none transition-colors focus:border-accent/50"
                  />
                  {query && (
                    <button
                      onClick={() => { setQuery(""); searchInputRef.current?.focus(); }}
                      className="absolute right-1.5 top-1/2 -translate-y-1/2 flex h-5 w-5 items-center justify-center rounded-full text-muted/60 hover:text-foreground"
                      aria-label={t.tagFilter.clearSearch}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  )}
                </div>
              )}

              {selectedTags.length > 0 && (
                <span className="shrink-0 text-[11px] text-muted">
                  {t.tagFilter.selectedGroup} {selectedTags.length}
                </span>
              )}

              {/* 小屏时头部收起按钮（左侧切换按钮在小屏可能换行到看不见的位置） */}
              {allTags.length > FOLD_THRESHOLD && (
                <button
                  onClick={toggleCollapsed}
                  className="ml-auto flex h-6 items-center gap-0.5 rounded-md border border-border/40 bg-card/50 px-1.5 text-[10px] font-medium text-muted transition-all hover:text-foreground hover:border-border sm:hidden"
                >
                  <ChevronRight className="h-3 w-3 rotate-90" />
                  <span>{t.common.collapse}</span>
                </button>
              )}
            </div>

            {/* 已选标签置顶区：始终展示全部选中标签，滚动/搜索时也不会丢失当前筛选 */}
            {selectedTags.length > 0 && (
              <div className="flex flex-wrap items-center gap-2 rounded-lg bg-accent/5 border border-accent/20 p-1.5">
                {selectedTags.map((tag) => renderTagButton(tag, true))}
              </div>
            )}

            {/* 标签滚动区 */}
            <div
              className="flex flex-wrap items-start gap-2 overflow-y-auto overscroll-contain p-0.5 max-h-[min(55vh,440px)]"
              style={{ scrollbarWidth: "thin" }}
            >
              {restTags.length === 0 && selectedTags.length === 0 ? (
                <span className="py-3 text-xs text-muted/60 italic">{t.tagFilter.noMatch}</span>
              ) : (
                restTags.map((tag) => renderTagButton(tag, false))
              )}

              {hiddenCount > 0 && (
                <span className="w-full py-1 text-center text-[11px] text-muted/60">
                  {t.tagFilter.moreHidden.replace("{n}", String(restTags.length))}
                </span>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
