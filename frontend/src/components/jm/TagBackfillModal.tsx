"use client";

/**
 * 标签补全弹窗(原「书库补标签」独立页改造,PRD docs/PRD_JM_SOURCE.md M13)
 * 接口:MOBILE_API.md §8 backfill 三端点(candidates / match / apply)
 *
 * - 入口在书库页(/books)工具条;候选按当前所选书库过滤(libraryIds,空 = 全部可管理书库)
 * - 选择流:逐本「匹配」→ JM 结果卡片(含封面)人工挑选 → 「应用」(后端按 aid 拉详情写标签)
 * - 标签只能来自 JM 匹配结果,不支持自定义添加(无添加标签入口)
 * - 进度即状态:应用成功的漫画下次打开自动退出候选列表
 * - 未登录在线源时弹窗内提示,不做页面级跳转
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import {
  BookOpen,
  Check,
  ChevronDown,
  ChevronUp,
  CircleAlert,
  Loader2,
  Search,
  SkipForward,
  Tags,
  Undo2,
  X,
} from "lucide-react";
import { useToast } from "@/components/Toast";
import { isJmApiError, jmBackfillApply, jmBackfillCandidates, jmBackfillMatch, resolveJmUrl } from "@/lib/jm/client";
import { useJmSession } from "@/lib/jm/session";
import type { JmBackfillCandidate, JmBackfillMatch } from "@/lib/jm/types";

type RowStatus =
  | "idle" // 待匹配
  | "invalid" // 搜索词清洗后为空,需手填
  | "matching"
  | "matched"
  | "noresult" // 上游搜索无结果
  | "noTags" // 选中结果的上游详情没有标签
  | "error"
  | "applied"
  | "skipped";

interface BackfillRow {
  candidate: JmBackfillCandidate;
  keyword: string;
  status: RowStatus;
  matches: JmBackfillMatch[];
  selectedAid: string | null;
  expanded: boolean; // 备选列表展开
  applying: boolean;
  appliedTags: string[];
  errorMsg: string;
}

type Filter = "todo" | "done" | "all";

export default function TagBackfillModal({
  open,
  onClose,
  libraryIds,
  onChanged,
}: {
  open: boolean;
  onClose: () => void;
  /** 当前所选书库(空数组/缺省 = 全部可管理书库) */
  libraryIds?: string[];
  /** 有标签写入后通知外层刷新(书库页重拉列表) */
  onChanged?: () => void;
}) {
  const { isLoggedIn } = useJmSession();

  // Esc 关闭
  useEffect(() => {
    if (!open) return;
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleEsc);
    return () => document.removeEventListener("keydown", handleEsc);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm sm:p-6"
      role="dialog"
      aria-modal="true"
      aria-label="标签补全"
    >
      <div className="absolute inset-0" onClick={onClose} />
      <div className="relative flex max-h-[86vh] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-border/60 bg-card shadow-2xl">
        {/* 标题栏 */}
        <div className="flex shrink-0 items-center gap-3 border-b border-border/40 px-5 py-4">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent/10">
            <Tags className="h-4 w-4 text-accent" />
          </span>
          <div className="min-w-0 flex-1">
            <h3 className="text-sm font-semibold text-foreground">标签补全</h3>
            <p className="mt-0.5 truncate text-xs text-muted">
              用 JM 在线源给无标签的旧书匹配补标;挑选结果后写入,不支持自定义标签
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1 text-muted transition-colors hover:bg-card-hover hover:text-foreground"
            aria-label="关闭"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* 内容 */}
        {isLoggedIn ? (
          <BackfillBody libraryIds={libraryIds} onChanged={onChanged} />
        ) : (
          <div className="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
            <BookOpen className="h-8 w-8 text-muted/50" />
            <p className="text-sm text-foreground">使用前请先登录在线漫画源</p>
            <Link
              href="/jm/login"
              className="rounded-lg bg-accent px-4 py-2 text-xs font-medium text-white transition-opacity hover:opacity-90"
              onClick={onClose}
            >
              前往登录
            </Link>
          </div>
        )}
      </div>
    </div>
  );
}

