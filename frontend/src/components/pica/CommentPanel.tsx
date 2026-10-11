"use client";

/**
 * 哔咔评论面板(只读;发表评论需哔咔等级 ≥2,v1 不做发表)
 * 分页加载 comics/{id}/comments。
 */

import { useCallback, useEffect, useState } from "react";
import { Heart, Loader2, MessageSquare } from "lucide-react";
import { isPicaApiError, picaComments } from "@/lib/pica/client";
import { resolvePicaUrl } from "@/lib/pica/config";
import type { PicaComment } from "@/lib/pica/types";

export function PicaCommentPanel({ comicId }: { comicId: string }) {
  const [comments, setComments] = useState<PicaComment[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (targetPage: number) => {
      setLoading(true);
      setError(null);
      try {
        const res = await picaComments(comicId, targetPage);
        setComments(res.list);
        setTotal(res.total);
        setHasNext(res.hasNext);
        setPage(res.page);
      } catch (err) {
        setError(isPicaApiError(err) ? err.message : "评论加载失败");
      } finally {
        setLoading(false);
      }
    },
    [comicId]
  );

  useEffect(() => {
    void load(1);
  }, [load]);

  return (
    <section className="mt-8">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-foreground">
        <MessageSquare className="h-4 w-4 text-accent" />
        评论
        {total > 0 && <span className="text-xs font-normal text-muted">({total})</span>}
      </h2>

      {loading ? (
        <div className="flex items-center justify-center py-10 text-muted">
          <Loader2 className="h-5 w-5 animate-spin" />
        </div>
      ) : error ? (
        <p className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-500">
          {error}
        </p>
      ) : comments.length === 0 ? (
        <p className="py-8 text-center text-sm text-muted">暂无评论</p>
      ) : (
        <>
          <div className="space-y-3">
            {comments.map((cm) => (
              <div key={cm.id} className="rounded-xl border border-border bg-card p-4">
                <div className="flex items-center gap-2.5">
                  {cm.avatarUrl ? (
                    <img
                      src={resolvePicaUrl(cm.avatarUrl)}
                      alt={cm.userName}
                      loading="lazy"
                      className="h-8 w-8 rounded-full object-cover"
                    />
                  ) : (
                    <div className="flex h-8 w-8 items-center justify-center rounded-full bg-muted/20 text-xs text-muted">
                      {(cm.userName || "?").slice(0, 1).toUpperCase()}
                    </div>
                  )}
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-foreground/90">{cm.userName || "匿名"}</p>
                    <p className="text-[11px] text-muted">{cm.createdAt}</p>
                  </div>
                  {cm.likes > 0 && (
                    <span className="flex shrink-0 items-center gap-1 text-xs text-muted">
                      <Heart className="h-3.5 w-3.5" />
                      {cm.likes}
                    </span>
                  )}
                </div>
                <p className="mt-2.5 whitespace-pre-wrap break-all text-sm leading-relaxed text-foreground/80">
                  {cm.content}
                </p>
              </div>
            ))}
          </div>

          {(hasNext || page > 1) && (
            <div className="mt-4 flex items-center justify-center gap-3">
              <button
                type="button"
                disabled={page <= 1 || loading}
                onClick={() => void load(page - 1)}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
              >
                上一页
              </button>
              <span className="text-xs text-muted">第 {page} 页</span>
              <button
                type="button"
                disabled={!hasNext || loading}
                onClick={() => void load(page + 1)}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
              >
                下一页
              </button>
            </div>
          )}
        </>
      )}
    </section>
  );
}

export default PicaCommentPanel;
