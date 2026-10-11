"use client";

/**
 * 哔咔在线源(PicACG)契约类型与错误码。
 * 字段口径与后端 /api/pica 契约一致(对齐 JM 流结构,便于组件复用模式)。
 */

/** 契约错误码(与后端 pica 包一致;复用 JM 前端同一套 UX 语义) */
export const PICA_ERROR_CODES = {
  /** 成功 */
  OK: 0,
  /** 邮箱或密码错误 */
  BAD_CREDENTIALS: 1001,
  /** 登录已失效(HTTP 401) */
  UNAUTHORIZED: 1002,
  /** 哔咔服务端返回错误 */
  UPSTREAM: 2001,
  /** 网络不可达/超时 */
  NETWORK: 2002,
  /** 资源不存在 */
  NOT_FOUND: 3001,
} as const;

/** 用户资料(GET /auth/profile) */
export interface PicaUserInfo {
  email: string;
  name: string;
  level: number;
  exp: number;
  gender: string;
  title: string;
  avatarUrl: string | null;
  isPunched: boolean;
  createdAt: string;
}

/** 漫画卡片(列表/搜索/排行/收藏/历史通用) */
export interface PicaComicItem {
  id: string;
  title: string;
  author: string;
  coverUrl: string;
  categories: string[];
  tags: string[];
  likes: number;
  views: number;
  imageCount: number;
  epsCount: number;
  finished: boolean;
  updateAt: string;
}

/** 分页列表 */
export interface PicaComicPage {
  page: number;
  total: number;
  hasNext: boolean;
  list: PicaComicItem[];
}

/** 分类条目 */
export interface PicaCategory {
  title: string;
  description: string;
  coverUrl: string;
  isWeb: boolean;
  active: boolean;
  link: string;
}

/** 神魔推荐区块 */
export interface PicaCollectionSection {
  title: string;
  description: string;
  thumbUrl: string;
  list: PicaComicItem[];
}

/** 章节条目 */
export interface PicaEpisode {
  order: number;
  id: string;
  title: string;
  updatedAt: string;
}

/** 章节列表响应 */
export interface PicaEpsPage {
  page: number;
  total: number;
  hasNext: boolean;
  list: PicaEpisode[];
}

/** 章节图片(站内代理地址 + 上游原始引用) */
export interface PicaPageImage {
  url: string;
  fileServer: string;
  path: string;
}

/** 章节图片列表响应 */
export interface PicaPagesResult {
  page: number;
  total: number;
  hasNext: boolean;
  list: PicaPageImage[];
}

/** 漫画详情 */
export interface PicaComicDetail extends PicaComicItem {
  description: string;
  chineseTeam: string;
  createdAt: string;
  allowDownload: boolean;
  liked: boolean;
  commentsCount: number;
  isFavourite: boolean;
}

/** 评论条目(只读) */
export interface PicaComment {
  id: string;
  content: string;
  userName: string;
  avatarUrl: string;
  likes: number;
  createdAt: string;
}

/** 评论分页 */
export interface PicaCommentPage {
  page: number;
  total: number;
  hasNext: boolean;
  list: PicaComment[];
}

/** 服务端设置 */
export interface PicaSettings {
  proxy: string;
  imageQuality: "original" | "low" | "medium" | "high";
  downloadDir: string;
  downloadTags: boolean;
}

/** 阅读历史条目(阅读单元 = 章节 order) */
export interface PicaHistoryItem {
  id: string;
  title: string;
  coverUrl: string;
  order: number;
  epTitle: string | null;
  imageIndex: number;
  updatedAt: string;
}

/** 历史分页 */
export interface PicaHistoryPage {
  list: PicaHistoryItem[];
  page: number;
  total: number;
  hasNext: boolean;
}

/** 下载目录候选 */
export interface PicaDownloadDir {
  label: string;
  path: string;
  kind: "library" | "test";
  libraryId?: string;
  libraryType?: string;
  canManage: boolean;
  isDefault: boolean;
  exists: boolean;
}

/** 下载任务章节进度 */
export interface PicaDownloadChapter {
  order: number;
  title: string;
  state: "pending" | "running" | "done" | "failed";
  total: number;
  done: number;
  error?: string;
}

/** 下载任务 */
export interface PicaDownloadTask {
  id: string;
  bookId: string;
  title: string;
  author: string;
  tags?: string[];
  destDir: string;
  destLabel: string;
  libraryId?: string;
  status: "queued" | "running" | "packing" | "done" | "failed" | "canceled";
  error?: string;
  warning?: string;
  chapters: PicaDownloadChapter[];
  totalImages: number;
  doneImages: number;
  zipName?: string;
  zipPath?: string;
  zipSize?: number;
  createdAt: string;
  updatedAt: string;
}
