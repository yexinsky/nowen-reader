"use client";

import { useCallback, useState } from "react";
import {
  Ban,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ListPlus,
  Loader2,
  Merge,
  RotateCcw,
  Trash2,
} from "lucide-react";
import { apiPath } from "@/lib/base-path";
import { useTranslation } from "@/lib/i18n";
import { useToast } from "@/components/Toast";
import { SearchableSelect } from "@/components/SearchableSelect";

// ── Types (frozen contract: /api/tags/normalization/*, /api/tags/aliases) ──

interface TagOption {
  id: number;
  name: string;
  count: number;
}

interface NormVariant {
  id: number;
  name: string;
  comicCount: number;
}

interface NormCluster {
  normKey: string;
  variants: NormVariant[];
  totalComics: number;
}

interface NormPreviewResponse {
  clusters: NormCluster[];
  total: number;
  page: number;
  pageSize: number;
}

interface NormOperation {
  id: number;
  kind: string;
  fromNames: string[];
  toTagId: number;
  toTagName: string;
  comicCount: number;
  undone: boolean;
  createdAt: string;
}

interface NormOperationsResponse {
  list: NormOperation[];
  total: number;
  page: number;
  pageSize: number;
}

interface AliasItem {
  alias: string;
  tagId: number;
  tagName: string;
  createdAt: string;
}

interface AliasListResponse {
  list: AliasItem[];
}

interface IgnoreItem {
  normKey: string;
  createdAt: string;
}

interface IgnoreListResponse {
  list: IgnoreItem[];
}

interface VocabItem {
  tagId: number;
  name: string;
  comicCount: number;
  createdAt: string;
}

interface VocabListResponse {
  list: VocabItem[];
}

type NormResult<T> = { ok: true; data: T } | { ok: false; error: string };

const CLUSTER_PAGE_SIZE = 20;
const OPERATION_PAGE_SIZE = 20;

// ── API helpers (same style as tag-manager page: apiPath + detail/error extraction) ──

async function normRequest<T>(path: string, init?: RequestInit): Promise<NormResult<T>> {
  try {
    const res = await fetch(apiPath(path), init);
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      const message = (data && (data.error || data.detail)) || `HTTP ${res.status}`;
      return { ok: false, error: String(message) };
    }
    return { ok: true, data: (await res.json()) as T };
  } catch (e) {
    return { ok: false, error: String(e) };
  }
}

