"use client";

/**
 * JM 在线源 · 搜索(关键字 / 分类 / 排行)
 * PRD docs/PRD_JM_SOURCE.md M5,#13 GET /api/comics/search
 *
 * - 关键字模式:keyword + searchType(全站/作品/作者/标签/角色)+ 年份/月份过滤;
 *   searchType=site 等价缺省不传;m 仅与 y 同传
 * - 分类浏览模式:无关键字时按 mainCategory + sort 浏览(与关键字互斥,由上游路由规则决定)
 * - 排序:最新 mr(默认)/ 最多点击 mv / 月·周·日排行 mv_m/mv_w/mv_t / 最多图片 mp / 最多爱心 tf
 * - URL 参数初始化:详情页「搜索标签」跳入 ?keyword=<标签>&searchType=tag,
 *   挂载时读一次并自动执行首搜(searchType 非法值回退 tag)
 * - 我的标签:顶部展示已收藏标签 chips,点击即按该标签搜索,× 就地取消收藏
 * - 上游怪癖(如实遵循):
 *   1. 日排行 mv_t 带分类时上游返回空 → 自动取消分类并提示;
 *   2. 页码封顶 120 → 达到后提示「仅展示前 120 页结果」并停止加载。
 * - 分页:「加载更多」按钮 + 触底自动加载(hasNext 才显示);换条件搜索重置到第 1 页
 * - 在途请求以 reqRef 代际守卫,重查/卸载后丢弃过期响应(与在线首页同模式)
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import Link from "next/link";
import { AlertTriangle, ChevronDown, Compass, Loader2, RotateCcw, Search, Tag, User, X } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { useToast } from "@/components/Toast";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmComicGrid } from "@/components/jm/ComicGrid";
import { JmBatchSelectionProvider } from "@/components/jm/download/BatchDownload";
import { isJmApiError, jmRemoveTagFavorite, jmSearch, jmTagFavorites } from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
import { JmDownloadTasksButton } from "@/components/jm/download/DownloadTasks";
import type {
  JmComicItem,
  JmSearchParams,
  JmSearchSort,
  JmSearchType,
  JmTagFavorite,
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

/* ── 条件 ⇄ URL ──
 * 条件写进 URL 才能在「进详情 → 返回」时原样还原(浏览器前进/后退、刷新、分享同理)。
 * 只放真正影响结果的字段,滚动位置由浏览器自行恢复。 */

/** 表单 → URLSearchParams(默认值不写入,保持链接干净) */
function formToQuery(form: SearchForm): URLSearchParams {
  const q = new URLSearchParams();
  const keyword = form.keyword.trim();
  if (keyword) {
    q.set("keyword", keyword);
    if (form.searchType !== "site") q.set("searchType", form.searchType);
    if (form.y) {
      q.set("y", form.y);
      if (form.m) q.set("m", form.m);
    }
  } else if (form.mainCategory) {
    q.set("mainCategory", form.mainCategory);
  }
  if (form.sort !== "mr") q.set("sort", form.sort);
  return q;
}

/** URLSearchParams → 表单;非法值一律回落默认,不让坏链接把页面搞崩 */
function queryToForm(q: URLSearchParams): SearchForm {
  const keyword = (q.get("keyword") ?? "").trim();
  const pickSort = q.get("sort");
  const sort: JmSearchSort = SORT_OPTIONS.some((o) => o.value === pickSort)
    ? (pickSort as JmSearchSort)
    : "mr";
  const pickType = q.get("searchType");
  const searchType: JmSearchType = SEARCH_TYPES.some((t) => t.value === pickType)
    ? (pickType as JmSearchType)
    : keyword
      ? "tag"
      : "site";
  const y = (q.get("y") ?? "").trim();
  const m = (q.get("m") ?? "").trim();
  const cat = q.get("mainCategory") ?? "";
  return {
    keyword,
    sort,
    searchType,
    y: parseRangeInt(y, 9999) ? y : "",
    m: parseRangeInt(m, 12) ? m : "",
    // 关键字模式下分类不生效,直接丢掉,避免 UI 上出现一个无效选择
    mainCategory: keyword ? "" : MAIN_CATEGORIES.some((c) => c.value === cat) ? cat : "",
  };
}

