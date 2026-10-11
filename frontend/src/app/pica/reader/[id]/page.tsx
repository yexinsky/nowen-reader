"use client";

/**
 * 哔咔在线源 · 在线阅读器(镜像 JM 阅读器页面壳,零改动现有阅读器组件)
 * 路由:/pica/reader/:id?ep={章节 order}&page={初始页}
 *
 * - 图片清单:GET /comics/:id/eps/:order/pages 分页聚合(每页约 40 张),
 *   images[].url 为后端代理地址(原样字节,无乱序还原)
 * - 进度上报:进入章节成功立即上报一次;页码变化防抖 1200ms 合并上报;
 *   离开章节补报未上报页码
 * - 章节衔接:翻过末页自动进入下一章(eps 按 order 升序,相邻项衔接)
 * - 章节导航:右下浮动按钮弹出章节清单(懒加载 eps)
 * - 图片失败 onErrorCapture 捕获,cache-bust 自动重试(最多 2 次)
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { BookX, ImageOff, Layers, Loader2, X } from "lucide-react";
import SinglePageView from "@/components/reader/SinglePageView";
import DoublePageView from "@/components/reader/DoublePageView";
import WebtoonView from "@/components/reader/WebtoonView";
import ReaderToolbar, { type ReaderTheme } from "@/components/reader/ReaderToolbar";
import ReaderOptionsPanel from "@/components/reader/ReaderOptionsPanel";
import { useReaderOptions } from "@/hooks/useReaderOptions";
import { useTheme } from "@/lib/theme-context";
import { PicaErrorCard, PicaGate } from "@/components/pica/PicaGate";
import { PicaDownloadFab } from "@/components/pica/download/DownloadTasks";
import { isPicaApiError, picaComicDetail, picaEps, picaEpsPages, picaHistoryReport } from "@/lib/pica/client";
import { resolvePicaUrl } from "@/lib/pica/config";
import { usePicaSession } from "@/lib/pica/session";
import { PICA_ERROR_CODES, type PicaEpisode, type PicaPageImage } from "@/lib/pica/types";
import type { ComicReadingMode, ReadingDirection } from "@/types/reader";

const REPORT_DEBOUNCE_MS = 1200;
const TOOLBAR_AUTO_HIDE_MS = 4000;
const MAX_IMAGE_RETRY = 2;

/* ── 阅读器全屏状态底壳 ── */

function FullScreenShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-dvh w-full items-center justify-center overflow-hidden bg-[#050505] px-4">
      {children}
    </div>
  );
}

function ReaderLoadingScreen() {
  return (
    <FullScreenShell>
      <div className="flex flex-col items-center gap-5">
        <div className="h-10 w-10 animate-spin rounded-full border-2 border-white/10 border-t-white/60" />
        <p className="text-sm font-medium text-white/50">正在加载章节...</p>
      </div>
    </FullScreenShell>
  );
}

function ChapterNotFoundCard() {
  return (
    <FullScreenShell>
      <div className="w-full max-w-md rounded-xl border border-white/[0.08] bg-zinc-900/95 p-8 text-center shadow-2xl">
        <span className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-red-500/10 text-red-400">
          <BookX className="h-6 w-6" />
        </span>
        <h2 className="text-lg font-semibold text-white">章节不存在</h2>
        <p className="mt-2 text-sm text-white/50">该章节可能已被删除,或链接中的章节序号无效。</p>
        <button
          type="button"
          onClick={() => window.history.back()}
          className="mt-6 inline-flex items-center justify-center rounded-lg border border-white/[0.08] px-4 py-2 text-sm font-medium text-white/80 transition-colors hover:bg-white/[0.06]"
        >
          返回上一页
        </button>
      </div>
    </FullScreenShell>
  );
}

