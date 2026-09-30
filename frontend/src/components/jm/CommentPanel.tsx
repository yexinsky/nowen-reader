"use client";

/**
 * JM 在线源 · 评论面板(#17 GET /api/comics/{aid}/comments、#18 POST 发布)
 * PRD docs/PRD_JM_SOURCE.md M7
 *
 * - 分页加载(20/页),hasNext 时提供「加载更多」;不做轮询
 * - 楼中楼 replies 缩进展示;replyTo 非空显示「回复 @xxx」
 * - 头像经 resolveJmUrl 走服务端代理,null 用首字母占位
 * - content 为纯文本(服务端已剥 HTML),whitespace-pre-wrap 渲染,禁止 dangerouslySetInnerHTML
 * - 发布成功后清空输入并刷新第一页;失败按错误码提示(2001 上游错误 / 2002 网络)
 * - aid 变化 / 卸载时作废在途请求(reqRef 序号守卫),不更新过期状态
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, MessageSquare, Send, ThumbsUp } from "lucide-react";
import { useToast } from "@/components/Toast";
import { isJmApiError, jmAddComment, jmComments } from "@/lib/jm/client";
import { resolveJmUrl } from "@/lib/jm/config";
import type { JmCommentItem } from "@/lib/jm/types";

/** 评论相关失败 → 用户提示文案(2001 上游错误 / 2002 网络 / 其余透传消息) */
function commentErrorText(err: unknown, fallback: string): string {
  if (isJmApiError(err)) {
    if (err.code === 2001) return `上游返回错误${err.message ? `:${err.message}` : ""}`;
    if (err.code === 2002) return `${err.message || "网络不可达"}(请检查网络与上游代理设置)`;
    return err.message || fallback;
  }
  return err instanceof Error ? err.message : fallback;
}

/** 评论头像:null 用首字母占位(与在线首页用户卡同口径) */
function CommentAvatar({
  name,
  avatarUrl,
  size = "md",
}: {
  name: string;
  avatarUrl: string | null;
  size?: "md" | "sm";
}) {
  const url = resolveJmUrl(avatarUrl);
  const initial = (name || "?").trim().charAt(0).toUpperCase() || "?";
  const boxCls = size === "sm" ? "h-7 w-7 text-xs" : "h-9 w-9 text-sm";

  if (url) {
    return (
      <img
        src={url}
        alt={name}
        loading="lazy"
        className={`${boxCls} shrink-0 rounded-full object-cover ring-1 ring-border`}
      />
    );
  }
  return (
    <span
      className={`${boxCls} flex shrink-0 items-center justify-center rounded-full bg-accent/15 font-semibold text-accent`}
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}

/** 单条评论(顶层与楼中楼复用;内容一律纯文本渲染) */
function CommentEntry({ comment, isReply = false }: { comment: JmCommentItem; isReply?: boolean }) {
  return (
    <div className={isReply ? "flex gap-2" : "flex gap-3"}>
      <CommentAvatar name={comment.user.name} avatarUrl={comment.user.avatarUrl} size={isReply ? "sm" : "md"} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="text-xs font-semibold text-foreground">
            {comment.user.name || "匿名用户"}
          </span>
          {comment.replyTo && (
            <span className="text-[11px] text-muted">回复 @{comment.replyTo}</span>
          )}
          {comment.createdAt && <span className="text-[11px] text-muted/70">{comment.createdAt}</span>}
        </div>
        <p className="mt-1 whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground/90">
          {comment.content}
        </p>
        {comment.likes > 0 && (
          <span className="mt-1 inline-flex items-center gap-1 text-[11px] text-muted" title="点赞数">
            <ThumbsUp className="h-3 w-3" />
            {comment.likes}
          </span>
        )}
      </div>
    </div>
  );
}

/** 骨架行 */
function CommentSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="space-y-4 py-1">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex animate-pulse gap-3">
          <div className="h-9 w-9 shrink-0 rounded-full bg-muted/15" />
          <div className="flex-1 space-y-2 pt-1">
            <div className="h-3 w-24 rounded bg-muted/15" />
            <div className="h-3.5 w-3/4 rounded bg-muted/15" />
          </div>
        </div>
      ))}
    </div>
  );
}

