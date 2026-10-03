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
 * - 标签可点(支持多选):「搜索」取最近点选的一个跳 /jm/search?keyword&searchType=tag 自动搜索;
 *   「收藏/取消收藏」对选中集合批量切换(乐观更新,失败精确回滚;设备级标签收藏)
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
  User,
  X,
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
import {
  JM_ERROR_CODES,
  type JmComicDetail,
  type JmEpisode,
  type JmTagFavoriteType,
} from "@/lib/jm/types";

/** 继续阅读命中信息(#24 第一页按 aid 匹配) */
interface ContinueInfo {
  pid: string;
  epTitle: string | null;
  imageIndex: number;
}

/** 可点选项(标签/作者统一模型):type 决定搜索 searchType 与收藏类别 */
interface SelectableItem {
  type: JmTagFavoriteType;
  value: string;
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

  // ── 标签/作者搜索与收藏(私有扩展):统一选中模型,批量收藏;搜索取最近点选 ──
  const navigate = useNavigate();
  // 选中集合(数组保序:末位 = 最近点选项,「搜索」作用对象);type 区分搜索与收藏类别
  const [selectedItems, setSelectedItems] = useState<SelectableItem[]>([]);
  const [savedTags, setSavedTags] = useState<Set<string>>(() => new Set());
  const [savedAuthors, setSavedAuthors] = useState<Set<string>>(() => new Set());
  const [tagBusy, setTagBusy] = useState(false);
  const [tagError, setTagError] = useState<string | null>(null);