function postJson(body: unknown): RequestInit {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

function defaultTargetId(cluster: NormCluster): number {
  const sorted = [...cluster.variants].sort((a, b) => b.comicCount - a.comicCount);
  return sorted[0]?.id ?? 0;
}

type NormTab = "clusters" | "manual" | "operations" | "aliases" | "vocab";

function SimplePager({
  page,
  totalPages,
  disabled,
  onChange,
  prevTitle,
  nextTitle,
}: {
  page: number;
  totalPages: number;
  disabled: boolean;
  onChange: (page: number) => void;
  prevTitle?: string;
  nextTitle?: string;
}) {
  return (
    <div className="flex items-center justify-end gap-1.5 pt-2 text-xs text-muted">
      <button
        onClick={() => onChange(page - 1)}
        disabled={disabled || page <= 1}
        className="flex h-6 w-6 items-center justify-center rounded-md border border-border/50 transition-colors hover:text-foreground disabled:pointer-events-none disabled:opacity-30"
        title={prevTitle}
      >
        <ChevronLeft className="h-3 w-3" />
      </button>
      <span className="tabular-nums">
        {page} / {totalPages}
      </span>
      <button
        onClick={() => onChange(page + 1)}
        disabled={disabled || page >= totalPages}
        className="flex h-6 w-6 items-center justify-center rounded-md border border-border/50 transition-colors hover:text-foreground disabled:pointer-events-none disabled:opacity-30"
        title={nextTitle}
      >
        <ChevronRight className="h-3 w-3" />
      </button>
    </div>
  );
}

export function TagNormalizationPanel({
  tags,
  onDataChanged,
}: {
  tags: TagOption[];
  onDataChanged: () => void;
}) {
  const t = useTranslation();
  const n = t.tagManager?.normalization;
  const { success: toastSuccess, error: toastError } = useToast();

  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<NormTab>("clusters");
  const [initialLoading, setInitialLoading] = useState(false);

  // Candidate clusters
  const [clusters, setClusters] = useState<NormCluster[]>([]);
  const [clustersTotal, setClustersTotal] = useState(0);
  const [clusterPage, setClusterPage] = useState(1);
  const [clustersLoading, setClustersLoading] = useState(false);
  const [selectedTargets, setSelectedTargets] = useState<Record<string, number>>({});
  const [busyClusterKey, setBusyClusterKey] = useState<string | null>(null);

  // Manual merge
  const [sourceName, setSourceName] = useState("");
  const [manualTargetId, setManualTargetId] = useState(0);
  const [manualBusy, setManualBusy] = useState(false);

  // Recent operations
  const [operations, setOperations] = useState<NormOperation[]>([]);
  const [opsTotal, setOpsTotal] = useState(0);
  const [opsPage, setOpsPage] = useState(1);
  const [opsLoading, setOpsLoading] = useState(false);
  const [undoingId, setUndoingId] = useState<number | null>(null);

  // Aliases & ignores
  const [aliases, setAliases] = useState<AliasItem[]>([]);
  const [ignores, setIgnores] = useState<IgnoreItem[]>([]);
  const [deletingAlias, setDeletingAlias] = useState<string | null>(null);
  const [unignoringKey, setUnignoringKey] = useState<string | null>(null);

  // 目标词表
  const [vocab, setVocab] = useState<VocabItem[]>([]);
  const [vocabBusy, setVocabBusy] = useState(false);
  const [vocabAddId, setVocabAddId] = useState(0);
  const [vocabTopN, setVocabTopN] = useState(60);

  const loadClusters = useCallback(
    async (page: number, silent = false): Promise<boolean> => {
      if (!silent) setClustersLoading(true);
      const r = await normRequest<NormPreviewResponse>(
        `/api/tags/normalization/preview?page=${page}&pageSize=${CLUSTER_PAGE_SIZE}`
      );
      if (!silent) setClustersLoading(false);
      if (!r.ok) return false;
      setClusters(r.data.clusters || []);
      setClustersTotal(r.data.total || 0);
      setClusterPage(r.data.page || page);
      return true;
    },
    []
  );

  const loadOperations = useCallback(
    async (page: number, silent = false): Promise<boolean> => {
      if (!silent) setOpsLoading(true);
      const r = await normRequest<NormOperationsResponse>(
        `/api/tags/normalization/operations?page=${page}&pageSize=${OPERATION_PAGE_SIZE}`
      );
      if (!silent) setOpsLoading(false);
      if (!r.ok) return false;
      setOperations(r.data.list || []);
      setOpsTotal(r.data.total || 0);
      setOpsPage(r.data.page || page);
      return true;
    },
    []
  );

  const loadAliases = useCallback(async (): Promise<boolean> => {
    const r = await normRequest<AliasListResponse>("/api/tags/aliases");
    if (!r.ok) return false;
    setAliases(r.data.list || []);
    return true;
  }, []);

  const loadIgnores = useCallback(async (): Promise<boolean> => {
    const r = await normRequest<IgnoreListResponse>("/api/tags/normalization/ignores");
    if (!r.ok) return false;
    setIgnores(r.data.list || []);
    return true;
  }, []);

  const loadVocab = useCallback(async (): Promise<boolean> => {
    const r = await normRequest<VocabListResponse>("/api/tags/vocabulary");
    if (!r.ok) return false;
    setVocab(r.data.list || []);
    return true;
  }, []);

  const refreshAll = useCallback(async () => {
    setInitialLoading(true);
    const results = await Promise.all([
      loadClusters(clusterPage, true),
      loadOperations(opsPage, true),
      loadAliases(),
      loadIgnores(),
      loadVocab(),
    ]);
    setInitialLoading(false);
    if (n && results.some((ok) => !ok)) {
      toastError(n.loadFailed);
    }
  }, [loadClusters, loadOperations, loadAliases, loadIgnores, loadVocab, clusterPage, opsPage, n, toastError]);

  const handleToggle = () => {
    const next = !open;
    setOpen(next);
    if (next) refreshAll();
  };

  const targetFor = (cluster: NormCluster): number =>
    selectedTargets[cluster.normKey] ?? defaultTargetId(cluster);

  const removeCluster = (normKey: string) => {
    setClusters((prev) => prev.filter((c) => c.normKey !== normKey));
    setClustersTotal((prev) => Math.max(0, prev - 1));
  };

  const handleMergeCluster = async (cluster: NormCluster) => {
    const targetId = targetFor(cluster);
    const sourceIds = cluster.variants.filter((v) => v.id !== targetId).map((v) => v.id);
    if (!targetId || sourceIds.length === 0) return;
    setBusyClusterKey(cluster.normKey);
    const r = await normRequest<{ ok: boolean; comicCount: number }>(
      "/api/tags/normalization/apply",
      postJson({ targetTagId: targetId, sourceTagIds: sourceIds })
    );
    setBusyClusterKey(null);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(`${n.mergeSuccess} ${r.data.comicCount} ${n.comicsUnit}`);
    removeCluster(cluster.normKey);
    onDataChanged();
  };

  const handleIgnoreCluster = async (cluster: NormCluster) => {
    setBusyClusterKey(cluster.normKey);
    const r = await normRequest<{ ok: boolean }>(
      "/api/tags/normalization/ignore",
      postJson({ normKey: cluster.normKey })
    );
    setBusyClusterKey(null);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(n.ignoreSuccess);
    removeCluster(cluster.normKey);
    await loadIgnores();
  };

  const handleManualMerge = async () => {
    const name = sourceName.trim();
    if (!manualTargetId) return;
    const source = tags.find((tg) => tg.name === name);
    if (source && source.id === manualTargetId) {
      if (n) toastError(n.manualSameTag);
      return;
    }
    setManualBusy(true);
    // 源标签尚不存在 → 保存为预设别名(主从映射):下次标签补全/下载写入该名时
    // 由写入口别名解析自动归并到目标标签
    if (!source) {
      const r = await normRequest<{ ok: boolean }>(
        "/api/tags/aliases",
        postJson({ alias: name, tagId: manualTargetId })
      );
      setManualBusy(false);
      if (!r.ok) {
        toastError(r.error);
        return;
      }
      if (n) toastSuccess(n.presetAliasSaved);
      setSourceName("");
      await loadAliases();
      return;
    }
    const r = await normRequest<{ ok: boolean; comicCount: number }>(
      "/api/tags/normalization/apply",
      postJson({ targetTagId: manualTargetId, sourceTagIds: [source.id] })
    );
    setManualBusy(false);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(`${n.manualMergeSuccess} ${r.data.comicCount} ${n.comicsUnit}`);
    setSourceName("");
    setManualTargetId(0);
    onDataChanged();
  };

  const handleUndo = async (operationId: number) => {
    setUndoingId(operationId);
    const r = await normRequest<{ ok: boolean }>(
      "/api/tags/normalization/undo",
      postJson({ operationId })
    );
    setUndoingId(null);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(n.undoSuccess);
    await loadOperations(opsPage);
    onDataChanged();
  };

  const handleDeleteAlias = async (alias: string) => {
    setDeletingAlias(alias);
    const r = await normRequest<{ ok: boolean }>(
      `/api/tags/aliases?alias=${encodeURIComponent(alias)}`,
      { method: "DELETE" }
    );
    setDeletingAlias(null);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(n.aliasDeleteSuccess);
    setAliases((prev) => prev.filter((a) => a.alias !== alias));
  };

  const handleUnignore = async (normKey: string) => {
    setUnignoringKey(normKey);
    const r = await normRequest<{ ok: boolean }>(
      "/api/tags/normalization/unignore",
      postJson({ normKey })
    );
    setUnignoringKey(null);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(n.unignoreSuccess);
    setIgnores((prev) => prev.filter((i) => i.normKey !== normKey));
    await loadClusters(clusterPage, true);
  };

  // ── 目标词表 ──

  const handleAddVocab = async () => {
    if (!vocabAddId) return;
    setVocabBusy(true);
    const r = await normRequest<{ ok: boolean }>("/api/tags/vocabulary", postJson({ add: [vocabAddId] }));
    setVocabBusy(false);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(n.vocabAddSuccess);
    setVocabAddId(0);
    await loadVocab();
  };

  const handleRemoveVocab = async (tagId: number) => {
    setVocabBusy(true);
    const r = await normRequest<{ ok: boolean }>("/api/tags/vocabulary", postJson({ remove: [tagId] }));
    setVocabBusy(false);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(n.vocabRemoveSuccess);
    setVocab((prev) => prev.filter((v) => v.tagId !== tagId));
  };

  const handleInitVocab = async () => {
    const top = [...tags]
      .sort((a, b) => b.count - a.count)
      .slice(0, vocabTopN)
      .map((tg) => tg.id);
    if (top.length === 0) return;
    setVocabBusy(true);
    const r = await normRequest<{ ok: boolean }>("/api/tags/vocabulary", postJson({ add: top }));
    setVocabBusy(false);
    if (!r.ok) {
      toastError(r.error);
      return;
    }
    if (n) toastSuccess(n.vocabInitialized);
    await loadVocab();
  };

  const clusterTotalPages = Math.max(1, Math.ceil(clustersTotal / CLUSTER_PAGE_SIZE));
  const opsTotalPages = Math.max(1, Math.ceil(opsTotal / OPERATION_PAGE_SIZE));
  const sourceNameTrimmed = sourceName.trim();
  const targetCandidates = tags.filter((tg) => tg.name !== sourceNameTrimmed);

  const tabs: { key: NormTab; label?: string }[] = [
    { key: "clusters", label: n?.clustersTab },
    { key: "manual", label: n?.manualTab },
    { key: "operations", label: n?.operationsTab },
    { key: "aliases", label: n?.aliasesTab },
    { key: "vocab", label: n?.vocabTab },
  ];

  return (
    <div className="mb-4 rounded-2xl border border-border/50 bg-card">
      {/* Collapsible card header */}
      <button
        onClick={handleToggle}
        aria-expanded={open}
        className="flex w-full items-center gap-3 p-4 text-left transition-colors hover:bg-card-hover"
      >
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-accent/10">
          <Merge className="h-4 w-4 text-accent" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold text-foreground">{n?.title}</h3>
          <p className="truncate text-xs text-muted">{n?.subtitle}</p>
        </div>
        <ChevronDown
          className={`h-4 w-4 shrink-0 text-muted transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>

      {open && (
        <div className="space-y-3 border-t border-border/40 p-4">
          {/* Section tabs */}
          <div className="flex flex-wrap gap-1">
            {tabs.map((item) => (
              <button
                key={item.key}
                onClick={() => setTab(item.key)}
                className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                  tab === item.key
                    ? "bg-accent/15 text-accent ring-1 ring-accent/30"
                    : "bg-background text-muted hover:text-foreground"
                }`}
              >
                {item.label}
              </button>
            ))}
          </div>

          {initialLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-4 w-4 animate-spin text-muted" />
            </div>
          ) : tab === "clusters" ? (
            /* ── Candidate clusters ── */
            <div className="space-y-2">
              {clusters.length === 0 ? (
                <div className="py-6 text-center text-xs text-muted">{n?.clustersEmpty}</div>
              ) : (
                clusters.map((cluster) => {
                  const targetId = targetFor(cluster);
                  const sortedVariants = [...cluster.variants].sort(
                    (a, b) => b.comicCount - a.comicCount
                  );
                  const busy = busyClusterKey === cluster.normKey;
                  return (
                    <div
                      key={cluster.normKey}
                      className="space-y-2 rounded-xl border border-border/40 bg-background p-3"
                    >
                      <div className="flex flex-wrap items-center gap-1.5">
                        {cluster.variants.map((v) => (
                          <span
                            key={v.id}
                            className={`rounded-full px-2 py-0.5 text-xs font-medium ${
                              v.id === targetId
                                ? "bg-accent/15 text-accent"
                                : "bg-muted/10 text-foreground"
                            }`}
                          >
                            {v.name} <span className="text-[10px] text-muted">{v.comicCount}</span>
                          </span>
                        ))}
                        <span className="ml-auto text-[10px] text-muted" title={n?.totalComicsLabel}>
                          {cluster.totalComics} {n?.comicsUnit}
                        </span>
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        <select
                          value={targetId}
                          onChange={(e) =>
                            setSelectedTargets((prev) => ({
                              ...prev,
                              [cluster.normKey]: Number(e.target.value),
                            }))
                          }
                          title={n?.mergeTargetLabel}
                          className="min-w-0 flex-1 rounded-lg border border-border/50 bg-card px-2 py-1.5 text-xs text-foreground outline-none focus:border-accent/50"
                        >
                          {sortedVariants.map((v) => (
                            <option key={v.id} value={v.id}>
                              {v.name} ({v.comicCount})
                            </option>
                          ))}
                        </select>
                        <div className="flex items-center gap-1.5">
                          <button
                            onClick={() => handleMergeCluster(cluster)}
                            disabled={busy}
                            className="flex items-center gap-1 rounded-lg bg-accent px-2.5 py-1.5 text-xs font-medium text-white transition-opacity hover:bg-accent/90 disabled:opacity-50"
                          >
                            {busy ? (
                              <Loader2 className="h-3 w-3 animate-spin" />
                            ) : (
                              <Merge className="h-3 w-3" />
                            )}
                            {n?.merge}
                          </button>
                          <button
                            onClick={() => handleIgnoreCluster(cluster)}
                            disabled={busy}
                            className="flex items-center gap-1 rounded-lg border border-border/50 px-2.5 py-1.5 text-xs font-medium text-muted transition-colors hover:text-foreground disabled:opacity-50"
                          >
                            <Ban className="h-3 w-3" />
                            {n?.ignore}
                          </button>
                        </div>
                      </div>
                    </div>
                  );
                })
              )}
              <SimplePager
                page={clusterPage}
                totalPages={clusterTotalPages}
                disabled={clustersLoading}
                onChange={(p) => loadClusters(p)}
                prevTitle={t.home?.prevPage}
                nextTitle={t.home?.nextPage}
              />
            </div>
          ) : tab === "manual" ? (
            /* ── Manual merge ── */
            <div className="space-y-2">
              <p className="text-xs text-muted">{n?.manualDesc}</p>
              <div className="grid gap-2 sm:grid-cols-2">
                <div className="space-y-1">
                  <label className="text-xs font-medium text-muted">{n?.sourceTagLabel}</label>
                  <input
                    list="tag-norm-source-options"
                    value={sourceName}
                    onChange={(e) => setSourceName(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") handleManualMerge();
                    }}
                    placeholder={n?.sourceTagPlaceholder}
                    className="w-full rounded-lg border border-border/50 bg-background px-3 py-2 text-sm text-foreground placeholder-muted/50 outline-none focus:border-accent/50"
                  />
                  <datalist id="tag-norm-source-options">
                    {tags.map((tg) => (
                      <option key={tg.id} value={tg.name} />
                    ))}
                  </datalist>
                </div>
                <div className="space-y-1">
                  <label className="text-xs font-medium text-muted">{n?.targetTagLabel}</label>
                  <SearchableSelect
                    value={manualTargetId}
                    onChange={setManualTargetId}
                    options={targetCandidates.map((tg) => ({
                      value: tg.id,
                      label: tg.name,
                      hint: String(tg.count),
                    }))}
                    placeholder={n?.targetTagPlaceholder}
                    searchPlaceholder={n?.searchPlaceholder}
                    noMatchText={t.common?.noSearchResults}
                  />
                </div>
              </div>
              <div className="flex justify-end">
                <button
                  onClick={handleManualMerge}
                  disabled={manualBusy || !sourceNameTrimmed || !manualTargetId}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-xs font-medium text-white transition-opacity disabled:opacity-50"
                >
                  {manualBusy ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Merge className="h-3.5 w-3.5" />
                  )}
                  {n?.applyManual}
                </button>
              </div>
            </div>
          ) : tab === "operations" ? (
            /* ── Recent operations ── */
            <div className="space-y-2">
              {operations.length === 0 ? (
                <div className="py-6 text-center text-xs text-muted">{n?.operationsEmpty}</div>
              ) : (
                operations.map((op) => (
                  <div
                    key={op.id}
                    className="flex flex-wrap items-center gap-2 rounded-xl border border-border/40 bg-background px-3 py-2 text-xs"
                  >
                    <span className="rounded bg-accent/10 px-1.5 py-0.5 text-[10px] font-medium text-accent">
                      {op.kind === "merge" ? (n?.kindMerge ?? op.kind) : op.kind}
                    </span>
                    <span className="min-w-0 text-foreground">
                      <span className="font-medium">{op.fromNames.join(", ")}</span>
                      <span className="mx-1 text-muted">→</span>
                      <span className="font-medium text-accent">{op.toTagName}</span>
                    </span>
                    <span className="text-muted">
                      {op.comicCount} {n?.comicsUnit}
                    </span>
                    <span className="ml-auto text-[10px] text-muted">
                      {formatDateTime(op.createdAt)}
                    </span>
                    {op.undone ? (
                      <span className="rounded bg-muted/10 px-1.5 py-0.5 text-[10px] text-muted">
                        {n?.undoneBadge}
                      </span>
                    ) : (
                      <button
                        onClick={() => handleUndo(op.id)}
                        disabled={undoingId === op.id}
                        className="flex items-center gap-1 rounded-lg border border-border/50 px-2 py-1 font-medium text-muted transition-colors hover:text-foreground disabled:opacity-50"
                      >
                        {undoingId === op.id ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <RotateCcw className="h-3 w-3" />
                        )}
                        {n?.undo}
                      </button>
                    )}
                  </div>
                ))
              )}
              <SimplePager
                page={opsPage}
                totalPages={opsTotalPages}
                disabled={opsLoading}
                onChange={(p) => loadOperations(p)}
                prevTitle={t.home?.prevPage}
                nextTitle={t.home?.nextPage}
              />
            </div>
          ) : tab === "aliases" ? (
            /* ── Aliases & ignores ── */
            <div className="space-y-3">
              <div className="space-y-1">
                {aliases.length === 0 ? (
                  <div className="py-4 text-center text-xs text-muted">{n?.aliasesEmpty}</div>
                ) : (
                  aliases.map((a) => (
                    <div
                      key={a.alias}
                      className="flex items-center gap-2 rounded-lg border border-border/40 bg-background px-3 py-2 text-xs"
                    >
                      <span className="font-medium text-foreground">{a.alias}</span>
                      <span className="text-muted">→</span>
                      <span className="font-medium text-accent">{a.tagName}</span>
                      <button
                        onClick={() => handleDeleteAlias(a.alias)}
                        disabled={deletingAlias === a.alias}
                        title={n?.deleteAlias}
                        className="ml-auto rounded-md p-1 text-muted transition-colors hover:bg-red-500/10 hover:text-red-400 disabled:opacity-50"
                      >
                        {deletingAlias === a.alias ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <Trash2 className="h-3 w-3" />
                        )}
                      </button>
                    </div>
                  ))
                )}
              </div>

              <div className="space-y-1 border-t border-border/40 pt-3">
                <h4 className="text-xs font-semibold text-muted">{n?.ignoresTitle}</h4>
                {ignores.length === 0 ? (
                  <div className="py-4 text-center text-xs text-muted">{n?.ignoresEmpty}</div>
                ) : (
                  ignores.map((item) => (
                    <div
                      key={item.normKey}
                      className="flex items-center gap-2 rounded-lg border border-border/40 bg-background px-3 py-2 text-xs"
                    >
                      <span className="min-w-0 truncate font-medium text-foreground">
                        {item.normKey}
                      </span>
                      <span className="ml-auto shrink-0 text-[10px] text-muted">
                        {formatDateTime(item.createdAt)}
                      </span>
                      <button
                        onClick={() => handleUnignore(item.normKey)}
                        disabled={unignoringKey === item.normKey}
                        className="flex shrink-0 items-center gap-1 rounded-lg border border-border/50 px-2 py-1 font-medium text-muted transition-colors hover:text-foreground disabled:opacity-50"
                      >
                        {unignoringKey === item.normKey ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <RotateCcw className="h-3 w-3" />
                        )}
                        {n?.unignore}
                      </button>
                    </div>
                  ))
                )}
              </div>
            </div>
          ) : (
            /* ── 目标词表 ── */
            <div className="space-y-3">
              <p className="text-xs text-muted">{n?.vocabDesc}</p>
              <div className="flex flex-wrap items-center gap-2">
                <div className="min-w-[12rem] flex-1">
                  <SearchableSelect
                    value={vocabAddId}
                    onChange={setVocabAddId}
                    options={tags
                      .filter((tg) => !vocab.some((v) => v.tagId === tg.id))
                      .map((tg) => ({ value: tg.id, label: tg.name, hint: String(tg.count) }))}
                    placeholder={n?.vocabAddPlaceholder}
                    searchPlaceholder={n?.searchPlaceholder}
                    noMatchText={t.common?.noSearchResults}
                  />
                </div>
                <button
                  onClick={handleAddVocab}
                  disabled={vocabBusy || !vocabAddId}
                  className="flex items-center gap-1 rounded-lg bg-accent px-2.5 py-1.5 text-xs font-medium text-white transition-opacity hover:bg-accent/90 disabled:opacity-50"
                >
                  {vocabBusy ? <Loader2 className="h-3 w-3 animate-spin" /> : <ListPlus className="h-3 w-3" />}
                  {n?.vocabAddSuccess}
                </button>
                <span className="ml-auto flex items-center gap-1.5">
                  <input
                    type="number"
                    min={10}
                    max={200}
                    value={vocabTopN}
                    onChange={(e) => setVocabTopN(Math.max(10, Math.min(200, parseInt(e.target.value) || 60)))}
                    className="h-7 w-16 rounded-lg border border-border/50 bg-background px-2 text-xs text-foreground outline-none focus:border-accent/50"
                  />
                  <button
                    onClick={handleInitVocab}
                    disabled={vocabBusy}
                    className="flex items-center gap-1 rounded-lg border border-accent/40 bg-accent/10 px-2.5 py-1.5 text-xs font-medium text-accent transition-colors hover:bg-accent/20 disabled:opacity-50"
                  >
                    {n?.vocabInitTopN}
                  </button>
                </span>
              </div>
              {vocab.length === 0 ? (
                <div className="py-6 text-center text-xs text-muted">{n?.vocabEmpty}</div>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {vocab.map((v) => (
                    <span
                      key={v.tagId}
                      className="flex items-center gap-1 rounded-full bg-accent/10 px-2 py-1 text-xs font-medium text-accent"
                    >
                      {v.name}
                      <span className="text-[10px] text-muted">{v.comicCount}</span>
                      <button
                        onClick={() => handleRemoveVocab(v.tagId)}
                        disabled={vocabBusy}
                        title={n?.vocabRemoveSuccess}
                        className="rounded-full px-0.5 text-muted transition-colors hover:text-red-400 disabled:opacity-50"
                      >
                        ×
                      </button>
                    </span>
                  ))}
                </div>
              )}
              <div className="text-[10px] text-muted">
                {vocab.length} {n?.vocabCountUnit}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
