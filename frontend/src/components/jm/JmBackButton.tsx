"use client";

/**
 * JM 二级页返回按钮
 * 优先 `history.back()`(保留来源页的标签、条件、滚动位置);
 * 直接打开/刷新进来(history 里没有上一页)时退回固定 SPA 导航 `to`。
 * 首页各标签带会话级缓存,返回首页会命中缓存不重新拉取上游数据。
 * 放在 PageHeader 的 actions 中使用。
 */

import { useNavigate } from "react-router-dom";
import { ArrowLeft } from "lucide-react";

export function JmBackButton({ to = "/jm", label = "返回在线漫画" }: { to?: string; label?: string }) {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      onClick={() => {
        // history.state.idx > 0 说明本次会话里有上一页:回退即可保住现场
        const idx = (window.history.state as { idx?: number } | null)?.idx ?? 0;
        if (idx > 0) {
          navigate(-1);
          return;
        }
        navigate(to);
      }}
      aria-label={label}
      title={label}
      className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border bg-card px-2.5 text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground sm:px-3"
    >
      <ArrowLeft className="h-4 w-4" />
      {/* 手机上只留图标:页头宽度有限,标题常被两个按钮挤成省略号 */}
      <span className="hidden sm:inline">返回</span>
    </button>
  );
}
