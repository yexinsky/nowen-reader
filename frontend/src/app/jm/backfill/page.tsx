"use client";

/**
 * JM 在线源 · 书库补标签(私有扩展,PRD docs/PRD_JM_SOURCE.md M13)
 * 接口:MOBILE_API.md §8 backfill 三端点(candidates / match / apply)
 *
 * - 数据源:书库中无任何标签的漫画(comic/mixed 书库,有管理权;后端已按标题清洗出默认搜索词)
 * - 流程:预览式补标——逐行「匹配」(上游搜索 + 标题打分,不改库)→ 人工过目 → 「应用」
 *   (后端自行拉详情写标签;标签不从客户端透传)。高置信行可「应用全部高置信」批量写入
 * - 进度即状态:应用成功的漫画自动退出候选列表,刷新页面天然断点续跑
 * - 上游纪律:后端全局限速 ≥1.2s/次,前端自动循环仅额外留 400ms 缓冲,可随时停止
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Check,
  ChevronDown,
  ChevronUp,
  CircleAlert,
  Loader2,
  Play,
  Search,
  SkipForward,
  Square,
  Tags,
  Undo2,
} from "lucide-react";
import { PageContent, PageHeader } from "@/components/PageHeader";
import { JmErrorCard, JmGate } from "@/components/jm/JmGate";
import { useToast } from "@/components/Toast";
import { isJmApiError, jmBackfillApply, jmBackfillCandidates, jmBackfillMatch } from "@/lib/jm/client";
import { JmBackButton } from "@/components/jm/JmBackButton";
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
  attempts: number; // 实际搜索次数(error 自动重试上限用)
  searchedKeyword: string; // 最近一次实际搜索的词;改词后允许自动重搜
}

type Filter = "todo" | "done" | "all";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

export default function JmBackfillPage() {
  return (
    <>
      <PageHeader
        title="在线·书库补标签"
        description="用 JM 搜索给书库中无标签的旧书补标;先预览匹配结果,再确认写入"
        icon={Tags}
        actions={<JmBackButton />}
      />
      <PageContent width="management">
        <JmGate>
          <BackfillContent />
        </JmGate>
      </PageContent>
    </>
  );
}

function BackfillContent() {
  const toast = useToast();

  const [rows, setRows] = useState<BackfillRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [filter, setFilter] = useState<Filter>("todo");
  const [busy, setBusy] = useState<"match" | "apply" | null>(null);

  // 行状态的同步镜像:自动循环跨 await 读最新值,且更新与 setState 同帧生效
  const rowsRef = useRef<BackfillRow[]>([]);
  const stopRef = useRef(false);

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
      const data = await jmBackfillCandidates();
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
        attempts: 0,
        searchedKeyword: "",
      }));
      rowsRef.current = next;
      setRows(next);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  }, []);

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
        attempts: r.attempts + 1,
        searchedKeyword: kw,
      }));
      try {
        const data = await jmBackfillMatch({
          keyword: kw,
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
        } else {
          // noTags 终态:必须退出 matched,否则「应用全部高置信」会对它无限重放
          updateRow(id, { applying: false, status: "noTags" });
          toast.warning("该作品上游详情没有标签,未写入;可换备选或改词重搜");
        }
      } catch (err) {
        const msg = isJmApiError(err) ? err.message : "应用失败,请稍后重试";
        // 转入 error 终态退出 matched,避免批量应用对持续失败行无限重放
        updateRow(id, { applying: false, status: "error", errorMsg: msg });
        toast.error(msg);
      }
    },
    [toast, updateRow],
  );

  const stopAuto = useCallback(() => {
    stopRef.current = true;
  }, []);

  /** 自动匹配:逐行跑完所有待匹配行(后端已限速,这里只留小缓冲),可随时停止 */
  const runAutoMatch = useCallback(async () => {
    if (busy) return;
    setBusy("match");
    stopRef.current = false;
    // 待处理行的判定必须保证「每处理一次,行要么进入终态、要么耗尽重试额度」,
    // 否则同一行会被反复选中(实测 bug:noresult 行重搜必然复现 noresult,死循环):
    // - idle:从未搜过;
    // - 改过词的行(kw ≠ searchedKeyword):新词未搜过,视为新任务;
    // - error:同一词最多自动重试 1 次(attempts < 2);
    // - noresult 同词不自动重试(需人工改词/跳过)。
    const pending = () =>
      rowsRef.current.filter((r) => {
        const kw = r.keyword.trim();
        if (kw === "") return false;
        if (
          r.status === "applied" ||
          r.status === "skipped" ||
          r.status === "noTags" ||
          r.status === "matching"
        ) {
          return false;
        }
        return (
          r.status === "idle" ||
          kw !== r.searchedKeyword ||
          (r.status === "error" && r.attempts < 2)
        );
      });
    while (!stopRef.current) {
      const row = pending()[0];
      if (!row) break;
      await matchRow(row.candidate.id, row.keyword);
      if (stopRef.current) break;
      await sleep(400);
    }
    setBusy(null);
    if (!stopRef.current) toast.info("自动匹配完成");
  }, [busy, matchRow, toast]);

  /** 批量应用:写入全部「高置信」且未处理的行 */
  const runAutoApply = useCallback(async () => {
    if (busy) return;
    setBusy("apply");
    stopRef.current = false;
    const targets = () =>
      rowsRef.current.filter(
        (r) =>
          r.status === "matched" &&
          !r.applying &&
          (r.matches.find((m) => m.aid === r.selectedAid) ?? r.matches[0])?.confidence === "high",
      );
    while (!stopRef.current) {
      const row = targets()[0];
      if (!row) break;
      const chosen = row.matches.find((m) => m.aid === row.selectedAid) ?? row.matches[0];
      await applyRow(row.candidate.id, chosen.aid);
      if (stopRef.current) break;
      await sleep(400);
    }
    setBusy(null);
    if (!stopRef.current) toast.info("高置信批量应用完成");
  }, [busy, applyRow, toast]);

  const counts = useMemo(() => {
    const done = rows.filter((r) => r.status === "applied" || r.status === "skipped").length;
    return { total: rows.length, done, todo: rows.length - done };
  }, [rows]);

  const highCount = useMemo(
    () =>
      rows.filter(
        (r) =>
          r.status === "matched" &&
          (r.matches.find((m) => m.aid === r.selectedAid) ?? r.matches[0])?.confidence === "high",
      ).length,
    [rows],
  );

  const visibleRows = useMemo(() => {
    if (filter === "all") return rows;
    const doneSet = (r: BackfillRow) => r.status === "applied" || r.status === "skipped";
    return rows.filter((r) => (filter === "done" ? doneSet(r) : !doneSet(r)));
  }, [rows, filter]);

  const anyBusy = busy !== null;

  return (
    <div>
      {/* 顶部操作条 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <p className="text-xs text-muted">
          未打标 {counts.total} 本 · 待处理 {counts.todo} · 已处理 {counts.done}
        </p>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {/* 筛选 */}
          <div className="flex rounded-lg border border-border p-0.5 text-xs">
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
                  filter === key
                    ? "bg-accent/10 text-accent"
                    : "text-muted hover:text-foreground"
                }`}
              >
                {label}
              </button>
            ))}
          </div>

          {/* 批量动作 */}
          {busy === "match" ? (
            <StopButton onClick={stopAuto} label="停止匹配" />
          ) : (
            <button
              type="button"
              onClick={runAutoMatch}
              disabled={anyBusy || counts.todo === 0}
              className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Play className="h-3.5 w-3.5" />
              自动匹配未处理
            </button>
          )}
          {busy === "apply" ? (
            <StopButton onClick={stopAuto} label="停止应用" />
          ) : (
            <button
              type="button"
              onClick={runAutoApply}
              disabled={anyBusy || highCount === 0}
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Check className="h-3.5 w-3.5" />
              应用全部高置信{highCount > 0 ? `(${highCount})` : ""}
            </button>
          )}
        </div>
      </div>

      {/* 列表主体 */}
      {loading ? (
        <ListSkeleton />
      ) : error ? (
        <JmErrorCard error={error} onRetry={load} />
      ) : rows.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-lg bg-emerald-500/10">
            <Check className="h-7 w-7 text-emerald-400" />
          </div>
          <p className="text-sm text-foreground">书库里没有无标签的漫画</p>
          <p className="mt-1.5 text-xs text-muted/70">
            新下载的作品会按 JM 设置里的「下载自动打标」自动带上标签
          </p>
        </div>
      ) : (
        <ul className="space-y-2">
          {visibleRows.map((row) => (
            <BackfillRowCard
              key={row.candidate.id}
              row={row}
              locked={anyBusy}
              onKeyword={(v) => updateRow(row.candidate.id, { keyword: v, attempts: 0 })}
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
  );
}

/* ── 单行卡片 ── */

function BackfillRowCard({
  row,
  locked,
  onKeyword,
  onMatch,
  onApply,
  onSelect,
  onToggleExpand,
  onSkip,
  onReset,
}: {
  row: BackfillRow;
  locked: boolean;
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
            : "border-border bg-card"
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
            if (e.key === "Enter" && !locked && status !== "matching") onMatch();
          }}
          placeholder="搜索词"
          spellCheck={false}
          className="h-8 w-44 rounded-lg border border-border bg-background px-2.5 text-xs text-foreground outline-none transition-colors placeholder:text-muted/50 focus:border-accent/60"
        />

        <RowActionButton row={row} locked={locked} onMatch={onMatch} onReset={onReset} />
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
      {status === "skipped" && (
        <p className="mt-2 text-xs text-muted">已跳过,不会参与批量操作</p>
      )}

      {/* 匹配结果(选中项 + 备选);noTags 也保留卡片,便于换备选 */}
      {top && (status === "matched" || status === "noTags") && (
        <div className="mt-2.5">
          <div className="rounded-lg border border-border/70 bg-background/60 p-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <ConfidenceBadge confidence={top.confidence} />
              <span className="min-w-0 flex-1 truncate text-sm text-foreground" title={top.title}>
                {top.title}
              </span>
              {top.author && (
                <span className="max-w-32 truncate text-xs text-muted" title={top.author}>
                  {top.author}
                </span>
              )}
              <div className="flex shrink-0 items-center gap-1.5">
                <button
                  type="button"
                  onClick={onApply}
                  disabled={locked || row.applying}
                  className="inline-flex items-center gap-1 rounded-md bg-accent px-2.5 py-1 text-xs font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {row.applying ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : (
                    <Check className="h-3 w-3" />
                  )}
                  应用
                </button>
                {row.matches.length > 1 && (
                  <button
                    type="button"
                    onClick={onToggleExpand}
                    className="inline-flex items-center gap-1 rounded-md border border-border px-2.5 py-1 text-xs text-muted transition-colors hover:text-foreground"
                  >
                    备选 {row.matches.length - 1}
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
                  disabled={locked || row.applying}
                  className="inline-flex items-center gap-1 rounded-md border border-border px-2.5 py-1 text-xs text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <SkipForward className="h-3 w-3" />
                  跳过
                </button>
              </div>
            </div>
            {top.tags.length > 0 && (
              <div className="mt-2 flex flex-wrap items-center gap-1">
                {top.tags.slice(0, 10).map((t) => (
                  <span
                    key={t}
                    className="rounded bg-muted/10 px-1.5 py-0.5 text-[11px] text-muted"
                  >
                    {t}
                  </span>
                ))}
                {top.tags.length > 10 && (
                  <span className="text-[11px] text-muted/70">+{top.tags.length - 10}</span>
                )}
              </div>
            )}
          </div>

          {/* 备选列表 */}
          {row.expanded && (
            <ul className="mt-1.5 space-y-1">
              {row.matches
                .filter((m) => m.aid !== top.aid)
                .map((m) => (
                  <li
                    key={m.aid}
                    className="flex items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-2.5 py-1.5"
                  >
                    <ConfidenceBadge confidence={m.confidence} />
                    <span className="min-w-0 flex-1 truncate text-xs text-foreground/80" title={m.title}>
                      {m.title}
                    </span>
                    {m.author && (
                      <span className="max-w-28 truncate text-[11px] text-muted">{m.author}</span>
                    )}
                    <button
                      type="button"
                      onClick={() => onSelect(m.aid)}
                      disabled={locked || row.applying}
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

/** 行尾主操作按钮:随状态切换(匹配/重试/撤销) */
function RowActionButton({
  row,
  locked,
  onMatch,
  onReset,
}: {
  row: BackfillRow;
  locked: boolean;
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
        disabled={locked}
        className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-border px-2.5 py-1.5 text-xs font-medium text-muted transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
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
  const isRetry =
    row.status === "error" || row.status === "noresult" || row.status === "noTags";
  return (
    <button
      type="button"
      onClick={onMatch}
      disabled={locked}
      className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs font-medium text-foreground transition-colors hover:border-accent/50 hover:text-accent disabled:cursor-not-allowed disabled:opacity-40"
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
    <span
      className={`shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium ${map[confidence]}`}
    >
      {label[confidence]}
    </span>
  );
}

function StopButton({ onClick, label }: { onClick: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/30 bg-red-500/5 px-3 py-1.5 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/10"
    >
      <Square className="h-3 w-3" />
      {label}
    </button>
  );
}

/** 列表骨架屏 */
function ListSkeleton({ rows = 8 }: { rows?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex animate-pulse items-center gap-3 rounded-xl border border-border bg-card p-3">
          <div className="h-4 w-1/3 rounded bg-muted/15" />
          <div className="ml-auto h-8 w-44 rounded-lg bg-muted/15" />
          <div className="h-8 w-16 rounded-lg bg-muted/15" />
        </div>
      ))}
    </div>
  );
}
