"use client";

/**
 * JM 在线源 · 漫画详情(PRD docs/PRD_JM_SOURCE.md M7)
 * 接口:MOBILE_API.md #14 detail、#19 like、#22/#23 favorite、#17/#18 comments、#24 history(继续阅读)
 *
 * - 无壳路由(无侧边栏):自带头部(返回 window.history.back() + 标题),内容包 <JmGate>
 * - 切换语义:点赞/收藏一律以响应 liked/favorited 为准;响应值 null/undefined → 回查 jmComicDetail 同步
 * - 初始 liked/favorited 取自详情;继续阅读:挂载时查历史第一页(仅 20 条,不递归),按 aid 匹配
 * - 封面经 resolveJmUrl 走服务端代理;NSFW 命中且隐私模糊开启时恒定遮蔽(与 ComicCard 同口径)
 * - 章节 episodes(pid/title/order,imageCount 可缺省勿强依赖)跳 /jm/reader/{pid},支持正序/倒序
 * - 评论区 <JmCommentPanel>;3001 区分为「漫画不存在」;卸载/重查作废在途请求
 * - 标签可点:选中 → 搜索(跳 /jm/search?keyword&searchType=tag 自动搜索)/ 收藏(标签收藏,设备级)
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  ArrowDownUp,
  ArrowLeft,
  Bookmark,
  BookmarkCheck,
  BookOpen,
  BookX,
  ChevronDown,
  ChevronUp,
  Eye,
  Heart,
  ImageOff,
  Loader2,
  PlayCircle,
  Search,
  Star,
} from "lucide-react";
import { isNSFW } from "@/lib/nsfw";
import { usePrivacyMode } from "@/hooks/usePrivacyMode";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmCommentPanel } from "@/components/jm/CommentPanel";
import { JmDownloadButton } from "@/components/jm/download/DownloadButton";
import { resolveJmUrl } from "@/lib/jm/config";
import {
  isJmApiError,
  jmAddFavorite,
  jmAddTagFavorite,
  jmComicDetail,
  jmHistoryList,
  jmLike,
  jmRemoveFavorite,
  jmRemoveTagFavorite,
  jmTagFavorites,
} from "@/lib/jm/client";
import { JM_ERROR_CODES, type JmComicDetail, type JmEpisode } from "@/lib/jm/types";

/** 继续阅读命中信息(#24 第一页按 aid 匹配) */
interface ContinueInfo {
  pid: string;
  epTitle: string | null;
  imageIndex: number;
}

function trimTrailingZero(s: string): string {
  return s.endsWith(".0") ? s.slice(0, -2) : s;
}

/** 计数字段紧凑格式化(与 ComicCard 同口径) */
function formatCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 1e8) return `${trimTrailingZero((n / 1e8).toFixed(1))}亿`;
  if (n >= 1e4) return `${trimTrailingZero((n / 1e4).toFixed(1))}万`;
  return String(n);
}

/** 互动操作失败 → 用户提示文案(2001 上游 / 2002 网络 / 其余透传) */
function actionErrorText(err: unknown, fallback: string): string {
  if (isJmApiError(err)) {
    if (err.code === 2001) return `上游返回错误${err.message ? `:${err.message}` : ""}`;
    if (err.code === 2002) return `${err.message || "网络不可达"}(请检查网络与上游代理设置)`;
    return err.message || fallback;
  }
  return err instanceof Error ? err.message : fallback;
}

/** 3001 / 空 aid:漫画不存在卡片 */
function NotFoundCard() {
  return (
    <div className="flex min-h-[60vh] items-center justify-center px-4">
      <div className="w-full max-w-md rounded-xl border border-border bg-card p-8 text-center">
        <span className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-red-500/10 text-red-400">
          <BookX className="h-6 w-6" />
        </span>
        <h2 className="text-lg font-semibold text-foreground">漫画不存在</h2>
        <p className="mt-2 text-sm text-muted">该漫画可能已下架,或链接中的 aid 无效。</p>
        <button
          type="button"
          onClick={() => window.history.back()}
          className="mt-6 inline-flex items-center justify-center gap-2 rounded-lg border border-border px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-card-hover"
        >
          <ArrowLeft className="h-4 w-4" />
          返回上一页
        </button>
      </div>
    </div>
  );
}

export default function JmComicDetailPage() {
  const { aid = "" } = useParams();
  return (
    <div className="flex min-h-screen flex-col bg-background text-foreground">
      <JmGate>
        <DetailContent aid={aid} />
      </JmGate>
    </div>
  );
}

