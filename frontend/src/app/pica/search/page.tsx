"use client";

/**
 * 哔咔在线源 · 搜索页
 * - 关键词搜索(advanced-search)+ 分类过滤(多选)+ 排序(最新/最早/爱心/观看)
 * - 热词(keywords)点击直接搜索;分类清单(categories)下拉加载
 * - URL 参数初始化:?keyword=&c=&s=&page=
 */

import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Compass, Loader2, Search, X } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { PicaErrorCard, PicaGate } from "@/components/pica/PicaGate";
import { PicaComicGrid } from "@/components/pica/ComicGrid";
import { PicaBatchSelectionProvider } from "@/components/pica/download/BatchDownload";
import {
  isPicaApiError,
  picaCategories,
  picaComics,
  picaKeywords,
  picaSearch,
} from "@/lib/pica/client";
import type { PicaCategory, PicaComicItem } from "@/lib/pica/types";

const SORTS = [
  { value: "dd", label: "最新" },
  { value: "da", label: "最早" },
  { value: "ld", label: "爱心多" },
  { value: "vd", label: "观看多" },
] as const;
type SortKey = (typeof SORTS)[number]["value"];

export default function PicaSearchPage() {
  return (
    <>
      <PageHeader
        title="哔咔搜索"
        description="关键词 · 分类 · 排序"
        icon={Compass}
        width="management"
      />
      <PageContent width="management">
        <PicaGate>
          <SearchContent />
        </PicaGate>
      </PageContent>
    </>
  );
}

function SearchContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialKeyword = searchParams.get("keyword") ?? "";
  const initialCats = (searchParams.get("c") ?? "").split(",").filter(Boolean);
  const initialSort = (searchParams.get("s") as SortKey) || "dd";
  const initialPage = Math.max(1, Number.parseInt(searchParams.get("page") ?? "1", 10) || 1);

  const [keyword, setKeyword] = useState(initialKeyword);
  const [sort, setSort] = useState<SortKey>(SORTS.some((s) => s.value === initialSort) ? initialSort : "dd");
  const [cats, setCats] = useState<string[]>(initialCats);

  const [hotWords, setHotWords] = useState<string[]>([]);
  const [categories, setCategories] = useState<PicaCategory[]>([]);

  const [items, setItems] = useState<PicaComicItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(initialPage);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [searched, setSearched] = useState(false);

  // 热词与分类清单(进入页面加载一次)
  useEffect(() => {
    let cancelled = false;
    picaKeywords()
      .then((res) => {
        if (!cancelled) setHotWords(res.list ?? []);
      })
      .catch(() => {});
    picaCategories()
      .then((res) => {
        if (!cancelled) setCategories((res.list ?? []).filter((c) => !c.isWeb && c.title));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  // 执行搜索:关键词优先;无关键词但有分类 → 分类浏览
  const runSearch = useCallback(
    async (kw: string, catList: string[], sortKey: SortKey, targetPage: number) => {
      setLoading(true);
      setError(null);
      setSearched(true);
      try {
        let res;
        if (kw.trim()) {
          res = await picaSearch(kw.trim(), targetPage, { categories: catList, sort: sortKey });
        } else if (catList.length > 0) {
          res = await picaComics(catList[0], sortKey, targetPage);
        } else {
          setItems([]);
          setTotal(0);
          setHasNext(false);
          setLoading(false);
          return;
        }
        setItems(res.list ?? []);
        setTotal(res.total ?? 0);
        setHasNext(res.hasNext);
        setPage(res.page ?? targetPage);
      } catch (err) {
        setError(err);
      } finally {
        setLoading(false);
      }
    },
    []
  );

  // URL 参数变化驱动搜索(前进/后退/首次)
  useEffect(() => {
    const kw = searchParams.get("keyword") ?? "";
    const c = (searchParams.get("c") ?? "").split(",").filter(Boolean);
    const s = (searchParams.get("s") as SortKey) || "dd";
    const p = Math.max(1, Number.parseInt(searchParams.get("page") ?? "1", 10) || 1);
    if (kw.trim() || c.length > 0) {
      setKeyword(kw);
      setCats(c);
      setSort(SORTS.some((x) => x.value === s) ? s : "dd");
      void runSearch(kw, c, s, p);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams]);

  const pushParams = useCallback(
    (kw: string, catList: string[], sortKey: SortKey, targetPage: number) => {
      const params = new URLSearchParams();
      if (kw.trim()) params.set("keyword", kw.trim());
      if (catList.length > 0) params.set("c", catList.join(","));
      if (sortKey !== "dd") params.set("s", sortKey);
      if (targetPage > 1) params.set("page", String(targetPage));
      setSearchParams(params);
    },
    [setSearchParams]
  );

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    pushParams(keyword, cats, sort, 1);
  };

  const toggleCat = (title: string) => {
    const next = cats.includes(title) ? cats.filter((c) => c !== title) : [...cats, title];
    setCats(next);
    pushParams(keyword, next, sort, 1);
  };

  const changeSort = (key: SortKey) => {
    setSort(key);
    pushParams(keyword, cats, key, 1);
  };

  const changePage = (p: number) => {
    pushParams(keyword, cats, sort, p);
    window.scrollTo({ top: 0 });
  };

  return (
    <div>
      {/* 搜索表单 */}
      <form onSubmit={handleSubmit} className="mb-4 flex gap-2">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/60" />
          <input
            type="text"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索哔咔漫画(关键词)"
            spellCheck={false}
            className="h-10 w-full rounded-lg border border-border bg-card pl-9 pr-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted/60 focus:border-accent"
          />
        </div>
        <button
          type="submit"
          className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg bg-accent px-4 text-sm font-medium text-white transition-opacity hover:opacity-90"
        >
          <Search className="h-4 w-4" />
          搜索
        </button>
      </form>

      {/* 热词 */}
      {hotWords.length > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted">热词:</span>
          {hotWords.slice(0, 12).map((w) => (
            <button
              key={w}
              type="button"
              onClick={() => {
                setKeyword(w);
                pushParams(w, cats, sort, 1);
              }}
              className="rounded-full border border-border/40 bg-card px-2.5 py-1 text-xs text-muted transition-colors hover:border-accent/50 hover:text-foreground"
            >
              {w}
            </button>
          ))}
        </div>
      )}

      {/* 排序 */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted">排序:</span>
        {SORTS.map((s) => (
          <button
            key={s.value}
            type="button"
            onClick={() => changeSort(s.value)}
            className={`rounded-full px-3 py-1 text-xs font-medium transition-all ${
              sort === s.value
                ? "bg-accent text-white"
                : "border border-border/40 bg-card text-muted hover:border-border hover:text-foreground"
            }`}
          >
            {s.label}
          </button>
        ))}
      </div>

      {/* 分类过滤(多选;选中第一个作为浏览分类) */}
      {categories.length > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted">分类:</span>
          {cats.map((c) => (
            <button
              key={c}
              type="button"
              onClick={() => toggleCat(c)}
              className="inline-flex items-center gap-1 rounded-full bg-accent px-2.5 py-1 text-xs font-medium text-white"
              title="点击移除"
            >
              {c}
              <X className="h-3 w-3" />
            </button>
          ))}
          <select
            value=""
            onChange={(e) => {
              if (e.target.value) toggleCat(e.target.value);
            }}
            className="rounded-full border border-border/40 bg-card px-2.5 py-1 text-xs text-muted outline-none transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <option value="">+ 添加分类</option>
            {categories
              .filter((c) => !cats.includes(c.title))
              .map((c) => (
                <option key={c.title} value={c.title}>
                  {c.title}
                </option>
              ))}
          </select>
        </div>
      )}

      {/* 结果 */}
      {loading ? (
        <div className="flex items-center justify-center py-20 text-muted">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      ) : error ? (
        <PicaErrorCard
          error={error}
          onRetry={() => void runSearch(keyword, cats, sort, page)}
        />
      ) : !searched ? (
        <p className="py-16 text-center text-sm text-muted">输入关键词或选择分类开始浏览</p>
      ) : (
        <>
          <p className="mb-3 text-xs text-muted">
            共 {total} 条结果{total > 0 ? ` · 第 ${page} 页` : ""}
          </p>
          <PicaBatchSelectionProvider comics={items}>
            <PicaComicGrid comics={items} emptyText="没有找到相关漫画" />
          </PicaBatchSelectionProvider>
          {(hasNext || page > 1) && (
            <div className="mt-6 flex items-center justify-center gap-3">
              <button
                type="button"
                disabled={page <= 1}
                onClick={() => changePage(page - 1)}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
              >
                上一页
              </button>
              <span className="text-xs text-muted">第 {page} 页</span>
              <button
                type="button"
                disabled={!hasNext}
                onClick={() => changePage(page + 1)}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
              >
                下一页
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
