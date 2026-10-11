"use client";

/**
 * 哔咔在线源 · 首页(每周热门 / 日榜 / 月榜 / 神魔推荐)
 *
 * - 排行榜:GET /api/pica/leaderboard?tt=D7|H24|D30(每周推荐 = 官方周榜)
 * - 神魔推荐:GET /api/pica/collections(官方首页合集区块)
 * - 用户卡:头像/等级/经验 + 每日打卡(punch-in)+ 快捷入口 + 随机一本
 * - 标签页首次激活时挂载,切回不重复请求(保持挂载 + 隐藏的简单缓存)
 */

import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import Link from "next/link";
import {
  Bookmark,
  Compass,
  Dice5,
  History,
  Loader2,
  Search,
  Sparkles,
} from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { PicaDownloadTasksButton } from "@/components/pica/download/DownloadTasks";
import { PicaErrorCard, PicaGate } from "@/components/pica/PicaGate";
import { PicaComicCard } from "@/components/pica/ComicCard";
import { PicaComicGrid } from "@/components/pica/ComicGrid";
import { PicaBatchSelectionProvider } from "@/components/pica/download/BatchDownload";
import { resolvePicaUrl } from "@/lib/pica/config";
import {
  isPicaApiError,
  picaCollections,
  picaLeaderboard,
  picaPunchIn,
  picaRandom,
} from "@/lib/pica/client";
import { usePicaSession } from "@/lib/pica/session";
import type { PicaCollectionSection, PicaComicItem } from "@/lib/pica/types";

const TABS = [
  { key: "weekly", label: "每周热门" },
  { key: "daily", label: "日榜" },
  { key: "monthly", label: "月榜" },
  { key: "collections", label: "神魔推荐" },
] as const;
type TabKey = (typeof TABS)[number]["key"];

function trimTrailingZero(s: string): string {
  return s.endsWith(".0") ? s.slice(0, -2) : s;
}

function formatCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 1e4) return `${trimTrailingZero((n / 1e4).toFixed(1))}万`;
  return String(n);
}

export default function PicaHomePage() {
  return (
    <>
      <PageHeader
        title="哔咔漫画"
        description="每周热门 · 日榜 · 月榜 · 神魔推荐"
        icon={Compass}
        width="management"
        actions={<PicaDownloadTasksButton />}
      />
      <PageContent width="management">
        <PicaGate>
          <HomeContent />
        </PicaGate>
      </PageContent>
    </>
  );
}

function HomeContent() {
  const [activeTab, setActiveTab] = useState<TabKey>("weekly");
  const [mountedTabs, setMountedTabs] = useState<Record<TabKey, boolean>>({
    weekly: true,
    daily: false,
    monthly: false,
    collections: false,
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
            (tab.key === "collections" ? (
              <CollectionsTab />
            ) : (
              <BoardTab
                key={tab.key}
                tt={tab.key === "weekly" ? "D7" : tab.key === "daily" ? "H24" : "D30"}
              />
            ))}
        </div>
      ))}
    </div>
  );
}

/* ── 用户卡(账号信息 + 打卡 + 快捷入口 + 随机一本) ── */

