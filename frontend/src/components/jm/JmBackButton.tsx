"use client";

/**
 * JM 二级页返回按钮
 * 固定 SPA 导航回在线首页(navigate 而非 history.back):
 * 首页各标签带会话级缓存,返回时命中缓存不重新拉取上游数据。
 * 放在 PageHeader 的 actions 中使用。
 */

import { useNavigate } from "react-router-dom";
import { ArrowLeft } from "lucide-react";

export function JmBackButton({ to = "/jm", label = "返回在线漫画" }: { to?: string; label?: string }) {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      onClick={() => navigate(to)}
      aria-label={label}
      title={label}
      className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border bg-card px-3 text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground"
    >
      <ArrowLeft className="h-4 w-4" />
      返回
    </button>
  );
}
