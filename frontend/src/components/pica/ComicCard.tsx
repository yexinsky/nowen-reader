"use client";

/**
 * 哔咔在线源 · 漫画卡片(PicaComicCard)
 * 供哔咔首页 / 搜索 / 排行 / 收藏 / 历史等列表复用。
 *
 * - 封面走后端代理地址(/api/pica/image?fs=&path=),不直连上游图片域名
 * - 隐私策略:命中 NSFW 且开启隐私模糊时封面恒定遮蔽,hover 不解除(与站内口径一致)
 * - 点击跳转哔咔详情页 /pica/comic/{id}
 */

import Link from "next/link";
import { Check, Eye, Heart, ImageOff, Images } from "lucide-react";
import { isNSFW } from "@/lib/nsfw";
import { usePrivacyMode } from "@/hooks/usePrivacyMode";
import { resolvePicaUrl } from "@/lib/pica/config";
import { usePicaBatchSelection } from "@/components/pica/download/BatchDownload";
import type { PicaComicItem } from "@/lib/pica/types";

function trimTrailingZero(s: string): string {
  return s.endsWith(".0") ? s.slice(0, -2) : s;
}

/** 计数字段紧凑格式化(如 45000 → 4.5万) */
function formatCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 1e8) return `${trimTrailingZero((n / 1e8).toFixed(1))}亿`;
  if (n >= 1e4) return `${trimTrailingZero((n / 1e4).toFixed(1))}万`;
  return String(n);
}

export function PicaComicCard({ comic }: { comic: PicaComicItem }) {
  const { enabled: privacyEnabled, blurNSFW } = usePrivacyMode();
  const batch = usePicaBatchSelection();
  const selecting = batch?.selectMode ?? false;
  const checked = batch?.isSelected(comic.id) ?? false;
  const shouldBlur =
    isNSFW({ tags: comic.tags, title: comic.title }) && privacyEnabled && blurNSFW;
  const coverUrl = resolvePicaUrl(comic.coverUrl);
  const detailHref = `/pica/comic/${comic.id}`;
  const category = comic.categories?.[0] ?? "";

  return (
    <div className="group flex min-w-0 flex-col">
      <Link
        href={detailHref}
        className="block"
        aria-label={comic.title}
        onClick={(e) => {
          if (selecting && batch) {
            e.preventDefault();
            batch.toggle(comic.id);
          }
        }}
      >
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
          {selecting && (
            <span
              className={`absolute left-1.5 top-1.5 flex h-6 w-6 items-center justify-center rounded-md border shadow-sm ${
                checked ? "border-accent bg-accent text-white" : "border-white/60 bg-black/40"
              }`}
            >
              {checked && <Check className="h-4 w-4" />}
            </span>
          )}
          {comic.finished && (
            <span className="absolute left-1.5 bottom-1.5 rounded bg-emerald-600/80 px-1.5 py-0.5 text-[10px] leading-none text-white">
              完结
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
          {category ? ` · ${category}` : ""}
        </p>
        {(comic.likes > 0 || comic.views > 0 || comic.imageCount > 0) && (
          <div className="mt-1 flex items-center gap-3 text-[11px] text-muted/80">
            {comic.likes > 0 && (
              <span className="flex items-center gap-1" title="爱心数">
                <Heart className="h-3 w-3" />
                {formatCount(comic.likes)}
              </span>
            )}
            {comic.views > 0 && (
              <span className="flex items-center gap-1" title="观看数">
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
