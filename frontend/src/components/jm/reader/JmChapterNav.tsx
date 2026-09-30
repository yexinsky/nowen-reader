"use client";

/**
 * JM 在线源 · 阅读器章节导航(AOP 装饰层)
 *
 * 以"装饰器"方式挂在 JM 阅读器页面壳(app/jm/reader)上:
 * - 浮动按钮打开章节清单弹层(数据来自 #14 详情的 episodes,打开时懒加载一次)
 * - 当前章高亮,支持点选切换 + 上一章/下一章快捷按钮
 * - 不修改、不包裹任何现有阅读器组件(ReaderToolbar/各 View 保持原样),
 *   仅以兄弟覆盖层形式叠加,事件不透传(stopPropagation)
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, List, X } from "lucide-react";
import { isJmApiError, jmComicDetail } from "@/lib/jm/client";
import type { JmEpisode } from "@/lib/jm/types";
import { useTheme } from "@/lib/theme-context";

const MAX_TITLE_LEN = 24;

interface JmChapterNavProps {
  /** 当前章节 pid(#15 的 pid) */
  currentPid: string;
  /** 漫画 aid(#15 返回或 ?aid= 参数),用于拉取章节清单 */
  aid: string;
  /** #15 的 hasNext/nextPid:下一章的第一真值来源 */
  hasNext: boolean;
  nextPid: string | null;
  /** 章节切换回调(由页面壳执行 replace 导航) */
  onNavigate: (pid: string) => void;
}

