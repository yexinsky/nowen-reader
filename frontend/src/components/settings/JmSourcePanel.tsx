"use client";

/**
 * 设置 · 在线漫画源面板(PRD docs/PRD_JM_SOURCE.md M1)
 *
 * 在线源已内置(同源 /api/jm),无「服务地址」配置项。
 * 区块:账号(含每日签到)/ 内置服务状态 / 代理设置 / 图片画质 / 维护
 * 接口:MOBILE_API.md #1 health、#5 logout、#26 clearHistory、#27/#28 settings
 * 资源约束:health 仅在面板挂载与手动「重新检测」时探测,不做轮询。
 */

import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  Crown,
  Gauge,
  Loader2,
  LogOut,
  Network,
  RefreshCw,
  Server,
  ShieldCheck,
  Trash2,
  User,
} from "lucide-react";
import { useToast } from "@/components/Toast";
import { JmSignCalendar } from "@/components/jm/SignCalendar";
import { resolveJmUrl } from "@/lib/jm/config";
import {
  isJmApiError,
  jmClearHistory,
  jmGetSettings,
  jmHealth,
  jmLogout,
  jmPutSettings,
} from "@/lib/jm/client";
import { clearJmSession, useJmSession } from "@/lib/jm/session";
import type { JmHealth, JmImageQuality, JmSettings } from "@/lib/jm/types";

/* ── 工具 ── */

function errText(err: unknown, fallback: string): string {
  if (isJmApiError(err)) return err.message || fallback;
  if (err instanceof Error && err.message) return err.message;
  return fallback;
}

/* ── 健康状态(#1)── */

interface HealthState {
  status: "idle" | "checking" | "ok" | "error";
  data: JmHealth | null;
  error: string | null;
}

const HEALTH_IDLE: HealthState = { status: "idle", data: null, error: null };

/** 服务可达状态行:Mock/Live 徽标 + 版本 + upstream */
function HealthLine({ health, onRetry }: { health: HealthState; onRetry: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      {health.status === "checking" && (
        <>
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted" />
          <span className="text-muted">正在检测服务…</span>
        </>
      )}
      {health.status === "ok" && health.data && (
        <>
          <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-emerald-500" />
          <span className="font-medium text-emerald-500">服务正常</span>
          <span
            className={`rounded-full border px-1.5 py-0.5 text-[10px] font-semibold ${
              health.data.mock
                ? "border-sky-500/30 bg-sky-500/10 text-sky-500"
                : "border-amber-500/30 bg-amber-500/10 text-amber-500"
            }`}
          >
            {health.data.mock ? "Mock" : "Live"}
          </span>
          <span className="min-w-0 truncate text-muted">
            v{health.data.version} · {health.data.upstream}
          </span>
        </>
      )}
      {health.status === "error" && (
        <>
          <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-red-500" />
          <span className="font-medium text-red-500">服务不可达</span>
          <span className="min-w-0 flex-1 truncate text-muted">{health.error}</span>
          <button
            type="button"
            onClick={onRetry}
            className="inline-flex shrink-0 items-center gap-1 text-accent transition-colors hover:underline"
          >
            <RefreshCw className="h-3 w-3" />
            重试
          </button>
        </>
      )}
      {health.status === "idle" && (
        <span className="text-muted">尚未检测连接</span>
      )}
    </div>
  );
}

/* ── 画质档位(#28 imageQuality,服务端 JPEG 质量 90/75/60)── */

const QUALITY_OPTIONS: { value: JmImageQuality; label: string; hint: string }[] = [
  { value: "high", label: "高", hint: "JPEG 90" },
  { value: "medium", label: "中", hint: "JPEG 75" },
  { value: "low", label: "低", hint: "JPEG 60" },
];

const QUALITY_LABEL: Record<JmImageQuality, string> = { high: "高", medium: "中", low: "低" };

/* ── 主面板 ── */

