"use client";

/**
 * JM 在线源 · 标签收藏(PRD docs/PRD_JM_SOURCE.md;MOBILE_API.md §7 私有扩展)
 * 接口:GET/DELETE /api/jm/tag-favorites
 *
 * - 收藏来源:漫画详情页选中标签/作者后点「收藏」;设备级共享数据(同在线阅读历史口径)
 * - 数据按 type 分两组:「标签」(searchType=tag)与「作者」(searchType=author,👤 图标区分);
 *   同名值可同时以两种类型收藏,互不影响
 * - 交互:点 chip → 跳搜索页自动按对应类型搜索;× 取消收藏(失败回滚并提示)
 * - 空态引导到详情页收藏;错误用 JmErrorCard;不做分页(设备级列表小,全量返回)
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Loader2, Tag, User, X } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { useToast } from "@/components/Toast";
import { isJmApiError, jmRemoveTagFavorite, jmTagFavorites } from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
import { JmDownloadTasksButton } from "@/components/jm/download/DownloadTasks";
import type { JmTagFavorite } from "@/lib/jm/types";

export default function JmTagsPage() {
  return (
    <>
      <PageHeader
        title="标签收藏"
        description="收藏的标签与作者,点击即按对应类型搜索在线漫画"
        icon={Tag}
        actions={
          <>
            <JmDownloadTasksButton />
            <JmBackButton />
          </>
        }
      />
      <PageContent width="management">
        <JmGate>
          <TagsContent />
        </JmGate>
      </PageContent>
    </>
  );
}

function TagsContent() {
  const toast = useToast();
  const navigate = useNavigate();

  const [items, setItems] = useState<JmTagFavorite[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  /** 正在移除的项(type+tag),移除期间禁用其他 × */
  const [removing, setRemoving] = useState<string | null>(null);

  const reqRef = useRef(0);

  const load = useCallback(async () => {
    const req = ++reqRef.current;
    setLoading(true);
    setError(null);
    try {
      const data = await jmTagFavorites();
      if (reqRef.current !== req) return;
      setItems(data.list);
    } catch (err) {
      if (reqRef.current !== req) return;
      setError(err);
    } finally {
      if (reqRef.current === req) setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    // 卸载:作废在途请求
    return () => {
      reqRef.current += 1;
    };
  }, [load]);

  /** 点 chip → 跳搜索页,按该项类型(searchType=tag/author)自动搜索 */
  const searchByItem = useCallback(
    (it: JmTagFavorite) => {
      const params = new URLSearchParams({ keyword: it.tag, searchType: it.type });
      navigate(`/jm/search?${params.toString()}`);
    },
    [navigate]
  );

  /** 取消收藏:失败回滚(重拉真实状态)并提示(管理页操作,不静默) */
  const removeItem = useCallback(
    async (it: JmTagFavorite) => {
      const key = `${it.type}\u0000${it.tag}`;
      if (removing) return;
      setRemoving(key);
      setItems((prev) => prev.filter((x) => !(x.type === it.type && x.tag === it.tag))); // 乐观移除
      try {
        await jmRemoveTagFavorite(it.tag, it.type);
      } catch (err) {
        void load();
        toast.error(isJmApiError(err) ? err.message : "取消收藏失败,请稍后重试");
      } finally {
        setRemoving(null);
      }
    },
    [removing, toast, load]
  );

  const tagItems = items.filter((it) => it.type === "tag");
  const authorItems = items.filter((it) => it.type === "author");
  const hasItems = items.length > 0;

  return (
    <div>
      <p className="mb-4 text-xs text-muted">
        {hasItems
          ? `共 ${items.length} 项(标签 ${tagItems.length} · 作者 ${authorItems.length})`
          : "在漫画详情页点击标签或作者即可收藏到这里"}
      </p>

      {loading ? (
        <div className="flex animate-pulse flex-wrap gap-2">
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="h-7 w-20 animate-pulse rounded-full bg-muted/15" />
          ))}
        </div>
      ) : error ? (
        <JmErrorCard error={error} onRetry={load} />
      ) : !hasItems ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-muted/10">
            <Tag className="h-7 w-7 text-muted/40" />
          </div>
          <p className="text-sm text-muted">还没有收藏任何标签或作者</p>
          <p className="mt-1.5 text-xs text-muted/70">
            打开一部在线漫画,点击详情页的标签或作者并选择「收藏」即可
          </p>
        </div>
      ) : (
        <div className="space-y-5">
          {tagItems.length > 0 && (
            <FavoriteGroup
              title="标签"
              icon={<Tag className="h-3.5 w-3.5" />}
              items={tagItems}
              removing={removing}
              onSearch={searchByItem}
              onRemove={removeItem}
            />
          )}
          {authorItems.length > 0 && (
            <FavoriteGroup
              title="作者"
              icon={<User className="h-3.5 w-3.5" />}
              items={authorItems}
              removing={removing}
              onSearch={searchByItem}
              onRemove={removeItem}
              accent
            />
          )}
        </div>
      )}
    </div>
  );
}

/** 一组收藏 chips(标签或作者):点击按对应类型搜索,× 取消收藏 */
function FavoriteGroup({
  title,
  icon,
  items,
  removing,
  onSearch,
  onRemove,
  accent = false,
}: {
  title: string;
  icon: React.ReactNode;
  items: JmTagFavorite[];
  removing: string | null;
  onSearch: (it: JmTagFavorite) => void;
  onRemove: (it: JmTagFavorite) => void;
  /** 作者组:紫色系 + 👤 图标,与标签区分 */
  accent?: boolean;
}) {
  return (
    <section>
      <h3 className="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted">
        {icon}
        {title}
      </h3>
      <div className="flex flex-wrap gap-2">
        {items.map((it) => {
          const key = `${it.type}\u0000${it.tag}`;
          return (
            <span
              key={key}
              className={`inline-flex items-center gap-0.5 rounded-full border py-1.5 pl-3.5 pr-1.5 text-sm text-foreground ${
                accent ? "border-violet-400/40 bg-card" : "border-border bg-card"
              }`}
            >
              <button
                type="button"
                onClick={() => onSearch(it)}
                title={`按${title}「${it.tag}」搜索`}
                className="inline-flex max-w-48 items-center gap-1 truncate transition-colors hover:text-accent"
              >
                {accent && <User className="h-3.5 w-3.5 text-violet-400" />}
                {it.tag}
              </button>
              <button
                type="button"
                onClick={() => onRemove(it)}
                disabled={removing !== null}
                title="取消收藏"
                className="rounded-full p-0.5 text-muted/60 transition-colors hover:bg-muted/10 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
              >
                {removing === key ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <X className="h-3.5 w-3.5" />
                )}
              </button>
            </span>
          );
        })}
      </div>
    </section>
  );
}
