import { apiPath } from "@/lib/base-path";

/**
 * 标签 API client
 * 对应后端 /api/tags/*（读=登录，写=admin，403 由调用方按现有方式提示）
 */

// ============================================================
// 类型定义
// ============================================================

export interface TagScenarioTag {
  id: number;
  name: string;
  comicCount: number;
}

export interface TagScenario {
  id: number;
  name: string;
  color: string;
  sortOrder: number;
  tags: TagScenarioTag[];
}

export interface TagScenariosResponse {
  list: TagScenario[];
  unassigned: { tags: TagScenarioTag[] };
}

/** TagFilter 大面板按情景分组的渲染模型（成员标签为名称列表） */
export interface TagScenarioGroup {
  id: number;
  name: string;
  color: string;
  tags: string[];
}

export interface AIScenarioAssignment {
  tagId: number;
  tagName: string;
  scenarioId: number;
  scenarioName: string;
}

export interface AIAssignScenariosResult {
  assignments: AIScenarioAssignment[];
  skipped: string[];
}

export interface ApiResult<T = unknown> {
  ok: boolean;
  error?: string;
  data?: T;
}

const EMPTY_SCENARIOS: TagScenariosResponse = { list: [], unassigned: { tags: [] } };

async function readError(res: Response): Promise<string> {
  try {
    const data = await res.json();
    return data.detail || data.error || `HTTP ${res.status}`;
  } catch {
    return `HTTP ${res.status}`;
  }
}

// ============================================================
// API 函数
// ============================================================

/** GET /api/tags/scenarios — 情景列表（按 sortOrder）+ 未分配标签；失败时返回空结构 */
export async function fetchTagScenarios(): Promise<TagScenariosResponse> {
  try {
    const res = await fetch(apiPath("/api/tags/scenarios"));
    if (!res.ok) return EMPTY_SCENARIOS;
    const data = await res.json();
    return {
      list: Array.isArray(data.list) ? data.list : [],
      unassigned: { tags: data.unassigned?.tags || [] },
    };
  } catch {
    return EMPTY_SCENARIOS;
  }
}

/** POST /api/tags/scenarios — 创建情景（重名 422） */
export async function createTagScenario(name: string, color?: string): Promise<ApiResult<{ id: number }>> {
  try {
    const res = await fetch(apiPath("/api/tags/scenarios"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(color ? { name, color } : { name }),
    });
    if (!res.ok) return { ok: false, error: await readError(res) };
    const data = await res.json();
    return { ok: true, data: { id: data.id } };
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

/** PUT /api/tags/scenarios/:id — 更新名称/颜色/排序 */
export async function updateTagScenario(
  id: number,
  patch: { name?: string; color?: string; sortOrder?: number }
): Promise<ApiResult> {
  try {
    const res = await fetch(apiPath(`/api/tags/scenarios/${id}`), {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    });
    if (!res.ok) return { ok: false, error: await readError(res) };
    return { ok: true };
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

/** DELETE /api/tags/scenarios/:id — 删除情景（成员标签回到未分配） */
export async function deleteTagScenario(id: number): Promise<ApiResult> {
  try {
    const res = await fetch(apiPath(`/api/tags/scenarios/${id}`), { method: "DELETE" });
    if (!res.ok) return { ok: false, error: await readError(res) };
    return { ok: true };
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

/** POST /api/tags/scenarios/assign — 批量分配（scenarioId 为 null 表示移出情景） */
export async function assignTagsToScenario(
  tagIds: number[],
  scenarioId: number | null
): Promise<ApiResult<{ assigned: number }>> {
  try {
    const res = await fetch(apiPath("/api/tags/scenarios/assign"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ tagIds, scenarioId }),
    });
    if (!res.ok) return { ok: false, error: await readError(res) };
    const data = await res.json();
    return { ok: true, data: { assigned: data.assigned ?? tagIds.length } };
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

/** POST /api/ai/assign-tag-scenarios — AI 为未分配标签分配情景（仅使用已有情景） */
export async function aiAssignTagScenarios(body: {
  onlyUnassigned: boolean;
}): Promise<ApiResult<AIAssignScenariosResult>> {
  try {
    const res = await fetch(apiPath("/api/ai/assign-tag-scenarios"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) return { ok: false, error: await readError(res) };
    const data = await res.json();
    return {
      ok: true,
      data: {
        assignments: Array.isArray(data.assignments) ? data.assignments : [],
        skipped: Array.isArray(data.skipped) ? data.skipped : [],
      },
    };
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}