export function JmSourcePanel() {
  const toast = useToast();
  const { isLoggedIn, userInfo } = useJmSession();

  /* ── 健康探测(#1):挂载 / 手动「重新检测」 ── */
  const [health, setHealth] = useState<HealthState>(HEALTH_IDLE);

  const checkHealth = useCallback(async (): Promise<JmHealth | null> => {
    setHealth({ status: "checking", data: null, error: null });
    try {
      const data = await jmHealth();
      setHealth({ status: "ok", data, error: null });
      return data;
    } catch (err) {
      setHealth({ status: "error", data: null, error: errText(err, "无法连接内置在线源服务") });
      return null;
    }
  }, []);

  useEffect(() => {
    void checkHealth();
  }, [checkHealth]);

  /* ── 服务端设置(#27/#28):代理 + 画质,始终读取回显 ── */
  // 代理/画质是内置服务的服务器级配置,后端仅需 nowen 登录(组上 AuthRequired),
  // 未登录 JM 也可配置——否则「登录需要代理、改代理又要先登录」形成死锁。
  const [settings, setSettings] = useState<JmSettings | null>(null);
  const [settingsLoading, setSettingsLoading] = useState(false);
  const [settingsError, setSettingsError] = useState<string | null>(null);

  const loadSettings = useCallback(async () => {
    setSettingsLoading(true);
    setSettingsError(null);
    try {
      const s = await jmGetSettings();
      setSettings(s);
    } catch (err) {
      setSettingsError(errText(err, "读取服务端设置失败"));
    } finally {
      setSettingsLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadSettings();
  }, [loadSettings]);

  /* ── 代理(上游代理)── */
  const [proxyInput, setProxyInput] = useState("");
  const [savingProxy, setSavingProxy] = useState(false);

  useEffect(() => {
    if (settings) setProxyInput(settings.proxy ?? "");
  }, [settings]);

  const saveProxy = async (proxy: string) => {
    setSavingProxy(true);
    try {
      const next = await jmPutSettings({ proxy });
      setSettings(next);
      toast.success(proxy ? "代理已保存(已生效)" : "代理已保存(已直连)");
      // 保存后回调 health,反馈服务可达状态
      void checkHealth();
    } catch (err) {
      toast.error(errText(err, "代理保存失败"));
    } finally {
      setSavingProxy(false);
    }
  };

  /* ── 图片画质 ── */
  const [savingQuality, setSavingQuality] = useState<JmImageQuality | null>(null);

  const changeQuality = async (quality: JmImageQuality) => {
    if (!settings || quality === settings.imageQuality) return;
    setSavingQuality(quality);
    try {
      const next = await jmPutSettings({ imageQuality: quality });
      setSettings(next);
      toast.success(`图片画质已切换为「${QUALITY_LABEL[quality]}」`);
    } catch (err) {
      toast.error(errText(err, "画质保存失败"));
    } finally {
      setSavingQuality(null);
    }
  };

  /* ── 维护:清空历史(#26)/ 退出登录(#5)── */
  const [clearConfirm, setClearConfirm] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [logoutConfirm, setLogoutConfirm] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);

  const handleClearHistory = async () => {
    setClearing(true);
    try {
      await jmClearHistory();
      toast.success("已清空在线源阅读历史");
      setClearConfirm(false);
    } catch (err) {
      toast.error(errText(err, "清空历史失败"));
    } finally {
      setClearing(false);
    }
  };

  const handleLogout = async () => {
    setLoggingOut(true);
    let cleared = true;
    try {
      await jmLogout();
      toast.success("已退出在线源登录");
    } catch (err) {
      // 会话已失效(1002)时服务端会话已不存在,直接清本机登录态
      if (isJmApiError(err) && err.code === 1002) {
        toast.info("会话已失效,已清除本机登录态");
      } else {
        cleared = false;
        toast.error(errText(err, "退出登录失败"));
      }
    }
    if (cleared) clearJmSession();
    setLoggingOut(false);
    setLogoutConfirm(false);
  };

  const avatarUrl = userInfo?.avatarUrl ? resolveJmUrl(userInfo.avatarUrl) : "";

  const settingsPending = settingsLoading ? (
    <div className="flex items-center gap-2 px-5 py-4 text-sm text-muted">
      <Loader2 className="h-4 w-4 animate-spin" />
      正在读取服务端设置…
    </div>
  ) : settingsError ? (
    <div className="flex flex-wrap items-center justify-between gap-2 px-5 py-4">
      <p className="text-sm text-red-500">{settingsError}</p>
      <button
        type="button"
        onClick={() => void loadSettings()}
        className="shrink-0 text-sm text-accent transition-colors hover:underline"
      >
        重试
      </button>
    </div>
  ) : null;

  return (
    <div className="max-w-2xl space-y-5">
      {/* ── 账号 ── */}
      <section className="space-y-3">
        {isLoggedIn && userInfo ? (
          <>
            <div className="rounded-lg border border-border bg-card p-5">
              <div className="flex items-center gap-3">
                {avatarUrl ? (
                  <img
                    src={avatarUrl}
                    alt={userInfo.username}
                    className="h-11 w-11 shrink-0 rounded-full border border-border object-cover"
                  />
                ) : (
                  <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-accent/10 text-accent">
                    <User className="h-5 w-5" />
                  </span>
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-semibold text-foreground">
                      {userInfo.username}
                    </span>
                    {userInfo.vip && (
                      <span className="inline-flex shrink-0 items-center gap-0.5 rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-semibold text-amber-500">
                        <Crown className="h-2.5 w-2.5" />
                        VIP
                      </span>
                    )}
                  </div>
                  <div className="mt-0.5 truncate text-xs text-muted">
                    {userInfo.levelName || `等级 ${userInfo.level}`}
                    {userInfo.canFavorites > 0 &&
                      ` · 收藏 ${userInfo.favorites}/${userInfo.canFavorites}`}
                  </div>
                </div>
                <Link
                  to="/jm"
                  className="shrink-0 text-xs text-accent transition-colors hover:underline"
                >
                  进入在线漫画
                </Link>
              </div>
            </div>
            {/* 每日签到(#29/#30),自包含组件 */}
            <JmSignCalendar />
          </>
        ) : (
          <div className="rounded-lg border border-border bg-card p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-accent/10 text-accent">
                  <User className="h-5 w-5" />
                </span>
                <div>
                  <div className="text-sm font-medium text-foreground">未登录在线源</div>
                  <div className="mt-0.5 text-xs text-muted">
                    登录后可使用收藏、历史、评论与每日签到
                  </div>
                </div>
              </div>
              <Link
                to="/jm/login"
                className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90"
              >
                去登录
              </Link>
            </div>
          </div>
        )}
      </section>

      {/* ── 1 内置服务 ── */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex items-center gap-2.5 border-b border-border/50 px-5 py-3.5">
          <Server className="h-4 w-4 shrink-0 text-accent" />
          <h3 className="text-sm font-semibold text-foreground">内置服务</h3>
          <span className="ml-auto min-w-0 truncate text-xs text-muted">同源 /api/jm,无需配置</span>
        </div>
        <div className="space-y-4 p-5">
          <p className="text-xs text-muted">
            在线源已内置,首次访问自动连接上游;Live 模式需配置代理。
          </p>
          <div className="rounded-lg bg-background/60 px-3 py-2.5">
            <HealthLine health={health} onRetry={() => void checkHealth()} />
          </div>
          <button
            type="button"
            onClick={() => void checkHealth()}
            disabled={health.status === "checking"}
            className="inline-flex h-10 items-center gap-1.5 rounded-lg border border-border px-4 text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw
              className={`h-4 w-4 ${health.status === "checking" ? "animate-spin" : ""}`}
            />
            重新检测
          </button>
        </div>
      </section>

      {/* ── 2 代理设置(上游代理)── */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex items-center gap-2.5 border-b border-border/50 px-5 py-3.5">
          <Network className="h-4 w-4 shrink-0 text-accent" />
          <h3 className="text-sm font-semibold text-foreground">代理设置(上游代理)</h3>
        </div>
        {settingsPending ? (
          settingsPending
        ) : (
          <div className="space-y-3 p-5">
            <div className="flex flex-col gap-2 sm:flex-row">
              <input
                type="text"
                value={proxyInput}
                onChange={(e) => setProxyInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    void saveProxy(proxyInput.trim());
                  }
                }}
                placeholder="http://127.0.0.1:10809"
                spellCheck={false}
                autoComplete="off"
                className="h-10 min-w-0 flex-1 rounded-lg border border-border bg-card px-3 text-sm text-foreground outline-none transition-colors placeholder:text-muted/60 focus:border-accent"
              />
              <div className="flex shrink-0 gap-2">
                <button
                  type="button"
                  onClick={() => void saveProxy(proxyInput.trim())}
                  disabled={savingProxy}
                  className="inline-flex h-10 items-center gap-1.5 rounded-lg bg-accent px-4 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {savingProxy && <Loader2 className="h-4 w-4 animate-spin" />}
                  {savingProxy ? "保存中…" : "保存"}
                </button>
                <button
                  type="button"
                  onClick={() => void saveProxy("")}
                  disabled={savingProxy || proxyInput.trim() === ""}
                  className="inline-flex h-10 items-center rounded-lg border border-border px-3 text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                >
                  清除(直连)
                </button>
              </div>
            </div>
            <p className="text-xs text-muted">
              保存后立即生效于内置服务;留空即直连。代理作用于内置服务的上游访问;
              health 仅代表服务可达,上游连通性以实际浏览为准。
            </p>
            <p className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
              Live 模式未配置代理将无法访问内容(Mock 模式不受影响)。
            </p>
          </div>
        )}
      </section>

      {/* ── 3 图片画质 ── */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex items-center gap-2.5 border-b border-border/50 px-5 py-3.5">
          <Gauge className="h-4 w-4 shrink-0 text-accent" />
          <h3 className="text-sm font-semibold text-foreground">图片画质</h3>
        </div>
        {settingsPending ? (
          settingsPending
        ) : (
          <div className="space-y-3 p-5">
            <div className="grid grid-cols-3 gap-2">
              {QUALITY_OPTIONS.map((opt) => {
                const selected = settings?.imageQuality === opt.value;
                const busy = savingQuality === opt.value;
                return (
                  <button
                    key={opt.value}
                    type="button"
                    aria-pressed={selected}
                    disabled={savingQuality !== null}
                    onClick={() => void changeQuality(opt.value)}
                    className={`flex min-h-16 flex-col items-center justify-center gap-0.5 rounded-lg border px-3 py-2 text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
                      selected
                        ? "border-accent bg-accent/10 text-accent"
                        : "border-border text-muted hover:bg-card-hover hover:text-foreground"
                    }`}
                  >
                    <span className="inline-flex items-center gap-1 font-medium">
                      {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                      {opt.label}
                    </span>
                    <span className="text-[11px] text-muted">{opt.hint}</span>
                  </button>
                );
              })}
            </div>
            <p className="text-xs text-muted">
              切换立即保存,对新加载的图片生效;服务端按画质分别缓存,不会误命中旧画质缓存。
            </p>
          </div>
        )}
      </section>

      {/* ── 4 维护 ── */}
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex items-center gap-2.5 border-b border-border/50 px-5 py-3.5">
          <ShieldCheck className="h-4 w-4 shrink-0 text-accent" />
          <h3 className="text-sm font-semibold text-foreground">维护</h3>
        </div>
        <div className="divide-y divide-border/50">
          {isLoggedIn && (
            <div className="flex flex-col gap-3 p-5 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <div className="text-sm font-medium text-foreground">清空在线源阅读历史</div>
                <p className="mt-0.5 text-xs text-muted">
                  清空的是 JM 服务端的阅读历史(设备级共享),与 Nowen Reader
                  本地阅读历史无关,操作不可恢复。
                </p>
              </div>
              {clearConfirm ? (
                <div className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => void handleClearHistory()}
                    disabled={clearing}
                    className="inline-flex h-10 items-center gap-1.5 rounded-lg bg-red-500/10 px-4 text-sm font-medium text-red-500 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {clearing ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Trash2 className="h-4 w-4" />
                    )}
                    {clearing ? "清空中…" : "确认清空"}
                  </button>
                  <button
                    type="button"
                    onClick={() => setClearConfirm(false)}
                    disabled={clearing}
                    className="text-xs text-muted transition-colors hover:text-foreground"
                  >
                    取消
                  </button>
                </div>
              ) : (
                <button
                  type="button"
                  onClick={() => setClearConfirm(true)}
                  className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg border border-border px-4 text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground"
                >
                  <Trash2 className="h-4 w-4" />
                  清空在线源阅读历史
                </button>
              )}
            </div>
          )}
          <div className="flex flex-col gap-3 p-5 sm:flex-row sm:items-center sm:justify-between">
            {isLoggedIn ? (
              <>
                <div className="min-w-0">
                  <div className="text-sm font-medium text-foreground">退出登录</div>
                  <p className="mt-0.5 text-xs text-muted">
                    退出将销毁服务端会话并清除本机保存的 JM 登录态。
                  </p>
                </div>
                {logoutConfirm ? (
                  <div className="flex shrink-0 items-center gap-2">
                    <button
                      type="button"
                      onClick={() => void handleLogout()}
                      disabled={loggingOut}
                      className="inline-flex h-10 items-center gap-1.5 rounded-lg bg-red-500/10 px-4 text-sm font-medium text-red-500 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {loggingOut ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <LogOut className="h-4 w-4" />
                      )}
                      {loggingOut ? "退出中…" : "确认退出"}
                    </button>
                    <button
                      type="button"
                      onClick={() => setLogoutConfirm(false)}
                      disabled={loggingOut}
                      className="text-xs text-muted transition-colors hover:text-foreground"
                    >
                      取消
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setLogoutConfirm(true)}
                    className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-lg border border-border px-4 text-sm text-muted transition-colors hover:bg-card-hover hover:text-foreground"
                  >
                    <LogOut className="h-4 w-4" />
                    退出登录
                  </button>
                )}
              </>
            ) : (
              <>
                <p className="text-sm text-muted">未登录在线源账号。</p>
                <Link
                  to="/jm/login"
                  className="shrink-0 text-sm text-accent transition-colors hover:underline"
                >
                  去登录
                </Link>
              </>
            )}
          </div>
        </div>
      </section>
    </div>
  );
}

export default JmSourcePanel;
