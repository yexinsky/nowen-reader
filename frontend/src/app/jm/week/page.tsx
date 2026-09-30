"use client";

/**
 * JM 在线源 · 每周必看(期数列表 + 单期内容)
 * PRD docs/PRD_JM_SOURCE.md M6
 *
 * - #11 GET /api/comics/week:期数列表(上游新→旧),横向 chips 选择,默认最新一期
 * - #12 GET /api/comics/week/filter:单期内容,类型切换(漫画 manga / 韩漫 hanman / 其他 another),
 *   单期固定内容无翻页语义;未知期 id 返回空列表(不报 3001)
 * - 在途请求以 reqRef 代际守卫,重查/卸载后丢弃过期响应(与在线首页同模式)
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { CalendarRange } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmComicGrid } from "@/components/jm/ComicGrid";
import { JmBatchSelectionProvider } from "@/components/jm/download/BatchDownload";
import { jmWeek, jmWeekFilter } from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
import { JmDownloadTasksButton } from "@/components/jm/download/DownloadTasks";
import type { JmComicItem, JmWeekCategory } from "@/lib/jm/types";

const WEEK_TYPES = [
  { value: "manga", label: "漫画" },
  { value: "hanman", label: "韩漫" },
  { value: "another", label: "其他" },
] as const;
type WeekType = (typeof WEEK_TYPES)[number]["value"];

/** 期数/类型 chip 样式(与在线首页标签切换一致) */
const CHIP_CLASS = "whitespace-nowrap rounded-full px-3 py-1.5 text-xs font-medium transition-all";
const CHIP_ACTIVE = "bg-accent text-white";
const CHIP_IDLE =
  "border border-border/40 bg-card text-muted hover:border-border hover:text-foreground";

export default function JmWeekPage() {
  return (
    <>
      <PageHeader
        title="每周必看"
        description="期数 · 类型浏览"
        icon={CalendarRange}
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
          <WeekContent />
        </JmGate>
      </PageContent>
    </>
  );
}

/* ── 期数列表(#11) ── */

function WeekContent() {
  const [categories, setCategories] = useState<JmWeekCategory[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const reqRef = useRef(0);

  const load = useCallback(async () => {
    const req = ++reqRef.current;
    setLoading(true);
    setError(null);
    try {
      const data = await jmWeek();
      if (reqRef.current !== req) return;
      setCategories(data.categories);
    } catch (err) {
      if (reqRef.current !== req) return;
      setError(err);
    } finally {
      if (reqRef.current === req) setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  if (loading) return <GridSkeleton />;
  if (error) return <JmErrorCard error={error} onRetry={load} />;
  if (!categories || categories.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-muted/10">
          <CalendarRange className="h-7 w-7 text-muted/40" />
        </div>
        <p className="mb-4 text-sm text-muted">暂无每周必看期数</p>
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

  return <WeekBrowser categories={categories} />;
}

/* ── 单期浏览器(#12,无翻页) ── */

function WeekBrowser({ categories }: { categories: JmWeekCategory[] }) {
  // 默认最新一期(列表新→旧)、类型漫画
  const [issueId, setIssueId] = useState(categories[0]?.id ?? "");
  const [type, setType] = useState<WeekType>("manga");
  const [items, setItems] = useState<JmComicItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const reqRef = useRef(0);

  const load = useCallback(async (id: string, nextType: WeekType) => {
    if (!id) {
      setItems([]);
      setLoading(false);
      return;
    }
    const req = ++reqRef.current;
    setLoading(true);
    setError(null);
    try {
      const data = await jmWeekFilter(id, nextType);
      if (reqRef.current !== req) return;
      setItems(data.list);
    } catch (err) {
      if (reqRef.current !== req) return;
      setError(err);
    } finally {
      if (reqRef.current === req) setLoading(false);
    }
  }, []);

  // 切换期数 / 类型 → 重新拉取该期内容
  useEffect(() => {
    load(issueId, type);
  }, [issueId, type, load]);

  return (
    <div>
      <div className="mb-5 flex flex-col gap-3">
        {/* 期数选择:圆角下拉框,选择即跳转对应期数(新→旧) */}
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted">期数</span>
          <select
            value={issueId}
            onChange={(e) => setIssueId(e.target.value)}
            aria-label="选择期数"
            className="h-9 max-w-full rounded-lg border border-border bg-card px-3 text-sm text-foreground outline-none transition-colors focus:border-accent/60"
          >
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.title}
              </option>
            ))}
          </select>
        </div>

        {/* 类型切换 */}
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted">类型</span>
          {WEEK_TYPES.map((t) => (
            <button
              key={t.value}
              type="button"
              onClick={() => setType(t.value)}
              className={`${CHIP_CLASS} ${type === t.value ? CHIP_ACTIVE : CHIP_IDLE}`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <GridSkeleton />
      ) : error ? (
        <JmErrorCard error={error} onRetry={() => load(issueId, type)} />
      ) : (
        <JmBatchSelectionProvider comics={items}>
          <JmComicGrid comics={items} emptyText="该期暂无内容" />
        </JmBatchSelectionProvider>
      )}
    </div>
  );
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