  // 已收藏标签/作者集:挂载拉取一次按 type 分组(设备级数据);失败静默为空集,不影响浏览
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const data = await jmTagFavorites();
        if (cancelled) return;
        setSavedTags(new Set(data.list.filter((it) => it.type === "tag").map((it) => it.tag)));
        setSavedAuthors(new Set(data.list.filter((it) => it.type === "author").map((it) => it.tag)));
      } catch {
        if (!cancelled) {
          setSavedTags(new Set());
          setSavedAuthors(new Set());
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // 切换漫画:重置选中与错误(收藏集为设备级,无需重拉)
  useEffect(() => {
    setSelectedItems([]);
    setTagError(null);
  }, [aid]);

  /** 快捷搜索:跳搜索页,按最近点选项的类型决定 searchType(tag/author),自动执行搜索 */
  const handleQuickSearch = useCallback(() => {
    const last = selectedItems[selectedItems.length - 1];
    if (!last) return;
    const params = new URLSearchParams({
      keyword: last.value,
      searchType: last.type === "author" ? "author" : "tag",
    });
    navigate(`/jm/search?${params.toString()}`);
  }, [navigate, selectedItems]);

  /** 点选/取消点选一个标签或作者(保序,便于「搜索」取最近点选) */
  const handleToggleItem = useCallback((item: SelectableItem) => {
    setSelectedItems((prev) => {
      const exists = prev.some((it) => it.type === item.type && it.value === item.value);
      return exists ? prev.filter((it) => !(it.type === item.type && it.value === item.value)) : [...prev, item];
    });
  }, []);

  /** 清空选择(标签与作者一起清) */
  const handleClearItems = useCallback(() => setSelectedItems([]), []);

  /**
   * 批量收藏切换(标签与作者统一):存在未收藏的选中项 → 收藏它们(已收藏项不动);
   * 全部已收藏 → 批量取消。逐项并发调用(幂等端点,按 type 分流),失败项精确回滚。
   */
  const handleFavoriteBatch = useCallback(async () => {
    if (tagBusy || selectedItems.length === 0) return;
    setTagBusy(true);
    setTagError(null);
    const savedOf = (it: SelectableItem) => (it.type === "author" ? savedAuthors : savedTags).has(it.value);
    const toAdd = selectedItems.filter((it) => !savedOf(it));
    const toRemove = selectedItems.filter((it) => savedOf(it));
    const removing = toAdd.length === 0; // 全部已收藏 → 批量取消
    const targets = removing ? toRemove : toAdd;
    // 乐观更新(按类型分流到两个集合)
    const apply = (items: SelectableItem[], mode: "add" | "delete") => {
      setSavedTags((prev) => {
        const scope = items.filter((it) => it.type === "tag");
        if (scope.length === 0) return prev;
        const next = new Set(prev);
        scope.forEach((it) => (mode === "add" ? next.add(it.value) : next.delete(it.value)));
        return next;
      });
      setSavedAuthors((prev) => {
        const scope = items.filter((it) => it.type === "author");
        if (scope.length === 0) return prev;
        const next = new Set(prev);
        scope.forEach((it) => (mode === "add" ? next.add(it.value) : next.delete(it.value)));
        return next;
      });
    };
    apply(targets, removing ? "delete" : "add");
    try {
      const ops = targets.map((it) =>
        removing ? jmRemoveTagFavorite(it.value, it.type) : jmAddTagFavorite(it.value, it.type)
      );
      const results = await Promise.allSettled(ops);
      const failed = results
        .map((r, i) => (r.status === "rejected" ? targets[i] : null))
        .filter((it): it is SelectableItem => it !== null);
      if (failed.length > 0) {
        // 精确回滚失败项
        apply(failed, removing ? "add" : "delete");
        setTagError(
          `${removing ? "取消收藏" : "收藏"}部分失败(${failed.length}/${targets.length}),请稍后重试`
        );
      }
    } catch (err) {
      // 兜底(理论不可达):全量回滚
      apply(targets, removing ? "add" : "delete");
      setTagError(actionErrorText(err, removing ? "取消收藏失败,请稍后重试" : "收藏失败,请稍后重试"));
    } finally {
      setTagBusy(false);
    }
  }, [selectedItems, savedTags, savedAuthors, tagBusy]);

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
            selectedItems={selectedItems}
            savedTags={savedTags}
            savedAuthors={savedAuthors}
            tagBusy={tagBusy}
            tagError={tagError}
            onToggleItem={handleToggleItem}
            onClearItems={handleClearItems}
            onQuickSearch={handleQuickSearch}
            onFavoriteBatch={handleFavoriteBatch}
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
  /** 标签/作者搜索与收藏(私有扩展):统一选中集合、已收藏分组与回调 */
  selectedItems: SelectableItem[];
  savedTags: Set<string>;
  savedAuthors: Set<string>;
  tagBusy: boolean;
  tagError: string | null;
  onToggleItem: (item: SelectableItem) => void;
  onClearItems: () => void;
  onQuickSearch: () => void;
  onFavoriteBatch: () => void;
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
  selectedItems,
  savedTags,
  savedAuthors,
  tagBusy,
  tagError,
  onToggleItem,
  onClearItems,
  onQuickSearch,
  onFavoriteBatch,
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
            {/* 作者标签(可点):与普通标签区分(圆角 + 👤 + 紫色系);点击选中后与标签共用操作条 */}
            {detail.authors.length > 0 && (
              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                <span className="text-xs text-muted">作者:</span>
                {detail.authors.map((author) => {
                  const selected = selectedItems.some(
                    (it) => it.type === "author" && it.value === author
                  );
                  const saved = savedAuthors.has(author);
                  return (
                    <button
                      key={author}
                      type="button"
                      onClick={() => onToggleItem({ type: "author", value: author })}
                      title={
                        saved
                          ? "已收藏的作者(在「标签收藏」的作者区)"
                          : "点击选中作者(可搜索 / 收藏作者)"
                      }
                      className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-[11px] transition-colors ${
                        selected
                          ? "border-violet-500/70 bg-violet-500/10 text-violet-500"
                          : "border-violet-400/30 bg-background text-violet-400/90 hover:border-violet-500/60 hover:text-violet-500"
                      }`}
                    >
                      {saved ? (
                        <Star className="h-3 w-3 text-amber-400" fill="currentColor" />
                      ) : (
                        <User className="h-3 w-3" />
                      )}
                      {author}
                    </button>
                  );
                })}
              </div>
            )}
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
                {/* 标签可点(支持多选):选中高亮;已收藏标签带 ★ 角标 */}
                <div className="flex flex-wrap gap-1.5">
                  {detail.tags.map((tag) => {
                    const selected = selectedItems.some(
                      (it) => it.type === "tag" && it.value === tag
                    );
                    const saved = savedTags.has(tag);
                    return (
                      <button
                        key={tag}
                        type="button"
                        onClick={() => onToggleItem({ type: "tag", value: tag })}
                        title={saved ? "已收藏的标签" : "点击选中(可多选后批量收藏)"}
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
                {/* 统一操作条(标签/作者共用):搜索(最近点选的一个,按其类型)/ 批量收藏切换 / 清空 */}
                {selectedItems.length > 0 && (() => {
                  const last = selectedItems[selectedItems.length - 1];
                  const lastSaved = (last.type === "author" ? savedAuthors : savedTags).has(last.value);
                  const toAddCount = selectedItems.filter(
                    (it) => !(it.type === "author" ? savedAuthors : savedTags).has(it.value)
                  ).length;
                  const allSaved = toAddCount === 0;
                  return (
                  <div className="mt-2 flex flex-wrap items-center gap-2">
                    <span className="text-[11px] text-muted">
                      已选 {selectedItems.length} 项
                      {selectedItems.some((it) => it.type === "author") ? "(含作者)" : ""}
                    </span>
                    <button
                      type="button"
                      onClick={onQuickSearch}
                      title={`搜索最近选中的${last.type === "author" ? "作者" : "标签"}「${last.value}」`}
                      className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white transition-opacity hover:opacity-90"
                    >
                      <Search className="h-3.5 w-3.5" />
                      搜索
                    </button>
                    <button
                      type="button"
                      disabled={tagBusy}
                      onClick={onFavoriteBatch}
                      title={
                        allSaved
                          ? `移除选中的 ${selectedItems.length} 个已收藏项`
                          : `收藏选中的 ${toAddCount} 个未收藏项`
                      }
                      className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-60 ${
                        allSaved
                          ? "border-accent/50 text-accent"
                          : "border-border bg-background text-muted hover:border-accent/50 hover:text-foreground"
                      }`}
                    >
                      {tagBusy ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Star className="h-3.5 w-3.5" fill={allSaved ? "currentColor" : "none"} />
                      )}
                      {allSaved ? "取消收藏" : `收藏${toAddCount > 1 ? ` ${toAddCount} 个` : ""}`}
                    </button>
                    <button
                      type="button"
                      onClick={onClearItems}
                      disabled={tagBusy}
                      title="清空选择(标签与作者)"
                      className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs text-muted transition-colors hover:text-foreground disabled:opacity-60"
                    >
                      <X className="h-3.5 w-3.5" />
                      清空
                    </button>
                    {tagError && <span className="text-[11px] text-red-400">{tagError}</span>}
                  </div>
                  );
                })()}
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
