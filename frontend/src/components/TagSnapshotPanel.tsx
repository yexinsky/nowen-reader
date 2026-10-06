"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Camera, Loader2, RotateCcw, Trash2 } from "lucide-react";
import {
  listSnapshots,
  createSnapshot,
  restoreSnapshot,
  deleteSnapshot,
  parseSnapshotSummary,
  type SnapshotItem,
} from "@/api/snapshots";
import { humanBytes } from "@/api/admin";

function formatSnapshotTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString("zh-CN", { hour12: false });
}

// ============================================================
// 快照管理（标签+分类域）
//
// AI 批量写操作（批量标签/批量分类/情景分配/AI 归并）落库前会自动
// 创建快照（kind=auto，滚动保留最近 10 份）；此处支持手动快照的
// 创建、恢复与删除。恢复为整域替换，恢复前自动保存当前状态。
// ============================================================

function summaryText(item: SnapshotItem): string {
  const s = parseSnapshotSummary(item);
  const parts = [
    `标签 ${s.tags}`,
    `书目关联 ${s.comicTags}`,
    `别名 ${s.aliases}`,
    `情景 ${s.scenarios}`,
    `分类 ${s.categories}`,
    `分类关联 ${s.comicCategories}`,
  ];
  return parts.join(" · ");
}

/** 两步确认按钮：第一次点击进入待确认态，3 秒未确认自动复位 */
function ConfirmButton({
  label,
  confirmLabel,
  onConfirm,
  disabled,
  className,
  title,
}: {
  label: ReactNode;
  confirmLabel: ReactNode;
  onConfirm: () => void;
  disabled?: boolean;
  className?: string;
  title?: string;
}) {
  const [armed, setArmed] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  return (
    <button
      onClick={() => {
        if (!armed) {
          setArmed(true);
          timer.current = setTimeout(() => setArmed(false), 3000);
          return;
        }
        clearTimeout(timer.current);
        setArmed(false);
        onConfirm();
      }}
      disabled={disabled}
      title={title}
      className={className}
    >
      {armed ? confirmLabel : label}
    </button>
  );
}

