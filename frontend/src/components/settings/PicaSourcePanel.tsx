"use client";

/**
 * 哔咔漫画源设置面板(镜像 JmSourcePanel,口径一致)
 * 区块:账号(头像/等级/登出)、内置服务状态、代理设置、图片画质、漫画下载、维护(清空历史)。
 * 调用接口:health、auth/profile、auth/logout、settings GET/PUT、history DELETE。
 */

import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Activity,
  AlertTriangle,
  Check,
  Coins,
  Globe,
  HardDrive,
  Loader2,
  LogOut,
  RefreshCw,
  Sparkles,
  Trash2,
} from "lucide-react";
import { isPicaApiError, picaGetSettings, picaHealth, picaHistoryClear, picaLogout, picaProfile, picaSaveSettings } from "@/lib/pica/client";
import { resolvePicaUrl } from "@/lib/pica/config";
import { usePicaLogout, usePicaSession } from "@/lib/pica/session";
import { PicaDownloadTasksPanel, PicaDownloadTasksButton } from "@/components/pica/download/DownloadTasks";
import type { PicaSettings, PicaUserInfo } from "@/lib/pica/types";

type HealthState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ok"; version: string }
  | { kind: "fail"; message: string };

const QUALITIES: { value: PicaSettings["imageQuality"]; label: string }[] = [
  { value: "original", label: "原图" },
  { value: "high", label: "高" },
  { value: "medium", label: "中" },
  { value: "low", label: "低" },
];

function errText(err: unknown, fallback: string): string {
  if (isPicaApiError(err)) {
    if (err.code === 2002) return `${err.message || "网络不可达"}(请检查网络与上游代理设置)`;
    return err.message || fallback;
  }
  return err instanceof Error ? err.message : fallback;
}

function HealthLine({ health, onRetry }: { health: HealthState; onRetry: () => void }) {
  return (
    <div className="flex items-center justify-between gap-3 px-4 py-3">
      <div className="flex min-w-0 items-center gap-2.5">
        <Activity className="h-4 w-4 shrink-0 text-muted" />
        <div className="min-w-0">
          <p className="text-sm text-foreground">内置服务</p>
          <p className="mt-0.5 truncate text-xs text-muted">
            {health.kind === "ok" && `运行中 · v${health.version}`}
            {health.kind === "loading" && "检测中…"}
            {health.kind === "fail" && health.message}
            {health.kind === "idle" && "未检测"}
          </p>
        </div>
      </div>
      <button
        type="button"
        onClick={onRetry}
        aria-label="重新检测"
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted transition-colors hover:bg-background hover:text-foreground"
      >
        <RefreshCw className={`h-4 w-4 ${health.kind === "loading" ? "animate-spin" : ""}`} />
      </button>
    </div>
  );
}

