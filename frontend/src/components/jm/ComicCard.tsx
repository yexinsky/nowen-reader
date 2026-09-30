"use client";

/**
 * JM 在线源 · 漫画卡片(JmComicCard)
 * 供在线首页 / 搜索 / 每周必看 / 收藏 / 历史等列表复用。
 *
 * - 封面一律经 resolveJmUrl 走 JM 服务端代理,不直连上游图片域名(PRD §5.3)
 * - 隐私策略:命中 NSFW 且开启隐私模糊时封面恒定遮蔽,hover 不解除(PRD §5.2,与站内 ComicCard 口径一致)
 * - 点击跳转在线源详情页 /jm/comic/{aid}
 */

import Link from "next/link";
import { Eye, Heart, ImageOff, Images } from "lucide-react";
import { isNSFW } from "@/lib/nsfw";
import { usePrivacyMode } from "@/hooks/usePrivacyMode";
import { resolveJmUrl } from "@/lib/jm/config";
import type { JmComicItem } from "@/lib/jm/types";

function trimTrailingZero(s: string): string {
  return s.endsWith(".0") ? s.slice(0, -2) : s;
}

/** 计数字段紧凑格式化(服务端已换算为 int,如 45000 → 4.5万) */
function formatCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 1e8) return `${trimTrailingZero((n / 1e8).toFixed(1))}亿`;
  if (n >= 1e4) return `${trimTrailingZero((n / 1e4).toFixed(1))}万`;
  return String(n);
}

/** 精确导出签名:其他板块(收藏/历史/详情)依赖此签名,勿改动 */
export function JmComicCard({ comic }: { comic: JmComicItem }) {
  const { enabled: privacyEnabled, blurNSFW } = usePrivacyMode();
  // NSFW 判定:标签优先、标题兜底;隐私模式 + 模糊开关同时开启才遮蔽
  const shouldBlur =
    isNSFW({ tags: comic.tags, title: comic.title }) && privacyEnabled && blurNSFW;
  const coverUrl = resolveJmUrl(comic.coverUrl);
  const detailHref = `/jm/comic/${comic.aid}`;

  return (
    <div className="group flex min-w-0 flex-col">
      <Link href={detailHref} className="block" aria-label={comic.title}>
        <div className="relative aspect-[3/4] w-full overflow-hidden rounded-lg bg-muted/10">
          {coverUrl ? (
            <img
              src={coverUrl}
              alt={comic.title}
              loading="lazy"
              className={`h-full w-full object-cover transition-transform duration-300 group-hover:scale-105 ${
                shouldBlur ? "select-none blur-lg" : ""
              }`}
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center text-muted/30">
              <ImageOff className="h-8 w-8" />
            </div>
          )}
          {comic.updateAt && (
            <span className="absolute bottom-1.5 right-1.5 rounded bg-black/60 px-1.5 py-0.5 text-[10px] leading-none text-white/90">
              {comic.updateAt}
            </span>
          )}
        </div>
      </Link>

      <div className="mt-2 min-w-0">
        <Link
          href={detailHref}
          title={comic.title}
          className="line-clamp-2 text-sm font-medium leading-snug text-foreground/90 transition-colors hover:text-accent"
        >
          {comic.title || "无标题"}
        </Link>
        <p className="mt-1 truncate text-xs text-muted">
          {comic.author || "未知作者"}
          {comic.category ? ` · ${comic.category}` : ""}
        </p>
        {/* 上游列表接口不下发 likes/views/image_count(见 MOBILE_API.md §2.1),
            仅在拿到非 0 值时展示对应图标,避免恒显 0 造成「未获取」的观感 */}
        {(comic.likes > 0 || comic.views > 0 || comic.imageCount > 0) && (
          <div className="mt-1 flex items-center gap-3 text-[11px] text-muted/80">
            {comic.likes > 0 && (
              <span className="flex items-center gap-1" title="爱心数">
                <Heart className="h-3 w-3" />
                {formatCount(comic.likes)}
              </span>
            )}
            {comic.views > 0 && (
              <span className="flex items-center gap-1" title="点击数">
                <Eye className="h-3 w-3" />
                {formatCount(comic.views)}
              </span>
            )}
            {comic.imageCount > 0 && (
              <span className="flex items-center gap-1" title="图片数">
                <Images className="h-3 w-3" />
                {formatCount(comic.imageCount)}
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
