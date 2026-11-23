"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useLocation } from "react-router-dom";
import { LayoutDashboard, BookMarked, Settings, Layers, Globe2, Palette } from "lucide-react";
import { useTranslation } from "@/lib/i18n";
import { useAuth } from "@/lib/auth-context";

type NavItem = {
  href: string;
  icon: typeof LayoutDashboard;
  label: string;
  /** 该路由是否属于本入口(含全部子路径:搜索/周榜/收藏/历史等二级页) */
  match: (pathname: string, currentTab: string | null) => boolean;
};

/** 前缀匹配:自身或其子路径;"/" 只认根路径。 */
function matchPrefix(pathname: string, base: string): boolean {
  return pathname === base || pathname.startsWith(`${base}/`);
}

/**
 * 移动端底部导航栏
 * 仅在屏幕宽度 < 640px 时显示;入口与桌面侧栏保持一致(在线漫画 JM / 哔咔 Pica)。
 * 处于阅读器、漫画详情、登录页或未登录时不渲染。
 */
export default function MobileBottomNav() {
  const location = useLocation();
  const pathname = location.pathname;
  const searchParams = new URLSearchParams(location.search);
  const t = useTranslation();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const [isMobile, setIsMobile] = useState(false);
  const navRef = useRef<HTMLDivElement>(null);
  const [indicatorStyle, setIndicatorStyle] = useState<{ left: number; width: number }>({ left: 0, width: 0 });

  useEffect(() => {
    const check = () => setIsMobile(window.innerWidth < 640);
    check();
    window.addEventListener("resize", check);
    return () => window.removeEventListener("resize", check);
  }, []);

  // 在阅读器页面、漫画详情页以及未登录时不显示底部导航
  const isReaderPage =
    pathname?.startsWith("/reader/") ||
    pathname?.startsWith("/novel/") ||
    pathname?.startsWith("/jm/reader/") ||
    pathname?.startsWith("/pica/reader/");
  const isComicDetailPage =
    pathname?.startsWith("/comic/") ||
    pathname?.startsWith("/jm/comic/") ||
    pathname?.startsWith("/pica/comic/");
  const isLoginPage = pathname?.startsWith("/jm/login") || pathname?.startsWith("/pica/login");

  const currentTab = searchParams.get("tab");

  const shouldHide = !isMobile || isReaderPage || isComicDetailPage || isLoginPage || !user;

  const navItems: NavItem[] = [
    {
      href: "/",
      icon: LayoutDashboard,
      label: "首页",
      match: (p) => p === "/",
    },
    {
      href: "/books",
      icon: BookMarked,
      label: "书库",
      match: (p) => matchPrefix(p, "/books"),
    },
    {
      href: "/jm",
      icon: Globe2,
      label: "在线",
      // 搜索结果/周榜/收藏/历史/标签等二级页都归属「在线」
      match: (p) => matchPrefix(p, "/jm"),
    },
    {
      href: "/pica",
      icon: Palette,
      label: "哔咔",
      match: (p) => matchPrefix(p, "/pica"),
    },
    // 合集——仅管理员可见
    ...(isAdmin
      ? [
          {
            href: "/collections",
            icon: Layers,
            label: (t as any).collections?.navTitle || "合集",
            match: (p: string) => matchPrefix(p, "/collections"),
          },
        ]
      : []),
    {
      href: "/settings",
      icon: Settings,
      label: t.settings?.title || "设置",
      // 设置页内的 ?tab= 子面板同样保持聚焦
      match: (p) => matchPrefix(p, "/settings"),
    },
  ];

  const activeIndex = navItems.findIndex((item) => item.match(pathname, currentTab));

  // 计算指示器位置:无命中项时收起,避免停在上一页的位置造成「假聚焦」
  const updateIndicator = useCallback(() => {
    const container = navRef.current;
    if (!container) return;
    if (activeIndex < 0) {
      setIndicatorStyle((prev) => (prev.width === 0 ? prev : { left: prev.left, width: 0 }));
      return;
    }
    const buttons = container.querySelectorAll<HTMLElement>("[data-nav-item]");
    const activeBtn = buttons[activeIndex];
    if (!activeBtn) return;
    const containerRect = container.getBoundingClientRect();
    const btnRect = activeBtn.getBoundingClientRect();
    setIndicatorStyle({
      left: btnRect.left - containerRect.left + btnRect.width / 2 - 12,
      width: 24,
    });
  }, [activeIndex]);

  useEffect(() => {
    updateIndicator();
  }, [updateIndicator, pathname, currentTab, isAdmin]);

  // resize / 字体加载等导致布局变化时重新计算
  useEffect(() => {
    window.addEventListener("resize", updateIndicator);
    return () => window.removeEventListener("resize", updateIndicator);
  }, [updateIndicator]);

  useEffect(() => {
    const container = navRef.current;
    if (!container || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => updateIndicator());
    observer.observe(container);
    return () => observer.disconnect();
  }, [updateIndicator]);

  if (shouldHide) return null;

  return (
    <nav className="fixed bottom-0 left-0 right-0 z-50 border-t border-border/50 bg-surface/90 backdrop-blur-2xl sm:hidden safe-bottom overflow-hidden">
      <div ref={navRef} className="relative flex h-14 items-center justify-around px-1">
        {/* 滑动指示器(无聚焦项时宽度收为 0,自然隐藏)
            必须带 left-0:绝对定位元素没有 left/right 时会退回「静态位置」,
            而父级是 justify-around 的 flex,静态位置在容器正中,
            会把 translateX 整体偏移 (容器宽-24)/2,导致指示器落在错误的分栏上。 */}
        <div
          className="nav-indicator absolute left-0 top-0 h-[2px] rounded-full bg-gradient-to-r from-accent to-accent-purple shadow-[0_0_8px_var(--color-accent)]"
          style={{
            transform: `translateX(${indicatorStyle.left}px)`,
            width: `${indicatorStyle.width}px`,
            opacity: indicatorStyle.width > 0 ? 1 : 0,
          }}
        />

        {navItems.map((item) => {
          const Icon = item.icon;
          const active = item.href === navItems[activeIndex]?.href;
          return (
            <Link
              key={item.href}
              href={item.href}
              data-nav-item
              aria-current={active ? "page" : undefined}
              className={`group flex min-w-0 flex-1 flex-col items-center gap-0.5 rounded-xl px-0.5 py-1 transition-all duration-200 ${
                active ? "text-accent -translate-y-0.5" : "text-muted hover:text-foreground translate-y-0"
              }`}
            >
              <Icon
                className={`h-5 w-5 shrink-0 transition-transform duration-200 ${
                  active ? "scale-110" : "scale-100 group-active:scale-95"
                }`}
              />
              <span
                className={`w-full truncate text-center text-[10px] font-medium transition-opacity duration-200 ${
                  active ? "opacity-100" : "opacity-70"
                }`}
              >
                {item.label}
              </span>
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
