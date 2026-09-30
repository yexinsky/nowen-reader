"use client";

/**
 * JM 在线源 · 搜索(关键字 / 分类 / 排行)
 * PRD docs/PRD_JM_SOURCE.md M5,#13 GET /api/comics/search
 *
 * - 关键字模式:keyword + searchType(全站/作品/作者/标签/角色)+ 年份/月份过滤;
 *   searchType=site 等价缺省不传;m 仅与 y 同传
 * - 分类浏览模式:无关键字时按 mainCategory + sort 浏览(与关键字互斥,由上游路由规则决定)
 * - 排序:最新 mr(默认)/ 最多点击 mv / 月·周·日排行 mv_m/mv_w/mv_t / 最多图片 mp / 最多爱心 tf
 * - 上游怪癖(如实遵循):
 *   1. 日排行 mv_t 带分类时上游返回空 → 自动取消分类并提示;
 *   2. 页码封顶 120 → 达到后提示「仅展示前 120 页结果」并停止加载。
 * - 分页:「加载更多」按钮 + 触底自动加载(hasNext 才显示);换条件搜索重置到第 1 页
 * - 在途请求以 reqRef 代际守卫,重查/卸载后丢弃过期响应(与在线首页同模式)
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, Loader2, Search } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmComicGrid } from "@/components/jm/ComicGrid";
import { isJmApiError, jmSearch } from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
import type {
  JmComicItem,
  JmSearchParams,
  JmSearchSort,
  JmSearchType,
} from "@/lib/jm/types";

/** 上游页码封顶:达 120 页停止加载并提示 */
const MAX_PAGE = 120;

/** 排行类排序:上游 /categories/filter 路径会忽略关键字,仅限分类浏览模式(对齐 JM 官方前端) */
const RANK_SORTS: readonly string[] = ["mv_m", "mv_w", "mv_t"];

/** 上游实测 2017 年起年份过滤生效(对齐 JM 官方前端) */
const MIN_SEARCH_YEAR = 2017;
const YEAR_OPTIONS: number[] = (() => {
  const current = new Date().getFullYear();
  return Array.from({ length: current - MIN_SEARCH_YEAR + 1 }, (_, i) => current - i);
})();

const SORT_OPTIONS: { value: JmSearchSort; label: string; rankOnly?: boolean }[] = [
  { value: "mr", label: "最新" },
  { value: "mv", label: "最多点击" },
  { value: "mv_m", label: "月排行", rankOnly: true },
  { value: "mv_w", label: "周排行", rankOnly: true },
  { value: "mv_t", label: "日排行", rankOnly: true },
  { value: "mp", label: "最多图片" },
  { value: "tf", label: "最多爱心" },
];

const SEARCH_TYPES: { value: JmSearchType; label: string }[] = [
  { value: "site", label: "全站" },
  { value: "work", label: "作品" },
  { value: "author", label: "作者" },
  { value: "tag", label: "标签" },
  { value: "character", label: "角色" },
];

/** 主分类(无关键字时的浏览模式);"" = 全部,不传参数 */
const MAIN_CATEGORIES: { value: string; label: string }[] = [
  { value: "", label: "全部" },
  { value: "doujin", label: "同人志" },
  { value: "single", label: "单本" },
  { value: "short", label: "短篇" },
  { value: "another", label: "其他" },
  { value: "hanman", label: "韩漫" },
  { value: "meiman", label: "美漫" },
  { value: "another_cosplay", label: "同人Cosplay" },
  { value: "3D", label: "3D" },
];

/** 表单态(输入原样保存,提交时归一为查询条件) */
interface SearchForm {
  keyword: string;
  sort: JmSearchSort;
  searchType: JmSearchType;
  /** 年/月输入框原始文本,空串 = 不过滤 */
  y: string;
  m: string;
  mainCategory: string;
}

/** 提交态(实际发起请求的条件快照) */
interface SearchConditions {
  keyword: string;
  sort: JmSearchSort;
  searchType: JmSearchType;
  y?: number;
  m?: number;
  mainCategory: string;
}

const INITIAL_FORM: SearchForm = {
  keyword: "",
  sort: "mr",
  searchType: "site",
  y: "",
  m: "",
  mainCategory: "",
};

/** 解析 1..max 的整数;空/非法/越界一律视为未设置 */
function parseRangeInt(text: string, max: number): number | undefined {
  const t = text.trim();
  if (!t) return undefined;
  const n = Number(t);
  if (!Number.isInteger(n) || n < 1 || n > max) return undefined;
  return n;
}

/**
 * 表单 → 查询条件。
 * 怪癖:①日排行(mv_t)带分类时上游返回空,自动取消分类并要求提示;
 * ②月/周/日排行走分类筛选路径会忽略关键字,关键字模式下降级为最新并提示。
 */
