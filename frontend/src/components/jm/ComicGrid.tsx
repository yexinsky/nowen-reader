"use client";

/**
 * JM 在线源 · 漫画网格(JmComicGrid)
 * 网格布局渲染 JmComicCard;空数组展示 emptyText 空态。
 * 供在线首页 / 搜索 / 每周必看 / 收藏 / 历史等列表复用。
 */

import { BookOpen } from "lucide-react";
import { JmComicCard } from "@/components/jm/ComicCard";
import type { JmComicItem } from "@/lib/jm/types";

/** 精确导出签名:其他板块(收藏/历史/详情)依赖此签名,勿改动 */
export function JmComicGrid({
  comics,
  emptyText,
}: {
  comics: JmComicItem[];
  emptyText?: string;
}) {
  if (!comics || comics.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-muted/10">
          <BookOpen className="h-7 w-7 text-muted/40" />
        </div>
        <p className="text-sm text-muted">{emptyText || "暂无内容"}</p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
      {comics.map((comic, index) => (
        <JmComicCard key={`${comic.aid}-${index}`} comic={comic} />
      ))}
    </div>
  );
}