export default function JmSearchPage() {
  return (
    <>
      <PageHeader
        title="搜索在线漫画"
        description="关键字 · 分类 · 排行"
        icon={Search}
        width="management"
        actions={
          <>
            <JmDownloadTasksButton />
            <JmBackButton />
          </>
        }
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
  const toast = useToast();
  // URL 参数是条件的唯一真值来源:进详情再返回、刷新、分享都能原样还原
  const [searchParams, setSearchParams] = useSearchParams();
  const initialForm = useMemo<SearchForm>(
    () => (searchParams.toString() ? queryToForm(searchParams) : INITIAL_FORM),
    // 只在挂载时读一次;后续变化由 runSearch 自己写回,不需要再同步回来(否则光标/输入会被打断)
    // eslint-disable-next-line react-hooks/exhaustive-deps
    []
  );

  const [form, setForm] = useState<SearchForm>(initialForm);
  // 首屏即按初始条件搜索(URL 带关键字时自动执行,否则默认分类浏览模式)
  const [submitted, setSubmitted] = useState<SearchConditions>(
    () => toConditions(initialForm).conditions
  );
  const [notice, setNotice] = useState<string | null>(null);

  // 我的标签(收藏的标签):挂载拉取,失败静默为空(不阻塞搜索)
  const [savedTags, setSavedTags] = useState<JmTagFavorite[]>([]);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await jmTagFavorites();
        if (!cancelled) setSavedTags(data.list);
      } catch {
        if (!cancelled) setSavedTags([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

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

  /** 提交搜索:换条件一律重置到第 1 页,并把条件写进 URL(返回时原样还原) */
  const runSearch = useCallback(
    (next: SearchForm) => {
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
      // replace:连续调条件不应堆出一串历史,返回要回到进入搜索前的页面
      setSearchParams(formToQuery(corrected), { replace: true });
    },
    [setSearchParams]
  );

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    runSearch(form);
  };

  /** 回车显式提交(与 handleSubmit 同口径,不依赖表单隐式提交) */
  const submitKeyword = useCallback(() => {
    runSearch(form);
  }, [form, runSearch]);

  // 下拉条件变化 → 立即按新条件回到第 1 页(含年份/月份:不再需要额外点一次搜索)
  const updateSort = (sort: JmSearchSort) => runSearch({ ...form, sort });
  const updateSearchType = (searchType: JmSearchType) => runSearch({ ...form, searchType });
  const updateMainCategory = (mainCategory: string) => runSearch({ ...form, mainCategory });
  const updateYear = (y: string) => runSearch({ ...form, y, m: y ? form.m : "" });
  const updateMonth = (m: string) => runSearch({ ...form, m });

  /** 点击「我的标签」chip:按该标签搜索(searchType 强制为 tag) */
  /** 点「我的标签/作者」chip:按该项类型(searchType=tag/author)就地搜索 */
  const searchByItem = (it: Pick<JmTagFavorite, "type" | "tag">) =>
    runSearch({ ...form, keyword: it.tag, searchType: it.type });

  /** 就地取消收藏:乐观移除 + 失败回滚;与标签页(#22)保持同一套语义(成功/失败都给反馈) */
  const removeSavedItem = useCallback(async (it: Pick<JmTagFavorite, "type" | "tag">) => {
    const prev = savedTags;
    setSavedTags((cur) => cur.filter((x) => !(x.type === it.type && x.tag === it.tag)));
    try {
      await jmRemoveTagFavorite(it.tag, it.type);
      toast.success(`已取消${it.type === "author" ? "作者" : "标签"}收藏:${it.tag}`);
    } catch (err) {
      // 失败回滚:不能让"看起来删掉了、其实还在"的状态留在界面上
      setSavedTags(prev);
      toast.error(isJmApiError(err) ? err.message : "取消收藏失败,请稍后重试");
    }
  }, [savedTags, toast]);

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

  /** 恢复默认条件:清空关键字/年月/分类,排序回最新,并立即重查 */
  const resetConditions = () => {
    runSearch(INITIAL_FORM);
  };

  /** 条件区是否有任何「非默认」项(用于显示「重置条件」) */
  const dirty =
    form.keyword.trim() !== "" ||
    form.y !== "" ||
    form.m !== "" ||
    form.mainCategory !== "" ||
    form.sort !== "mr" ||
    form.searchType !== "site";

  return (
    <div>
      {savedTags.length > 0 && (
        <SavedTagsRow tags={savedTags} onSearch={searchByItem} onRemove={removeSavedItem} />
      )}

      {/* 表单常驻:骨架只替换下方结果区,换排序/分类/翻页时搜索框与条件不再整块消失 */}
      <SearchFormPanel
        form={form}
        keywordMode={keywordMode}
        onFormChange={setForm}
        onSubmit={handleSubmit}
        onEnter={submitKeyword}
        onSortChange={updateSort}
        onSearchTypeChange={updateSearchType}
        onMainCategoryChange={updateMainCategory}
        onYearChange={updateYear}
        onMonthChange={updateMonth}
      />

      {/* 上游怪癖提示(自动取消分类 / 页码封顶) */}
      {notice && (
        <div className="mb-3 flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span>{notice}</span>
        </div>
      )}

      {/* 结果概要行:计数 / 加载中 + 重置条件(常驻,不随结果区被骨架替换) */}
      <div className="mb-3 flex min-h-6 items-center justify-between gap-2 text-xs text-muted">
        <span className="inline-flex items-center gap-1.5">
          {loading ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              加载中…
            </>
          ) : total !== undefined ? (
            `共 ${total.toLocaleString("zh-CN")} 条`
          ) : null}
        </span>
        {dirty && (
          <button
            type="button"
            onClick={resetConditions}
            className="inline-flex shrink-0 items-center gap-1 rounded-md border border-border/60 px-2 py-1 text-[11px] text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <RotateCcw className="h-3 w-3" />
            重置条件
          </button>
        )}
      </div>

      {/* 结果区:只有这里随加载替换为骨架 */}
      {loading ? (
        <GridSkeleton />
      ) : error && items.length === 0 ? (
        <JmErrorCard error={error} onRetry={() => loadPage(submitted, 1)} />
      ) : (
        <>
          {items.length > 0 ? (
            <JmBatchSelectionProvider comics={items}>
              <JmComicGrid comics={items} />
            </JmBatchSelectionProvider>
          ) : (
            /* 空态给行动指引:不只说"没有结果",还告诉用户下一步能点什么 */
            <div className="flex flex-col items-center justify-center rounded-xl border border-border bg-card py-14 text-center">
              <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-muted/10">
                <Search className="h-7 w-7 text-muted/40" />
              </div>
              <p className="text-sm font-medium text-foreground">
                {isBrowseMode ? "该分类下暂无内容" : "没有找到相关漫画"}
              </p>
              <p className="mt-1.5 max-w-xs text-xs leading-relaxed text-muted">
                {isBrowseMode
                  ? "换个主分类,或直接用关键字搜索试试。"
                  : "试试减少条件、换同义词,或把搜索范围放宽到「全部」。"}
              </p>
              <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
                {dirty && (
                  <button
                    type="button"
                    onClick={resetConditions}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent"
                  >
                    <RotateCcw className="h-3.5 w-3.5" />
                    重置条件
                  </button>
                )}
                <Link
                  href="/jm/week"
                  className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent"
                >
                  <Compass className="h-3.5 w-3.5" />
                  去看每周必看
                </Link>
              </div>
            </div>
          )}
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
        </>
      )}
    </div>
  );
}

/* ── 搜索表单 ── */

const SELECT_CLASS =
  "h-8 rounded-lg border border-border bg-background px-2 text-xs text-foreground outline-none transition-colors focus:border-accent/60 disabled:cursor-not-allowed disabled:opacity-50";

/** 我的标签 chips 行:点击 chip 即按该标签搜索,× 就地取消收藏;「管理」进标签收藏页
 *  默认只占一行(可横滑),收藏多到 38 个时不会把搜索框挤出首屏 */
function SavedTagsRow({
  tags,
  onSearch,
  onRemove,
}: {
  tags: JmTagFavorite[];
  onSearch: (it: Pick<JmTagFavorite, "type" | "tag">) => void;
  onRemove: (it: Pick<JmTagFavorite, "type" | "tag">) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const tagItems = tags.filter((it) => it.type === "tag");
  const authorItems = tags.filter((it) => it.type === "author");
  return (
    <div className="mb-3 rounded-xl border border-border bg-card px-3.5 py-2.5">
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex shrink-0 items-center gap-1 text-xs font-medium text-muted transition-colors hover:text-foreground"
          title={expanded ? "收起收藏" : "展开全部收藏"}
        >
          <Tag className="h-3.5 w-3.5" />
          我的收藏
          <span className="text-[11px] text-muted/70">{tags.length}</span>
          <ChevronDown
            className={`h-3 w-3 transition-transform ${expanded ? "rotate-180" : ""}`}
          />
        </button>
        <Link
          href="/jm/tags"
          className="ml-auto shrink-0 text-[11px] text-accent transition-opacity hover:opacity-80"
        >
          管理
        </Link>
      </div>
      <div
        className={
          expanded
            ? "mt-2 flex flex-wrap gap-1.5"
            : "mt-2 flex items-center gap-1.5 overflow-x-auto pb-0.5 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        }
      >
        {tagItems.map(({ tag, type }) => (
          <span
            key={`${type}\u0000${tag}`}
            className="inline-flex shrink-0 items-center gap-0.5 rounded-full border border-border/60 bg-background py-0.5 pl-2.5 pr-1 text-xs text-foreground"
          >
            <button
              type="button"
              onClick={() => onSearch({ type, tag })}
              title={`按标签「${tag}」搜索`}
              className="transition-colors hover:text-accent"
            >
              {tag}
            </button>
            <button
              type="button"
              onClick={() => onRemove({ type, tag })}
              title="取消收藏"
              className="rounded-full p-0.5 text-muted/60 transition-colors hover:bg-muted/10 hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        {authorItems.map(({ tag, type }) => (
          <span
            key={`${type}\u0000${tag}`}
            className="inline-flex shrink-0 items-center gap-0.5 rounded-full border border-violet-400/40 bg-background py-0.5 pl-2.5 pr-1 text-xs text-foreground"
          >
            <button
              type="button"
              onClick={() => onSearch({ type, tag })}
              title={`按作者「${tag}」搜索`}
              className="inline-flex items-center gap-0.5 transition-colors hover:text-accent"
            >
              <User className="h-3 w-3 text-violet-400" />
              {tag}
            </button>
            <button
              type="button"
              onClick={() => onRemove({ type, tag })}
              title="取消收藏"
              className="rounded-full p-0.5 text-muted/60 transition-colors hover:bg-muted/10 hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
      </div>
    </div>
  );
}

function SearchFormPanel({
  form,
  keywordMode,
  onFormChange,
  onSubmit,
  onEnter,
  onSortChange,
  onSearchTypeChange,
  onMainCategoryChange,
  onYearChange,
  onMonthChange,
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
  /** 年份/月份也即时生效,和其余下拉一致(不再需要额外点一次「搜索」) */
  onYearChange: (y: string) => void;
  onMonthChange: (m: string) => void;
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
            onChange={(e) => onYearChange(e.target.value)}
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
            onChange={(e) => onMonthChange(e.target.value)}
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