function toConditions(form: SearchForm): {
  conditions: SearchConditions;
  notice: string | null;
} {
  const keyword = form.keyword.trim();
  let mainCategory = form.mainCategory;
  let sort = form.sort;
  let notice: string | null = null;
  if (form.sort === "mv_t" && !keyword && mainCategory) {
    mainCategory = "";
    notice = "日排行不支持分类筛选,已自动取消分类筛选";
  }
  if (keyword && RANK_SORTS.includes(sort)) {
    sort = "mr";
    notice = "月/周/日排行不支持关键字搜索,已切换为最新排序";
  }
  const y = parseRangeInt(form.y, 9999);
  return {
    conditions: {
      keyword,
      sort,
      searchType: form.searchType,
      y,
      // m 仅与 y 同传
      m: y ? parseRangeInt(form.m, 12) : undefined,
      mainCategory,
    },
    notice,
  };
}

/** 查询条件 → #13 请求参数(空值参数由 client 的 buildQuery 自动省略) */
function buildParams(c: SearchConditions, page: number): JmSearchParams {
  const params: JmSearchParams = { page, sort: c.sort };
  if (c.keyword) {
    params.keyword = c.keyword;
    // searchType 仅关键字模式传;site 等价缺省不传
    if (c.searchType !== "site") params.searchType = c.searchType;
    if (c.y) {
      params.y = c.y;
      if (c.m) params.m = c.m;
    }
  } else if (c.mainCategory) {
    // 分类浏览模式:mainCategory 与关键字互斥(上游路由规则)
    params.mainCategory = c.mainCategory;
  }
  return params;
}

export default function JmSearchPage() {
  return (
    <>
      <PageHeader
        title="搜索在线漫画"
        description="关键字 · 分类 · 排行"
        icon={Search}
        width="management"
        actions={<JmBackButton />}
      />
      <PageContent width="management">
        <JmGate>
          <SearchContent />
        </JmGate>
      </PageContent>
    </>
  );
}

function SearchContent() {
  const [form, setForm] = useState<SearchForm>(INITIAL_FORM);
  // 首屏即以默认条件(全部 + 最新)进入分类浏览模式
  const [submitted, setSubmitted] = useState<SearchConditions>(
    () => toConditions(INITIAL_FORM).conditions
  );
  const [notice, setNotice] = useState<string | null>(null);

  const [items, setItems] = useState<JmComicItem[]>([]);
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState<number | undefined>(undefined);
  const [hasNext, setHasNext] = useState(false);
  const [pageCap, setPageCap] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const reqRef = useRef(0);

  const loadPage = useCallback(
    async (conditions: SearchConditions, nextPage: number) => {
      const req = ++reqRef.current;
      if (nextPage === 1) {
        setLoading(true);
        setError(null);
        setItems([]);
      } else {
        setLoadingMore(true);
      }
      try {
        const data = await jmSearch(buildParams(conditions, nextPage));
        if (reqRef.current !== req) return;
        setPage(nextPage);
        setTotal(data.total);
        // 怪癖:页码达 120 停止加载并提示
        setPageCap(nextPage >= MAX_PAGE);
        setHasNext(nextPage >= MAX_PAGE ? false : data.hasNext);
        setItems((prev) => (nextPage === 1 ? data.list : [...prev, ...data.list]));
        if (nextPage >= MAX_PAGE) setNotice("仅展示前 120 页结果");
      } catch (err) {
        if (reqRef.current !== req) return;
        setError(err);
      } finally {
        if (reqRef.current === req) {
          setLoading(false);
          setLoadingMore(false);
        }
      }
    },
    []
  );

  /** 提交搜索:换条件一律重置到第 1 页 */
  const runSearch = useCallback((next: SearchForm) => {
    const { conditions, notice: n } = toConditions(next);
    // 怪癖:自动改写条件(去分类/排行降级)时同步表单选择框,保持 UI 与实际请求一致
    const corrected: SearchForm = {
      ...next,
      mainCategory: conditions.mainCategory ?? "",
      sort: conditions.sort,
    };
    setForm(corrected);
    setNotice(n);
    setPageCap(false);
    setSubmitted(conditions);
  }, []);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    runSearch(form);
  };

  /** 回车显式提交(与 handleSubmit 同口径,不依赖表单隐式提交) */
  const submitKeyword = useCallback(() => {
    runSearch(form);
  }, [form, runSearch]);

  // 下拉条件变化 → 立即按新条件回到第 1 页
  const updateSort = (sort: JmSearchSort) => runSearch({ ...form, sort });
  const updateSearchType = (searchType: JmSearchType) => runSearch({ ...form, searchType });
  const updateMainCategory = (mainCategory: string) => runSearch({ ...form, mainCategory });

  useEffect(() => {
    loadPage(submitted, 1);
  }, [submitted, loadPage]);

  const canLoadMore = hasNext && !pageCap;
  const loadMore = useCallback(() => {
    if (loading || loadingMore || error || !canLoadMore) return;
    loadPage(submitted, page + 1);
  }, [loading, loadingMore, error, canLoadMore, submitted, page, loadPage]);

  const sentinelRef = useBottomAutoLoad(loadMore, !loading && !error && canLoadMore);

  // 输入框是否有有效关键字:决定范围/年月(关键字模式)与主分类(浏览模式)的可用性
  const keywordMode = form.keyword.trim() !== "";
  const isBrowseMode = submitted.keyword === "";

  if (loading) return <GridSkeleton />;
  if (error && items.length === 0) {
    return (
      <div>
        <SearchFormPanel
          form={form}
          keywordMode={keywordMode}
          onFormChange={setForm}
          onSubmit={handleSubmit}
          onEnter={submitKeyword}
          onSortChange={updateSort}
          onSearchTypeChange={updateSearchType}
          onMainCategoryChange={updateMainCategory}
        />
        <JmErrorCard error={error} onRetry={() => loadPage(submitted, 1)} />
      </div>
    );
  }

  return (
    <div>
      <SearchFormPanel
        form={form}
        keywordMode={keywordMode}
        onFormChange={setForm}
        onSubmit={handleSubmit}
        onEnter={submitKeyword}
        onSortChange={updateSort}
        onSearchTypeChange={updateSearchType}
        onMainCategoryChange={updateMainCategory}
      />

      {/* 上游怪癖提示(自动取消分类 / 页码封顶) */}
      {notice && (
        <div className="mb-3 flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span>{notice}</span>
        </div>
      )}

      {/* 结果计数(total 存在时展示) */}
      {total !== undefined && (
        <div className="mb-3 text-xs text-muted">共 {total.toLocaleString("zh-CN")} 条</div>
      )}

      <JmComicGrid
        comics={items}
        emptyText={
          isBrowseMode ? "该分类下暂无内容" : "没有找到相关漫画,换个关键字或条件试试"
        }
      />

      {items.length > 0 && (
        <div ref={sentinelRef}>
          <LoadMoreFooter
            loadingMore={loadingMore}
            error={error}
            canLoadMore={canLoadMore}
            pageCap={pageCap}
            ended={!hasNext}
            onLoadMore={loadMore}
            onRetry={() => loadPage(submitted, page + 1)}
          />
        </div>
      )}
    </div>
  );
}