function BackfillBody({
  libraryIds,
  onChanged,
}: {
  libraryIds?: string[];
  onChanged?: () => void;
}) {
  const toast = useToast();

  const [rows, setRows] = useState<BackfillRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [filter, setFilter] = useState<Filter>("todo");

  // 行状态的同步镜像:跨 await 更新与 setState 同帧生效
  const rowsRef = useRef<BackfillRow[]>([]);
  const libKey = libraryIds?.join(",") ?? "";

  const updateRow = useCallback(
    (id: string, patch: Partial<BackfillRow> | ((r: BackfillRow) => Partial<BackfillRow>)) => {
      const apply = (r: BackfillRow): BackfillRow => ({
        ...r,
        ...(typeof patch === "function" ? patch(r) : patch),
      });
      rowsRef.current = rowsRef.current.map((r) => (r.candidate.id === id ? apply(r) : r));
      setRows(rowsRef.current);
    },
    [],
  );

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const ids = libKey ? libKey.split(",") : undefined;
      const data = await jmBackfillCandidates(ids);
      const next = data.list.map<BackfillRow>((c) => ({
        candidate: c,
        keyword: c.searchKeyword,
        status: c.searchKeyword ? "idle" : "invalid",
        matches: [],
        selectedAid: null,
        expanded: false,
        applying: false,
        appliedTags: [],
        errorMsg: "",
      }));
      rowsRef.current = next;
      setRows(next);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
    // libKey 变化(切换书库标签)时重拉
  }, [libKey]);

  useEffect(() => {
    load();
  }, [load]);

  /** 单行匹配:上游搜索 + 打分(不改库) */
  const matchRow = useCallback(
    async (id: string, keyword: string) => {
      const row = rowsRef.current.find((r) => r.candidate.id === id);
      if (!row) return;
      const kw = keyword.trim();
      if (!kw) {
        updateRow(id, { status: "invalid", matches: [], selectedAid: null });
        return;
      }
      updateRow(id, (r) => ({
        status: "matching",
        errorMsg: "",
        expanded: false,
      }));
      try {
        const data = await jmBackfillMatch({
          keyword: kw,
          aid: row.candidate.embeddedAid || undefined,
          title: row.candidate.title,
          author: row.candidate.author || undefined,
        });
        if (data.list.length === 0) {
          updateRow(id, { status: "noresult", matches: [], selectedAid: null });
        } else {
          updateRow(id, { status: "matched", matches: data.list, selectedAid: data.list[0].aid });
        }
      } catch (err) {
        updateRow(id, {
          status: "error",
          errorMsg: isJmApiError(err) ? err.message : "匹配失败,请稍后重试",
        });
      }
    },
    [updateRow],
  );

  /** 单行应用:后端按 aid 拉详情写标签/作者 */
  const applyRow = useCallback(
    async (id: string, aid: string) => {
      const row = rowsRef.current.find((r) => r.candidate.id === id);
      if (!row || !aid) return;
      updateRow(id, { applying: true, errorMsg: "" });
      try {
        const resp = await jmBackfillApply({ comicId: id, aid });
        if (resp.applied > 0) {
          updateRow(id, { applying: false, status: "applied", appliedTags: resp.tags });
          toast.success(`已写入 ${resp.applied} 个标签`);
          onChanged?.();
        } else {
          updateRow(id, { applying: false, status: "noTags" });
          toast.warning("该作品上游详情没有标签,未写入;可换备选或改词重搜");
        }
      } catch (err) {
        const msg = isJmApiError(err) ? err.message : "应用失败,请稍后重试";
        updateRow(id, { applying: false, status: "error", errorMsg: msg });
        toast.error(msg);
      }
    },
    [toast, updateRow, onChanged],
  );

  const counts = useMemo(() => {
    const done = rows.filter((r) => r.status === "applied" || r.status === "skipped").length;
    return { total: rows.length, done, todo: rows.length - done };
  }, [rows]);

  const visibleRows = useMemo(() => {
    if (filter === "all") return rows;
    const doneSet = (r: BackfillRow) => r.status === "applied" || r.status === "skipped";
    return rows.filter((r) => (filter === "done" ? doneSet(r) : !doneSet(r)));
  }, [rows, filter]);

  return (
    <>
      {/* 工具条:进度 + 筛选 */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/40 px-5 py-2.5">
        <p className="text-xs text-muted">
          未打标 {counts.total} 本 · 待处理 {counts.todo} · 已处理 {counts.done}
        </p>
        <div className="ml-auto flex rounded-lg border border-border p-0.5 text-xs">
          {(
            [
              ["todo", "待处理"],
              ["done", "已处理"],
              ["all", "全部"],
            ] as [Filter, string][]
          ).map(([key, label]) => (
            <button
              key={key}
              type="button"
              onClick={() => setFilter(key)}
              className={`rounded-md px-2.5 py-1 font-medium transition-colors ${
                filter === key ? "bg-accent/10 text-accent" : "text-muted hover:text-foreground"
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {/* 候选列表 */}
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {loading ? (
          <ListSkeleton />
        ) : error ? (
          <div className="flex flex-col items-center justify-center gap-3 py-14 text-center">
            <CircleAlert className="h-8 w-8 text-amber-400" />
            <p className="break-all text-sm text-muted">
              {error instanceof Error ? error.message : "加载失败,请稍后重试"}
            </p>
            <button
              type="button"
              onClick={load}
              className="rounded-lg border border-border px-4 py-1.5 text-xs font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent"
            >
              重试
            </button>
          </div>
        ) : rows.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-14 text-center">
            <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-emerald-500/10">
              <Check className="h-7 w-7 text-emerald-400" />
            </div>
            <p className="text-sm text-foreground">该范围内没有无标签的漫画</p>
            <p className="mt-1.5 text-xs text-muted/70">
              新下载的作品会按 JM 设置里的「下载自动打标」自动带上标签
            </p>
          </div>
        ) : visibleRows.length === 0 ? (
          <p className="py-10 text-center text-xs text-muted">该筛选下暂无内容</p>
        ) : (
          <ul className="space-y-2">
            {visibleRows.map((row) => (
              <BackfillRowCard
                key={row.candidate.id}
                row={row}
                onKeyword={(v) => updateRow(row.candidate.id, { keyword: v })}
                onMatch={() => matchRow(row.candidate.id, row.keyword)}
                onApply={() => {
                  const chosen =
                    row.matches.find((m) => m.aid === row.selectedAid) ?? row.matches[0];
                  if (chosen) applyRow(row.candidate.id, chosen.aid);
                }}
                onSelect={(aid) => updateRow(row.candidate.id, { selectedAid: aid, expanded: false })}
                onToggleExpand={() => updateRow(row.candidate.id, { expanded: !row.expanded })}
                onSkip={() => updateRow(row.candidate.id, { status: "skipped", expanded: false })}
                onReset={() =>
                  updateRow(row.candidate.id, {
                    status: row.keyword.trim() ? "idle" : "invalid",
                    errorMsg: "",
                  })
                }
              />
            ))}
          </ul>
        )}
      </div>

      {/* 底部说明 */}
      <div className="shrink-0 border-t border-border/40 px-5 py-2.5">
        <p className="text-[11px] leading-relaxed text-muted/70">
          应用后后端按所选作品的 aid 拉取详情写入标签,作者仅在书库记录为空时回填;写完的漫画下次打开不再出现。改词可修正搜索范围,Esc 或点击遮罩关闭。
        </p>
      </div>
    </>
  );
}

/* ── 单行卡片:匹配 → 选择流(JM 结果) → 应用 ── */

function BackfillRowCard({
  row,
  onKeyword,
  onMatch,
  onApply,
  onSelect,
  onToggleExpand,
  onSkip,
  onReset,
}: {
  row: BackfillRow;
  onKeyword: (v: string) => void;
  onMatch: () => void;
  onApply: () => void;
  onSelect: (aid: string) => void;
  onToggleExpand: () => void;
  onSkip: () => void;
  onReset: () => void;
}) {
  const { candidate, status } = row;
  const top = row.matches.find((m) => m.aid === row.selectedAid) ?? row.matches[0];

  return (
    <li
      className={`rounded-xl border p-3 transition-colors ${
        status === "applied"
          ? "border-emerald-500/30 bg-emerald-500/5"
          : status === "skipped"
            ? "border-border bg-card/50 opacity-60"
            : "border-border bg-background/40"
      }`}
    >
      {/* 行头:标题 + 搜索词 + 主操作 */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1 basis-48">
          <p className="truncate text-sm font-medium text-foreground/90" title={candidate.title}>
            {candidate.title || "无标题"}
          </p>
          {candidate.author && (
            <p className="mt-0.5 truncate text-xs text-muted/80" title={candidate.author}>
              {candidate.author}
            </p>
          )}
        </div>

        <input
          value={row.keyword}
          onChange={(e) => onKeyword(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && status !== "matching") onMatch();
          }}
          placeholder="搜索词"
          spellCheck={false}
          className="h-8 w-44 rounded-lg border border-border bg-card px-2.5 text-xs text-foreground outline-none transition-colors placeholder:text-muted/50 focus:border-accent/60"
        />

        <RowActionButton row={row} onMatch={onMatch} onReset={onReset} />
      </div>

      {/* 状态区 */}
      {status === "invalid" && (
        <p className="mt-2 flex items-center gap-1.5 text-xs text-amber-400">
          <CircleAlert className="h-3.5 w-3.5" />
          标题清洗后没有有效搜索词,请手动填写后匹配
        </p>
      )}
      {status === "noresult" && (
        <p className="mt-2 flex items-center gap-1.5 text-xs text-amber-400">
          <CircleAlert className="h-3.5 w-3.5" />
          上游无匹配结果,试试修改搜索词
        </p>
      )}
      {status === "noTags" && (
        <p className="mt-2 flex items-center gap-1.5 text-xs text-amber-400">
          <CircleAlert className="h-3.5 w-3.5" />
          选中结果的上游详情没有标签,未写入;可换备选、改词重搜或跳过
        </p>
      )}
      {status === "error" && (
        <p className="mt-2 flex items-center gap-1.5 text-xs text-red-400">
          <CircleAlert className="h-3.5 w-3.5" />
          {row.errorMsg || "操作失败,请重试"}
        </p>
      )}
      {status === "applied" && (
        <p className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-emerald-400">
          <Check className="h-3.5 w-3.5 shrink-0" />
          已写入 {row.appliedTags.length} 个标签:
          {row.appliedTags.slice(0, 8).map((t) => (
            <span key={t} className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-emerald-300">
              {t}
            </span>
          ))}
          {row.appliedTags.length > 8 && <span>+{row.appliedTags.length - 8}</span>}
        </p>
      )}
      {status === "skipped" && <p className="mt-2 text-xs text-muted">已跳过</p>}

      {/* 匹配结果(选中项 + 备选);noTags 也保留卡片,便于换备选 */}
      {top && (status === "matched" || status === "noTags") && (
        <div className="mt-2.5">
          <div className="rounded-lg border border-border/70 bg-card p-2.5">
            <div className="flex items-start gap-2.5">
              <MatchCover match={top} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <ConfidenceBadge confidence={top.confidence} />
                  {top.viaAid && (
                    <span className="shrink-0 rounded bg-accent/10 px-1.5 py-0.5 text-[11px] font-medium text-accent">
                      车号直达
                    </span>
                  )}
                  <span className="min-w-0 flex-1 truncate text-sm text-foreground" title={top.title}>
                    {top.title}
                  </span>
                  {top.author && (
                    <span className="max-w-32 truncate text-xs text-muted" title={top.author}>
                      {top.author}
                    </span>
                  )}
                </div>
                {top.tags.length > 0 && (
                  <div className="mt-1.5 flex flex-wrap items-center gap-1">
                    {top.tags.slice(0, 8).map((t) => (
                      <span
                        key={t}
                        className="rounded bg-muted/10 px-1.5 py-0.5 text-[11px] text-muted"
                      >
                        {t}
                      </span>
                    ))}
                    {top.tags.length > 8 && (
                      <span className="text-[11px] text-muted/70">+{top.tags.length - 8}</span>
                    )}
                  </div>
                )}
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  <button
                    type="button"
                    onClick={onApply}
                    disabled={row.applying}
                    className="inline-flex items-center gap-1 rounded-md bg-accent px-2.5 py-1 text-xs font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    {row.applying ? (
                      <Loader2 className="h-3 w-3 animate-spin" />
                    ) : (
                      <Check className="h-3 w-3" />
                    )}
                    应用此结果
                  </button>
                  {row.matches.length > 1 && (
                    <button
                      type="button"
                      onClick={onToggleExpand}
                      className="inline-flex items-center gap-1 rounded-md border border-border px-2.5 py-1 text-xs text-muted transition-colors hover:text-foreground"
                    >
                      换一个({row.matches.length - 1})
                      {row.expanded ? (
                        <ChevronUp className="h-3 w-3" />
                      ) : (
                        <ChevronDown className="h-3 w-3" />
                      )}
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={onSkip}
                    disabled={row.applying}
                    className="inline-flex items-center gap-1 rounded-md border border-border px-2.5 py-1 text-xs text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    <SkipForward className="h-3 w-3" />
                    跳过
                  </button>
                </div>
              </div>
            </div>
          </div>

          {/* 备选列表(选择流:点选换主结果) */}
          {row.expanded && (
            <ul className="mt-1.5 space-y-1">
              {row.matches
                .filter((m) => m.aid !== top.aid)
                .map((m) => (
                  <li
                    key={m.aid}
                    className="flex items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-2.5 py-1.5"
                  >
                    <MatchCover match={m} small />
                    <ConfidenceBadge confidence={m.confidence} />
                    <span
                      className="min-w-0 flex-1 truncate text-xs text-foreground/80"
                      title={m.title}
                    >
                      {m.title}
                    </span>
                    {m.author && (
                      <span className="max-w-28 truncate text-[11px] text-muted">{m.author}</span>
                    )}
                    <button
                      type="button"
                      onClick={() => onSelect(m.aid)}
                      disabled={row.applying}
                      className="shrink-0 rounded-md border border-border px-2 py-0.5 text-[11px] text-muted transition-colors hover:border-accent/50 hover:text-accent disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      选这个
                    </button>
                  </li>
                ))}
            </ul>
          )}
        </div>
      )}
    </li>
  );
}

/** JM 结果封面(代理路径 → 站内图片;加载失败隐藏) */
function MatchCover({ match, small }: { match: JmBackfillMatch; small?: boolean }) {
  const [failed, setFailed] = useState(false);
  const url = resolveJmUrl(match.coverUrl ?? "");
  if (!url || failed) {
    return (
      <span
        className={`flex shrink-0 items-center justify-center rounded bg-muted/10 text-muted/40 ${
          small ? "h-10 w-7" : "h-16 w-11"
        }`}
      >
        <BookOpen className={small ? "h-3 w-3" : "h-4 w-4"} />
      </span>
    );
  }
  return (
    <img
      src={url}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
      className={`shrink-0 rounded object-cover ${
        small ? "h-10 w-7" : "h-16 w-11"
      }`}
    />
  );
}

/** 行尾主操作按钮:随状态切换(匹配/重搜/恢复) */
function RowActionButton({
  row,
  onMatch,
  onReset,
}: {
  row: BackfillRow;
  onMatch: () => void;
  onReset: () => void;
}) {
  if (row.status === "applied") {
    return null;
  }
  if (row.status === "skipped") {
    return (
      <button
        type="button"
        onClick={onReset}
        className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-border px-2.5 py-1.5 text-xs font-medium text-muted transition-colors hover:text-foreground"
      >
        <Undo2 className="h-3.5 w-3.5" />
        恢复
      </button>
    );
  }
  if (row.status === "matching") {
    return (
      <span className="inline-flex shrink-0 items-center gap-1 px-1 text-xs text-muted">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        匹配中
      </span>
    );
  }
  const isRetry = row.status === "error" || row.status === "noresult" || row.status === "noTags";
  return (
    <button
      type="button"
      onClick={onMatch}
      className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent"
    >
      {isRetry ? <Undo2 className="h-3.5 w-3.5" /> : <Search className="h-3.5 w-3.5" />}
      {isRetry ? "重搜" : "匹配"}
    </button>
  );
}

function ConfidenceBadge({ confidence }: { confidence: JmBackfillMatch["confidence"] }) {
  const map = {
    high: "bg-emerald-500/10 text-emerald-400",
    medium: "bg-amber-500/10 text-amber-400",
    low: "bg-muted/10 text-muted",
  } as const;
  const label = { high: "高置信", medium: "中置信", low: "低置信" } as const;
  return (
    <span className={`shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium ${map[confidence]}`}>
      {label[confidence]}
    </span>
  );
}

/** 列表骨架屏 */
function ListSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: rows }, (_, i) => (
        <div
          key={i}
          className="flex animate-pulse items-center gap-3 rounded-xl border border-border bg-card p-3"
        >
          <div className="h-4 w-1/3 rounded bg-muted/15" />
          <div className="ml-auto h-8 w-44 rounded-lg bg-muted/15" />
          <div className="h-8 w-16 rounded-lg bg-muted/15" />
        </div>
      ))}
    </div>
  );
}
