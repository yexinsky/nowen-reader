"use client";

/**
 * JM 在线源 · 每日签到日历(#29 GET /api/user/sign、#30 POST /api/user/sign)
 * PRD docs/PRD_JM_SOURCE.md M11
 *
 * 自包含组件(无 props):内部拉取签到状态、执行签到、渲染本月日历。
 * 可嵌入在线首页用户卡与设置面板;不做轮询,挂载拉一次。
 * - todaySigned=true → 按钮禁用态「已签到」
 * - days[].date 语义为「当月第几日」(int),未返回的日期视为未签到
 * - 点击签到 → jmSign() → 以返回 msg 做内联/Toast 提示 → 重新拉取状态
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { CalendarCheck, Check, Loader2 } from "lucide-react";
import { jmSign, jmSignStatus } from "@/lib/jm/client";
import type { JmSignStatus } from "@/lib/jm/types";
import { useToast } from "@/components/Toast";

/** 周一为一周起点 */
const WEEK_LABELS = ["一", "二", "三", "四", "五", "六", "日"] as const;

export function JmSignCalendar() {
  const toast = useToast();
  const [status, setStatus] = useState<JmSignStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [signing, setSigning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);

  /** 拉取签到状态;初始/刷新共用,不触发整卡骨架 */
  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const s = await jmSignStatus();
      if (signal?.aborted) return;
      setStatus(s);
      setError(null);
    } catch (err) {
      if (signal?.aborted) return;
      setError(err instanceof Error ? err.message : "签到状态加载失败");
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const handleSign = useCallback(async () => {
    if (signing) return;
    setSigning(true);
    setError(null);
    setMsg(null);
    try {
      const res = await jmSign();
      const text = res.msg || "签到成功";
      setMsg(text);
      toast.success(text);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "签到失败,请稍后重试");
    } finally {
      setSigning(false);
    }
  }, [signing, toast, load]);

  const now = new Date();
  const year = now.getFullYear();
  const month = now.getMonth(); // 0 基
  const today = now.getDate();

  /** 已签到日期集合(days[].date 为当月第几日) */
  const signedDays = useMemo(
    () => new Set((status?.days ?? []).filter((d) => d.signed).map((d) => d.date)),
    [status]
  );

  /** 本月日历格(前置空白 + 1..daysInMonth),周一为一周起点 */
  const cells = useMemo<(number | null)[]>(() => {
    const daysInMonth = new Date(year, month + 1, 0).getDate();
    const firstDay = new Date(year, month, 1).getDay(); // 0=周日
    const lead = (firstDay + 6) % 7;
    const list: (number | null)[] = Array.from({ length: lead }, () => null);
    for (let d = 1; d <= daysInMonth; d++) list.push(d);
    return list;
  }, [year, month]);

  const todaySigned = status?.todaySigned ?? false;

  return (
    <div className="w-full max-w-[300px] rounded-xl border border-border/60 bg-card p-4">
      {/* 标题行 */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <CalendarCheck className="h-4 w-4 text-accent" />
          <h3 className="text-sm font-semibold text-foreground">每日签到</h3>
        </div>
        <span className="text-[11px] text-muted">
          {year} 年 {month + 1} 月
        </span>
      </div>

      {/* 星期表头 */}
      <div className="mt-3 grid grid-cols-7 gap-1 text-center">
        {WEEK_LABELS.map((w) => (
          <span key={w} className="text-[10px] text-muted">
            {w}
          </span>
        ))}
      </div>

      {/* 日历格 */}
      <div className="mt-1 grid grid-cols-7 gap-1" aria-label="本月签到日历">
        {loading
          ? Array.from({ length: 28 }).map((_, i) => (
              <div key={i} className="aspect-square animate-pulse rounded-full bg-muted/15" />
            ))
          : cells.map((d, i) => {
              if (d === null) return <div key={`blank-${i}`} aria-hidden="true" />;
              const signed = signedDays.has(d);
              const isToday = d === today;
              const isFuture = d > today;
              return (
                <div
                  key={d}
                  title={signed ? "已签到" : isFuture ? "" : "未签到"}
                  className={`flex aspect-square items-center justify-center rounded-full text-[10px] font-medium transition-colors ${
                    signed
                      ? "bg-accent text-white"
                      : isFuture
                        ? "text-muted/40"
                        : "bg-muted/10 text-muted"
                  } ${isToday ? "ring-2 ring-accent ring-offset-1 ring-offset-card" : ""}`}
                >
                  {d}
                </div>
              );
            })}
      </div>

      {/* 内联提示 */}
      {msg && !error && <p className="mt-3 text-xs text-emerald-400">{msg}</p>}
      {error && <p className="mt-3 break-all text-xs text-red-400">{error}</p>}

      {/* 签到按钮 */}
      <button
        onClick={handleSign}
        disabled={loading || signing || todaySigned}
        className={`mt-3 flex w-full items-center justify-center gap-1.5 rounded-lg py-2 text-sm font-medium transition-colors ${
          todaySigned
            ? "cursor-default bg-emerald-500/15 text-emerald-400"
            : "bg-accent text-white hover:bg-accent-hover disabled:opacity-60"
        }`}
      >
        {loading ? (
          <>
            <Loader2 className="h-4 w-4 animate-spin" />
            加载中…
          </>
        ) : signing ? (
          <>
            <Loader2 className="h-4 w-4 animate-spin" />
            签到中…
          </>
        ) : todaySigned ? (
          <>
            <Check className="h-4 w-4" />
            已签到
          </>
        ) : (
          "签到"
        )}
      </button>
    </div>
  );
}
