"use client";

/**
 * 批量选择操作条(浮在视口底部)
 *
 * 为什么用 fixed 而不是 sticky:页面链路上 AppShell 与路由容器都带 `overflow-x-hidden`,
 * 它会把祖先变成滚动容器,`position: sticky` 的吸附基准因此失效 —— 列表一滚,
 * 操作条(含「批量下载」按钮)就跟着滚出可视区,表现就是「选完之后找不到开始下载」。
 * fixed 相对视口定位,不受祖先 overflow / 层叠上下文影响。
 *
 * 手机端底边抬高「底部导航高度 + 安全区」,不会被底部导航压住;
 * 桌面端脱离侧栏宽度居中,底部留白。
 */

import type { ReactNode } from "react";

export function BatchActionBar({ children }: { children: ReactNode }) {
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-0 z-40 px-3 pb-[calc(3.5rem+env(safe-area-inset-bottom,0px)+0.5rem)] sm:px-6 sm:pb-6 lg:pl-[calc(220px+1.5rem)] xl:pl-[calc(240px+1.5rem)]">
      <div className="pointer-events-auto mx-auto flex max-w-2xl flex-wrap items-center gap-2 rounded-xl border border-border/60 bg-card/95 px-3 py-2 shadow-2xl backdrop-blur-xl">
        {children}
      </div>
    </div>
  );
}

/** 悬浮条占位:避免列表最后一行被悬浮条永久遮住 */
export function BatchActionBarSpacer() {
  return <div aria-hidden className="h-24 sm:h-16" />;
}
