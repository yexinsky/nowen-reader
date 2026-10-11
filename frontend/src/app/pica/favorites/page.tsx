"use client";

/**
 * 哔咔在线源 · 收藏页
 * GET /api/pica/favourites?s=dd|da&page=;排序切换 + 分页。
 */

import { useCallback, useEffect, useState } from "react";
import { Bookmark, Loader2 } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { PicaErrorCard, PicaGate } from "@/components/pica/PicaGate";
import { PicaComicGrid } from "@/components/pica/ComicGrid";
import { PicaBatchSelectionProvider } from "@/components/pica/download/BatchDownload";
import { picaFavorites } from "@/lib/pica/client";
import type { PicaComicItem } from "@/lib/pica/types";

const SORTS = [
  { value: "dd", label: "新→旧" },
  { value: "da", label: "旧→新" },
] as const;
type SortKey = (typeof SORTS)[number]["value"];

export default function PicaFavoritesPage() {
  return (
    <>
      <PageHeader
        title="我的收藏"
        description="哔咔账号收藏"
        icon={Bookmark}
        width="management"
      />
      <PageContent width="management">
        <PicaGate>
          <FavoritesContent />
        </PicaGate>
      </PageContent>
    </>
  );
}

function FavoritesContent() {
  const [sort, setSort] = useState<SortKey>("dd");
  const [items, setItems] = useState<PicaComicItem[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [retryTick, setRetryTick] = useState(0);

  const load = useCallback(async (sortKey: SortKey, targetPage: number) => {
    setLoading(true);
    setError(null);
    try {
      const res = await picaFavorites(sortKey, targetPage);
      setItems(res.list ?? []);
      setTotal(res.total ?? 0);
      setHasNext(res.hasNext);
      setPage(res.page ?? targetPage);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(sort, 1);
  }, [load, sort, retryTick]);

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted">
          共 <span className="font-medium text-foreground">{total}</span> 部收藏
        </p>
        <div className="flex items-center gap-1.5">
          {SORTS.map((s) => (
            <button
              key={s.value}
              type="button"
              onClick={() => setSort(s.value)}
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
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-20 text-muted">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      ) : error ? (
        <PicaErrorCard error={error} onRetry={() => setRetryTick((t) => t + 1)} />
      ) : (
        <>
          <PicaBatchSelectionProvider comics={items}>
            <PicaComicGrid comics={items} emptyText="还没有收藏任何漫画" />
          </PicaBatchSelectionProvider>
          {(hasNext || page > 1) && (
            <div className="mt-6 flex items-center justify-center gap-3">
              <button
                type="button"
                disabled={page <= 1}
                onClick={() => void load(sort, page - 1)}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
              >
                上一页
              </button>
              <span className="text-xs text-muted">第 {page} 页</span>
              <button
                type="button"
                disabled={!hasNext}
                onClick={() => void load(sort, page + 1)}
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