/** 精确导出签名:详情页消费,勿改动 */
export function JmCommentPanel({ aid }: { aid: string }) {
  const toast = useToast();
  const [comments, setComments] = useState<JmCommentItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const [content, setContent] = useState("");
  const [posting, setPosting] = useState(false);
  const [postError, setPostError] = useState<string | null>(null);

  const reqRef = useRef(0);

  const loadPage = useCallback(
    async (nextPage: number) => {
      const req = ++reqRef.current;
      if (nextPage === 1) {
        setLoading(true);
        setError(null);
      } else {
        setLoadingMore(true);
      }
      try {
        const data = await jmComments(aid, nextPage);
        if (reqRef.current !== req) return;
        setPage(nextPage);
        setTotal(data.total ?? 0);
        setHasNext(data.hasNext);
        setComments((prev) => (nextPage === 1 ? data.list : [...prev, ...data.list]));
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
    [aid]
  );

  // aid 变化(或卸载)→ 作废在途请求并重置,重新加载第一页
  useEffect(() => {
    setComments([]);
    setPage(0);
    setHasNext(false);
    setError(null);
    loadPage(1);
    return () => {
      reqRef.current += 1;
    };
  }, [loadPage]);

  const handlePublish = useCallback(async () => {
    const text = content.trim();
    if (!text || posting) return;
    setPosting(true);
    setPostError(null);
    try {
      await jmAddComment(aid, text);
      toast.success("评论发布成功");
      setContent("");
      await loadPage(1); // 刷新第一页(新评论在顶部)
    } catch (err) {
      setPostError(commentErrorText(err, "评论发布失败,请稍后重试"));
    } finally {
      setPosting(false);
    }
  }, [aid, content, posting, toast, loadPage]);

  const hasInput = content.trim().length > 0;

  return (
    <section className="rounded-xl border border-border bg-card p-4 sm:p-5">
      {/* 标题 */}
      <h3 className="flex items-center gap-2 text-sm font-semibold text-foreground">
        <MessageSquare className="h-4 w-4 text-accent" />
        评论
        {total > 0 && <span className="text-xs font-normal text-muted">共 {total} 条</span>}
      </h3>

      {/* 发布区 */}
      <div className="mt-3">
        <textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          onKeyDown={(e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
              e.preventDefault();
              handlePublish();
            }
          }}
          placeholder="说点什么吧…(Ctrl + Enter 快捷发布)"
          rows={3}
          maxLength={1000}
          className="w-full resize-none rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted/60 focus:border-accent"
        />
        <div className="mt-2 flex items-center justify-between gap-2">
          {postError ? (
            <p className="min-w-0 flex-1 break-all text-xs text-red-400">{postError}</p>
          ) : (
            <p className="min-w-0 flex-1 text-[11px] text-muted/60">内容按纯文本展示,发布后刷新列表</p>
          )}
          <button
            type="button"
            onClick={handlePublish}
            disabled={posting || !hasInput}
            className="inline-flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {posting ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                发布中…
              </>
            ) : (
              <>
                <Send className="h-4 w-4" />
                发布
              </>
            )}
          </button>
        </div>
      </div>

      {/* 列表 */}
      <div className="mt-2">
        {loading ? (
          <CommentSkeleton />
        ) : error && comments.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-8 text-sm text-muted">
            <span>{commentErrorText(error, "评论加载失败,请稍后重试")}</span>
            <button
              type="button"
              onClick={() => loadPage(1)}
              className="rounded-lg border border-border px-4 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-card-hover"
            >
              重试
            </button>
          </div>
        ) : comments.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted">还没有评论,来抢个沙发吧</p>
        ) : (
          <div>
            {comments.map((c) => (
              <div key={c.id} className="border-b border-border/40 py-4 first:pt-3 last:border-b-0">
                <CommentEntry comment={c} />
                {c.replies.length > 0 && (
                  <div className="mt-3 space-y-3 border-l-2 border-border/60 pl-3 sm:pl-4">
                    {c.replies.map((r) => (
                      <CommentEntry key={r.id} comment={r} isReply />
                    ))}
                  </div>
                )}
              </div>
            ))}

            {/* 分页尾部 */}
            {loadingMore ? (
              <div className="flex items-center justify-center gap-2 py-4 text-sm text-muted">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载中…
              </div>
            ) : error ? (
              <div className="flex flex-col items-center gap-2 py-4 text-sm text-muted">
                <span>{commentErrorText(error, "加载更多失败")}</span>
                <button
                  type="button"
                  onClick={() => loadPage(page + 1)}
                  className="rounded-lg border border-border px-4 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-card-hover"
                >
                  重试
                </button>
              </div>
            ) : hasNext ? (
              <div className="flex justify-center py-4">
                <button
                  type="button"
                  onClick={() => loadPage(page + 1)}
                  className="rounded-lg border border-border px-5 py-2 text-sm font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent"
                >
                  加载更多评论
                </button>
              </div>
            ) : (
              <p className="py-4 text-center text-xs text-muted/70">没有更多评论了</p>
            )}
          </div>
        )}
      </div>
    </section>
  );
}