export default function TagSnapshotPanel() {
  const [snapshots, setSnapshots] = useState<SnapshotItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [showNameInput, setShowNameInput] = useState(false);
  const [restoringId, setRestoringId] = useState<number | null>(null);
  const [deletingId, setDeletingId] = useState<number | null>(null);
  const [message, setMessage] = useState<{ text: string; ok: boolean } | null>(null);
  const messageTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  const flash = useCallback((text: string, ok: boolean) => {
    setMessage({ text, ok });
    if (messageTimer.current) clearTimeout(messageTimer.current);
    messageTimer.current = setTimeout(() => setMessage(null), 4000);
  }, []);
  useEffect(() => () => clearTimeout(messageTimer.current), []);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const data = await listSnapshots();
      setSnapshots(data.list || []);
    } catch (e) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const handleCreate = async () => {
    setCreating(true);
    try {
      const item = await createSnapshot(newName.trim() || undefined);
      flash(`快照已创建：${item.snapshot.name}`, true);
      setNewName("");
      setShowNameInput(false);
      await load();
    } catch (e) {
      flash(`创建失败：${e instanceof Error ? e.message : e}`, false);
    } finally {
      setCreating(false);
    }
  };

  const handleRestore = async (item: SnapshotItem) => {
    setRestoringId(item.id);
    try {
      const r = await restoreSnapshot(item.id);
      const restored = r.restored;
      flash(
        `已恢复「${item.name}」：标签 ${restored.tags}、书目关联 ${restored.comicTags}、分类 ${restored.categories}（恢复前状态已自动存为快照 #${restored.safetySnapshotId}）`,
        true
      );
      await load();
    } catch (e) {
      flash(`恢复失败：${e instanceof Error ? e.message : e}`, false);
    } finally {
      setRestoringId(null);
    }
  };

  const handleDelete = async (item: SnapshotItem) => {
    setDeletingId(item.id);
    try {
      await deleteSnapshot(item.id);
      setSnapshots((prev) => prev.filter((s) => s.id !== item.id));
      flash(`已删除快照「${item.name}」`, true);
    } catch (e) {
      flash(`删除失败：${e instanceof Error ? e.message : e}`, false);
      await load();
    } finally {
      setDeletingId(null);
    }
  };

  return (
    <section className="mt-6 rounded-lg border border-border bg-card">
      <div className="border-b border-border px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <Camera className="h-4 w-4 text-emerald-400" />
          <h2 className="text-sm font-semibold">快照管理</h2>
          <span className="text-xs text-muted">
            标签/分类/情景/别名的可恢复备份；AI 批量写标签前会自动创建（保留最近 10 份）
          </span>
          <div className="ml-auto flex items-center gap-1.5">
            {showNameInput ? (
              <>
                <input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !creating) handleCreate();
                  }}
                  placeholder="快照名称（可留空）"
                  className="h-7 w-44 rounded border border-border bg-background px-2 text-xs text-foreground placeholder-muted/50 outline-none focus:border-emerald-500/50"
                />
                <button
                  onClick={handleCreate}
                  disabled={creating}
                  className="flex h-7 items-center gap-1 rounded border border-emerald-500/40 bg-emerald-500/10 px-2.5 text-xs font-medium text-emerald-400 hover:bg-emerald-500/20 disabled:opacity-50"
                >
                  {creating ? <Loader2 className="h-3 w-3 animate-spin" /> : <Camera className="h-3 w-3" />}
                  创建
                </button>
                <button
                  onClick={() => {
                    setShowNameInput(false);
                    setNewName("");
                  }}
                  className="h-7 rounded px-2 text-xs text-muted hover:text-foreground"
                >
                  取消
                </button>
              </>
            ) : (
              <button
                onClick={() => setShowNameInput(true)}
                className="flex h-7 items-center gap-1 rounded border border-emerald-500/40 bg-emerald-500/10 px-2.5 text-xs font-medium text-emerald-400 hover:bg-emerald-500/20"
              >
                <Camera className="h-3 w-3" />
                创建快照
              </button>
            )}
            <button
              onClick={load}
              disabled={loading}
              title="刷新"
              className="flex h-7 w-7 items-center justify-center rounded border border-border text-muted hover:text-foreground disabled:opacity-50"
            >
              {loading ? <Loader2 className="h-3 w-3 animate-spin" /> : <RotateCcw className="h-3 w-3" />}
            </button>
          </div>
        </div>
      </div>

      <div className="p-4">
        {message && (
          <div
            className={`mb-3 rounded border p-2 text-xs ${
              message.ok
                ? "border-emerald-500/40 bg-emerald-500/5 text-emerald-400"
                : "border-red-500/40 bg-red-500/5 text-red-400"
            }`}
          >
            {message.text}
          </div>
        )}
        {error && (
          <div className="mb-3 rounded border border-red-500/40 bg-red-500/5 p-2 text-xs text-red-400">
            {error}
          </div>
        )}

        {loading ? (
          <div className="flex items-center justify-center py-6 text-xs text-muted">
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            加载中...
          </div>
        ) : snapshots.length === 0 ? (
          <div className="py-6 text-center text-xs text-muted">
            暂无快照。可在运行 AI 批量操作前手动创建，或直接依赖 AI 操作前的自动快照。
          </div>
        ) : (
          <div className="space-y-1.5">
            {snapshots.map((item) => {
              const busy = restoringId === item.id || deletingId === item.id;
              return (
                <div
                  key={item.id}
                  className="flex flex-wrap items-center gap-2 rounded-lg border border-border/60 bg-background px-3 py-2 text-xs"
                >
                  <span
                    className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${
                      item.kind === "auto"
                        ? "bg-blue-500/10 text-blue-400"
                        : "bg-emerald-500/10 text-emerald-400"
                    }`}
                  >
                    {item.kind === "auto" ? "自动" : "手动"}
                  </span>
                  <span className="min-w-0 max-w-[16rem] truncate font-medium text-foreground" title={item.name}>
                    {item.name}
                  </span>
                  <span className="min-w-0 truncate text-muted" title={summaryText(item)}>
                    {summaryText(item)}
                  </span>
                  <span className="shrink-0 text-[10px] text-muted">
                    {humanBytes(item.sizeBytes)} · {formatSnapshotTime(item.createdAt)}
                  </span>
                  <div className="ml-auto flex shrink-0 items-center gap-1.5">
                    <ConfirmButton
                      label="恢复"
                      confirmLabel="确认恢复？"
                      disabled={busy}
                      onConfirm={() => handleRestore(item)}
                      title="恢复为整域替换：当前标签/分类/情景/别名将被快照内容覆盖；恢复前会自动保存当前状态"
                      className="rounded border border-border px-2 py-1 font-medium text-muted transition-colors hover:border-amber-500/40 hover:text-amber-400 disabled:opacity-50"
                    />
                    <ConfirmButton
                      label={<Trash2 className="h-3 w-3" />}
                      confirmLabel={<span className="px-1">确认？</span>}
                      disabled={busy}
                      onConfirm={() => handleDelete(item)}
                      title="删除快照"
                      className="rounded border border-border p-1 text-muted transition-colors hover:border-red-500/40 hover:text-red-400 disabled:opacity-50"
                    />
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}
