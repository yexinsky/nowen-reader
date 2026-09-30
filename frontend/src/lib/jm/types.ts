/**
 * JM 在线源类型定义
 * 与 MOBILE_API.md(30 端点)一一对应,字段以该文档为准。
 */

/* ── 公共数据结构 ── */

/** 列表项(§2.1) */
export interface JmComicItem {
  aid: string;
  title: string;
  author: string;
  /** 经 JM 服务 /api/image 的相对地址(如 /api/image?path=...),需经 resolveJmUrl 拼接 */
  coverUrl: string;
  tags: string[];
  category: string;
  categorySub: string | null;
  likes: number;
  views: number;
  imageCount: number;
  updateAt: string;
}

/** 用户信息(§2.2) */
export interface JmUserInfo {
  userId: string;
  username: string;
  email: string;
  avatarUrl: string | null;
  levelName: string | null;
  level: number;
  gender: string;
  coin: number;
  soulCoin: number;
  exp: number;
  nextLevelExp: number;
  favorites: number;
  canFavorites: number;
  vip: boolean;
  vipExpire: unknown;
}

/** 评论(§2.3) */
export interface JmCommentItem {
  id: string;
  user: { name: string; avatarUrl: string | null };
  content: string;
  createdAt: string;
  likes: number;
  replyTo: string | null;
  replies: JmCommentItem[];
}

/* ── 列表分页 ── */

export interface JmComicPage {
  page: number;
  total?: number;
  hasNext: boolean;
  list: JmComicItem[];
}

/* ── 分类 ── */

export interface JmCategory {
  id: string;
  name: string;
  children: { id: string; name: string }[];
}

/* ── 首页推荐区块(#8) ── */

export interface JmPromoteSection {
  key: string;
  title: string;
  kind: "serialization" | "static";
  list?: JmComicItem[];
}

/* ── 每周必看 ── */

export interface JmWeekCategory {
  id: string;
  title: string;
}

/* ── 搜索参数(#13) ── */

export type JmSearchSort = "" | "mr" | "mv" | "mv_m" | "mv_w" | "mv_t" | "mp" | "tf";
export type JmSearchType = "site" | "work" | "author" | "tag" | "character";

export interface JmSearchParams {
  keyword?: string;
  page?: number;
  sort?: JmSearchSort;
  searchType?: JmSearchType;
  y?: number;
  m?: number;
  mainCategory?: string;
}

/* ── 连载表参数(#10) ── */

export type JmSerializationDay = 1 | 2 | 3 | 4 | 5 | 6 | 7;
export type JmSerializationType = "all" | "manga" | "hanman";

/* ── 详情(#14) ── */

export interface JmEpisode {
  pid: string;
  title: string;
  order: number;
  /** live 模式不含该字段,按可缺省处理(见 MOBILE_API.md 差异 #3) */
  imageCount?: number;
}

export interface JmComicDetail {
  aid: string;
  title: string;
  author: string;
  coverUrl: string;
  description: string;
  tags: string[];
  category: { id: string; name: string; sub: string | null };
  likes: number;
  views: number;
  imageCount: number;
  epCount: number;
  updateAt: string;
  liked: boolean;
  favorited: boolean;
  episodes: JmEpisode[];
}

/* ── 章节图片清单(#15) ── */

export interface JmPhotoItem {
  index: number;
  path: string;
  width: number;
  height: number;
}

export interface JmPhotos {
  pid: string;
  aid: string;
  title: string;
  epIndex: number;
  scramble: string;
  hasNext: boolean;
  nextPid: string | null;
  images: JmPhotoItem[];
}

/* ── 评论分页(#17) ── */

export interface JmCommentPage {
  page: number;
  total: number;
  hasNext: boolean;
  list: JmCommentItem[];
}

/* ── 点赞/收藏结果(#19/#22/#23,切换语义) ── */

export interface JmToggleResult {
  ok: boolean;
  /** live 无法判定时可能为 null,调用方应以回查为准 */
  liked?: boolean | null;
  favorited?: boolean | null;
}

/* ── 收藏夹(#20) ── */

export interface JmFavoriteFolder {
  id: string;
  name: string;
  count: number;
}

/* ── 阅读历史(#24/#25) ── */

export interface JmHistoryItem {
  aid: string;
  title: string;
  coverUrl: string;
  pid: string;
  epTitle: string | null;
  imageIndex: number;
  updatedAt: string;
}

export interface JmHistoryPage {
  list: JmHistoryItem[];
  page: number;
  total: number;
  hasNext: boolean;
}

export interface JmHistoryReport {
  aid: string;
  title?: string;
  coverUrl?: string;
  pid: string;
  epTitle?: string;
  imageIndex?: number;
}

/* ── 服务端设置(#27/#28) ── */

export type JmImageQuality = "high" | "medium" | "low";

export interface JmSettings {
  proxy: string;
  imageQuality: JmImageQuality;
  mock: boolean;
}

/* ── 签到(#29/#30) ── */

export interface JmSignStatus {
  dailyId: number;
  todaySigned: boolean;
  days: { date: number; signed: boolean }[];
}

/* ── 健康检查(#1) ── */

export interface JmHealth {
  status: string;
  mock: boolean;
  version: string;
  upstream: string;
}

/* ── 登录(#3) ── */

export interface JmLoginResult {
  token: string;
  userInfo: JmUserInfo;
}

/* ── 错误码(§0.3) ── */

export const JM_ERROR_CODES = {
  OK: 0,
  BAD_CREDENTIALS: 1001,
  UNAUTHORIZED: 1002,
  CAPTCHA_REQUIRED: 1003,
  UPSTREAM: 2001,
  NETWORK: 2002,
  NOT_FOUND: 3001,
  INTERNAL: 4000,
} as const;
