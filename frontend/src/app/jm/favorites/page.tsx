"use client";

/**
 * JM 在线源 · 我的收藏(PRD docs/PRD_JM_SOURCE.md M9)
 * 接口:MOBILE_API.md #20 folders、#21 favorites、#23 remove favorite
 *
 * - 收藏夹 chips(#20):id + name + count,点击切换(默认 "0");切换重置回第一页
 * - 列表分页(#21,20/页):hasNext 时「加载更多」
 * - 取消收藏(#23 切换语义):网格项右上角按钮,stopPropagation + preventDefault 后调用;
 *   响应 favorited===false → 本地移除;null/其他 → 重拉当前页校准
 * - 误删防护:移除后顶部出现「已取消收藏 · 撤销」条(8 秒),撤销走 #22 重新收藏并插回原位置;
 *   手机上按钮常显,靠撤销而不是确认弹窗(确认弹窗会挡住每次正常取消)
 * - JmComicCard 内部是 Link,取消按钮为兄弟层覆盖(z-10),不破坏卡片跳转
 * - 空态 / 错误(JmErrorCard)/ 骨架屏齐全;不做轮询
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { BookmarkX, FolderOpen, Loader2, Undo2, X } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmComicCard } from "@/components/jm/ComicCard";
import { JmComicGrid } from "@/components/jm/ComicGrid";
import { JmBatchSelectionProvider } from "@/components/jm/download/BatchDownload";
import { useToast } from "@/components/Toast";
import {
  isJmApiError,
  jmAddFavorite,
  jmFavoriteFolders,
  jmFavorites,
  jmRemoveFavorite,
} from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
import { JmDownloadTasksButton } from "@/components/jm/download/DownloadTasks";
import type { JmComicItem, JmFavoriteFolder } from "@/lib/jm/types";

const DEFAULT_FOLDER_ID = "0";

/** 撤销条保留时长(ms):足够看清并点撤销,又不至于长期占据版面 */
const UNDO_TTL_MS = 8000;

/** 刚被移除、可撤销的一项(记住原位置,撤销时插回原处) */
interface Undoable {
  aid: string;
  comic: JmComicItem;
  index: number;
}

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

  // 可撤销的最近一次取消收藏(单条:连续取消时以最新为准)
  const [undoable, setUndoable] = useState<Undoable | null>(null);
  const undoTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

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
   * - favorited === false → 确认已移除,本地剔除(收藏夹计数后台静默刷新)+ 弹撤销条
   * - null / 其他(结果不明)→ 重拉当前页校准
   */
  const handleRemove = useCallback(
    async (aid: string) => {
      setRemoving((prev) => {
        if (prev.has(aid)) return prev;
        return new Set(prev).add(aid);
      });
      try {
        // 先记下被移除项及其位置,成功后才能撤销回原位
        const index = items.findIndex((it) => it.aid === aid);
        const comic = index >= 0 ? items[index] : null;
        const res = await jmRemoveFavorite(aid);
        if (res.favorited === false) {
          setItems((prev) => prev.filter((it) => it.aid !== aid));
          loadFolders(); // 静默刷新收藏夹计数,失败不影响列表
          if (comic) {
            if (undoTimerRef.current) clearTimeout(undoTimerRef.current);
            setUndoable({ aid, comic, index });
            undoTimerRef.current = setTimeout(() => setUndoable(null), UNDO_TTL_MS);
          }
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
    [items, folderId, page, loadFolders, loadPage, toast]
  );

  /** 撤销取消收藏:重新收藏并插回原位置,失败则提示并重拉校准 */
  const handleUndo = useCallback(async () => {
    if (!undoable) return;
    const target = undoable;
    if (undoTimerRef.current) clearTimeout(undoTimerRef.current);
    setUndoable(null);
    try {
      const res = await jmAddFavorite(target.aid);
      if (res.favorited === false) {
        toast.error("撤销失败:该作品未能恢复收藏");
        await loadPage(folderId, page || 1);
        return;
      }
      setItems((prev) => {
        if (prev.some((it) => it.aid === target.aid)) return prev;
        const next = [...prev];
        next.splice(Math.min(target.index, next.length), 0, target.comic);
        return next;
      });
      loadFolders();
      toast.success("已恢复收藏");
    } catch (err) {
      toast.error(isJmApiError(err) ? err.message : "撤销失败,请稍后重试");
      await loadPage(folderId, page || 1);
    }
  }, [undoable, folderId, page, loadFolders, loadPage, toast]);

  // 卸载时清掉撤销计时器,避免对已卸载组件 setState
  useEffect(
    () => () => {
      if (undoTimerRef.current) clearTimeout(undoTimerRef.current);
    },
    []
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

      {/* 撤销条:刚才那张卡片仍在原位置,误点一下就能立刻撤回 */}
      {undoable && (
        <div className="mb-4 flex items-center gap-2 rounded-xl border border-border/60 bg-card px-3 py-2 text-xs">
          <BookmarkX className="h-3.5 w-3.5 shrink-0 text-muted" />
          <span className="min-w-0 flex-1 truncate text-muted">
            已取消收藏「{undoable.comic.title || undoable.aid}」
          </span>
          <button
            type="button"
            onClick={() => void handleUndo()}
            className="inline-flex shrink-0 items-center gap-1 rounded-md border border-accent/50 bg-accent/10 px-2 py-1 font-medium text-accent transition-colors hover:bg-accent/15"
          >
            <Undo2 className="h-3 w-3" />
            撤销
          </button>
          <button
            type="button"
            onClick={() => setUndoable(null)}
            aria-label="关闭提示"
            className="shrink-0 rounded p-0.5 text-muted/60 transition-colors hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}

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
                  className="absolute right-1.5 top-1.5 z-10 flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white/90 opacity-0 backdrop-blur-sm transition-opacity hover:bg-red-500/80 focus-visible:opacity-100 disabled:cursor-not-allowed group-hover/item:opacity-100 [@media(hover:none)]:opacity-100"
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
