"use client";

/**
 * 哔咔在线源 · 漫画详情页
 * 接口:GET /comics/:id、/comics/:id/eps、/comics/:id/comments、/comics/:id/recommendation;
 * POST /comics/:id/like、/comics/:id/favourite(切换式)。
 * 爱心/收藏态以详情接口返回的 liked/isFavourite 初始化,操作后以响应 action 为准。
 */

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  BookOpen,
  Heart,
  Images,
  Layers,
  Loader2,
  Star,
  User,
} from "lucide-react";
import { PicaCommentPanel } from "@/components/pica/CommentPanel";
import { PicaComicGrid } from "@/components/pica/ComicGrid";
import { PicaDownloadButton } from "@/components/pica/download/DownloadButton";
import { resolvePicaUrl } from "@/lib/pica/config";
import {
  isPicaApiError,
  picaComicDetail,
  picaEps,
  picaRecommendation,
  picaToggleFavorite,
  picaToggleLike,
} from "@/lib/pica/client";
import type { PicaComicDetail, PicaComicItem, PicaEpisode } from "@/lib/pica/types";

function trimTrailingZero(s: string): string {
  return s.endsWith(".0") ? s.slice(0, -2) : s;
}

function formatCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 1e4) return `${trimTrailingZero((n / 1e4).toFixed(1))}万`;
  return String(n);
}

export default function PicaComicDetailPage() {
  return <DetailContent />;
}