export function JmChapterNav({ currentPid, aid, hasNext, nextPid, onNavigate }: JmChapterNavProps) {
  const { theme } = useTheme();
  const light = theme === "light";
  const [open, setOpen] = useState(false);
  const [episodes, setEpisodes] = useState<JmEpisode[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  /** 已拉取章节清单的 aid:同一部漫画内切换章节不重复请求 */
  const loadedAidRef = useRef<string>("");

  const fetchEpisodes = useCallback(async (targetAid: string, force: boolean) => {
    if (!targetAid) return;
    if (!force && loadedAidRef.current === targetAid) return;
    loadedAidRef.current = targetAid;
    setLoading(true);
    setError(null);
    try {
      const detail = await jmComicDetail(targetAid);
      setEpisodes([...detail.episodes].sort((a, b) => a.order - b.order));
    } catch (err) {
      setError(
        isJmApiError(err) && err.code === 3001
          ? "章节清单不可用"
          : "章节清单加载失败,请重试"
      );
      loadedAidRef.current = "";
    } finally {
      setLoading(false);
    }
  }, []);

  const openSheet = useCallback(() => {
    setOpen(true);
    void fetchEpisodes(aid, false);
  }, [aid, fetchEpisodes]);

  // 卸载/换书时重置缓存标记
  useEffect(() => {
    if (loadedAidRef.current && aid && loadedAidRef.current !== aid) {
      loadedAidRef.current = "";
      setEpisodes(null);
    }
  }, [aid]);

  /* 当前章在清单中的位置 → 上一章/下一章(下一章优先用 #15 的 nextPid) */
  const currentIndex = useMemo(
    () => (episodes ? episodes.findIndex((e) => e.pid === currentPid) : -1),
    [episodes, currentPid]
  );
  const prevEpisode = currentIndex > 0 ? episodes?.[currentIndex - 1] ?? null : null;
  const nextEpisode =
    currentIndex >= 0 && episodes && currentIndex < episodes.length - 1
      ? episodes[currentIndex + 1]
      : null;
  const nextTargetPid = nextPid || (hasNext ? nextEpisode?.pid ?? null : null);

  const sheetBg = light ? "bg-white text-gray-900" : "bg-zinc-900 text-white";
  const subtleText = light ? "text-gray-500" : "text-white/50";
  const borderColor = light ? "border-gray-200" : "border-white/[0.08]";

  return (
    <>
      {/* 浮动入口:底部右侧;工具栏弹出时被其底栏覆盖(z 更低),隐藏后可点 */}
      <button
        type="button"
        aria-label="章节列表"
        title="章节列表"
        onClick={(e) => {
          e.stopPropagation();
          openSheet();
        }}
        className={`fixed bottom-5 right-4 z-30 flex h-11 w-11 items-center justify-center rounded-full border shadow-lg backdrop-blur-xl transition-transform active:scale-95 ${
          light
            ? "border-black/[0.06] bg-white/85 text-gray-700 shadow-black/10"
            : "border-white/[0.08] bg-zinc-900/85 text-white/80 shadow-black/40"
        }`}
      >
        <List className="h-5 w-5" />
      </button>

      {/* 章节清单弹层 */}
      {open && (
        <div
          className="fixed inset-0 z-[60] flex items-end justify-center bg-black/50 backdrop-blur-[2px]"
          onClick={(e) => {
            e.stopPropagation();
            setOpen(false);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.stopPropagation();
              setOpen(false);
            }
          }}
        >
          <div
            className={`max-h-[70dvh] w-full max-w-lg overflow-hidden rounded-t-2xl border-t shadow-2xl ${sheetBg} ${borderColor}`}
            onClick={(e) => e.stopPropagation()}
          >
            {/* 头部 */}
            <div className={`flex items-center justify-between border-b px-4 py-3 ${borderColor}`}>
              <span className="text-sm font-semibold">章节列表</span>
              <button
                type="button"
                aria-label="关闭"
                onClick={() => setOpen(false)}
                className={`rounded-lg p-1.5 transition-colors ${
                  light ? "hover:bg-gray-100" : "hover:bg-white/[0.06]"
                }`}
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {/* 清单 */}
            <div className="max-h-[46dvh] overflow-y-auto overscroll-contain px-2 py-2">
              {loading && (
                <div className="flex items-center justify-center gap-2 py-10">
                  <span className="h-5 w-5 animate-spin rounded-full border-2 border-white/10 border-t-current opacity-60" />
                  <span className={`text-sm ${subtleText}`}>加载章节...</span>
                </div>
              )}
              {!loading && error && (
                <div className="flex flex-col items-center gap-3 py-8">
                  <p className={`text-sm ${subtleText}`}>{error}</p>
                  <button
                    type="button"
                    onClick={() => void fetchEpisodes(aid, true)}
                    className={`rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors ${borderColor} ${
                      light ? "hover:bg-gray-50" : "hover:bg-white/[0.06]"
                    }`}
                  >
                    重试
                  </button>
                </div>
              )}
              {!loading && !error && episodes && episodes.length === 0 && (
                <p className={`py-8 text-center text-sm ${subtleText}`}>暂无章节信息</p>
              )}
              {!loading && !error && episodes && episodes.length > 0 && (
                <ul className="space-y-0.5">
                  {episodes.map((ep, i) => {
                    const active = ep.pid === currentPid;
                    return (
                      <li key={ep.pid}>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            if (active) {
                              setOpen(false);
                              return;
                            }
                            setOpen(false);
                            onNavigate(ep.pid);
                          }}
                          className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm transition-colors ${
                            active
                              ? light
                                ? "bg-accent/10 font-medium text-accent"
                                : "bg-accent/20 font-medium text-accent"
                              : light
                                ? "hover:bg-gray-50"
                                : "hover:bg-white/[0.05]"
                          }`}
                        >
                          <span
                            className={`w-8 shrink-0 text-right font-mono text-xs ${
                              active ? "" : subtleText
                            }`}
                          >
                            {ep.order || i + 1}
                          </span>
                          <span className="min-w-0 flex-1 truncate">
                            {ep.title.length > MAX_TITLE_LEN
                              ? `${ep.title.slice(0, MAX_TITLE_LEN)}…`
                              : ep.title}
                          </span>
                          {typeof ep.imageCount === "number" && (
                            <span className={`shrink-0 text-xs ${subtleText}`}>{ep.imageCount}P</span>
                          )}
                          {active && (
                            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-accent" />
                          )}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>

            {/* 底部快捷翻章 */}
            <div className={`flex items-center gap-2 border-t px-3 py-2.5 ${borderColor}`}>
              <button
                type="button"
                disabled={!prevEpisode}
                onClick={(e) => {
                  e.stopPropagation();
                  if (!prevEpisode) return;
                  setOpen(false);
                  onNavigate(prevEpisode.pid);
                }}
                className={`flex flex-1 items-center justify-center gap-1 rounded-lg border py-2 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${borderColor} ${
                  light ? "hover:bg-gray-50" : "hover:bg-white/[0.06]"
                }`}
              >
                <ChevronLeft className="h-3.5 w-3.5" />
                上一章
              </button>
              <button
                type="button"
                disabled={!nextTargetPid}
                onClick={(e) => {
                  e.stopPropagation();
                  if (!nextTargetPid) return;
                  setOpen(false);
                  onNavigate(nextTargetPid);
                }}
                className={`flex flex-1 items-center justify-center gap-1 rounded-lg border py-2 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${borderColor} ${
                  light ? "hover:bg-gray-50" : "hover:bg-white/[0.06]"
                }`}
              >
                下一章
                <ChevronRight className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
