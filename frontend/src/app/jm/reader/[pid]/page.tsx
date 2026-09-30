"use client";

/**
 * JM 在线源 · 在线阅读器(PRD docs/PRD_JM_SOURCE.md M8「沿用现有阅读器体验」)
 * 接口:MOBILE_API.md #15 photos、#16 image、#25 reportHistory
 *
 * 视图层完全沿用现有阅读器组件(SinglePageView / DoublePageView / WebtoonView、
 * ReaderToolbar、ReaderOptionsPanel),阅读偏好与本地阅读器共享同一份
 * useReaderOptions 存储(模式/方向/适应/预加载/滤镜/进度跟踪等);本页面壳只承载
 * JM 特化逻辑,不修改任何现有阅读器文件:
 * - #15 拉取章节图片清单,逐张经 jmImageUrl(#16,服务端乱序还原)拼代理 URL;
 *   live 模式 images[].width/height 恒 0,不参与布局,占位比例由视图组件内置
 * - #25 进度上报(幂等键 aid+pid,imageIndex 1 基):①进入章节成功立即上报一次;
 *   ②页码变化防抖 1200ms 合并上报;③离开章节/卸载时若有未上报的最新页码,
 *   fire-and-forget 补报;progressTracking 关闭或未登录时一律不上报
 * - 章节衔接:onBoundaryReached("next") 且 hasNext/nextPid 时 replace 进入下一章;
 *   "prev" 越界不做
 * - AOP 增强(全部在本页面壳层实现,零改动现有阅读器组件):
 *   ① <JmChapterNav/> 以兄弟覆盖层提供章节清单入口(装饰器,见组件头注释);
 *   ② 阅读容器 onErrorCapture 捕获视图内部 <img> 加载失败,自动加 cache-bust
 *      重试(最多 2 次)——事件捕获切面,不侵入视图组件
 * - 初始页码:?page=N(1 基)clamp 到 [1, 总页数];返回优先详情页(?aid= 或 #15 的 aid)
 * - 内部页码状态统一 1 基(URL/report 口径),传给现有视图组件与工具栏(0 基口径)时换算
 * - 资源约束:预加载张数完全由 readerOpts.preloadCount 经视图组件内置逻辑控制,
 *   本页面不做任何全量图片加载、不做轮询;定时器/监听均随卸载清理
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { BookX, ImageOff } from "lucide-react";
import SinglePageView from "@/components/reader/SinglePageView";
import DoublePageView from "@/components/reader/DoublePageView";
import WebtoonView from "@/components/reader/WebtoonView";
import ReaderToolbar, { type ReaderTheme } from "@/components/reader/ReaderToolbar";
import ReaderOptionsPanel from "@/components/reader/ReaderOptionsPanel";
import { useReaderOptions } from "@/hooks/useReaderOptions";
import { useTheme } from "@/lib/theme-context";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { JmChapterNav } from "@/components/jm/reader/JmChapterNav";
import { JmDownloadFab } from "@/components/jm/download/DownloadTasks";
import { isJmApiError, jmPhotos, jmReportHistory } from "@/lib/jm/client";
import { jmImageUrl } from "@/lib/jm/config";
import { useJmSession } from "@/lib/jm/session";
import { JM_ERROR_CODES, type JmPhotos } from "@/lib/jm/types";
import type { ComicReadingMode, ReadingDirection } from "@/types/reader";

/** #25 页码变化防抖上报间隔(MOBILE_API.md 口径 1.2s) */
const REPORT_DEBOUNCE_MS = 1200;
/** 工具栏自动隐藏时长(与现有阅读器一致) */
const TOOLBAR_AUTO_HIDE_MS = 4000;
/** 单图加载失败自动重试次数上限(cache-bust 重试,AOP 切面) */
const MAX_IMAGE_RETRY = 2;

/* ── 阅读器全屏状态底壳(加载/错误/空章节共用) ── */

function FullScreenShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-dvh w-full items-center justify-center overflow-hidden bg-[#050505] px-4">
      {children}
    </div>
  );
}

/** 加载中:全屏 spinner */
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

