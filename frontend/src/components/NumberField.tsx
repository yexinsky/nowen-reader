"use client";

import { useState } from "react";

/**
 * 数字输入框：纯文本输入（无原生上下箭头），支持全选/清空自由编辑；
 * 输入过程不改动数值，失焦或回车时才解析并夹取到 [min, max]（可选），
 * 非法/空值回退到 fallback。回车提交，Esc 撤销本次编辑。
 */
export function NumberField({
  value,
  fallback,
  min,
  max,
  onCommit,
  placeholder,
  className,
}: {
  value: number;
  fallback: number;
  min?: number;
  max?: number;
  onCommit: (value: number) => void;
  placeholder?: string;
  className?: string;
}) {
  // null = 未在编辑，直接显示外部值；非 null = 编辑中的草稿（允许空串）
  const [draft, setDraft] = useState<string | null>(null);

  const commit = () => {
    const n = parseInt(draft ?? String(value), 10);
    let next = Number.isNaN(n) ? fallback : n;
    if (min !== undefined) next = Math.max(min, next);
    if (max !== undefined) next = Math.min(max, next);
    setDraft(null);
    if (next !== value) onCommit(next);
  };

  return (
    <input
      type="text"
      inputMode="numeric"
      value={draft ?? String(value)}
      placeholder={placeholder}
      onChange={(e) => {
        const stripped = e.target.value.replace(/\D/g, "");
        // 剥离后与原始输入不一致（全角数字/字母/小数点等）时回写 DOM：
        // 若剥离结果与当前草稿相同，React 会跳过重渲染，DOM 会残留非法字符
        if (stripped !== e.target.value) e.target.value = stripped;
        setDraft(stripped);
      }}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur();
        if (e.key === "Escape") setDraft(null);
      }}
      className={className}
    />
  );
}
