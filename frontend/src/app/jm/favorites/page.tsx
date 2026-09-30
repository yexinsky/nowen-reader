"use client";

/**
 * JM 在线源 · 我的收藏(PRD docs/PRD_JM_SOURCE.md M9)
 * 接口:MOBILE_API.md #20 folders、#21 favorites、#23 remove favorite
 *
 * - 收藏夹 chips(#20):id + name + count,点击切换(默认 "0");切换重置回第一页
 * - 列表分页(#21,20/页):hasNext 时「加载更多」
 * - 取消收藏(#23 切换语义):网格项右上角 hover 出现按钮,stopPropagation + preventDefault
 *   后调用;响应 favorited===false → 本地移除;null/其他 → 重拉当前页校准
 * - JmComicCard 内部是 Link,取消按钮为兄弟层覆盖(z-10),不破坏卡片跳转
 * - 空态 / 错误(JmErrorCard)/ 骨架屏齐全;不做轮询
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { BookmarkX, FolderOpen, Loader2 } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmComicCard } from "@/components/jm/ComicCard";
import { JmComicGrid } from "@/components/jm/ComicGrid";
import { JmBatchSelectionProvider } from "@/components/jm/download/BatchDownload";
import { useToast } from "@/components/Toast";
import { isJmApiError, jmFavoriteFolders, jmFavorites, jmRemoveFavorite } from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
import { JmDownloadTasksButton } from "@/components/jm/download/DownloadTasks";
import type { JmComicItem, JmFavoriteFolder } from "@/lib/jm/types";

const DEFAULT_FOLDER_ID = "0";

export default function JmFavoritesPage() {
  return (
    <>
      <PageHeader
        title="在线收藏"
        description="JM 收藏夹浏览与管理"
        icon={FolderOpen}
        actions={
          <>
            <JmDownloadTasksButton />
            <JmBackButton />
          </>
        }
      />
      <PageContent width="management">
        <JmGate>
          <FavoritesContent />
        </JmGate>
      </PageContent>
    </>
  );
}

function FavoritesContent() {
  const toast = useToast();

  // 收藏夹(#20)
  const [folders, setFolders] = useState<JmFavoriteFolder[]>([]);
  const [foldersLoading, setFoldersLoading] = useState(true);
  const [foldersError, setFoldersError] = useState<unknown>(null);
  const [folderId, setFolderId] = useState<string>(DEFAULT_FOLDER_ID);

  // 收藏列表(#21)
  const [items, setItems] = useState<JmComicItem[]>([]);
  const [page, setPage] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>(null);

  // 取消收藏进行中的 aid 集合
  const [removing, setRemoving] = useState<ReadonlySet<string>>(() => new Set());

  const reqRef = useRef(0);

  const loadFolders = useCallback(async () => {
    setFoldersLoading(true);
    setFoldersError(null);
    try {
      const data = await jmFavoriteFolders();
      setFolders(data.folders ?? []);
    } catch (err) {
      setFoldersError(err);
    } finally {
      setFoldersLoading(false);
    }
  }, []);

  const loadPage = useCallback(async (nextFolderId: string, nextPage: number) => {
    const req = ++reqRef.current;
    if (nextPage === 1) {
      setLoading(true);
      setError(null);
    } else {
      setLoadingMore(true);
    }
    try {
      const data = await jmFavorites(nextFolderId, nextPage);
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
  }, []);

  // 切换收藏夹 → 重置回第一页
  useEffect(() => {
    loadPage(folderId, 1);
    return () => {
      reqRef.current += 1;
    };
  }, [folderId, loadPage]);

  useEffect(() => {
    loadFolders();
  }, [loadFolders]);

  /**
   * 取消收藏(#23 切换语义):
   * - favorited === false → 确认已移除,本地剔除(收藏夹计数后台静默刷新)
   * - null / 其他(结果不明)→ 重拉当前页校准
   */
  const handleRemove = useCallback(
    async (aid: string) => {
      setRemoving((prev) => {
        if (prev.has(aid)) return prev;
        return new Set(prev).add(aid);
      });
      try {
        const res = await jmRemoveFavorite(aid);
        if (res.favorited === false) {
          setItems((prev) => prev.filter((it) => it.aid !== aid));
          loadFolders(); // 静默刷新收藏夹计数,失败不影响列表
        } else {
          await loadPage(folderId, page || 1);
        }
      } catch (err) {
        toast.error(isJmApiError(err) ? err.message : "取消收藏失败,请稍后重试");
      } finally {
        setRemoving((prev) => {
          const next = new Set(prev);
          next.delete(aid);
          return next;
        });
      }
    },
    [folderId, page, loadFolders, loadPage, toast]
  );

  return (
    <div>
      {/* 收藏夹 chips */}
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <span className="mr-1 flex items-center gap-1 text-xs text-muted">
          <FolderOpen className="h-3.5 w-3.5" />
          收藏夹
        </span>
        {foldersLoading ? (
          <>
            {Array.from({ length: 2 }, (_, i) => (
              <div key={i} className="h-8 w-24 animate-pulse rounded-full bg-card/60" />
            ))}
          </>
        ) : foldersError ? (
          <span className="flex items-center gap-2 text-xs text-muted">
            收藏夹加载失败
            <button
              type="button"
              onClick={loadFolders}
              className="rounded-lg border border-border px-3 py-1 font-medium text-foreground transition-colors hover:bg-card-hover"
            >
              重试
            </button>
          </span>
        ) : (
          folders.map((f) => (
            <button
              key={f.id}
              type="button"
              onClick={() => setFolderId(f.id)}
              className={`rounded-full px-4 py-1.5 text-sm font-medium transition-all ${
                folderId === f.id
                  ? "bg-accent text-white"
                  : "border border-border/40 bg-card text-muted hover:border-border hover:text-foreground"
              }`}
            >
              {f.name || `收藏夹 ${f.id}`}
              <span className="ml-1 opacity-80">({f.count})</span>
            </button>
          ))
        )}
      </div>

      {/* 列表主体 */}
      {loading ? (
        <GridSkeleton />
      ) : error && items.length === 0 ? (
        <JmErrorCard error={error} onRetry={() => loadPage(folderId, 1)} />
      ) : items.length === 0 ? (
        <JmComicGrid comics={[]} emptyText="该收藏夹暂无收藏" />
      ) : (
        <>
          {/* 网格项包相对定位容器:取消收藏按钮覆盖在卡片右上角,不影响卡片 Link 跳转 */}
          {/* 多选容器:提供批量下载(卡片读 context,不改 JmComicCard 签名) */}
          <JmBatchSelectionProvider comics={items}>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {items.map((comic, index) => (
              <div key={`${comic.aid}-${index}`} className="group/item relative">
                <JmComicCard comic={comic} />
                <button
                  type="button"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    handleRemove(comic.aid);
                  }}
                  disabled={removing.has(comic.aid)}
                  title="取消收藏"
                  aria-label={`取消收藏 ${comic.title || comic.aid}`}
                  className="absolute right-1.5 top-1.5 z-10 flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white/90 opacity-100 backdrop-blur-sm transition-colors hover:bg-red-500/80 focus-visible:opacity-100 disabled:cursor-not-allowed sm:opacity-0 sm:group-hover/item:opacity-100"
                >
                  {removing.has(comic.aid) ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <BookmarkX className="h-4 w-4" />
                  )}
                </button>
              </div>
            ))}
          </div>
          </JmBatchSelectionProvider>

          {/* 分页尾部 */}
          {loadingMore ? (
            <div className="flex items-center justify-center gap-2 py-6 text-sm text-muted">
              <Loader2 className="h-4 w-4 animate-spin" />
              加载中…
            </div>
          ) : error ? (
            <div className="flex flex-col items-center gap-2 py-6 text-sm text-muted">
              <span>加载更多失败{isJmApiError(error) ? `:${error.message}` : ""}</span>
              <button
                type="button"
                onClick={() => loadPage(folderId, page + 1)}
                className="rounded-lg border border-border px-4 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-card-hover"
              >
                重试
              </button>
            </div>
          ) : hasNext ? (
            <div className="flex justify-center py-6">
              <button
                type="button"
                onClick={() => loadPage(folderId, page + 1)}
                className="rounded-lg border border-border px-6 py-2 text-sm font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent"
              >
                加载更多
              </button>
            </div>
          ) : (
            <p className="py-6 text-center text-xs text-muted/70">没有更多了</p>
          )}
        </>
      )}
    </div>
  );
}

/** 网格骨架屏(与在线首页同布局) */
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