export function PicaSourcePanel() {
  const navigate = useNavigate();
  const { userInfo } = usePicaSession();
  const logout = usePicaLogout();

  const [health, setHealth] = useState<HealthState>({ kind: "idle" });
  const [profile, setProfile] = useState<PicaUserInfo | null>(userInfo);
  const [settings, setSettings] = useState<PicaSettings | null>(null);
  const [settingsError, setSettingsError] = useState<string | null>(null);

  const [proxyInput, setProxyInput] = useState("");
  const [savingProxy, setSavingProxy] = useState(false);
  const [savingQuality, setSavingQuality] = useState<PicaSettings["imageQuality"] | null>(null);
  const [tasksOpen, setTasksOpen] = useState(false);
  const [clearConfirm, setClearConfirm] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [logoutConfirm, setLogoutConfirm] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);

  const checkHealth = useCallback(() => {
    setHealth({ kind: "loading" });
    picaHealth()
      .then((res) => setHealth({ kind: "ok", version: res.version ?? "1.0.0" }))
      .catch((err) => setHealth({ kind: "fail", message: errText(err, "服务不可达") }));
  }, []);

  const loadSettings = useCallback(() => {
    setSettingsError(null);
    picaGetSettings()
      .then((res) => {
        setSettings(res);
        setProxyInput(res.proxy ?? "");
      })
      .catch((err) => setSettingsError(errText(err, "设置加载失败")));
  }, []);

  useEffect(() => {
    checkHealth();
    loadSettings();
    // 实时资料(校验 token 有效性,顺带刷新打卡态)
    picaProfile()
      .then((res) => setProfile(res))
      .catch(() => {});
  }, [checkHealth, loadSettings]);

  const saveSettings = useCallback(
    async (patch: Partial<PicaSettings>) => {
      const res = await picaSaveSettings(patch);
      setSettings(res);
      setProxyInput(res.proxy ?? "");
      return res;
    },
    []
  );

  const handleSaveProxy = async () => {
    if (savingProxy || !settings) return;
    setSavingProxy(true);
    try {
      await saveSettings({ proxy: proxyInput.trim() });
      // 代理变更后重检服务
      checkHealth();
    } catch (err) {
      setSettingsError(errText(err, "代理保存失败"));
    } finally {
      setSavingProxy(false);
    }
  };

  const handleQuality = async (q: PicaSettings["imageQuality"]) => {
    if (savingQuality || !settings) return;
    setSavingQuality(q);
    try {
      await saveSettings({ imageQuality: q });
    } catch (err) {
      setSettingsError(errText(err, "画质保存失败"));
    } finally {
      setSavingQuality(null);
    }
  };

  const handleClearHistory = async () => {
    if (clearing) return;
    setClearing(true);
    try {
      await picaHistoryClear();
      setClearConfirm(false);
    } catch (err) {
      setSettingsError(errText(err, "清空历史失败"));
    } finally {
      setClearing(false);
    }
  };

  const handleLogout = async () => {
    if (loggingOut) return;
    setLoggingOut(true);
    try {
      await picaLogout().catch(() => {});
    } finally {
      logout();
      setLoggingOut(false);
      navigate("/pica/login");
    }
  };

  const avatarUrl = resolvePicaUrl(profile?.avatarUrl);
  const initial = (profile?.name || "?").trim().charAt(0).toUpperCase() || "?";

  return (
    <div className="space-y-6">
      {settingsError && (
        <div
          className="flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-500"
          role="alert"
        >
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          {settingsError}
        </div>
      )}

      {/* 账号 */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="border-b border-border/60 px-4 py-3">
          <h3 className="text-sm font-semibold text-foreground">哔咔账号</h3>
        </div>
        {profile ? (
          <div className="flex flex-wrap items-center gap-4 px-4 py-4">
            {avatarUrl ? (
              <img
                src={avatarUrl}
                alt={profile.name}
                className="h-12 w-12 shrink-0 rounded-full object-cover ring-1 ring-border"
              />
            ) : (
              <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-accent/15 text-base font-semibold text-accent">
                {initial}
              </span>
            )}
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className="truncate text-sm font-medium text-foreground">
                  {profile.name || "哔咔用户"}
                </span>
                <span className="rounded-full bg-accent/10 px-2 py-0.5 text-[10px] font-medium text-accent">
                  Lv.{profile.level}
                  {profile.title ? ` · ${profile.title}` : ""}
                </span>
                <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] text-emerald-500">
                  {profile.isPunched ? "今日已打卡" : "今日未打卡"}
                </span>
              </div>
              <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted">
                <span>{profile.email || "—"}</span>
                <span className="flex items-center gap-1">
                  <Sparkles className="h-3 w-3" />
                  经验 {profile.exp}
                </span>
              </p>
            </div>
            <button
              type="button"
              onClick={() => (logoutConfirm ? handleLogout() : setLogoutConfirm(true))}
              onBlur={() => setLogoutConfirm(false)}
              className={`inline-flex shrink-0 items-center gap-1.5 rounded-lg border px-3 py-2 text-sm transition-colors ${
                logoutConfirm
                  ? "border-red-500/50 bg-red-500/10 text-red-400"
                  : "border-border text-muted hover:border-red-500/40 hover:text-red-400"
              }`}
            >
              {loggingOut ? <Loader2 className="h-4 w-4 animate-spin" /> : <LogOut className="h-4 w-4" />}
              {logoutConfirm ? "确认登出?" : "登出"}
            </button>
          </div>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-4">
            <p className="text-sm text-muted">未登录哔咔账号</p>
            <button
              type="button"
              onClick={() => navigate("/pica/login?from=%2Fpica")}
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
            >
              去登录
            </button>
          </div>
        )}
      </section>

      {/* 内置服务状态 */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="border-b border-border/60 px-4 py-3">
          <h3 className="text-sm font-semibold text-foreground">服务状态</h3>
        </div>
        <HealthLine health={health} onRetry={checkHealth} />
      </section>

      {/* 代理设置 */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="border-b border-border/60 px-4 py-3">
          <h3 className="flex items-center gap-1.5 text-sm font-semibold text-foreground">
            <Globe className="h-4 w-4" />
            代理设置(上游代理)
          </h3>
        </div>
        <div className="px-4 py-4">
          <p className="mb-2 text-xs leading-relaxed text-muted">
            上游 picaapi.picacomic.com 不可直连时配置 HTTP 代理(如 http://127.0.0.1:7890);
            留空为直连。哔咔上游按 TLS 指纹风控,服务端已内置浏览器指纹模拟,无需额外配置。
          </p>
          <div className="flex gap-2">
            <input
              type="text"
              value={proxyInput}
              onChange={(e) => setProxyInput(e.target.value)}
              placeholder="http://127.0.0.1:7890(留空直连)"
              spellCheck={false}
              className="h-10 min-w-0 flex-1 rounded-lg border border-border bg-background px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted/60 focus:border-accent"
            />
            <button
              type="button"
              onClick={handleSaveProxy}
              disabled={savingProxy || proxyInput.trim() === (settings?.proxy ?? "")}
              className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg bg-accent px-4 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {savingProxy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
              保存
            </button>
          </div>
        </div>
      </section>

      {/* 图片画质 */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="border-b border-border/60 px-4 py-3">
          <h3 className="text-sm font-semibold text-foreground">图片画质</h3>
        </div>
        <div className="flex flex-wrap items-center gap-2 px-4 py-4">
          {QUALITIES.map((q) => (
            <button
              key={q.value}
              type="button"
              onClick={() => handleQuality(q.value)}
              disabled={savingQuality !== null}
              className={`rounded-full px-4 py-1.5 text-sm font-medium transition-all disabled:cursor-not-allowed disabled:opacity-60 ${
                settings?.imageQuality === q.value
                  ? "bg-accent text-white"
                  : "border border-border/40 bg-card text-muted hover:border-border hover:text-foreground"
              }`}
            >
              {q.label}
            </button>
          ))}
        </div>
      </section>

      {/* 漫画下载 */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="border-b border-border/60 px-4 py-3">
          <h3 className="flex items-center gap-1.5 text-sm font-semibold text-foreground">
            <HardDrive className="h-4 w-4" />
            漫画下载
          </h3>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-4">
          <p className="min-w-0 flex-1 text-xs leading-relaxed text-muted">
            在哔咔详情页/阅读器点击「下载」可将漫画打包为 zip 并归档到书库目录,
            完成后自动触发扫描入库并挂接标签。
          </p>
          <PicaDownloadTasksButton />
        </div>
      </section>

      {/* 维护 */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="border-b border-border/60 px-4 py-3">
          <h3 className="text-sm font-semibold text-foreground">维护</h3>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-4">
          <p className="min-w-0 flex-1 text-xs text-muted">清空哔咔源内阅读历史(不影响本地书库阅读进度)。</p>
          <button
            type="button"
            onClick={() => (clearConfirm ? handleClearHistory() : setClearConfirm(true))}
            onBlur={() => setClearConfirm(false)}
            className={`inline-flex shrink-0 items-center gap-1.5 rounded-lg border px-3 py-2 text-sm transition-colors ${
              clearConfirm
                ? "border-red-500/50 bg-red-500/10 text-red-400"
                : "border-border text-muted hover:border-red-500/40 hover:text-red-400"
            }`}
          >
            {clearing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
            {clearConfirm ? "确认清空?" : "清空阅读历史"}
          </button>
        </div>
      </section>

      <PicaDownloadTasksPanel open={tasksOpen} onClose={() => setTasksOpen(false)} />
    </div>
  );
}

export default PicaSourcePanel;
