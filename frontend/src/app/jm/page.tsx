"use client";

/**
 * JM 在线源 · 首页(推荐 / 最新 / 连载表)
 * PRD docs/PRD_JM_SOURCE.md M3
 *
 * - 推荐:#8 promote,serialization 区块渲染为「查看连载表」入口卡片,static 区块横向卡片流
 * - 最新:#9 latest,「加载更多」按钮 + 触底自动加载;上游无 total,满页视为可能有下一页,空页终止
 * - 连载表:#10 serialization,星期(1-7) + 类型(全部/漫画/韩漫)筛选 + 分页,默认今天(周日 0 → 7)
 * - 标签页首次激活时挂载,切回不重复请求(保持挂载 + 隐藏的简单缓存)
 */

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  Bookmark,
  CalendarDays,
  ChevronRight,
  Coins,
  Compass,
  Heart,
  History,
  LibraryBig,
  Loader2,
  Search,
  Sparkles,
} from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmDownloadTasksButton } from "@/components/jm/download/DownloadTasks";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmComicCard } from "@/components/jm/ComicCard";
import { JmComicGrid } from "@/components/jm/ComicGrid";
import { JmBatchSelectionProvider } from "@/components/jm/download/BatchDownload";
import { JmSignCalendar } from "@/components/jm/SignCalendar";
import { resolveJmUrl } from "@/lib/jm/config";
import { isJmApiError, jmLatest, jmPromote, jmSearch, jmSerialization } from "@/lib/jm/client";
import { useJmSession } from "@/lib/jm/session";
import type {
  JmComicItem,
  JmPromoteSection,
  JmSerializationDay,
  JmSerializationType,
  JmWeekCategory,
} from "@/lib/jm/types";

const TABS = [
  { key: "recommend", label: "推荐" },
  { key: "latest", label: "最新" },
  { key: "weekly", label: "每周热门" },
  { key: "serialization", label: "连载表" },
] as const;type TabKey = (typeof TABS)[number]["key"];

const WEEK_DAYS: { value: JmSerializationDay; label: string }[] = [
  { value: 1, label: "周一" },
  { value: 2, label: "周二" },
  { value: 3, label: "周三" },
  { value: 4, label: "周四" },
  { value: 5, label: "周五" },
  { value: 6, label: "周六" },
  { value: 7, label: "周日" },
];

const SERIAL_TYPES: { value: JmSerializationType; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "manga", label: "漫画" },
  { value: "hanman", label: "韩漫" },
];

/** 今天对应的连载表 day(1=周一…7=周日;getDay 周日=0 → 7) */
function todaySerializationDay(): JmSerializationDay {
  const dow = new Date().getDay();
  return (dow === 0 ? 7 : dow) as JmSerializationDay;
}

function trimTrailingZero(s: string): string {
  return s.endsWith(".0") ? s.slice(0, -2) : s;
}

function formatCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 1e4) return `${trimTrailingZero((n / 1e4).toFixed(1))}万`;
  return String(n);
}

export default function JmHomePage() {
  return (
    <>
      <PageHeader
        title="在线漫画"
        description="推荐 · 最新 · 每周热门 · 连载表"
        icon={Compass}
        width="management"
        actions={<JmDownloadTasksButton />}
      />
      <PageContent width="management">
        <JmGate>
          <HomeContent />
        </JmGate>
      </PageContent>
    </>
  );
}