function DetailContent() {
  const { id = "" } = useParams();
  const navigate = useNavigate();

  const [detail, setDetail] = useState<PicaComicDetail | null>(null);
  const [episodes, setEpisodes] = useState<PicaEpisode[]>([]);
  const [recs, setRecs] = useState<PicaComicItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [retryTick, setRetryTick] = useState(0);

  // 互动态(以服务端响应为准更新)
  const [liked, setLiked] = useState(false);
  const [likes, setLikes] = useState(0);
  const [faved, setFaved] = useState(false);
  const [actionBusy, setActionBusy] = useState<"like" | "fav" | null>(null);
  const [actionHint, setActionHint] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    Promise.all([
      picaComicDetail(id),
      picaEps(id, 1).catch(() => null),
      picaRecommendation(id).catch(() => ({ list: [] as PicaComicItem[] })),
    ])
      .then(([d, eps, rec]) => {
        if (cancelled) return;
        setDetail(d);
        setLiked(d.liked);
        setLikes(d.likes);
        setFaved(d.isFavourite);
        setEpisodes(eps?.list ?? []);
        setRecs(rec.list ?? []);
        setLoading(false);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id, retryTick]);

  const handleLike = useCallback(async () => {
    if (!id || actionBusy) return;
    setActionBusy("like");
    setActionHint(null);
    try {
      const res = await picaToggleLike(id);
      const nowLiked = res.action === "like";
      setLiked(nowLiked);
      setLikes((n) => Math.max(0, n + (nowLiked ? 1 : -1)));
    } catch (err) {
      setActionHint(isPicaApiError(err) ? err.message : "操作失败,请稍后重试");
    } finally {
      setActionBusy(null);
    }
  }, [id, actionBusy]);

  const handleFavorite = useCallback(async () => {
    if (!id || actionBusy) return;
    setActionBusy("fav");
    setActionHint(null);
    try {
      const res = await picaToggleFavorite(id);
      const nowFaved = res.action === "favourite";
      setFaved(nowFaved);
      setActionHint(nowFaved ? "已加入收藏" : "已取消收藏");
    } catch (err) {
      setActionHint(isPicaApiError(err) ? err.message : "操作失败,请稍后重试");
    } finally {
      setActionBusy(null);
    }
  }, [id, actionBusy]);

  if (loading) {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-background text-muted">
        <Loader2 className="h-7 w-7 animate-spin" />
      </div>
    );
  }
  if (error || !detail) {
    return (
      <div className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background px-4 text-center">
        <p className="text-sm text-muted">
          {isPicaApiError(error) ? error.message : "加载失败,请稍后重试"}
        </p>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => setRetryTick((t) => t + 1)}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
          >
            重试
          </button>
          <button
            type="button"
            onClick={() => navigate(-1)}
            className="rounded-lg border border-border px-4 py-2 text-sm text-muted transition-colors hover:text-foreground"
          >
            返回
          </button>
        </div>
      </div>
    );
  }

  const coverUrl = resolvePicaUrl(detail.coverUrl);
  const firstEp = episodes[0]?.order ?? 1;

  return (
    <div className="min-h-dvh bg-background text-foreground">
      {/* 顶栏 */}
      <header className="sticky top-0 z-40 flex h-14 items-center gap-3 border-b border-border bg-background/90 px-3 backdrop-blur-xl sm:px-4">
        <button
          type="button"
          onClick={() => {
            const histState = window.history.state as { idx?: number } | null;
            if ((histState?.idx ?? 0) > 0) navigate(-1);
            else navigate("/pica");
          }}
          aria-label="返回"
          className="flex h-10 w-10 items-center justify-center rounded-lg text-muted transition-colors hover:bg-card hover:text-foreground"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <span className="min-w-0 truncate text-sm font-medium text-foreground">{detail.title}</span>
      </header>

      <main className="mx-auto w-full max-w-5xl px-4 py-6">
        {/* 基础信息 */}
        <section className="flex flex-col gap-5 sm:flex-row">
          <div className="w-40 shrink-0 sm:w-52">
            {coverUrl ? (
              <img
                src={coverUrl}
                alt={detail.title}
                className="aspect-[3/4] w-full rounded-xl object-cover shadow-lg"
              />
            ) : (
              <div className="flex aspect-[3/4] w-full items-center justify-center rounded-xl bg-muted/10 text-muted/30">
                <BookOpen className="h-10 w-10" />
              </div>
            )}
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="text-lg font-semibold leading-snug text-foreground sm:text-xl">
              {detail.title}
            </h1>
            <p className="mt-2 flex items-center gap-1.5 text-sm text-muted">
              <User className="h-3.5 w-3.5" />
              {detail.author || "未知作者"}
              {detail.chineseTeam ? ` · ${detail.chineseTeam} 汉化` : ""}
              {detail.finished ? " · 已完结" : ""}
            </p>

            <div className="mt-3 flex flex-wrap gap-1.5">
              {(detail.categories ?? []).map((c) => (
                <span
                  key={c}
                  className="rounded-full bg-accent/10 px-2.5 py-1 text-xs text-accent"
                >
                  {c}
                </span>
              ))}
            </div>
            {(detail.tags ?? []).length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {(detail.tags ?? []).map((t) => (
                  <span
                    key={t}
                    className="rounded-full border border-border/50 px-2.5 py-1 text-xs text-muted"
                  >
                    {t}
                  </span>
                ))}
              </div>
            )}

            <div className="mt-4 flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-muted">
              <span className="flex items-center gap-1">
                <Heart className="h-3.5 w-3.5" />
                {formatCount(likes)}
              </span>
              <span className="flex items-center gap-1">
                <Images className="h-3.5 w-3.5" />
                {formatCount(detail.imageCount)} 张
              </span>
              <span className="flex items-center gap-1">
                <Layers className="h-3.5 w-3.5" />
                {Math.max(detail.epsCount, episodes.length)} 话
              </span>
            </div>

            {/* 操作条 */}
            <div className="mt-5 flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={handleLike}
                disabled={actionBusy === "like"}
                className={`inline-flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
                  liked
                    ? "border-red-500/40 bg-red-500/10 text-red-400"
                    : "border-border bg-background text-muted hover:border-red-500/40 hover:text-red-400"
                }`}
              >
                {actionBusy === "like" ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Heart className={`h-4 w-4 ${liked ? "fill-current" : ""}`} />
                )}
                {liked ? "已点赞" : "点赞"}
              </button>
              <button
                type="button"
                onClick={handleFavorite}
                disabled={actionBusy === "fav"}
                className={`inline-flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
                  faved
                    ? "border-amber-500/40 bg-amber-500/10 text-amber-500"
                    : "border-border bg-background text-muted hover:border-amber-500/40 hover:text-amber-500"
                }`}
              >
                {actionBusy === "fav" ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Star className={`h-4 w-4 ${faved ? "fill-current" : ""}`} />
                )}
                {faved ? "已收藏" : "收藏"}
              </button>
              <PicaDownloadButton
                bookId={detail.id}
                title={detail.title}
                author={detail.author}
                episodes={episodes}
              />
              <Link
                href={`/pica/reader/${detail.id}?ep=${firstEp}`}
                className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
              >
                <BookOpen className="h-4 w-4" />
                开始阅读
              </Link>
            </div>
            {actionHint && <p className="mt-2 text-xs text-muted">{actionHint}</p>}
          </div>
        </section>

        {/* 简介 */}
        {detail.description && (
          <section className="mt-8">
            <h2 className="mb-2 text-sm font-semibold text-foreground">简介</h2>
            <p className="whitespace-pre-wrap text-sm leading-relaxed text-muted">
              {detail.description}
            </p>
          </section>
        )}

        {/* 章节列表 */}
        <section className="mt-8">
          <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-foreground">
            <Layers className="h-4 w-4 text-accent" />
            章节({episodes.length})
          </h2>
          {episodes.length === 0 ? (
            <p className="text-sm text-muted">暂无章节信息</p>
          ) : (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4">
              {episodes.map((ep) => (
                <Link
                  key={`${ep.order}-${ep.id}`}
                  href={`/pica/reader/${detail.id}?ep=${ep.order}`}
                  className="flex items-center justify-between gap-2 rounded-lg border border-border bg-card px-3 py-2.5 text-sm text-foreground/90 transition-colors hover:border-accent/50 hover:text-accent"
                >
                  <span className="min-w-0 truncate">{ep.title || `第${ep.order}话`}</span>
                  <span className="shrink-0 text-[11px] text-muted">{ep.order}</span>
                </Link>
              ))}
            </div>
          )}
        </section>

        {/* 评论(只读) */}
        <PicaCommentPanel comicId={detail.id} />

        {/* 相关推荐 */}
        {recs.length > 0 && (
          <section className="mt-8">
            <h2 className="mb-3 text-sm font-semibold text-foreground">相关推荐</h2>
            <PicaComicGrid comics={recs} />
          </section>
        )}
      </main>
    </div>
  );
}
