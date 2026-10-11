"use client";

/**
 * 哔咔在线源 · 漫画网格(PicaComicGrid)
 * 网格布局渲染 PicaComicCard;空数组展示 emptyText 空态。
 */

import { BookOpen } from "lucide-react";
import { PicaComicCard } from "@/components/pica/ComicCard";
import type { PicaComicItem } from "@/lib/pica/types";

export function PicaComicGrid({
  comics,
  emptyText,
}: {
  comics: PicaComicItem[];
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
        <PicaComicCard key={`${comic.id}-${index}`} comic={comic} />
      ))}
    </div>
  );
}