function UserCard() {
  const { userInfo } = usePicaSession();
  const navigate = useNavigate();
  const [punching, setPunching] = useState(false);
  const [punchHint, setPunchHint] = useState<string | null>(null);
  const [randomBusy, setRandomBusy] = useState(false);

  if (!userInfo) return null;

  const avatarUrl = resolvePicaUrl(userInfo.avatarUrl);
  const initial = (userInfo.name || "?").trim().charAt(0).toUpperCase() || "?";
  const punched = !!userInfo.isPunched;

  const handlePunchIn = async () => {
    if (punching || punched) return;
    setPunching(true);
    setPunchHint(null);
    try {
      await picaPunchIn();
      setPunchHint("打卡成功,经验 +10");
      // 本地快照同步打卡态(下次进入页面经 profile 校验刷新)
      try {
        const raw = localStorage.getItem("pica_user_info");
        if (raw) {
          const info = JSON.parse(raw);
          info.isPunched = true;
          localStorage.setItem("pica_user_info", JSON.stringify(info));
        }
      } catch {
        /* ignore */
      }
    } catch (err) {
      setPunchHint(
        isPicaApiError(err) ? err.message : "打卡失败,请稍后重试"
      );
    } finally {
      setPunching(false);
    }
  };

  // 随机一本:拉随机列表取第 1 部跳详情
  const handleRandom = async () => {
    if (randomBusy) return;
    setRandomBusy(true);
    try {
      const res = await picaRandom();
      const first = res.list?.[0];
      if (first) {
        navigate(`/pica/comic/${first.id}`);
        return;
      }
      setPunchHint("随机结果为空,请重试");
    } catch {
      setPunchHint("随机一本失败,请稍后重试");
    } finally {
      setRandomBusy(false);
    }
  };

  return (
    <section className="mb-6 rounded-xl border border-border bg-card p-4 sm:p-5">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center">
        <div className="flex min-w-0 flex-1 items-center gap-4">
          {avatarUrl ? (
            <img
              src={avatarUrl}
              alt={userInfo.name}
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
                {userInfo.name || "哔咔用户"}
              </span>
              <span className="rounded-full bg-accent/10 px-2 py-0.5 text-[10px] font-medium text-accent">
                Lv.{userInfo.level}
                {userInfo.title ? ` · ${userInfo.title}` : ""}
              </span>
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
              <span className="flex items-center gap-1">
                <Sparkles className="h-3 w-3" />
                经验 {formatCount(userInfo.exp)}
              </span>
              {punchHint && <span className="text-accent">{punchHint}</span>}
            </div>
          </div>
        </div>

        {/* 快捷入口 */}
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={handlePunchIn}
            disabled={punching || punched}
            className={`inline-flex items-center justify-center gap-2 rounded-lg border px-4 py-2 text-sm transition-colors ${
              punched
                ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-500"
                : "border-border bg-background text-muted hover:border-accent/50 hover:text-foreground"
            } disabled:cursor-not-allowed`}
          >
            {punching ? <Loader2 className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
            {punched ? "已打卡" : "每日打卡"}
          </button>
          <button
            type="button"
            onClick={handleRandom}
            disabled={randomBusy}
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground disabled:cursor-not-allowed"
          >
            {randomBusy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Dice5 className="h-4 w-4" />}
            随机一本
          </button>
          <Link
            href="/pica/search"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <Search className="h-4 w-4" />
            搜索
          </Link>
          <Link
            href="/pica/favorites"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <Bookmark className="h-4 w-4" />
            我的收藏
          </Link>
          <Link
            href="/pica/history"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-border bg-background px-4 py-2 text-sm text-muted transition-colors hover:border-accent/50 hover:text-foreground"
          >
            <History className="h-4 w-4" />
            阅读历史
          </Link>
        </div>
      </div>
    </section>
  );
}

/* ── 排行榜 Tab ── */

function BoardTab({ tt }: { tt: "H24" | "D7" | "D30" }) {
  const [items, setItems] = useState<PicaComicItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [retryTick, setRetryTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    picaLeaderboard(tt)
      .then((res) => {
        if (!cancelled) setItems(res.list ?? []);
      })
      .catch((err) => {
        if (!cancelled) setError(err);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [tt, retryTick]);

  if (loading) return <BoardSkeleton />;
  if (error) return <PicaErrorCard error={error} onRetry={() => setRetryTick((t) => t + 1)} />;
  return (
    <PicaBatchSelectionProvider comics={items}>
      <PicaComicGrid comics={items} emptyText="榜单暂无内容" />
    </PicaBatchSelectionProvider>
  );
}

/* ── 神魔推荐 Tab ── */

function CollectionsTab() {
  const [sections, setSections] = useState<PicaCollectionSection[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [retryTick, setRetryTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    picaCollections()
      .then((res) => {
        if (!cancelled) setSections(res.list ?? []);
      })
      .catch((err) => {
        if (!cancelled) setError(err);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [retryTick]);

  if (loading) return <BoardSkeleton />;
  if (error) return <PicaErrorCard error={error} onRetry={() => setRetryTick((t) => t + 1)} />;
  if (sections.length === 0) {
    return <p className="py-16 text-center text-sm text-muted">暂无推荐内容</p>;
  }
  return (
    <div className="space-y-8">
      {sections.map((section) => (
        <section key={section.title}>
          <div className="mb-3 flex items-center gap-2">
            {section.thumbUrl && (
              <img
                src={resolvePicaUrl(section.thumbUrl)}
                alt=""
                loading="lazy"
                className="h-8 w-8 rounded-lg object-cover"
              />
            )}
            <h2 className="text-base font-semibold text-foreground">{section.title}</h2>
            {section.description && (
              <span className="truncate text-xs text-muted">{section.description}</span>
            )}
          </div>
          {(section.list?.length ?? 0) === 0 ? (
            <p className="py-6 text-center text-sm text-muted">该合集暂无漫画</p>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
              {section.list.map((comic) => (
                <PicaComicCard key={comic.id} comic={comic} />
              ))}
            </div>
          )}
        </section>
      ))}
    </div>
  );
}

/* ── 加载骨架 ── */

function BoardSkeleton() {
  return (
    <div className="flex items-center justify-center py-20 text-muted">
      <Loader2 className="h-6 w-6 animate-spin" />
    </div>
  );
}
