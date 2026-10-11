"use client";

/**
 * 哔咔在线源 · 阅读历史页
 * GET/DELETE /api/pica/history(源内服务端历史,与本地历史页独立)。
 */

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { History, Loader2, Trash2 } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { PicaErrorCard, PicaGate } from "@/components/pica/PicaGate";
import { resolvePicaUrl } from "@/lib/pica/config";
import { isPicaApiError, picaHistoryClear, picaHistoryList } from "@/lib/pica/client";
import type { PicaHistoryItem } from "@/lib/pica/types";

export default function PicaHistoryPage() {
  return (
    <>
      <PageHeader
        title="阅读历史"
        description="哔咔源内阅读记录"
        icon={History}
        width="management"
      />
      <PageContent width="management">
        <PicaGate>
          <HistoryContent />
        </PicaGate>
      </PageContent>
    </>
  );
}

function HistoryContent() {
  const [items, setItems] = useState<PicaHistoryItem[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [clearing, setClearing] = useState(false);
  const [retryTick, setRetryTick] = useState(0);

  const load = useCallback(async (targetPage: number) => {
    setLoading(true);
    setError(null);
    try {
      const res = await picaHistoryList(targetPage);
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
    void load(1);
  }, [load, retryTick]);

  const handleClear = async () => {
    if (clearing || total === 0) return;
    if (!window.confirm("确定清空全部哔咔阅读历史?此操作不可撤销。")) return;
    setClearing(true);
    try {
      await picaHistoryClear();
      await load(1);
    } catch (err) {
      setError(err);
    } finally {
      setClearing(false);
    }
  };

  return (
    <div>
      <div className="mb-4 flex items-center justify-between gap-2">
        <p className="text-sm text-muted">
          共 <span className="font-medium text-foreground">{total}</span> 条记录
        </p>
        <button
          type="button"
          onClick={handleClear}
          disabled={clearing || total === 0}
          className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs text-muted transition-colors hover:border-red-500/50 hover:text-red-400 disabled:cursor-not-allowed disabled:opacity-40"
        >
          {clearing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
          清空历史
        </button>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-20 text-muted">
          <Loader2 className="h-6 w-6 animate-spin" />
        </div>
      ) : error ? (
        <PicaErrorCard error={error} onRetry={() => setRetryTick((t) => t + 1)} />
      ) : items.length === 0 ? (
        <p className="py-16 text-center text-sm text-muted">暂无阅读记录</p>
      ) : (
        <>
          <div className="space-y-2">
            {items.map((item) => (
              <Link
                key={`${item.id}-${item.order}`}
                href={`/pica/reader/${item.id}?ep=${item.order}`}
                className="flex items-center gap-3 rounded-xl border border-border bg-card p-3 transition-colors hover:border-accent/40"
              >
                {item.coverUrl ? (
                  <img
                    src={resolvePicaUrl(item.coverUrl)}
                    alt={item.title}
                    loading="lazy"
                    className="h-16 w-12 shrink-0 rounded-md object-cover"
                  />
                ) : (
                  <div className="flex h-16 w-12 shrink-0 items-center justify-center rounded-md bg-muted/10 text-muted/40">
                    <History className="h-4 w-4" />
                  </div>
                )}
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-foreground/90">{item.title || "无标题"}</p>
                  <p className="mt-0.5 text-xs text-muted">
                    读到第 {item.order} 话 · 第 {item.imageIndex} 页
                  </p>
                  <p className="mt-0.5 text-[11px] text-muted/70">{item.updatedAt}</p>
                </div>
                <span className="shrink-0 rounded-md bg-accent/10 px-2 py-1 text-[11px] font-medium text-accent">
                  继续阅读
                </span>
              </Link>
            ))}
          </div>
          {(hasNext || page > 1) && (
            <div className="mt-6 flex items-center justify-center gap-3">
              <button
                type="button"
                disabled={page <= 1}
                onClick={() => void load(page - 1)}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
              >
                上一页
              </button>
              <span className="text-xs text-muted">第 {page} 页</span>
              <button
                type="button"
                disabled={!hasNext}
                onClick={() => void load(page + 1)}
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
