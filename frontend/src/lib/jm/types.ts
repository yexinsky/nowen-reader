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
  /** 作者名数组(上游 author 归一;作者标签/作者搜索用;空数组 → 前端回退纯文本) */
  authors: string[];
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
  /** 批量下载默认归档目录(书库管理中的目录;空 = 用内置测试目录) */
  downloadDir: string;
  /** 下载入库后自动把 JM 标签挂到书库漫画(私有扩展,默认开) */
  downloadTags: boolean;
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

/* ── 批量下载(私有扩展,非 MOBILE_API.md 契约) ── */

/** 下载目录候选(书库管理目录 + 内置测试目录) */
export interface JmDownloadDir {
  label: string;
  path: string;
  kind: "library" | "test";
  libraryId?: string;
  libraryType?: string;
  /** 当前用户是否有该书库的管理权限(无权限则不可选) */
  canManage: boolean;
  isDefault: boolean;
  exists: boolean;
}

export type JmDownloadStatus = "queued" | "running" | "packing" | "done" | "failed" | "canceled";
export type JmDownloadChapterState = "pending" | "running" | "done" | "failed";

export interface JmDownloadChapter {
  pid: string;
  title: string;
  order: number;
  state: JmDownloadChapterState;
  total: number;
  done: number;
  error?: string;
}

export interface JmDownloadTask {
  id: string;
  aid: string;
  title: string;
  author: string;
  /** JM 标签(入库后自动挂到书库漫画;仅 aid 下载的任务有值) */
  tags?: string[];
  destDir: string;
  destLabel: string;
  libraryId?: string;
  status: JmDownloadStatus;
  error?: string;
  /** 部分章节失败时的提示(任务仍完成,zip 已生成) */
  warning?: string;
  chapters: JmDownloadChapter[];
  totalImages: number;
  doneImages: number;
  zipName?: string;
  zipPath?: string;
  zipSize?: number;
  createdAt: string;
  updatedAt: string;
}

export interface JmDownloadStartParams {
  aid?: string;
  title?: string;
  author?: string;
  /** 指定章节;缺省 = 全部章节 */
  pids?: string[];
  destDir: string;
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

/* ── 标签/作者收藏(私有扩展,非 MOBILE_API.md 契约) ── */

export type JmTagFavoriteType = "tag" | "author";

/** 单条标签/作者收藏(设备级共享,服务端 tag-favorites.json;同 type+tag 幂等) */
export interface JmTagFavorite {
  type: JmTagFavoriteType;
  tag: string;
  /** 收藏时间,nowen 后端 nowStamp 格式(本地时区秒精度) */
  createdAt: string;
}

/* ── 标签补全(私有扩展,非 MOBILE_API.md 契约) ── */

/** 无标签漫画候选(searchKeyword 为后端按标题清洗出的默认搜索词,前端可改) */
export interface JmBackfillCandidate {
  id: string;
  libraryId: string;
  title: string;
  author: string;
  searchKeyword: string;
  /** 标题内嵌的 JM aid(爬虫落库痕迹);有值时 match 优先车号直达 */
  embeddedAid: string;
}

/** match 结果条目(score 0~1,confidence 由后端按阈值分档;list 已按 score 倒序) */
export interface JmBackfillMatch {
  aid: string;
  title: string;
  author: string;
  tags: string[];
  score: number;
  confidence: "high" | "medium" | "low";
  /** 站内 /api/image 代理封面路径(选择流缩略图用) */
  coverUrl?: string;
  /** 车号直达命中(aid 即权威匹配,与标题相似度无关) */
  viaAid: boolean;
}

/** rename 结果(改写书库标题;newTitle 为服务端权威口径,前端预填仅作预览) */
export interface JmBackfillRenameResult {
  oldTitle: string;
  newTitle: string;
  /** 是否车号直达命中 */
  viaAid: boolean;
  /** false = 新旧名称一致,未写入 */
  changed: boolean;
}