function DetailContent({ aid }: { aid: string }) {
  const { enabled: privacyEnabled, blurNSFW } = usePrivacyMode();

  const [detail, setDetail] = useState<JmComicDetail | null>(null);
  const [liked, setLiked] = useState(false);
  const [favorited, setFavorited] = useState(false);
  const [likesCount, setLikesCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);

  const [likeBusy, setLikeBusy] = useState(false);
  const [favBusy, setFavBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const [descExpanded, setDescExpanded] = useState(false);
  const [epAsc, setEpAsc] = useState(true);
  const [continueInfo, setContinueInfo] = useState<ContinueInfo | null>(null);

  const reqRef = useRef(0);

  /** 以详情数据校准互动状态(初始加载与 null 回查共用) */
  const applyDetail = useCallback((d: JmComicDetail) => {
    setDetail(d);
    setLiked(d.liked);
    setFavorited(d.favorited);
    setLikesCount(d.likes);
  }, []);

  const load = useCallback(async () => {
    if (!aid) {
      setLoading(false);
      return;
    }
    const req = ++reqRef.current;
    setLoading(true);
    setError(null);
    try {
      const d = await jmComicDetail(aid);
      if (reqRef.current !== req) return;
      applyDetail(d);
    } catch (err) {
      if (reqRef.current !== req) return;
      setError(err);
    } finally {
      if (reqRef.current === req) setLoading(false);
    }
  }, [aid, applyDetail]);

  useEffect(() => {
    load();
    // aid 变化 / 卸载:作废在途请求
    return () => {
      reqRef.current += 1;
    };
  }, [load]);

  /** 切换结果不明(响应 liked/favorited 为 null/undefined)→ 回查详情同步真实状态 */
  const resyncDetail = useCallback(async () => {
    const req = ++reqRef.current;
    try {
      const d = await jmComicDetail(aid);
      if (reqRef.current !== req) return;
      applyDetail(d);
    } catch {
      // 回查失败不打断当前视图,保留现状
    }
  }, [aid, applyDetail]);

  // 继续阅读:仅查历史第一页(20 条,不递归),命中 aid 则记录进度;增强能力,失败静默
  useEffect(() => {
    if (!aid) return;
    let cancelled = false;
    (async () => {
      try {
        const data = await jmHistoryList(1);
        if (cancelled) return;
        const hit = data.list.find((item) => item.aid === aid) ?? null;
        setContinueInfo(
          hit ? { pid: hit.pid, epTitle: hit.epTitle, imageIndex: hit.imageIndex } : null
        );
      } catch {
        if (!cancelled) setContinueInfo(null);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [aid]);

  // ── 标签搜索/收藏(私有扩展):选中标签 → 搜索/收藏 操作条 ──
  const navigate = useNavigate();
  const [selectedTag, setSelectedTag] = useState<string | null>(null);
  const [savedTags, setSavedTags] = useState<Set<string>>(() => new Set());
  const [tagBusy, setTagBusy] = useState(false);
  const [tagError, setTagError] = useState<string | null>(null);

  // 已收藏标签集:挂载拉取一次(设备级数据);失败静默为空集,不影响浏览
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await jmTagFavorites();
        if (cancelled) return;
        setSavedTags(new Set(data.list.map((it) => it.tag)));
      } catch {
        if (!cancelled) setSavedTags(new Set());
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // 切换漫画:重置选中与错误(收藏集为设备级,无需重拉)
  useEffect(() => {
    setSelectedTag(null);
    setTagError(null);
  }, [aid]);

  /** 标签搜索:跳搜索页,URL 参数初始化并自动执行 searchType=tag 搜索 */
  const handleTagSearch = useCallback(
    (tag: string) => {
      const params = new URLSearchParams({ keyword: tag, searchType: "tag" });
      navigate(`/jm/search?${params.toString()}`);
    },
    [navigate]
  );

  /** 标签收藏切换:乐观更新集合,失败回滚 */
  const handleTagFavorite = useCallback(
    async (tag: string) => {
      if (tagBusy) return;
      setTagBusy(true);
      setTagError(null);
      const saved = savedTags.has(tag);
      setSavedTags((prev) => {
        const next = new Set(prev);
        if (saved) next.delete(tag);
        else next.add(tag);
        return next;
      });
      try {
        if (saved) await jmRemoveTagFavorite(tag);
        else await jmAddTagFavorite(tag);
      } catch (err) {
        setSavedTags((prev) => {
          const next = new Set(prev);
          if (saved) next.add(tag);
          else next.delete(tag);
          return next;
        });
        setTagError(actionErrorText(err, saved ? "取消收藏失败,请稍后重试" : "收藏失败,请稍后重试"));
      } finally {
        setTagBusy(false);
      }
    },
    [savedTags, tagBusy]
  );

  /** 点赞(#19 切换语义):以响应 liked 为准,null → 回查详情 */
  const handleLike = useCallback(async () => {
    if (likeBusy || !aid) return;
    setLikeBusy(true);
    setActionError(null);
    try {
      const res = await jmLike(aid);
      const next = res.liked;
      if (typeof next === "boolean") {
        setLiked(next);
        setLikesCount((n) => Math.max(0, n + (next ? 1 : -1)));
      } else {
        await resyncDetail();
      }
    } catch (err) {
      setActionError(actionErrorText(err, "点赞失败,请稍后重试"));
    } finally {
      setLikeBusy(false);
    }
  }, [aid, likeBusy, resyncDetail]);

  /** 收藏/取消(#22/#23 切换语义):按当前状态选择调用,以响应 favorited 为准,null → 回查详情 */
  const handleFavorite = useCallback(async () => {
    if (favBusy || !aid) return;
    setFavBusy(true);
    setActionError(null);
    try {
      const res = favorited ? await jmRemoveFavorite(aid) : await jmAddFavorite(aid);
      const next = res.favorited;
      if (typeof next === "boolean") {
        setFavorited(next);
      } else {
        await resyncDetail();
      }
    } catch (err) {
      setActionError(actionErrorText(err, "收藏操作失败,请稍后重试"));
    } finally {
      setFavBusy(false);
    }
  }, [aid, favBusy, favorited, resyncDetail]);

  // 第一章 pid(开始阅读/从头开始用;episodes 按 order 正序取第一)
  const firstPid = useMemo(() => {
    const eps = [...(detail?.episodes ?? [])].sort((a, b) => a.order - b.order);
    return eps[0]?.pid ?? "";
  }, [detail]);

  // 空 aid(异常路由):按不存在处理
  if (!aid) return <NotFoundCard />;

  if (error) {
    if (isJmApiError(error) && error.code === JM_ERROR_CODES.NOT_FOUND) {
      return <NotFoundCard />;
    }
    return (
      <>
        <DetailHeader title="漫画详情" />
        <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-5 sm:px-6 sm:py-6">
          <JmErrorCard error={error} onRetry={load} />
        </main>
      </>
    );
  }

  return (
    <>
      <DetailHeader title={detail?.title || "漫画详情"} />
      <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-5 sm:px-6 sm:py-6">
        {loading || !detail ? (
          <DetailSkeleton />
        ) : (
          <DetailBody
            aid={aid}
            detail={detail}
            firstPid={firstPid}
            liked={liked}
            favorited={favorited}
            likesCount={likesCount}
            likeBusy={likeBusy}
            favBusy={favBusy}
            actionError={actionError}
            continueInfo={continueInfo}
            descExpanded={descExpanded}
            onToggleDesc={() => setDescExpanded((v) => !v)}
            epAsc={epAsc}
            onToggleEpOrder={() => setEpAsc((v) => !v)}
            onLike={handleLike}
            onFavorite={handleFavorite}
            privacyEnabled={privacyEnabled}
            blurNSFW={blurNSFW}
            selectedTag={selectedTag}
            savedTags={savedTags}
            tagBusy={tagBusy}
            tagError={tagError}
            onSelectTag={setSelectedTag}
            onTagSearch={handleTagSearch}
            onTagFavorite={handleTagFavorite}
          />
        )}
      </main>
    </>
  );
}

/** 无壳页面自带头部:返回 + 标题(样式对齐 PageHeader) */
function DetailHeader({ title }: { title: string }) {
  return (
    <header className="sticky top-0 z-30 border-b border-border/50 bg-background/90 backdrop-blur-xl">
      <div className="mx-auto flex h-14 min-h-14 w-full max-w-5xl items-center gap-2 px-4 sm:px-6">
        <button
          type="button"
          onClick={() => window.history.back()}
          aria-label="返回"
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-muted transition-colors hover:bg-card hover:text-foreground"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <h1 className="min-w-0 flex-1 truncate text-base font-semibold text-foreground" title={title}>
          {title}
        </h1>
      </div>
    </header>
  );
}

/* ── 详情主体 ── */

interface DetailBodyProps {
  aid: string;
  detail: JmComicDetail;
  firstPid: string;
  liked: boolean;
  favorited: boolean;
  likesCount: number;
  likeBusy: boolean;
  favBusy: boolean;
  actionError: string | null;
  continueInfo: ContinueInfo | null;
  descExpanded: boolean;
  onToggleDesc: () => void;
  epAsc: boolean;
  onToggleEpOrder: () => void;
  onLike: () => void;
  onFavorite: () => void;
  privacyEnabled: boolean;
  blurNSFW: boolean;
  /** 标签搜索/收藏(私有扩展):选中态、已收藏集与回调 */
  selectedTag: string | null;
  savedTags: Set<string>;
  tagBusy: boolean;
  tagError: string | null;
  onSelectTag: (tag: string | null) => void;
  onTagSearch: (tag: string) => void;
  onTagFavorite: (tag: string) => void;
}

function DetailBody({
  aid,
  detail,
  firstPid,
  liked,
  favorited,
  likesCount,
  likeBusy,
  favBusy,
  actionError,
  continueInfo,
  descExpanded,
  onToggleDesc,
  epAsc,
  onToggleEpOrder,
  onLike,
  onFavorite,
  privacyEnabled,
  blurNSFW,
  selectedTag,
  savedTags,
  tagBusy,
  tagError,
  onSelectTag,
  onTagSearch,
  onTagFavorite,
}: DetailBodyProps) {
  // NSFW 遮蔽:与 ComicCard 同口径(标签优先、标题兜底;隐私模式 + 模糊开关同时开启)
  const shouldBlur =
    isNSFW({ tags: detail.tags, title: detail.title }) && privacyEnabled && blurNSFW;
  const coverUrl = resolveJmUrl(detail.coverUrl);
  const descLong = detail.description.length > 80;

  const rawEpisodes = detail.episodes;
  // 章节排序:服务端 order 兜底排序,支持正序/倒序
  const sortedEpisodes = useMemo<JmEpisode[]>(
    () => [...(rawEpisodes ?? [])].sort((a, b) => (epAsc ? a.order - b.order : b.order - a.order)),
    [rawEpisodes, epAsc]
  );

  return (
    <div>
      {/* 信息卡:封面 + 元数据 + 操作条 */}
      <section className="rounded-xl border border-border bg-card p-4 sm:p-5">
        <div className="flex flex-col gap-5 sm:flex-row">
          {/* 封面(resolveJmUrl 服务端代理;NSFW 恒定遮蔽,hover 不解除) */}
          <div className="aspect-[3/4] w-36 shrink-0 self-center overflow-hidden rounded-lg bg-muted/10 sm:self-start sm:w-44">
            {coverUrl ? (
              <img
                src={coverUrl}
                alt={detail.title}
                loading="lazy"
                className={`h-full w-full object-cover ${shouldBlur ? "select-none blur-lg" : ""}`}
              />
            ) : (
              <div className="flex h-full w-full items-center justify-center text-muted/30">
                <ImageOff className="h-8 w-8" />
              </div>
            )}
          </div>

          {/* 元数据 */}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              {detail.category?.name && (
                <span className="rounded-full bg-accent/10 px-2 py-0.5 text-[10px] font-medium text-accent">
                  {detail.category.name}
                </span>
              )}
              {detail.category?.sub && (
                <span className="rounded-full bg-muted/15 px-2 py-0.5 text-[10px] text-muted">
                  {detail.category.sub}
                </span>
              )}
            </div>
            <h2 className="mt-2 text-lg font-semibold leading-snug text-foreground" title={detail.title}>
              {detail.title || "无标题"}
            </h2>
            <p className="mt-1.5 truncate text-sm text-muted">
              {detail.author || "未知作者"}
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted">
              <span className="flex items-center gap-1" title="爱心数">
                <Heart className="h-3.5 w-3.5" />
                {formatCount(likesCount)}
              </span>
              <span className="flex items-center gap-1" title="点击数">
                <Eye className="h-3.5 w-3.5" />
                {formatCount(detail.views)}
              </span>
              <span className="flex items-center gap-1" title="章节数">
                <BookOpen className="h-3.5 w-3.5" />
                {detail.epCount} 章
              </span>
              {detail.updateAt && <span>更新于 {detail.updateAt}</span>}
            </div>
            {detail.tags.length > 0 && (
              <div className="mt-3">
                {/* 标签可点:选中高亮;已收藏标签带 ★ 角标 */}
                <div className="flex flex-wrap gap-1.5">
                  {detail.tags.map((tag) => {
                    const selected = selectedTag === tag;
                    const saved = savedTags.has(tag);
                    return (
                      <button
                        key={tag}
                        type="button"
                        onClick={() => onSelectTag(selected ? null : tag)}
                        title={saved ? "已收藏的标签" : "点击选中标签"}
                        className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] transition-colors ${
                          selected
                            ? "border-accent/60 bg-accent/10 text-accent"
                            : "border-border/60 bg-background text-muted hover:border-accent/40 hover:text-foreground"
                        }`}
                      >
                        {saved && <Star className="h-3 w-3 text-amber-400" fill="currentColor" />}
                        {tag}
                      </button>
                    );
                  })}
                </div>
                {/* 选中标签 → 搜索 / 收藏 操作条 */}
                {selectedTag && (
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    <span className="text-[11px] text-muted">已选标签:</span>
                    <button
                      type="button"
                      onClick={() => onTagSearch(selectedTag)}
                      className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white transition-opacity hover:opacity-90"
                    >
                      <Search className="h-3.5 w-3.5" />
                      搜索
                    </button>
                    <button
                      type="button"
                      disabled={tagBusy}
                      onClick={() => onTagFavorite(selectedTag)}
                      className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-60 ${
                        savedTags.has(selectedTag)
                          ? "border-accent/50 text-accent"
                          : "border-border bg-background text-muted hover:border-accent/50 hover:text-foreground"
                      }`}
                    >
                      <Star
                        className="h-3.5 w-3.5"
                        fill={savedTags.has(selectedTag) ? "currentColor" : "none"}
                      />
                      {savedTags.has(selectedTag) ? "已收藏" : "收藏"}
                    </button>
                    {tagError && <span className="text-[11px] text-red-400">{tagError}</span>}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* 操作条:开始/继续阅读(主)+ 点赞 / 收藏(切换语义) */}
        <div className="mt-5 flex flex-wrap items-center gap-2 border-t border-border/60 pt-4">
          {continueInfo ? (
            <>
              <Link
                to={`/jm/reader/${continueInfo.pid}?page=${continueInfo.imageIndex}`}
                className="inline-flex max-w-full items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
              >
                <PlayCircle className="h-4 w-4 shrink-0" />
                <span className="min-w-0 truncate">
                  继续阅读{continueInfo.epTitle ? ` ${continueInfo.epTitle}` : ""} · 第
                  {continueInfo.imageIndex}页
                </span>
              </Link>
              {firstPid && (
                <Link
                  to={`/jm/reader/${firstPid}`}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-4 py-2 text-sm font-medium text-muted transition-colors hover:border-accent/50 hover:text-foreground"
                >
                  <PlayCircle className="h-4 w-4 shrink-0" />
                  从头开始
                </Link>
              )}
            </>
          ) : (
            firstPid && (
              <Link
                to={`/jm/reader/${firstPid}`}
                className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
              >
                <PlayCircle className="h-4 w-4 shrink-0" />
                开始阅读
              </Link>
            )
          )}

          <button
            type="button"
            onClick={onLike}
            disabled={likeBusy}
            className={`inline-flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
              liked
                ? "border-red-500/40 bg-red-500/10 text-red-400"
                : "border-border bg-background text-muted hover:border-accent/50 hover:text-foreground"
            }`}
          >
            {likeBusy ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Heart className={`h-4 w-4 ${liked ? "fill-current" : ""}`} />
            )}
            {liked ? "已点赞" : "点赞"} · {formatCount(likesCount)}
          </button>

          <button
            type="button"
            onClick={onFavorite}
            disabled={favBusy}
            className={`inline-flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
              favorited
                ? "border-amber-500/40 bg-amber-500/10 text-amber-500"
                : "border-border bg-background text-muted hover:border-accent/50 hover:text-foreground"
            }`}
          >
            {favBusy ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : favorited ? (
              <BookmarkCheck className="h-4 w-4" />
            ) : (
              <Bookmark className="h-4 w-4" />
            )}
            {favorited ? "已收藏" : "收藏"}
          </button>

          {/* 下载:章节选择 + 归档目录(书库管理目录),打包 zip 后清理临时目录 */}
          <JmDownloadButton
            aid={aid}
            title={detail.title}
            author={detail.author}
            chapters={rawEpisodes}
          />
        </div>
        {actionError && <p className="mt-2 break-all text-xs text-red-400">{actionError}</p>}
      </section>

      {/* 简介(纯文本,超长折叠展开) */}
      {detail.description && (
        <section className="mt-4 rounded-xl border border-border bg-card p-4 sm:p-5">
          <h3 className="text-sm font-semibold text-foreground">简介</h3>
          <p
            className={`mt-2 whitespace-pre-wrap break-words text-sm leading-relaxed text-muted ${
              descExpanded ? "" : "line-clamp-4"
            }`}
          >
            {detail.description}
          </p>
          {descLong && (
            <button
              type="button"
              onClick={onToggleDesc}
              className="mt-2 inline-flex items-center gap-1 text-xs text-accent transition-opacity hover:opacity-80"
            >
              {descExpanded ? (
                <>
                  <ChevronUp className="h-3.5 w-3.5" />
                  收起
                </>
              ) : (
                <>
                  <ChevronDown className="h-3.5 w-3.5" />
                  展开全部
                </>
              )}
            </button>
          )}
        </section>
      )}

      {/* 章节列表 */}
      <section className="mt-4 rounded-xl border border-border bg-card p-4 sm:p-5">
        <div className="flex items-center justify-between gap-2">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <BookOpen className="h-4 w-4 text-accent" />
            章节
            <span className="text-xs font-normal text-muted">
              共 {detail.epCount || sortedEpisodes.length} 章
            </span>
          </h3>
          {sortedEpisodes.length > 1 && (
            <button
              type="button"
              onClick={onToggleEpOrder}
              className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted transition-colors hover:border-accent/50 hover:text-foreground"
            >
              <ArrowDownUp className="h-3.5 w-3.5" />
              {epAsc ? "正序" : "倒序"}
            </button>
          )}
        </div>

        <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {sortedEpisodes.map((ep) => {
            const isCurrent = continueInfo?.pid === ep.pid;
            return (
              <Link
                key={`${ep.pid}-${ep.order}`}
                to={`/jm/reader/${ep.pid}`}
                title={ep.title}
                className={`flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm transition-colors ${
                  isCurrent
                    ? "border-accent bg-accent/10 text-accent"
                    : "border-border/60 bg-background text-foreground/90 hover:border-accent/40 hover:text-accent"
                }`}
              >
                <span className="min-w-0 truncate">{ep.title || `第${ep.order}话`}</span>
                {typeof ep.imageCount === "number" && ep.imageCount > 0 && (
                  <span className="shrink-0 text-[11px] text-muted">{ep.imageCount}P</span>
                )}
              </Link>
            );
          })}
        </div>
      </section>

      {/* 评论区(#17/#18) */}
      <div className="mt-4">
        <JmCommentPanel aid={aid} />
      </div>
    </div>
  );
}

/** 详情骨架屏 */
function DetailSkeleton() {
  return (
    <div className="animate-pulse">
      <div className="rounded-xl border border-border bg-card p-4 sm:p-5">
        <div className="flex flex-col gap-5 sm:flex-row">
          <div className="aspect-[3/4] w-36 shrink-0 self-center rounded-lg bg-muted/15 sm:w-44" />
          <div className="flex-1 space-y-3 py-1">
            <div className="h-4 w-20 rounded bg-muted/15" />
            <div className="h-5 w-3/4 rounded bg-muted/15" />
            <div className="h-3.5 w-1/3 rounded bg-muted/15" />
            <div className="h-3.5 w-1/2 rounded bg-muted/15" />
            <div className="flex gap-1.5 pt-1">
              {Array.from({ length: 3 }, (_, i) => (
                <div key={i} className="h-5 w-14 rounded bg-muted/15" />
              ))}
            </div>
          </div>
        </div>
        <div className="mt-5 flex gap-2 border-t border-border/60 pt-4">
          <div className="h-9 w-28 rounded-lg bg-muted/15" />
          <div className="h-9 w-24 rounded-lg bg-muted/15" />
          <div className="h-9 w-24 rounded-lg bg-muted/15" />
        </div>
      </div>
      <div className="mt-4 h-24 rounded-xl border border-border bg-card" />
      <div className="mt-4 h-44 rounded-xl border border-border bg-card" />
    </div>
  );
}