/* ── 搜索表单 ── */

const SELECT_CLASS =
  "h-8 rounded-lg border border-border bg-background px-2 text-xs text-foreground outline-none transition-colors focus:border-accent/60 disabled:cursor-not-allowed disabled:opacity-50";

function SearchFormPanel({
  form,
  keywordMode,
  onFormChange,
  onSubmit,
  onEnter,
  onSortChange,
  onSearchTypeChange,
  onMainCategoryChange,
}: {
  form: SearchForm;
  keywordMode: boolean;
  onFormChange: (updater: (prev: SearchForm) => SearchForm) => void;
  onSubmit: (e: React.FormEvent) => void;
  /** 回车显式提交(不依赖表单隐式提交) */
  onEnter: () => void;
  onSortChange: (sort: JmSearchSort) => void;
  onSearchTypeChange: (t: JmSearchType) => void;
  onMainCategoryChange: (c: string) => void;
}) {
  return (
    <form
      onSubmit={onSubmit}
      className="mb-5 space-y-3 rounded-xl border border-border bg-card p-4"
    >
      {/* 关键字 + 搜索按钮(回车触发) */}
      <div className="flex gap-2">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/50" />
          <input
            type="text"
            value={form.keyword}
            onChange={(e) => onFormChange((f) => ({ ...f, keyword: e.target.value }))}
            onKeyDown={(e) => {
              // 显式回车提交:部分 WebView/容器不触发表单隐式提交
              if (e.key === "Enter") {
                e.preventDefault();
                onEnter();
              }
            }}
            placeholder="输入关键字:作品 / 作者 / 标签 / 角色名"
            className="h-10 w-full rounded-lg border border-border bg-background pl-9 pr-3 text-sm text-foreground placeholder:text-muted/50 outline-none transition-colors focus:border-accent/60"
          />
        </div>
        <button
          type="submit"
          className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg bg-accent px-5 text-sm font-medium text-white transition-opacity hover:opacity-90"
        >
          <Search className="h-4 w-4" />
          搜索
        </button>
      </div>

      {/* 条件行:排序 / 范围 / 年月 / 主分类 */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <label className="flex items-center gap-1.5">
          <span className="text-xs text-muted">排序</span>
          <select
            value={form.sort}
            onChange={(e) => onSortChange(e.target.value as JmSearchSort)}
            className={SELECT_CLASS}
          >
            {SORT_OPTIONS.map((o) => (
              <option
                key={o.value}
                value={o.value}
                disabled={o.rankOnly && keywordMode}
                title={o.rankOnly && keywordMode ? "排行不支持关键字搜索,仅分类浏览可用" : undefined}
              >
                {o.rankOnly && keywordMode ? `${o.label}(不可用)` : o.label}
              </option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-1.5">
          <span className="text-xs text-muted">范围</span>
          <select
            value={form.searchType}
            onChange={(e) => onSearchTypeChange(e.target.value as JmSearchType)}
            disabled={!keywordMode}
            title={keywordMode ? undefined : "仅关键字搜索时可用"}
            className={SELECT_CLASS}
          >
            {SEARCH_TYPES.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-1.5">
          <span className="text-xs text-muted">年份</span>
          <select
            value={form.y}
            onChange={(e) =>
              onFormChange((f) => ({ ...f, y: e.target.value, m: e.target.value ? f.m : "" }))
            }
            disabled={!keywordMode}
            title={keywordMode ? undefined : "仅关键字搜索时可用"}
            className={SELECT_CLASS}
          >
            <option value="">全部年份</option>
            {YEAR_OPTIONS.map((y) => (
              <option key={y} value={String(y)}>
                {y}
              </option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-1.5">
          <span className="text-xs text-muted">月份</span>
          <select
            value={form.m}
            onChange={(e) => onFormChange((f) => ({ ...f, m: e.target.value }))}
            disabled={!keywordMode || !form.y}
            title={!form.y ? "需先选择年份" : keywordMode ? undefined : "仅关键字搜索时可用"}
            className={SELECT_CLASS}
          >
            <option value="">全部月份</option>
            {Array.from({ length: 12 }, (_, i) => i + 1).map((m) => (
              <option key={m} value={String(m)}>
                {m} 月
              </option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-1.5">
          <span className="text-xs text-muted">分类</span>
          <select
            value={form.mainCategory}
            onChange={(e) => onMainCategoryChange(e.target.value)}
            disabled={keywordMode}
            title={keywordMode ? "关键字搜索时不按分类浏览" : undefined}
            className={SELECT_CLASS}
          >
            {MAIN_CATEGORIES.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
      </div>
    </form>
  );
}

/* ── 通用小件 ── */

/** 触底自动加载:观察 sentinel,可见即触发 loadMore;「加载更多」按钮为兜底双支持 */
function useBottomAutoLoad(loadMore: () => void, enabled: boolean) {
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el || !enabled) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) loadMore();
      },
      { rootMargin: "480px 0px" }
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [loadMore, enabled]);
  return sentinelRef;
}

/** 分页列表尾部状态:加载中 / 出错重试 / 加载更多按钮 / 页码封顶 / 终止提示 */
function LoadMoreFooter({
  loadingMore,
  error,
  canLoadMore,
  pageCap,
  ended,
  onLoadMore,
  onRetry,
}: {
  loadingMore: boolean;
  error: unknown;
  canLoadMore: boolean;
  pageCap: boolean;
  ended: boolean;
  onLoadMore: () => void;
  onRetry: () => void;
}) {
  if (loadingMore) {
    return (
      <div className="flex items-center justify-center gap-2 py-6 text-sm text-muted">
        <Loader2 className="h-4 w-4 animate-spin" />
        加载中…
      </div>
    );
  }
  if (error) {
    return (
      <div className="flex flex-col items-center gap-2 py-6 text-sm text-muted">
        <span>加载更多失败{isJmApiError(error) ? `:${error.message}` : ""}</span>
        <button
          type="button"
          onClick={onRetry}
          className="rounded-lg border border-border px-4 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-card-hover"
        >
          重试
        </button>
      </div>
    );
  }
  if (canLoadMore) {
    return (
      <div className="flex justify-center py-6">
        <button
          type="button"
          onClick={onLoadMore}
          className="rounded-lg border border-border px-6 py-2 text-sm font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent"
        >
          加载更多
        </button>
      </div>
    );
  }
  if (pageCap) {
    return <p className="py-6 text-center text-xs text-muted/70">仅展示前 120 页结果</p>;
  }
  if (ended) {
    return <p className="py-6 text-center text-xs text-muted/70">没有更多了</p>;
  }
  return null;
}

/** 网格骨架屏(与在线首页一致) */
function GridSkeleton({ count = 12 }: { count?: number }) {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="animate-pulse">
          <div className="aspect-[3/4] w-full rounded-lg bg-card/60" />
          <div className="mt-2 h-3.5 w-3/4 rounded bg-card/60" />
          <div className="mt-1.5 h-3 w-1/2 rounded bg-card/60" />
        </div>
      ))}
    </div>
  );
}
