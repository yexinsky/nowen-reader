"use client";

import type { ReactNode } from "react";
import DesktopSidebar from "@/components/DesktopSidebar";

interface AppShellProps {
  children: ReactNode;
  className?: string;
}

export default function AppShell({ children, className = "" }: AppShellProps) {
  return (
    // pb 值 = 底部导航高度(h-14=3.5rem)+ 安全区内边距:移动端内容不被导航条遮挡
    <div
      className={`min-h-screen bg-background pb-[calc(3.5rem+env(safe-area-inset-bottom,0px))] sm:pb-0 ${className}`}
    >
      <DesktopSidebar />
      <div className="min-h-screen lg:ml-[220px] xl:ml-[240px]">{children}</div>
    </div>
  );
}
