"use client";

/**
 * JM 在线源 · 阅读历史(PRD docs/PRD_JM_SOURCE.md M10)
 * 接口:MOBILE_API.md #24 history list、#26 clear
 *
 * - 此历史为 JM 服务端数据(设备级共享),与 Nowen Reader 本地「阅读历史」页完全独立
 * - 分页加载(20/页),hasNext 时「加载更多」;按 updatedAt 倒序由服务端保证
 * - 列表项:封面缩略图(resolveJmUrl)+ 标题 + 章节名 + 「看到第 N 页」+ 更新时间,
 *   整项点击直达 /jm/reader/{pid}?page={imageIndex}&aid={aid}
 * - 封面按敏感内容口径处理:标题命中 NSFW 且隐私模糊开启时恒定遮蔽(与 ComicCard 同口径,历史项无标签走标题兜底)
 * - 顶部「清空历史」危险按钮:确认对话框 → jmClearHistory → 重新加载第一页
 * - 空态 / 错误(JmErrorCard)/ 骨架屏齐全;不做轮询
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { ChevronRight, History, ImageOff, Loader2, Trash2 } from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { useToast } from "@/components/Toast";
import { isNSFW } from "@/lib/nsfw";
import { usePrivacyMode } from "@/hooks/usePrivacyMode";
import { resolveJmUrl } from "@/lib/jm/config";
import { isJmApiError, jmClearHistory, jmHistoryList } from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
import { JmDownloadTasksButton } from "@/components/jm/download/DownloadTasks";
import type { JmHistoryItem } from "@/lib/jm/types";

/** updatedAt(本地时区 ISO8601)→ "YYYY-MM-DD HH:mm";解析失败原样返回 */
function formatDateTime(raw: string): string {
  if (!raw) return "";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return raw;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export default function JmHistoryPage() {
  return (
    <>
      <PageHeader
        title="在线·阅读历史"
        description="JM 服务端历史,与本地阅读历史相互独立"
        icon={History}
        actions={
          <>
            <JmDownloadTasksButton />
            <JmBackButton />
          </>
        }
      />
      <PageContent width="management">
        <JmGate>
          <HistoryContent />
        </JmGate>
      </PageContent>
    </>
  );
}

function HistoryContent() {
  const toast = useToast();
  const { enabled: privacyEnabled, blurNSFW } = usePrivacyMode();

  const [items, setItems] = useState<JmHistoryItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [hasNext, setHasNext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const [clearOpen, setClearOpen] = useState(false);
  const [clearing, setClearing] = useState(false);

  const reqRef = useRef(0);

  const loadPage = useCallback(async (nextPage: number) => {
    const req = ++reqRef.current;
    if (nextPage === 1) {
      setLoading(true);
      setError(null);
    } else {
      setLoadingMore(true);
    }
    try {
      const data = await jmHistoryList(nextPage);
      if (reqRef.current !== req) return;
      setPage(nextPage);
      setTotal(data.total ?? 0);
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

  useEffect(() => {
    loadPage(1);
    // 卸载:作废在途请求
    return () => {
      reqRef.current += 1;
    };
  }, [loadPage]);

  /** 清空历史(#26):确认后执行,成功重拉第一页 */
  const handleClear = useCallback(async () => {
    if (clearing) return;
    setClearing(true);
    try {
      await jmClearHistory();
      toast.success("在线阅读历史已清空");
      setClearOpen(false);
      await loadPage(1);
    } catch (err) {
      toast.error(isJmApiError(err) ? err.message : "清空失败,请稍后重试");
    } finally {
      setClearing(false);
    }
  }, [clearing, toast, loadPage]);

  const hasItems = items.length > 0;

  return (
    <div>
      {/* 顶部操作条 */}
      <div className="mb-4 flex items-center justify-between gap-3">
        <p className="min-w-0 truncate text-xs text-muted">
          {total > 0 ? `共 ${total} 条记录` : "按阅读时间倒序"}
        </p>
        <button
          type="button"
          onClick={() => setClearOpen(true)}
          disabled={!hasItems || clearing}
          className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-red-500/30 bg-red-500/5 px-3 py-1.5 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-40"
        >
          <Trash2 className="h-3.5 w-3.5" />
          清空历史
        </button>
      </div>

      {/* 列表主体 */}
      {loading ? (
        <ListSkeleton />
      ) : error && !hasItems ? (
        <JmErrorCard error={error} onRetry={() => loadPage(1)} />
      ) : !hasItems ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-muted/10">
            <History className="h-7 w-7 text-muted/40" />
          </div>
          <p className="text-sm text-muted">暂无在线阅读历史</p>
          <p className="mt-1.5 text-xs text-muted/70">
            在线源内开始阅读后,进度会自动同步到这里
          </p>
        </div>
      ) : (
        <>
          <div className="space-y-2">
            {items.map((item, index) => (
              <HistoryRow
                key={`${item.aid}-${item.pid}-${index}`}
                item={item}
                privacyEnabled={privacyEnabled}
                blurNSFW={blurNSFW}
              />
            ))}
          </div>

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
                onClick={() => loadPage(page + 1)}
                className="rounded-lg border border-border px-4 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-card-hover"
              >
                重试
              </button>
            </div>
          ) : hasNext ? (
            <div className="flex justify-center py-6">
              <button
                type="button"
                onClick={() => loadPage(page + 1)}
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

      {/* 清空确认对话框 */}
      {clearOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4"
          role="dialog"
          aria-modal="true"
          aria-label="确认清空在线阅读历史"
        >
          <div className="w-full max-w-sm rounded-xl border border-border bg-card p-6">
            <h3 className="flex items-center gap-2 text-base font-semibold text-foreground">
              <Trash2 className="h-4 w-4 text-red-400" />
              清空在线阅读历史
            </h3>
            <p className="mt-2 text-sm leading-relaxed text-muted">
              将删除 JM 服务端的全部阅读历史{total > 0 ? `(共 ${total} 条)` : ""},
              该操作不可恢复;不影响本地阅读历史。
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setClearOpen(false)}
                disabled={clearing}
                className="rounded-lg border border-border px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-card-hover disabled:opacity-60"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleClear}
                disabled={clearing}
                className="inline-flex items-center gap-1.5 rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {clearing && <Loader2 className="h-4 w-4 animate-spin" />}
                {clearing ? "清空中…" : "确认清空"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/** 历史行:整项为 Link,直达阅读器并定位页码 */
function HistoryRow({
  item,
  privacyEnabled,
  blurNSFW,
}: {
  item: JmHistoryItem;
  privacyEnabled: boolean;
  blurNSFW: boolean;
}) {
  // 历史项无标签数据,按标题兜底判定 NSFW;命中且模糊开启时恒定遮蔽
  const shouldBlur = isNSFW({ title: item.title }) && privacyEnabled && blurNSFW;
  const coverUrl = resolveJmUrl(item.coverUrl);
  const timeText = formatDateTime(item.updatedAt);

  return (
    <Link
      to={`/jm/reader/${item.pid}?page=${item.imageIndex}&aid=${item.aid}`}
      className="group flex items-center gap-3 rounded-xl border border-border bg-card p-3 transition-colors hover:border-accent/40 hover:bg-card-hover"
    >
      {/* 封面缩略图 */}
      <div className="aspect-[3/4] w-16 shrink-0 overflow-hidden rounded-md bg-muted/10 sm:w-[72px]">
        {coverUrl ? (
          <img
            src={coverUrl}
            alt={item.title}
            loading="lazy"
            className={`h-full w-full object-cover ${shouldBlur ? "select-none blur-lg" : ""}`}
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-muted/30">
            <ImageOff className="h-5 w-5" />
          </div>
        )}
      </div>

      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium text-foreground/90 transition-colors group-hover:text-accent" title={item.title}>
          {item.title || "无标题"}
        </p>
        <p className="mt-1 truncate text-xs text-muted" title={item.epTitle ?? undefined}>
          {item.epTitle || "默认章节"}
        </p>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted/80">
          <span>看到第 {item.imageIndex} 页</span>
          {timeText && <span>{timeText}</span>}
        </div>
      </div>

      <ChevronRight className="h-4 w-4 shrink-0 text-muted/50 transition-transform group-hover:translate-x-0.5" />
    </Link>
  );
}

/** 列表骨架屏 */
function ListSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex animate-pulse items-center gap-3 rounded-xl border border-border bg-card p-3">
          <div className="aspect-[3/4] w-16 shrink-0 rounded-md bg-muted/15 sm:w-[72px]" />
          <div className="min-w-0 flex-1 space-y-2">
            <div className="h-4 w-2/3 rounded bg-muted/15" />
            <div className="h-3 w-1/2 rounded bg-muted/15" />
            <div className="h-3 w-1/3 rounded bg-muted/15" />
          </div>
        </div>
      ))}
    </div>
  );
}
