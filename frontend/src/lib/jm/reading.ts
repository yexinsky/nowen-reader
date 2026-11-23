"use client";

/**
 * JM 在线阅读器 · 起始页与进度上报的元信息解析
 *
 * 解决两个数据正确性问题:
 * 1. 进章节把进度打回第 1 页:历史版本在无 `?page` 时固定取第 1 页,并立即上报
 *    覆盖服务端记录。现在起始页按「显式 ?page → 服务端设置 → 续读记录」三级决定,
 *    且起始页本身不再上报(见阅读器页面),只有真正翻页才写回。
 * 2. 进度上报污染历史:上报只需「看到第几页」,标题/封面应取作品级元信息;历史版本
 *    恒传 `coverUrl: ""` 与章节名当漫画名,把服务端已存封面覆盖成空 → 历史列表裂图。
 *    这里提供按 aid 缓存的元信息,缺失时由上报侧省略字段(服务端「空值不覆盖」)。
 */

import { jmComicDetail, jmGetSettings, jmHistoryList } from "./client";

export interface JmAlbumMeta {
  /** 作品名(非章节名) */
  title: string;
  /** 作品封面(服务端代理相对地址) */
  coverUrl: string;
}

/** 作品元信息缓存:同一作品在会话内只查一次详情 */
const metaCache = new Map<string, JmAlbumMeta>();
const metaInflight = new Map<string, Promise<JmAlbumMeta | null>>();

/**
 * 读取作品级标题与封面(上报进度用);失败返回 null,调用方按「不传该字段」处理。
 * 按 aid 缓存,阅读器内切章节不重复请求。
 */
export function loadJmAlbumMeta(aid: string): Promise<JmAlbumMeta | null> {
  if (!aid) return Promise.resolve(null);
  const cached = metaCache.get(aid);
  if (cached) return Promise.resolve(cached);
  const inflight = metaInflight.get(aid);
  if (inflight) return inflight;
  const task = jmComicDetail(aid)
    .then((d) => {
      const meta: JmAlbumMeta = { title: d.title || "", coverUrl: d.coverUrl || "" };
      metaCache.set(aid, meta);
      return meta;
    })
    .catch(() => null)
    .finally(() => {
      metaInflight.delete(aid);
    });
  metaInflight.set(aid, task);
  return task;
}

/** 清除元信息缓存(登出/切换账号时调用,避免串号) */
export function clearJmAlbumMetaCache(): void {
  metaCache.clear();
  metaInflight.clear();
}

function clampPage(n: number, total: number): number {
  return Math.min(Math.max(n, 1), Math.max(total, 1));
}

/**
 * 解析进章节的起始页(1 基)。
 *
 * 优先级:
 * 1. 显式 `?page=N`(历史/继续阅读入口) —— 用户明确要求的位置,最优先;
 * 2. 服务端设置 `startFromFirstPage` 为开 —— 一律第 1 页(忽略续读记录);
 * 3. 否则查服务端历史,命中**本章节**(pid)时续读到该页,未命中则第 1 页。
 *
 * 失败一律回退第 1 页(不阻塞阅读);因为起始页不再上报,回退不会破坏已有进度。
 */
export async function resolveJmStartPage(
  pid: string,
  rawPage: string | null,
  totalPages: number
): Promise<number> {
  const raw = Number.parseInt(rawPage ?? "", 10);
  if (Number.isFinite(raw) && raw >= 1) return clampPage(raw, totalPages);

  try {
    const settings = await jmGetSettings();
    if (settings.startFromFirstPage) return 1;
  } catch {
    // 设置读不到时按默认口径(续读)继续尝试,仍失败则第 1 页
  }

  try {
    const page = await jmHistoryList(1);
    const hit = page.list.find((item) => item.pid === pid);
    if (hit && Number.isFinite(hit.imageIndex) && hit.imageIndex >= 1) {
      return clampPage(hit.imageIndex, totalPages);
    }
  } catch {
    /* 未登录/历史不可用时从头开始 */
  }
  return 1;
}
