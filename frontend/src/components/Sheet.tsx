"use client";

/**
 * 弹层基础件(下载对话框 / 下载任务面板共用)
 *
 * 手机端「弹层上下偏移、底部按钮点不到/看不见」的三个根因在这里统一解决:
 * 1. 面板挂在页面树深处,祖先的层叠上下文与 overflow 会干扰 fixed 定位;
 *    → createPortal 到 body,相对视口定位。
 * 2. 限高用 88vh 取的是「大视口」高度(移动浏览器地址栏收起时),地址栏展开后
 *    实际可视区更矮,底部对齐的抽屉就被顶出屏幕;底部 fixed 元素也会落到浏览器
 *    工具栏下面 —— 这正好是「任务多时窗口偏、开始下载按钮看不见」的成因。
 *    → 遮罩层用 dvh 跟随可视区(.sheet-layer),卡片限高 100%,底部再补安全区。
 * 3. 背景可滚动时长页面被手势顶偏 → 弹层打开期间锁定 body 滚动。
 */

import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";

/** 背景滚动锁的引用计数:多层弹层叠加时,只由最后关闭的一层还原 */
let lockCount = 0;
let savedOverflow = "";
let savedPaddingRight = "";

function useBodyScrollLock(active: boolean) {
  useEffect(() => {
    if (!active) return;
    if (lockCount === 0) {
      const scrollbar = window.innerWidth - document.documentElement.clientWidth;
      savedOverflow = document.body.style.overflow;
      savedPaddingRight = document.body.style.paddingRight;
      document.body.style.overflow = "hidden";
      // 桌面端隐藏滚动条会让内容横向跳一下,补上等宽内边距抵掉
      if (scrollbar > 0) document.body.style.paddingRight = `${scrollbar}px`;
    }
    lockCount += 1;
    return () => {
      lockCount = Math.max(0, lockCount - 1);
      if (lockCount === 0) {
        document.body.style.overflow = savedOverflow;
        document.body.style.paddingRight = savedPaddingRight;
      }
    };
  }, [active]);
}

/** 挂到 body:等客户端挂载后再渲染,首帧不会碰到 document */
export function Portal({ children }: { children: ReactNode }) {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  if (!mounted) return null;
  return createPortal(children, document.body);
}

export interface SheetOverlayProps {
  onClose: () => void;
  children: ReactNode;
  /** 是否锁定背景滚动(默认锁) */
  lockScroll?: boolean;
}

/**
 * 弹层遮罩:手机端贴着可视区底边升起(底部对齐),>=640px 居中。
 * children 为弹层卡片本身;遮罩点击关闭,卡片内部需自行 stopPropagation。
 */
export function SheetOverlay({ onClose, children, lockScroll = true }: SheetOverlayProps) {
  useBodyScrollLock(lockScroll);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <Portal>
      <div
        className="sheet-layer fixed inset-x-0 top-0 z-[70] flex items-end justify-center bg-black/50 backdrop-blur-[2px] sm:inset-0 sm:h-auto sm:items-center"
        onClick={onClose}
      >
        {children}
      </div>
    </Portal>
  );
}

/**
 * 弹层卡片:底部对齐、贴安全区;内容超高时主体内部滚动,
 * 底部操作区因此始终留在可视区内(不会被内容顶出屏幕)。
 */
export const sheetCardClass =
  "flex w-full max-w-lg min-h-0 flex-col overflow-hidden rounded-t-2xl border border-border bg-card pb-[env(safe-area-inset-bottom,0px)] shadow-2xl sheet-max-h sm:rounded-2xl sm:pb-0";

/** 弹层头部(标题 + 关闭) */
export function SheetHeader({
  title,
  extra,
  onClose,
}: {
  title: ReactNode;
  extra?: ReactNode;
  onClose: () => void;
}) {
  return (
    <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border/60 px-4 py-3">
      <h3 className="flex min-w-0 items-center gap-2 truncate text-sm font-semibold text-foreground">
        {title}
        {extra}
      </h3>
      <button
        type="button"
        onClick={onClose}
        aria-label="关闭"
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted transition-colors hover:bg-background hover:text-foreground"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}

/** 弹层主体:min-h-0 + flex-1 让它在限高内滚动,不把底部按钮顶出可视区 */
export function SheetBody({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return <div className={`min-h-0 flex-1 overflow-y-auto ${className}`}>{children}</div>;
}

/** 弹层底部操作区(固定在卡片底部,不随内容滚动) */
export function SheetFooter({ children }: { children: ReactNode }) {
  return (
    <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t border-border/60 px-4 py-3">
      {children}
    </div>
  );
}