/** 3001:章节不存在提示卡 */
function ChapterNotFoundCard() {
  return (
    <FullScreenShell>
      <div className="w-full max-w-md rounded-xl border border-white/[0.08] bg-zinc-900/95 p-8 text-center shadow-2xl">
        <span className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-red-500/10 text-red-400">
          <BookX className="h-6 w-6" />
        </span>
        <h2 className="text-lg font-semibold text-white">章节不存在</h2>
        <p className="mt-2 text-sm text-white/50">该章节可能已被删除,或链接中的章节 id 无效。</p>
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

/** 章节清单为空(#15 成功但无图片) */
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

export default function JmReaderPage() {
  const { pid = "" } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const { isLoggedIn } = useJmSession();
  const aidParam = searchParams.get("aid") ?? "";

  // 阅读偏好(与本地阅读器共享同一份 localStorage 偏好 —— 「体验沿用」落点)
  const { options: readerOpts, updateOptions: updateReaderOpts } = useReaderOptions();
  const { theme: globalTheme, toggleTheme: toggleGlobalTheme } = useTheme();
  const readerTheme: ReaderTheme = globalTheme === "light" ? "day" : "night";

  // 章节数据(#15)
  const [photos, setPhotos] = useState<JmPhotos | null>(null);
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

  /* ── #25 进度上报:ref 记录最新章节/页码/开关,避免闭包过期 ── */
  const photosRef = useRef<JmPhotos | null>(null);
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

  /** 立即上报进度(#25,幂等键 aid+pid);进度跟踪关闭或未登录时静默跳过 */
  const reportProgress = useCallback((page: number) => {
    const p = photosRef.current;
    if (!p || !progressTrackingRef.current || !loggedInRef.current) return;
    lastReportedPageRef.current = page;
    pendingPageRef.current = null;
    void jmReportHistory({
      aid: p.aid,
      title: p.title,
      coverUrl: "",
      pid: p.pid,
      epTitle: p.title,
      imageIndex: page,
    }).catch(() => {});
  }, []);

  // 加载章节(#15);成功后立即上报一次;离开章节(切换 pid/卸载)补报未上报页码
  useEffect(() => {
    if (!pid || !isLoggedIn) return;
    let cancelled = false;
    photosRef.current = null;
    pendingPageRef.current = null;
    lastReportedPageRef.current = 0;
    setPhotos(null);
    setError(null);
    setLoading(true);
    jmPhotos(pid)
      .then((data) => {
        if (cancelled) return;
        photosRef.current = data;
        // 初始页码:?page=N(1 基)→ clamp [1, 总页数]
        const total = data.images.length;
        const raw = Number.parseInt(searchParams.get("page") ?? "", 10);
        const initial =
          Number.isFinite(raw) && raw >= 1 ? Math.min(raw, Math.max(total, 1)) : 1;
        setCurrentPage(initial);
        setPhotos(data);
        setLoading(false);
        // ① 进入章节成功后立即上报一次(当前页)
        reportProgress(initial);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err);
        setLoading(false);
      });
    return () => {
      cancelled = true;
      // ③ 离开章节:若有未上报的最新页码,fire-and-forget 补报(不 await)
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
        debounceTimerRef.current = null;
      }
      const pending = pendingPageRef.current;
      pendingPageRef.current = null;
      if (pending !== null) reportProgress(pending);
    };
  }, [pid, searchParams, isLoggedIn, retryTick, reportProgress]);

  // ② 页码变化 → 防抖 1200ms 合并上报(每次变化重置计时,只报最新页码)
  useEffect(() => {
    if (!photos || !readerOpts.progressTracking) return;
    if (currentPage === lastReportedPageRef.current) return;
    pendingPageRef.current = currentPage;
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current);
    debounceTimerRef.current = setTimeout(() => {
      debounceTimerRef.current = null;
      const pending = pendingPageRef.current;
      if (pending !== null) reportProgress(pending);
    }, REPORT_DEBOUNCE_MS);
  }, [currentPage, photos, readerOpts.progressTracking, reportProgress]);

  /* ── 模式/方向:直接从共享偏好派生(与现有阅读器同口径:ttb/无限滚动 → 条漫) ── */
  const effectiveMode: ComicReadingMode =
    readerOpts.direction === "ttb" || readerOpts.infiniteScroll ? "webtoon" : readerOpts.mode;

  const totalPages = photos ? photos.images.length : 0;

  // pages:images[].path 逐张经 #16 代理(服务端乱序还原),不直接访问上游图片域名
  const pages = useMemo(
    () => (photos ? photos.images.map((img) => jmImageUrl(img.path, photos.scramble, photos.aid)) : []),
    [photos]
  );

  // 图片 CSS 滤镜(与现有阅读器 487 行 useMemo 同口径;全默认值时 undefined)
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

  // 工具栏自动隐藏(设置面板打开/交互中不隐藏)
  useEffect(() => {
    if (!toolbarVisible || showOptionsPanel || toolbarInteracting) return;
    const timer = setTimeout(() => setToolbarVisible(false), TOOLBAR_AUTO_HIDE_MS);
    return () => clearTimeout(timer);
  }, [toolbarVisible, currentPage, showOptionsPanel, toolbarInteracting]);

  // 全屏:fullscreenchange 为唯一真值来源
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
      // 浏览器拒绝或不支持时保持当前状态,交由 fullscreenchange 同步真实结果
    }
  }, []);

  const handleTapCenter = useCallback(() => setToolbarVisible((v) => !v), []);

  /** 视图组件/工具栏回调(0 基)→ 内部 1 基页码,clamp 到 [1, totalPages] */
  const handleViewPageChange = useCallback(
    (page0: number) => {
      setCurrentPage(Math.min(Math.max(page0 + 1, 1), Math.max(totalPages, 1)));
    },
    [totalPages]
  );

  // 章节衔接:翻过末页 → 下一章(replace 避免历史栈堆积);"prev" 越界不做
  const handleBoundaryReached = useCallback(
    (dir: "next" | "prev") => {
      if (dir !== "next" || !photos) return;
      if (photos.hasNext && photos.nextPid) {
        navigate(`/jm/reader/${photos.nextPid}`, { replace: true });
      }
    },
    [photos, navigate]
  );

  /* ── AOP 切面 ①:图片加载失败捕获重试 ──
   * React onErrorCapture 在捕获阶段收集视图组件内部 <img> 的 error 事件,
   * 对经 #16 代理的图片自动追加 cache-bust 重试(按 img 计数,上限 MAX_IMAGE_RETRY)。
   * 直接改 DOM src 不走 React 状态,视图组件无感知、零改动;换章后节点重建计数自然清零 */
  const handleImageErrorCapture = useCallback((e: React.SyntheticEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement | null;
    if (!target || target.tagName !== "IMG") return;
    const img = target as HTMLImageElement;
    if (!img.src.includes("/api/image")) return;
    const retries = Number.parseInt(img.dataset.jmRetry ?? "0", 10) || 0;
    if (retries >= MAX_IMAGE_RETRY) return;
    img.dataset.jmRetry = String(retries + 1);
    try {
      const url = new URL(img.src);
      url.searchParams.set("r", String(Date.now()));
      img.src = url.toString();
    } catch {
      /* URL 异常时放弃重试,保留原失败态 */
    }
  }, []);

  /* ── AOP 切面 ②:章节导航装饰层(JmChapterNav)的切换回调 ── */
  const handleChapterNavigate = useCallback(
    (nextPid: string) => {
      navigate(`/jm/reader/${nextPid}`, { replace: true });
    },
    [navigate]
  );

  // 返回:优先详情页(?aid= 来源参数或 #15 返回的 aid),无 aid 则浏览器后退
  // 返回:优先在应用历史内回退(回到进入阅读器前的详情/搜索页,栈自然收缩,
  // 不会把 reader 残留在栈里造成「返回后又回到阅读页」的循环);
  // 无应用历史(直接打开/刷新进入)时回详情页。
  const handleBack = useCallback(() => {
    const histState = window.history.state as { idx?: number } | null;
    if ((histState?.idx ?? 0) > 0) {
      navigate(-1);
      return;
    }
    const aid = aidParam || photos?.aid;
    if (aid) navigate(`/jm/comic/${aid}`);
    else window.history.back();
  }, [aidParam, photos, navigate]);

  // 工具栏模式切换 → 写共享偏好(与现有阅读器 handleModeChange 同口径)
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

  // 工具栏方向切换 → 写共享偏好(ttb 即条漫;切回水平时由条漫恢复单页)
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

  // 自动翻页(与现有阅读器一致:条漫模式不适用,双页步进 2,到末页自动停止)
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
      isJmApiError(error) && error.code === JM_ERROR_CODES.NOT_FOUND ? (
        <ChapterNotFoundCard />
      ) : (
        <FullScreenShell>
          <JmErrorCard error={error} onRetry={() => setRetryTick((t) => t + 1)} />
        </FullScreenShell>
      );
  } else if (!photos || pages.length === 0) {
    content = <EmptyChapterCard />;
  } else {
    content = (
      <div
        onErrorCapture={handleImageErrorCapture}
        className={`reader-shell h-dvh w-full overflow-hidden overflow-x-hidden transition-colors duration-300 ${
          readerTheme === "day" ? "bg-gray-100" : "bg-[#050505]"
        }`}
      >
        {/* 阅读视图:按共享偏好分发;预加载张数完全由 preloadCount 经视图组件控制 */}
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
            nextVolumeTitle={photos.hasNext ? "下一章" : undefined}
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

        {/* 页码指示器(工具栏隐藏时、非条漫模式;与现有阅读器同款式) */}
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

        {/* 工具栏:书签/沉浸/缩略图/真实翻页等本地特有可选能力一律不接 */}
        <ReaderToolbar
          visible={toolbarVisible}
          title={photos.title}
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

        {/* 设置面板:受控,直接写共享偏好(模式/方向由本页从偏好派生,无需额外同步) */}
        {showOptionsPanel && (
          <ReaderOptionsPanel
            options={readerOpts}
            onChange={updateReaderOpts}
            onClose={() => setShowOptionsPanel(false)}
          />
        )}

        {/* AOP 装饰层:章节清单入口(浮动按钮 + 弹层,不侵入工具栏/视图) */}
        <JmChapterNav
          currentPid={pid}
          aid={photos.aid || aidParam}
          hasNext={photos.hasNext}
          nextPid={photos.nextPid}
          onNavigate={handleChapterNavigate}
        />

        {/* AOP 装饰层:批量下载入口(左下浮动按钮,避开右下角章节导航)
            「下载整本 / 下载本章」→ 服务端逐章抓图并打包 zip,完成后清理临时目录 */}
        <JmDownloadFab
          comic={{
            aid: photos.aid || aidParam,
            title: photos.title || "JM 漫画",
            currentPid: pid,
            currentTitle: photos.title,
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

  return <JmGate>{content}</JmGate>;
}