function EmptyChapterCard() {
  return (
    <FullScreenShell>
      <div className="w-full max-w-md rounded-xl border border-white/[0.08] bg-zinc-900/95 p-8 text-center shadow-2xl">
        <span className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-amber-500/10 text-amber-400">
          <ImageOff className="h-6 w-6" />
        </span>
        <h2 className="text-lg font-semibold text-white">本章暂无内容</h2>
        <p className="mt-2 text-sm text-white/50">该章节没有可阅读的图片。</p>
        <button
          type="button"
          onClick={() => window.history.back()}
          className="mt-6 inline-flex items-center justify-center rounded-lg border border-white/[0.08] px-4 py-2 text-sm font-medium text-white/80 transition-colors hover:bg-white/[0.06]"
        >
          返回上一页
        </button>
      </div>
    </FullScreenShell>
  );
}

export default function PicaReaderPage() {
  const { id = "" } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const { isLoggedIn } = usePicaSession();

  // 章节 order:URL ?ep=(缺省 1)
  const rawEp = Number.parseInt(searchParams.get("ep") ?? "1", 10);
  const epOrder = Number.isFinite(rawEp) && rawEp >= 1 ? rawEp : 1;

  // 阅读偏好(与本地阅读器共享同一份 localStorage 偏好)
  const { options: readerOpts, updateOptions: updateReaderOpts } = useReaderOptions();
  const { theme: globalTheme, toggleTheme: toggleGlobalTheme } = useTheme();
  const readerTheme: ReaderTheme = globalTheme === "light" ? "day" : "night";

  // 章节图片(分页聚合)
  const [images, setImages] = useState<PicaPageImage[]>([]);
  const [title, setTitle] = useState("");
  const [coverUrl, setCoverUrl] = useState("");
  const [episodes, setEpisodes] = useState<PicaEpisode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [retryTick, setRetryTick] = useState(0);

  // 页码(内部 1 基;现有视图组件/工具栏为 0 基口径,边界处换算)
  const [currentPage, setCurrentPage] = useState(1);
  const [toolbarVisible, setToolbarVisible] = useState(false);
  const [toolbarInteracting, setToolbarInteracting] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showOptionsPanel, setShowOptionsPanel] = useState(false);
  const [autoPageActive, setAutoPageActive] = useState(false);
  const [chapterNavOpen, setChapterNavOpen] = useState(false);

  /* ── 进度上报:ref 记录最新状态,避免闭包过期 ── */
  const metaRef = useRef<{ id: string; title: string; coverUrl: string; order: number }>({
    id: "", title: "", coverUrl: "", order: epOrder,
  });
  const pendingPageRef = useRef<number | null>(null);
  const lastReportedPageRef = useRef(0);
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const progressTrackingRef = useRef(readerOpts.progressTracking);
  const loggedInRef = useRef(isLoggedIn);
  useEffect(() => {
    progressTrackingRef.current = readerOpts.progressTracking;
  }, [readerOpts.progressTracking]);
  useEffect(() => {
    loggedInRef.current = isLoggedIn;
  }, [isLoggedIn]);

  /** 立即上报进度(幂等键 id+order);进度跟踪关闭或未登录时静默跳过 */
  const reportProgress = useCallback((page: number) => {
    const meta = metaRef.current;
    if (!meta.id || !progressTrackingRef.current || !loggedInRef.current) return;
    lastReportedPageRef.current = page;
    pendingPageRef.current = null;
    void picaHistoryReport({
      id: meta.id,
      title: meta.title,
      coverUrl: meta.coverUrl,
      order: meta.order,
      imageIndex: page,
    }).catch(() => {});
  }, []);

  // 加载章节图片(分页聚合)+ 详情/章节清单(标题与章节导航用)
  useEffect(() => {
    if (!id || !isLoggedIn) return;
    let cancelled = false;
    metaRef.current = { id, title: "", coverUrl: "", order: epOrder };
    pendingPageRef.current = null;
    lastReportedPageRef.current = 0;
    setImages([]);
    setError(null);
    setLoading(true);

    const loadPages = async () => {
      const all: PicaPageImage[] = [];
      for (let page = 1; page <= 500; page++) {
        const res = await picaEpsPages(id, epOrder, page);
        if (cancelled) return null;
        all.push(...(res.list ?? []));
        if (!res.hasNext || (res.list ?? []).length === 0) break;
      }
      return all;
    };

    Promise.all([
      loadPages(),
      picaComicDetail(id).catch(() => null),
      picaEps(id, 1).catch(() => null),
    ])
      .then(([imgs, detail, eps]) => {
        if (cancelled) return;
        if (!imgs) {
          setError(new Error("章节加载失败"));
          setLoading(false);
          return;
        }
        const detailTitle = detail?.title ?? "";
        const detailCover = detail?.coverUrl ?? "";
        metaRef.current = { id, title: detailTitle, coverUrl: detailCover, order: epOrder };
        setTitle(detailTitle);
        setCoverUrl(detailCover);
        setEpisodes(eps?.list ?? []);
        const total = imgs.length;
        const raw = Number.parseInt(searchParams.get("page") ?? "", 10);
        const initial =
          Number.isFinite(raw) && raw >= 1 ? Math.min(raw, Math.max(total, 1)) : 1;
        setCurrentPage(initial);
        setImages(imgs);
        setLoading(false);
        reportProgress(initial);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err);
        setLoading(false);
      });

    return () => {
      cancelled = true;
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
        debounceTimerRef.current = null;
      }
      const pending = pendingPageRef.current;
      pendingPageRef.current = null;
      if (pending !== null) reportProgress(pending);
    };
  }, [id, epOrder, searchParams, isLoggedIn, retryTick, reportProgress]);

  // 页码变化 → 防抖 1200ms 合并上报
  useEffect(() => {
    if (images.length === 0 || !readerOpts.progressTracking) return;
    if (currentPage === lastReportedPageRef.current) return;
    pendingPageRef.current = currentPage;
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current);
    debounceTimerRef.current = setTimeout(() => {
      debounceTimerRef.current = null;
      const pending = pendingPageRef.current;
      if (pending !== null) reportProgress(pending);
    }, REPORT_DEBOUNCE_MS);
  }, [currentPage, images.length, readerOpts.progressTracking, reportProgress]);

  /* ── 模式/方向:从共享偏好派生 ── */
  const effectiveMode: ComicReadingMode =
    readerOpts.direction === "ttb" || readerOpts.infiniteScroll ? "webtoon" : readerOpts.mode;

  const totalPages = images.length;

  // pages:后端已下发代理地址(/api/pica/image?fs=&path=),仅叠加 Base Path
  const pages = useMemo(
    () => images.map((img) => resolvePicaUrl(img.url)),
    [images]
  );

  const imageFilter = useMemo(() => {
    const b = readerOpts.imageBrightness;
    const co = readerOpts.imageContrast;
    const g = readerOpts.imageGrayscale;
    if (b === 100 && co === 100 && g === 0) return undefined;
    return `brightness(${b}%) contrast(${co}%) grayscale(${g}%)`;
  }, [readerOpts.imageBrightness, readerOpts.imageContrast, readerOpts.imageGrayscale]);

  const containerWidthStyle = readerOpts.containerWidth
    ? (readerOpts.containerWidth.includes("%") || readerOpts.containerWidth.includes("px")
        ? readerOpts.containerWidth
        : `${readerOpts.containerWidth}px`)
    : undefined;

  // 工具栏自动隐藏
  useEffect(() => {
    if (!toolbarVisible || showOptionsPanel || toolbarInteracting) return;
    const timer = setTimeout(() => setToolbarVisible(false), TOOLBAR_AUTO_HIDE_MS);
    return () => clearTimeout(timer);
  }, [toolbarVisible, currentPage, showOptionsPanel, toolbarInteracting]);

  // 全屏
  useEffect(() => {
    const onFsChange = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onFsChange);
    return () => document.removeEventListener("fullscreenchange", onFsChange);
  }, []);

  const toggleFullscreen = useCallback(async () => {
    try {
      if (!document.fullscreenElement) {
        await document.documentElement.requestFullscreen?.();
      } else {
        await document.exitFullscreen?.();
      }
    } catch {
      /* 保持当前状态 */
    }
  }, []);

  const handleTapCenter = useCallback(() => setToolbarVisible((v) => !v), []);

  const handleViewPageChange = useCallback(
    (page0: number) => {
      setCurrentPage(Math.min(Math.max(page0 + 1, 1), Math.max(totalPages, 1)));
    },
    [totalPages]
  );

  // 章节衔接:翻过末页 → 下一章(replace 避免历史栈堆积)
  const handleBoundaryReached = useCallback(
    (dir: "next" | "prev") => {
      if (dir !== "next" || episodes.length === 0) return;
      const idx = episodes.findIndex((e) => e.order === epOrder);
      const next = idx >= 0 ? episodes[idx + 1] : undefined;
      if (next) {
        navigate(`/pica/reader/${id}?ep=${next.order}`, { replace: true });
      }
    },
    [episodes, epOrder, id, navigate]
  );

  // 图片加载失败 cache-bust 重试
  const handleImageErrorCapture = useCallback((e: React.SyntheticEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement | null;
    if (!target || target.tagName !== "IMG") return;
    const img = target as HTMLImageElement;
    if (!img.src.includes("/api/pica/image")) return;
    const retries = Number.parseInt(img.dataset.picaRetry ?? "0", 10) || 0;
    if (retries >= MAX_IMAGE_RETRY) return;
    img.dataset.picaRetry = String(retries + 1);
    try {
      const url = new URL(img.src);
      url.searchParams.set("r", String(Date.now()));
      img.src = url.toString();
    } catch {
      /* URL 异常时放弃重试 */
    }
  }, []);

  const handleBack = useCallback(() => {
    const histState = window.history.state as { idx?: number } | null;
    if ((histState?.idx ?? 0) > 0) {
      navigate(-1);
      return;
    }
    if (id) navigate(`/pica/comic/${id}`);
    else window.history.back();
  }, [id, navigate]);

  const handleModeChange = useCallback(
    (m: ComicReadingMode) => {
      updateReaderOpts({
        mode: m,
        infiniteScroll: m === "webtoon",
        direction:
          m === "webtoon"
            ? "ttb"
            : readerOpts.direction === "ttb"
              ? "ltr"
              : readerOpts.direction,
      });
    },
    [updateReaderOpts, readerOpts.direction]
  );

  const handleDirectionChange = useCallback(
    (d: ReadingDirection) => {
      if (d === "ttb") {
        updateReaderOpts({ direction: d, infiniteScroll: true, mode: "webtoon" });
      } else {
        const nextMode: ComicReadingMode = effectiveMode === "webtoon" ? "single" : effectiveMode;
        updateReaderOpts({ direction: d, infiniteScroll: false, mode: nextMode });
      }
    },
    [updateReaderOpts, effectiveMode]
  );

  // 自动翻页(条漫模式不适用,双页步进 2)
  useEffect(() => {
    if (!autoPageActive || readerOpts.autoPageInterval <= 0) return;
    if (effectiveMode === "webtoon") return;
    const step = effectiveMode === "double" ? 2 : 1;
    const timer = setInterval(() => {
      setCurrentPage((p) => {
        const next = p + step;
        if (next > totalPages) {
          setAutoPageActive(false);
          return p;
        }
        return next;
      });
    }, readerOpts.autoPageInterval * 1000);
    return () => clearInterval(timer);
  }, [autoPageActive, readerOpts.autoPageInterval, effectiveMode, totalPages]);

  /* ── 渲染分支 ── */
  let content: ReactNode;
  if (loading) {
    content = <ReaderLoadingScreen />;
  } else if (error) {
    content =
      isPicaApiError(error) && error.code === PICA_ERROR_CODES.NOT_FOUND ? (
        <ChapterNotFoundCard />
      ) : (
        <FullScreenShell>
          <PicaErrorCard error={error} onRetry={() => setRetryTick((t) => t + 1)} />
        </FullScreenShell>
      );
  } else if (pages.length === 0) {
    content = <EmptyChapterCard />;
  } else {
    const currentEpisode = episodes.find((e) => e.order === epOrder);
    content = (
      <div
        onErrorCapture={handleImageErrorCapture}
        className={`reader-shell h-dvh w-full overflow-hidden overflow-x-hidden transition-colors duration-300 ${
          readerTheme === "day" ? "bg-gray-100" : "bg-[#050505]"
        }`}
      >
        {effectiveMode === "webtoon" ? (
          <WebtoonView
            pages={pages}
            currentPage={currentPage - 1}
            onPageChange={handleViewPageChange}
            onTapCenter={handleTapCenter}
            useRealData={true}
            readerTheme={readerTheme}
            containerWidth={containerWidthStyle}
            preloadCount={readerOpts.preloadCount}
            onBoundaryReached={handleBoundaryReached}
            nextVolumeTitle={episodes.length > 0 ? "下一章" : undefined}
            imageFilter={imageFilter}
          />
        ) : effectiveMode === "double" ? (
          <DoublePageView
            pages={pages}
            currentPage={currentPage - 1}
            onPageChange={handleViewPageChange}
            onTapCenter={handleTapCenter}
            direction={readerOpts.direction === "ttb" ? "ltr" : readerOpts.direction}
            useRealData={true}
            readerTheme={readerTheme}
            fitMode={readerOpts.fitMode}
            containerWidth={containerWidthStyle}
            preloadCount={readerOpts.preloadCount}
            onBoundaryReached={handleBoundaryReached}
            coverAlone={readerOpts.doubleCoverAlone}
            noGap={readerOpts.doublePageNoGap}
            imageFilter={imageFilter}
          />
        ) : (
          <SinglePageView
            pages={pages}
            currentPage={currentPage - 1}
            onPageChange={handleViewPageChange}
            onTapCenter={handleTapCenter}
            direction={readerOpts.direction === "ttb" ? "ltr" : readerOpts.direction}
            useRealData={true}
            readerTheme={readerTheme}
            fitMode={readerOpts.fitMode}
            containerWidth={containerWidthStyle}
            preloadCount={readerOpts.preloadCount}
            onBoundaryReached={handleBoundaryReached}
            imageFilter={imageFilter}
          />
        )}

        {/* 页码指示器 */}
        {readerOpts.headerVisible && effectiveMode !== "webtoon" && !toolbarVisible && (
          <div
            className={`pointer-events-none fixed bottom-4 left-1/2 z-40 -translate-x-1/2 rounded-full border border-white/[0.06] px-3.5 py-1.5 shadow-lg backdrop-blur-xl ${
              readerTheme === "day" ? "bg-white/80 shadow-black/10" : "bg-zinc-900/80 shadow-black/30"
            }`}
          >
            <span className={`font-mono text-xs ${readerTheme === "day" ? "text-gray-500" : "text-white/50"}`}>
              {currentPage} / {totalPages}
            </span>
          </div>
        )}

        <ReaderToolbar
          visible={toolbarVisible}
          title={title || `第${epOrder}话`}
          currentPage={currentPage - 1}
          totalPages={totalPages}
          mode={effectiveMode}
          direction={readerOpts.direction}
          isFullscreen={isFullscreen}
          readerTheme={readerTheme}
          onBack={handleBack}
          onPageChange={handleViewPageChange}
          onModeChange={handleModeChange}
          onDirectionChange={handleDirectionChange}
          onToggleFullscreen={toggleFullscreen}
          onToggleTheme={toggleGlobalTheme}
          onShowSettings={() => setShowOptionsPanel(true)}
          autoPageActive={autoPageActive}
          autoPageInterval={readerOpts.autoPageInterval}
          onToggleAutoPage={() => setAutoPageActive((v) => !v)}
          onInteracting={setToolbarInteracting}
        />

        {showOptionsPanel && (
          <ReaderOptionsPanel
            options={readerOpts}
            onChange={updateReaderOpts}
            onClose={() => setShowOptionsPanel(false)}
          />
        )}

        {/* 章节导航浮动按钮 + 弹层 */}
        <button
          type="button"
          aria-label="章节导航"
          onClick={() => setChapterNavOpen((v) => !v)}
          className={`fixed bottom-5 right-4 z-30 flex h-11 w-11 items-center justify-center rounded-full border shadow-lg backdrop-blur-xl transition-transform active:scale-95 ${
            readerTheme === "day"
              ? "border-black/[0.06] bg-white/85 text-gray-700 shadow-black/10"
              : "border-white/[0.08] bg-zinc-900/85 text-white/80 shadow-black/40"
          }`}
        >
          <Layers className="h-5 w-5" />
        </button>
        {chapterNavOpen && (
          <div
            className={`fixed bottom-20 right-4 z-[65] flex max-h-[60vh] w-72 flex-col overflow-hidden rounded-xl border shadow-2xl ${
              readerTheme === "day"
                ? "border-black/[0.06] bg-white shadow-black/10"
                : "border-white/[0.08] bg-zinc-900 shadow-black/40"
            }`}
            onClick={(e) => e.stopPropagation()}
          >
            <div
              className={`flex items-center justify-between border-b px-3 py-2 ${
                readerTheme === "day" ? "border-black/5" : "border-white/10"
              }`}
            >
              <span className={`text-sm font-medium ${readerTheme === "day" ? "text-gray-700" : "text-white/80"}`}>
                章节({episodes.length})
              </span>
              <button
                type="button"
                aria-label="关闭章节导航"
                onClick={() => setChapterNavOpen(false)}
                className={`rounded p-1 ${readerTheme === "day" ? "text-gray-400 hover:text-gray-700" : "text-white/40 hover:text-white/80"}`}
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
              {episodes.length === 0 ? (
                <p className={`px-2 py-6 text-center text-xs ${readerTheme === "day" ? "text-gray-400" : "text-white/40"}`}>
                  章节清单加载中或不可用
                </p>
              ) : (
                episodes.map((e) => (
                  <button
                    key={`${e.order}-${e.id}`}
                    type="button"
                    onClick={() => {
                      setChapterNavOpen(false);
                      if (e.order !== epOrder) {
                        navigate(`/pica/reader/${id}?ep=${e.order}`, { replace: true });
                      }
                    }}
                    className={`flex w-full items-center justify-between gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors ${
                      e.order === epOrder
                        ? readerTheme === "day"
                          ? "bg-accent/10 font-medium text-accent"
                          : "bg-accent/20 font-medium text-accent"
                        : readerTheme === "day"
                          ? "text-gray-600 hover:bg-black/[0.04]"
                          : "text-white/60 hover:bg-white/[0.06]"
                    }`}
                  >
                    <span className="min-w-0 truncate">{e.title || `第${e.order}话`}</span>
                    <span className="shrink-0 text-[11px] opacity-60">{e.order}</span>
                  </button>
                ))
              )}
            </div>
          </div>
        )}

        {/* 批量下载浮动入口(左下,避开右下角章节导航) */}
        <PicaDownloadFab
          comic={{
            bookId: id,
            title: title || "哔咔漫画",
            currentOrder: epOrder,
            currentTitle: currentEpisode?.title,
          }}
          label="批量下载"
          className={`fixed bottom-5 left-4 z-30 flex h-11 w-11 items-center justify-center rounded-full border shadow-lg backdrop-blur-xl transition-transform active:scale-95 ${
            readerTheme === "day"
              ? "border-black/[0.06] bg-white/85 text-gray-700 shadow-black/10"
              : "border-white/[0.08] bg-zinc-900/85 text-white/80 shadow-black/40"
          }`}
        />
      </div>
    );
  }

  return <PicaGate>{content}</PicaGate>;
}