function HomeContent() {
  const [activeTab, setActiveTab] = useState<TabKey>("recommend");
  // 已挂载过的标签保持挂载(隐藏),切回不重复请求
  const [mountedTabs, setMountedTabs] = useState<Record<TabKey, boolean>>({
    recommend: true,
    latest: false,
    weekly: false,
    serialization: false,
  });

  const switchTab = useCallback((key: TabKey) => {
    setActiveTab(key);
    setMountedTabs((prev) => (prev[key] ? prev : { ...prev, [key]: true }));
  }, []);

  return (
    <div>
      <UserCard />

      {/* 标签切换 */}
      <div className="mb-5 flex flex-wrap items-center gap-2">
        {TABS.map((tab) => (
          <button
            key={tab.key}
            type="button"
            onClick={() => switchTab(tab.key)}
            className={`rounded-full px-4 py-1.5 text-sm font-medium transition-all ${
              activeTab === tab.key
                ? "bg-accent text-white"
                : "border border-border/40 bg-card text-muted hover:border-border hover:text-foreground"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* 标签面板:懒挂载 + hidden 缓存 */}
      {TABS.map((tab) => (
        <div key={tab.key} className={activeTab === tab.key ? "" : "hidden"}>
          {mountedTabs[tab.key] &&
            (tab.key === "recommend" ? (
              <RecommendTab onOpenSerialization={() => switchTab("serialization")} />
            ) : tab.key === "latest" ? (
              <LatestTab />
            ) : tab.key === "weekly" ? (
              <WeeklyTab />
            ) : (
              <SerializationTab />
            ))}
        </div>
      ))}
    </div>
  );
}

/* ── 顶部用户卡(登录态由 JmGate 保证) ── */

function UserCard() {
  const { userInfo } = useJmSession();
  if (!userInfo) return null;

  const avatarUrl = resolveJmUrl(userInfo.avatarUrl);
  const initial = (userInfo.username || "?").trim().charAt(0).toUpperCase() || "?";

  return (
    <section className="mb-6 rounded-xl border border-border bg-card p-4 sm:p-5">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center">
        <div className="flex min-w-0 flex-1 items-center gap-4">
          {avatarUrl ? (
            <img
              src={avatarUrl}
              alt={userInfo.username}
              loading="lazy"
              className="h-14 w-14 shrink-0 rounded-full object-cover ring-1 ring-border"
            />
          ) : (
            <span className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-accent/15 text-lg font-semibold text-accent">
              {initial}
            </span>
          )}
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className="truncate text-base font-semibold text-foreground">
                {userInfo.username}
              </span>
              {userInfo.vip && (
                <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-semibold text-amber-500">
                  VIP
                </span>
              )}
              <span className="rounded-full bg-accent/10 px-2 py-0.5 text-[10px] font-medium text-accent">
                Lv.{userInfo.level}
                {userInfo.levelName ? ` · ${userInfo.levelName}` : ""}
              </span>
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
              <span className="flex items-center gap-1">
                <Heart className="h-3 w-3" />
                收藏 {userInfo.favorites}/{userInfo.canFavorites}
              </span>
              <span className="flex items-center gap-1">
                <Coins className="h-3 w-3" />
                金币 {formatCount(userInfo.coin)}
              </span>
              <span className="flex items-center gap-1">
                <Sparkles className="h-3 w-3" />
                经验 {formatCount(userInfo.exp)}
              </span>
            </div>
          </div>
        </div>

        {/* 二级页快捷入口:搜索 / 每周必看 / 我的收藏 / 阅读历史 */}
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Link
            href="/jm/search"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <Search className="h-4 w-4" />
            搜索
          </Link>
          <Link
            href="/jm/week"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <CalendarDays className="h-4 w-4" />
            每周必看
          </Link>
          <Link
            href="/jm/favorites"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <Bookmark className="h-4 w-4" />
            我的收藏
          </Link>
          <Link
            href="/jm/history"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <History className="h-4 w-4" />
            阅读历史
          </Link>
        </div>
      </div>

      {/* 每日签到日历 */}
      <div className="mt-4 border-t border-border/60 pt-4">
        <JmSignCalendar />
      </div>
    </section>
  );
}

/* ── 会话级缓存:从二级页返回首页时命中缓存,不重新拉取上游数据 ── */
const homeTabCache: {
  sections?: JmPromoteSection[];
  latest?: { items: JmComicItem[]; page: number; hasNext: boolean; ended: boolean };
  weekly?: { items: JmComicItem[]; page: number; hasNext: boolean };
} = {};

/* ── 推荐(#8 promote) ── */

function RecommendTab({ onOpenSerialization }: { onOpenSerialization: () => void }) {
  const [sections, setSections] = useState<JmPromoteSection[] | null>(() => homeTabCache.sections ?? null);
  const [loading, setLoading] = useState(!homeTabCache.sections);
  const [error, setError] = useState<unknown>(null);
  const reqRef = useRef(0);

  const load = useCallback(async () => {
    const req = ++reqRef.current;
    setLoading(true);
    setError(null);
    try {
      const data = await jmPromote();
      if (reqRef.current !== req) return;
      homeTabCache.sections = data.sections;
      setSections(data.sections);
    } catch (err) {
      if (reqRef.current !== req) return;
      setError(err);
    } finally {
      if (reqRef.current === req) setLoading(false);
    }
  }, []);

  useEffect(() => {
    // 有会话级缓存时跳过首拉(返回首页不重新请求)
    if (homeTabCache.sections) {
      setSections(homeTabCache.sections);
      setLoading(false);
      return;
    }
    load();
  }, [load]);

  if (loading) return <GridSkeleton />;
  if (error) return <JmErrorCard error={error} onRetry={load} />;
  if (!sections || sections.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-muted/10">
          <Compass className="h-7 w-7 text-muted/40" />
        </div>
        <p className="mb-4 text-sm text-muted">暂无推荐内容</p>
        <button
          type="button"
          onClick={load}
          className="rounded-lg border border-border px-4 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-card-hover"
        >
          重新加载
        </button>
      </div>
    );
  }

  const serialSections = sections.filter((s) => s.kind === "serialization");
  const staticSections = sections.filter(
    (s) => s.kind === "static" && Array.isArray(s.list) && s.list.length > 0
  );

  return (
    <div className="space-y-6">
      {/* serialization 区块 → 「查看连载表」入口卡片(切到连载表标签) */}
      {serialSections.map((s) => (
        <button
          key={s.key}
          type="button"
          onClick={onOpenSerialization}
          className="group flex w-full items-center justify-between rounded-xl border border-border bg-card p-4 text-left transition-colors hover:border-accent/40 hover:bg-card-hover"
        >
          <span className="flex min-w-0 items-center gap-3">
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-accent/10 text-accent">
              <LibraryBig className="h-5 w-5" />
            </span>
            <span className="min-w-0">
              <span className="block truncate text-sm font-semibold text-foreground">
                {s.title || "连载更新"}
              </span>
              <span className="mt-0.5 block text-xs text-muted">
                按星期浏览本周连载,支持漫画 / 韩漫筛选
              </span>
            </span>
          </span>
          <ChevronRight className="h-5 w-5 shrink-0 text-muted transition-transform group-hover:translate-x-0.5" />
        </button>
      ))}

      {/* static 区块 → 标题 + 横向滚动卡片流 */}
      {staticSections.map((s) => (
        <section key={s.key}>
          <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-foreground">
            <Sparkles className="h-4 w-4 text-accent" />
            {s.title}
          </h2>
          <div className="flex gap-3 overflow-x-auto pb-2">
            {(s.list ?? []).map((comic, index) => (
              <div key={`${comic.aid}-${index}`} className="w-32 shrink-0 sm:w-36">
                <JmComicCard comic={comic} />
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

/* ── 最新(#9 latest,分页加载) ── */

function LatestTab() {
  const cached = homeTabCache.latest;
  const [items, setItems] = useState<JmComicItem[]>(() => cached?.items ?? []);
  const [page, setPage] = useState(() => cached?.page ?? 0);
  const [hasNext, setHasNext] = useState(() => cached?.hasNext ?? true);
  const [loading, setLoading] = useState(!cached);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>(null);
  // 上游无 total,满页即认为可能有下一页;空页(或 hasNext=false)视为终止
  const [ended, setEnded] = useState(() => cached?.ended ?? false);
  const reqRef = useRef(0);

  const loadPage = useCallback(async (nextPage: number) => {
    const req = ++reqRef.current;
    if (nextPage === 1) {
      setLoading(true);
      setError(null);
    } else {
      setLoadingMore(true);
    }
    try {
      const data = await jmLatest(nextPage);
      if (reqRef.current !== req) return;
      setPage(nextPage);
      setHasNext(data.hasNext);
      const merged =
        nextPage === 1 ? data.list : [...(homeTabCache.latest?.items ?? []), ...data.list];
      homeTabCache.latest = {
        items: merged,
        page: nextPage,
        hasNext: data.hasNext,
        ended: !data.hasNext || data.list.length === 0,
      };
      setItems(merged);
      if (!data.hasNext || data.list.length === 0) setEnded(true);
    } catch (err) {
      if (reqRef.current !== req) return;
      setError(err);
    } finally {
      if (reqRef.current === req) {
        setLoading(false);
        setLoadingMore(false);
      }
    }
  }, []);

  useEffect(() => {
    if (homeTabCache.latest) {
      setLoading(false);
      return;
    }
    loadPage(1);
  }, [loadPage]);

  const canLoadMore = hasNext && !ended;
  const loadMore = useCallback(() => {
    if (loading || loadingMore || error || !canLoadMore) return;
    loadPage(page + 1);
  }, [loading, loadingMore, error, canLoadMore, page, loadPage]);

  const sentinelRef = useBottomAutoLoad(loadMore, !loading && !error && canLoadMore);

  if (loading) return <GridSkeleton />;
  if (error && items.length === 0) {
    return <JmErrorCard error={error} onRetry={() => loadPage(1)} />;
  }

  return (
    <div>
      <JmBatchSelectionProvider comics={items}>
        <JmComicGrid comics={items} emptyText="暂无最新更新" />
      </JmBatchSelectionProvider>
      {items.length > 0 && (
        <div ref={sentinelRef}>
          <LoadMoreFooter
            loadingMore={loadingMore}
            error={error}
            canLoadMore={canLoadMore}
            ended={ended}
            onLoadMore={loadMore}
            onRetry={() => loadPage(page + 1)}
          />
        </div>
      )}
    </div>
  );
}

/* ── 每周热门(#13 search 的周排行 mv_w,对齐原应用板块) ── */

function WeeklyTab() {
  const cached = homeTabCache.weekly;
  const [items, setItems] = useState<JmComicItem[]>(() => cached?.items ?? []);
  const [page, setPage] = useState(() => cached?.page ?? 0);
  const [hasNext, setHasNext] = useState(() => cached?.hasNext ?? true);
  const [loading, setLoading] = useState(!cached);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const reqRef = useRef(0);

  const loadPage = useCallback(async (nextPage: number) => {
    const req = ++reqRef.current;
    if (nextPage === 1) {
      setLoading(true);
      setError(null);
    } else {
      setLoadingMore(true);
    }
    try {
      // 周排行:无关键字走 /categories/filter 分支(mv_w 合法),页大小 80
      const data = await jmSearch({ sort: "mv_w", page: nextPage });
      if (reqRef.current !== req) return;
      setPage(nextPage);
      setHasNext(data.hasNext);
      const merged =
        nextPage === 1 ? data.list : [...(homeTabCache.weekly?.items ?? []), ...data.list];
      homeTabCache.weekly = { items: merged, page: nextPage, hasNext: data.hasNext };
      setItems(merged);
    } catch (err) {
      if (reqRef.current !== req) return;
      setError(err);
    } finally {
      if (reqRef.current === req) {
        setLoading(false);
        setLoadingMore(false);
      }
    }
  }, []);

  useEffect(() => {
    if (homeTabCache.weekly) {
      setLoading(false);
      return;
    }
    loadPage(1);
  }, [loadPage]);

  const canLoadMore = hasNext;
  const loadMore = useCallback(() => {
    if (loading || loadingMore || error || !canLoadMore) return;
    loadPage(page + 1);
  }, [loading, loadingMore, error, canLoadMore, page, loadPage]);

  const sentinelRef = useBottomAutoLoad(loadMore, !loading && !error && canLoadMore);

  if (loading) return <GridSkeleton />;
  if (error && items.length === 0) {
    return <JmErrorCard error={error} onRetry={() => loadPage(1)} />;
  }

  return (
    <div>
      <JmBatchSelectionProvider comics={items}>
        <JmComicGrid comics={items} emptyText="暂无每周热门" />
      </JmBatchSelectionProvider>
      {items.length > 0 && (
        <div ref={sentinelRef}>
          <LoadMoreFooter
            loadingMore={loadingMore}
            error={error}
            canLoadMore={canLoadMore}
            ended={!hasNext}
            onLoadMore={loadMore}
            onRetry={() => loadPage(page + 1)}
          />
        </div>
      )}
    </div>
  );
}

/* ── 连载表(#10 serialization) ── */

function SerializationTab() {
  const [day, setDay] = useState<JmSerializationDay>(todaySerializationDay);
  const [type, setType] = useState<JmSerializationType>("all");
  const [items, setItems] = useState<JmComicItem[]>([]);
  const [page, setPage] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const reqRef = useRef(0);

  const load = useCallback(
    async (nextDay: JmSerializationDay, nextType: JmSerializationType, nextPage: number) => {
      const req = ++reqRef.current;
      if (nextPage === 1) {
        setLoading(true);
        setError(null);
      } else {
        setLoadingMore(true);
      }
      try {
        const data = await jmSerialization(nextDay, nextType, nextPage);
        if (reqRef.current !== req) return;
        setPage(nextPage);
        setHasNext(data.hasNext);
        setItems((prev) => (nextPage === 1 ? data.list : [...prev, ...data.list]));
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

  // 切换星期 / 类型 → 重置并回到第一页
  useEffect(() => {
    load(day, type, 1);
  }, [day, type, load]);

  const loadMore = useCallback(() => {
    if (loading || loadingMore || error || !hasNext) return;
    load(day, type, page + 1);
  }, [loading, loadingMore, error, hasNext, day, type, page, load]);

  const sentinelRef = useBottomAutoLoad(loadMore, !loading && !error && hasNext);

  if (loading) return <GridSkeleton />;
  if (error && items.length === 0) {
    return <JmErrorCard error={error} onRetry={() => load(day, type, 1)} />;
  }

  return (
    <div>
      {/* 星期 + 类型筛选 */}
      <div className="mb-5 flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <ClockLabel />
          {WEEK_DAYS.map((d) => (
            <button
              key={d.value}
              type="button"
              onClick={() => setDay(d.value)}
              className={`rounded-full px-3 py-1.5 text-xs font-medium transition-all ${
                day === d.value
                  ? "bg-accent text-white"
                  : "border border-border/40 bg-card text-muted hover:border-border hover:text-foreground"
              }`}
            >
              {d.label}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted">类型</span>
          {SERIAL_TYPES.map((t) => (
            <button
              key={t.value}
              type="button"
              onClick={() => setType(t.value)}
              className={`rounded-full px-3 py-1.5 text-xs font-medium transition-all ${
                type === t.value
                  ? "bg-accent text-white"
                  : "border border-border/40 bg-card text-muted hover:border-border hover:text-foreground"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      <JmBatchSelectionProvider comics={items}>
        <JmComicGrid comics={items} emptyText="该筛选下暂无连载更新" />
      </JmBatchSelectionProvider>
      {items.length > 0 && (
        <div ref={sentinelRef}>
          <LoadMoreFooter
            loadingMore={loadingMore}
            error={error}
            canLoadMore={hasNext}
            ended={!hasNext}
            onLoadMore={loadMore}
            onRetry={() => load(day, type, page + 1)}
          />
        </div>
      )}
    </div>
  );
}

function ClockLabel() {
  return <span className="text-xs text-muted">星期</span>;
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

/** 分页列表尾部状态:加载中 / 出错重试 / 加载更多按钮 / 终止提示 */
function LoadMoreFooter({
  loadingMore,
  error,
  canLoadMore,
  ended,
  onLoadMore,
  onRetry,
}: {
  loadingMore: boolean;
  error: unknown;
  canLoadMore: boolean;
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
  if (ended) {
    return <p className="py-6 text-center text-xs text-muted/70">没有更多了</p>;
  }
  return null;
}

/** 网格骨架屏 */
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
