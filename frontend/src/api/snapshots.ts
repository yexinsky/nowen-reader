import { apiPath } from "@/lib/base-path";

/**
 * 快照模块 API（标签+分类域）
 * 对应后端 /api/snapshots
 */

const getBase = () => apiPath("/api/snapshots");

// ============================================================
// Types
// ============================================================

export interface SnapshotItem {
  id: number;
  domain: string;
  name: string;
  kind: "manual" | "auto" | string;
  reason: string;
  summary: string;
  sizeBytes: number;
  createdAt: string;
}

export interface SnapshotSummary {
  tags: number;
  comicTags: number;
  aliases: number;
  normIgnores: number;
  scenarios: number;
  categories: number;
  comicCategories: number;
}

export interface SnapshotRestoreResult {
  tags: number;
  comicTags: number;
  aliases: number;
  normIgnores: number;
  scenarios: number;
  categories: number;
  comicCategories: number;
  safetySnapshotId: number;
}

// ============================================================
// Helpers
// ============================================================

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiPath(path), init);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error((data && (data.error || data.detail)) || `HTTP ${res.status}`);
  }
  return data as T;
}

function postJson(body?: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  };
}

/** 解析快照 summary JSON（容错） */
export function parseSnapshotSummary(item: SnapshotItem): SnapshotSummary {
  try {
    const s = JSON.parse(item.summary || "{}");
    return {
      tags: s.tags ?? 0,
      comicTags: s.comicTags ?? 0,
      aliases: s.aliases ?? 0,
      normIgnores: s.normIgnores ?? 0,
      scenarios: s.scenarios ?? 0,
      categories: s.categories ?? 0,
      comicCategories: s.comicCategories ?? 0,
    };
  } catch {
    return {
      tags: 0,
      comicTags: 0,
      aliases: 0,
      normIgnores: 0,
      scenarios: 0,
      categories: 0,
      comicCategories: 0,
    };
  }
}

// ============================================================
// API
// ============================================================

/** 快照列表（最新在前） */
export function listSnapshots(): Promise<{ list: SnapshotItem[] }> {
  return request<{ list: SnapshotItem[] }>(`${getBase()}`);
}

/** 手动创建快照 */
export function createSnapshot(name?: string): Promise<{ ok: boolean; snapshot: SnapshotItem }> {
  return request<{ ok: boolean; snapshot: SnapshotItem }>(`${getBase()}`, postJson({ name }));
}

/** 恢复快照（整域替换，恢复前自动保存当前状态） */
export function restoreSnapshot(id: number): Promise<{ ok: boolean; restored: SnapshotRestoreResult }> {
  return request<{ ok: boolean; restored: SnapshotRestoreResult }>(
    `${getBase()}/${id}/restore`,
    postJson()
  );
}

/** 删除快照 */
export function deleteSnapshot(id: number): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>(`${getBase()}/${id}`, { method: "DELETE" });
}
