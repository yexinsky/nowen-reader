"use client";

/**
 * JM 在线源 · 标签收藏(PRD docs/PRD_JM_SOURCE.md;MOBILE_API.md §7 私有扩展)
 * 接口:GET/DELETE /api/jm/tag-favorites
 *
 * - 收藏来源:漫画详情页选中标签后点「收藏」;设备级共享数据(同在线阅读历史口径)
 * - 交互:点标签 chip → 跳搜索页自动按该标签执行 searchType=tag 搜索;× 取消收藏(失败回滚并提示)
 * - 空态引导到详情页收藏;错误用 JmErrorCard;不做分页(设备级列表小,全量返回)
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Loader2, Tag, X } from "lucide-react";
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
        description="收藏的标签,点击即按标签搜索在线漫画"
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

  const [tags, setTags] = useState<JmTagFavorite[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [removing, setRemoving] = useState<string | null>(null);

  const reqRef = useRef(0);

  const load = useCallback(async () => {
    const req = ++reqRef.current;
    setLoading(true);
    setError(null);
    try {
      const data = await jmTagFavorites();
      if (reqRef.current !== req) return;
      setTags(data.list);
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

  /** 点标签 → 跳搜索页,自动按该标签执行 searchType=tag 搜索 */
  const searchByTag = useCallback(
    (tag: string) => {
      const params = new URLSearchParams({ keyword: tag, searchType: "tag" });
      navigate(`/jm/search?${params.toString()}`);
    },
    [navigate]
  );

  /** 取消收藏:失败回滚(重拉真实状态)并提示(管理页操作,不静默) */
  const removeTag = useCallback(
    async (tag: string) => {
      if (removing) return;
      setRemoving(tag);
      setTags((prev) => prev.filter((it) => it.tag !== tag)); // 乐观移除
      try {
        await jmRemoveTagFavorite(tag);
      } catch (err) {
        void load();
        toast.error(isJmApiError(err) ? err.message : "取消收藏失败,请稍后重试");
      } finally {
        setRemoving(null);
      }
    },
    [removing, toast, load]
  );

  return (
    <div>
      <p className="mb-4 text-xs text-muted">
        {tags.length > 0 ? `共 ${tags.length} 个标签` : "在漫画详情页点击标签即可收藏到这里"}
      </p>

      {loading ? (
        <div className="flex animate-pulse flex-wrap gap-2">
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="h-7 w-20 animate-pulse rounded-full bg-muted/15" />
          ))}
        </div>
      ) : error ? (
        <JmErrorCard error={error} onRetry={load} />
      ) : tags.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-muted/10">
            <Tag className="h-7 w-7 text-muted/40" />
          </div>
          <p className="text-sm text-muted">还没有收藏任何标签</p>
          <p className="mt-1.5 text-xs text-muted/70">
            打开一部在线漫画,点击详情页的标签并选择「收藏」即可
          </p>
        </div>
      ) : (
        <div className="flex flex-wrap gap-2">
          {tags.map(({ tag }) => (
            <span
              key={tag}
              className="inline-flex items-center gap-0.5 rounded-full border border-border bg-card py-1.5 pl-3.5 pr-1.5 text-sm text-foreground"
            >
              <button
                type="button"
                onClick={() => searchByTag(tag)}
                title={`按标签「${tag}」搜索`}
                className="max-w-48 truncate transition-colors hover:text-accent"
              >
                {tag}
              </button>
              <button
                type="button"
                onClick={() => removeTag(tag)}
                disabled={removing !== null}
                title="取消收藏"
                className="rounded-full p-0.5 text-muted/60 transition-colors hover:bg-muted/10 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
              >
                {removing === tag ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <X className="h-3.5 w-3.5" />
                )}
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
