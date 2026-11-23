"use client";

/**
 * JM 在线源 · 封面兜底(JmCover)
 *
 * 统一处理「没有封面 / 封面加载失败」两种情形:回退到标题首字生成的字母封面,
 * 而不是灰底 ImageOff 图标——后者会让整列表看起来「全坏了」。
 *
 * - 有封面时渲染 <img>,onError 后切字母封面(不再重试,避免抖动)
 * - 无封面时直接渲染字母封面
 * - 字母封面:同标题恒同色(字符码求和取模),列表滚动不闪色
 * - 只负责「填充父容器」,宽高比/圆角/模糊等由调用方在父级控制
 */

import { useEffect, useMemo, useState } from "react";

const PALETTE = [
  "from-accent/35 to-accent/5 text-accent",
  "from-violet-500/35 to-violet-500/5 text-violet-400",
  "from-emerald-500/35 to-emerald-500/5 text-emerald-400",
  "from-amber-500/35 to-amber-500/5 text-amber-400",
  "from-sky-500/35 to-sky-500/5 text-sky-400",
  "from-rose-500/35 to-rose-500/5 text-rose-400",
] as const;

/** 取标题首字(跳过空白与符号);全无则 "?" */
function initialOf(title: string): string {
  for (const ch of (title ?? "").trim()) {
    if (/[\p{L}\p{N}]/u.test(ch)) return ch.toUpperCase();
  }
  return "?";
}

/** 稳定配色:同一标题恒同色 */
function paletteOf(title: string): string {
  let sum = 0;
  for (let i = 0; i < title.length; i++) sum = (sum + title.charCodeAt(i)) % 9973;
  return PALETTE[sum % PALETTE.length];
}

export function JmLetterCover({ title, className = "" }: { title: string; className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={`flex h-full w-full items-center justify-center bg-gradient-to-br ${paletteOf(title)} ${className}`}
    >
      <span className="select-none text-xl font-semibold leading-none sm:text-2xl">
        {initialOf(title)}
      </span>
    </div>
  );
}

export interface JmCoverProps {
  /** 已 resolveJmUrl 处理过的可访问地址;空串表示无封面 */
  src: string;
  /** 用于 alt 与字母封面的标题 */
  title: string;
  /** 传入时命中即加 blur-lg(隐私模糊口径) */
  blurred?: boolean;
  className?: string;
}

export function JmCover({ src, title, blurred = false, className = "" }: JmCoverProps) {
  const [failed, setFailed] = useState(false);

  // 换了封面(例如换了漫画)后重置失败态
  useEffect(() => setFailed(false), [src]);

  const blurClass = blurred ? "select-none blur-lg" : "";
  const palette = useMemo(() => paletteOf(title), [title]);

  if (!src || failed) {
    return (
      <div
        aria-hidden="true"
        className={`flex h-full w-full items-center justify-center bg-gradient-to-br ${palette} ${blurClass} ${className}`}
      >
        <span className="select-none text-xl font-semibold leading-none sm:text-2xl">
          {initialOf(title)}
        </span>
      </div>
    );
  }

  return (
    <img
      src={src}
      alt={title}
      loading="lazy"
      onError={() => setFailed(true)}
      className={`h-full w-full object-cover ${blurClass} ${className}`}
    />
  );
}

export default JmCover;
