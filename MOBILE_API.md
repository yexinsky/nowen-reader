# JMComic-qt 移动端 REST API 清单（MOBILE_API）

> **文档版本** v1.0 ｜ **生成日期** 2026-09-27
> 本文逐条对照 `mobile/server` 当前代码整理，是 `docs/API.md`（前后端契约文档）的**代码级核对清单**。若本文与 `docs/API.md` 不一致，**以代码为准**；已发现的两文档差异汇总见文末 [第 5 节](#5-与-docsapimd-的差异点以代码为准)。
> 除本文外未改动任何代码与其他文档。

**代码依据**（2026-09-27 工作区快照，均为相对 `mobile/server/` 的路径）：

| 文件 | 作用 |
|---|---|
| `app.py` | FastAPI 入口：路由注册、统一异常处理、CORS、静态托管 |
| `api/system.py` `api/auth.py` `api/comic.py` `api/favorite.py` `api/history.py` `api/settings.py` `api/user.py` | 7 组路由 |
| `api/deps.py` | Bearer 鉴权依赖 `auth_session` |
| `models.py` | Pydantic 请求模型（校验规则出处） |
| `core/errors.py` | 错误码表 + `{code,msg,data}` 包装 + `ApiError` |
| `core/security.py` | Bearer token 内存会话（TTL 7 天） |
| `core/store.py` | history/settings 本地 JSON 持久化 |
| `core/live.py` | 真实模式：jmcomic SDK 对接上游、AES 登录、图片下载/还原/缓存 |
| `core/mock.py` `core/mockdata.py` | Mock 模式端点实现与确定性 fixture |
| `core/imagepath.py` | `/api/image` path 白名单（防 SSRF） |
| `config.py` | 环境变量/路径/开关 |
| `../web/js/api.js` | 前端 REST 封装（反向核对消费方） |

---

## 0. 全局约定

### 0.1 Base URL 与部署

- 默认端口 **8964**（`JM_PORT` 可覆盖），前端同源部署：`GET /` 返回 `mobile/web/index.html`（目录不存在时返回占位页，不报错）。
- CORS 全放开（`allow_origins/methods/headers = *`），前端"服务地址"可指向局域网任意主机。
- OpenAPI 文档：`/api-docs` 与 `/api-docs/openapi.json`（`JM_DOCS=0/false/no/off` 关闭）；无 redoc。
- 非业务路由：`/` 及静态资源由 `StaticFiles` 挂载（`html=True`）；`/sw.js` 与所有 `*.html` 响应追加 `Cache-Control: no-cache`（PWA 更新保护）。
- **非统一包装的响应**：`GET /api/auth/captcha`、`GET /api/image` 返回图片二进制；路由未匹配时 FastAPI 默认返回 `{"detail": "Not Found"}`（HTTP 404，非 `{code,msg,data}` 包装）（代码未另行定义 404 处理器）。

### 0.2 统一响应包装

所有 `/api/*` JSON 端点（含业务失败）返回：

```json
{"code": 0, "msg": "ok", "data": { ... }}
```

- `code=0` 成功；非 0 失败。业务失败仍为 **HTTP 200**；仅两种例外：
  - **HTTP 401**：`code=1002`（未鉴权/token 失效），响应体仍为统一包装；
  - **HTTP 422**：FastAPI 请求参数校验失败（Query/Body 不合法），返回 FastAPI 默认结构（非包装体），前端按 `422` 统一提示（`api.js` 已处理）。
- 全局兜底：未捕获异常 → `code=4000`，**`msg` 为异常信息截断 300 字符**（`str(exc)[:300]`），**`data=null`**（`app.py` 全局异常处理 + `err_internal`，HTTP 200）。

### 0.3 错误码表（`core/errors.py`）

| code | 含义 | HTTP | data 附加字段 | 触发出处 |
|---|---|---|---|---|
| `0` | 成功 | 200 | — | `ok()` |
| `1001` | 用户名或密码错误 | 200 | — | 登录失败（live 上游 code≠200 且无验证码提示 / mock 密码为 `error`） |
| `1002` | token 无效/过期 | **401** | — | 任何 🔒 端点缺头/格式错/会话不存在 |
| `1003` | 需要验证码 | 200 | `captchaRequired: true` | 登录时上游/mock 要求验证码 |
| `2001` | JM 服务端返回错误 | 200 | `upstream`（原始信息截断） | 上游业务失败/解密失败/图片解码失败等 |
| `2002` | 网络不可达/超时 | 200 | `reason` | 网络关键字命中（timeout/Connection/Proxy/SSL/DNS…） |
| `3001` | 资源不存在 | 200 | — | 漫画/章节不存在、图片 path 非法、mock 收藏对象不存在 |
| `4000` | 内部错误 | 200 | —（data=null） | 未捕获异常兜底（msg=异常信息截断 300 字符） |

live 异常分类顺序（`live._map_live_error`）：`ApiError` 直通 → `MissingAlbumPhotoException`→3001 → `JmcomicException`→2001（data.upstream）→ 网络关键字→2002 → 其余兜底 2001。

### 0.4 鉴权与会话（`core/security.py` / `api/deps.py`）

- 需鉴权端点校验请求头 `Authorization: Bearer <token>`（`bearer` 大小写不敏感），无效抛 `1002`/401。
- token：服务端 `secrets.token_hex(32)` 随机 **64 hex**；会话存**进程内存表**，**TTL 7 天**（自创建时刻起算，惰性清理）。
- **服务重启即全部失效**（内存表），前端需处理 401 重登（`api.js` 收到 401/code=1002 时清会话并广播事件跳登录页）。
- live 模式会话内保存该用户的 jmcomic 客户端（登录 cookies）与建会话时的代理快照 `proxy_key`；设置中代理变更后会话客户端**惰性重建并搬运 cookies**（保持登录态）。
- 多端点并发登录各自持有独立 token，互不影响；`POST /api/auth/logout` 销毁对应会话。

### 0.5 mock / live 双模式

- 环境变量 `JM_MOCK=1`（`1/true/yes/on` 均可）→ **Mock 模式**：全部端点返回确定性 Mock 数据，零网络请求；`/api/image` 返回程序生成的编号占位 JPEG。
- 未设置 → **live 模式**：经 jmcomic SDK（2.6.17）+ JM 移动端 API 域名池对接上游，登录为移动端 AES 加密登录。
- `GET /api/health` 的 `data.mock` 标识当前模式；`GET /api/settings` 的 `mock` 字段同理。
- **history 与 settings 为本地 JSON 持久化（`mobile/server/data/`），两种模式共用同一份**，不依赖上游。
- Mock 分页页大小**默认 8**（`MOCK_PAGE_SIZE`）；**favorites 与 comments 为 20**（`mock.py` 分页显式传 `page_size=20`）、**history 为 20**（`store.list_history` 页大小 20）；live 页大小见各端点（80/40/20）。

### 0.6 运行期配置（`config.py`）

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `JM_MOCK` | 未设置 | `1` 启用 Mock |
| `JM_PROXY` | `http://127.0.0.1:10809` | 上游代理；显式空串 = 直连；运行期可经 `PUT /api/settings` 修改 |
| `JM_DATA_DIR` | `mobile/server/data` | history/settings 持久化目录 |
| `JM_PORT` | `8964` | 服务端口 |
| `JM_DOCS` | 开启 | `0`/`false`/`no`/`off` 时关闭 `/api-docs` |
| `JM_IMAGE_CACHE_LIMIT` | `500` | 图片缓存按文件数 LRU 上限（**最小 10**，低于取 10） |

### 0.7 时间与计数字段口径（live）

- **epoch → 东八区**：上游 epoch 秒（≥9 位数字）转为 UTC+8 的 `%Y-%m-%d %H:%M`（评论 `createdAt`）或 `%Y-%m-%d`（列表 `updateAt`）；**非纯数字字符串原样透传；纯数字但不足 9 位视为无效返回空串**；空值 → 空串（`live._epoch_to_str`）。
- **计数字符串**：`"918"`/`"[1K]"`/`"40K"`/int → int（K/M 单位换算；解析失败 → 0）（`live._parse_count`）。
- **HTML 剥离**（`live._strip_html`，用于详情简介与评论内容）：`<br>` → 换行 → 剥全部标签 → HTML 实体还原（`html.unescape`）→ 连续空行压缩为最多 2 个换行 → 首尾去空白。

---

## 1. 接口总览（共 30 个 REST 端点）

🔒 = 需 `Authorization: Bearer`；"上游端点"列仅指 live 模式的 JM 上游调用，"—" = 无上游调用（本地实现）。

| # | 方法 | 路径 | 鉴权 | 用途 | live 上游 JM 端点 |
|---|---|---|---|---|---|
| 1 | GET | `/api/health` | | 健康检查/模式探测 | —（仅读 SDK 版本号） |
| 2 | GET | `/api/auth/captcha` | | 验证码图片（二进制） | `GET {上游域名}/captcha`（**API 域名优先、网页端域名回退**；带 web UA+Referer `/signup`） |
| 3 | POST | `/api/auth/login` | | 登录，换取 token | `POST /login`（SDK 移动端 AES 加密） |
| 4 | GET | `/api/auth/profile` 🔒 | 🔒 | 当前用户信息 | —（登录快照，不回源） |
| 5 | POST | `/api/auth/logout` 🔒 | 🔒 | 销毁服务端会话 | — |
| 6 | GET | `/api/categories` | | 分类树 | `GET /categories?lang=CN` |
| 7 | GET | `/api/comics/index` | | 首页/全部漫画列表（Latest/View） | `GET /categories/filter`（SDK 生成 query：`page` / `order`=空串 / `c=0` / `o=mr|mv`，无 `t` 键；1 基页码；本服务 `order=Latest|View` → 上游 `o=mr/mv`） |
| 8 | GET | `/api/comics/promote` | | 首页推荐区块 | `GET /promote?page=0&lang=CN` |
| 9 | GET | `/api/comics/latest` | | 最近更新（分页） | `GET /latest?page=<page-1>&lang=CN`（**0 基**） |
| 10 | GET | `/api/comics/serialization` | | 每周连载（分页） | `GET /serialization?type&date=<day>&page&lang=CN`（1 基） |
| 11 | GET | `/api/comics/week` | | 每周必看期数列表 | `GET /week?page=0&lang=CN` |
| 12 | GET | `/api/comics/week/filter` | | 每周必看单期列表（无翻页） | `GET /week/filter?page=0&id&type&lang=CN` |
| 13 | GET | `/api/comics/search` | | 搜索/分类/排行（分页） | `GET /categories/filter` 或 `GET /search`（路由规则见 #13 详情） |
| 14 | GET | `/api/comics/{aid}` | | 漫画详情 | `GET /album?id={aid}` |
| 15 | GET | `/api/photos/{pid}` | | 章节（图片清单） | `GET /chapter?id={pid}`（经 SDK `get_photo_detail` 展开；伴 `GET /album?id={album_id}` 与 `GET /chapter_view_template`） |
| 16 | GET | `/api/image` | | 图片代理（下载→乱序还原→缓存，二进制） | `GET {图片域名池}/{path}`（`JmModuleConfig.DOMAIN_IMAGE_LIST`） |
| 17 | GET | `/api/comics/{aid}/comments` 🔒 | 🔒 | 评论列表（分页） | `GET /forum?mode=manhua&aid&page` |
| 18 | POST | `/api/comics/{aid}/comments` 🔒 | 🔒 | 发布评论 | `POST /comment`（表单 `comment`+`aid`） |
| 19 | POST | `/api/comics/{aid}/like` 🔒 | 🔒 | 点赞（切换语义） | `POST /like`（表单 `aid`） |
| 20 | GET | `/api/favorites/folders` 🔒 | 🔒 | 收藏夹列表 | SDK `client.favorite_folder(page=1)` |
| 21 | GET | `/api/favorites` 🔒 | 🔒 | 收藏列表（分页） | SDK `client.favorite_folder(page, folder_id)`（页大小 20） |
| 22 | POST | `/api/favorites` 🔒 | 🔒 | 添加收藏（切换语义） | `POST /favorite`（表单 `aid`） |
| 23 | DELETE | `/api/favorites/{aid}` 🔒 | 🔒 | 取消收藏（切换语义） | `POST /favorite`（表单 `aid`，同一端点） |
| 24 | GET | `/api/history` 🔒 | 🔒 | 阅读历史（本地，分页） | — |
| 25 | POST | `/api/history` 🔒 | 🔒 | 上报阅读进度（本地） | — |
| 26 | DELETE | `/api/history` 🔒 | 🔒 | 清空历史（本地） | — |
| 27 | GET | `/api/settings` 🔒 | 🔒 | 读服务端设置（本地） | — |
| 28 | PUT | `/api/settings` 🔒 | 🔒 | 写服务端设置（本地，proxy 变更影响上游请求） | —（proxy 变更时重建上游客户端） |
| 29 | GET | `/api/user/sign` 🔒 | 🔒 | 签到状态 | `GET /daily?user_id={uid}` |
| 30 | POST | `/api/user/sign` 🔒 | 🔒 | 执行签到（先查后签，幂等） | `GET /daily?user_id` + `POST /daily_chk`（表单 `user_id`+`daily_id`） |

---

## 2. 公共数据结构

以下结构在多个端点复用，字段与代码一一对应；Mock 模式恒有值，live 模式缺失值口径见括注。

### 2.1 `ComicItem`（列表项，`live._comic_item` / `mock.comic_item`）

| 字段 | 类型 | 语义 | live 口径 |
|---|---|---|---|
| `aid` | string | 漫画 id | `str(aid)` |
| `title` | string | 标题 | 上游 `name`，空 → `""` |
| `author` | string | 作者 | 上游 `author`，缺失 → `""` |
| `coverUrl` | string | 封面 URL（经 `/api/image`，可直接 `<img src>`） | 上游 `image`；绝对 URL 只保留路径部分；**剥离 `?query`**（如 serialization 列表项的 `?u=` 缓存参数，path 白名单不含 query）；反斜杠归一为 `/`；无 image 时回退 `media/albums/{aid}_3x4.jpg` |
| `tags` | string[] | 标签 | 上游缺失 → `[]` |
| `category` | string | 主分类名 | 上游 `category.title`；非 dict 时 `str()` |
| `categorySub` | string\|null | 子分类名 | 上游缺失/空 → `null` |
| `likes` | int | 爱心数 | 列表接口不返回时 **0**（`_parse_count`） |
| `views` | int | 点击数 | 同上，缺失 → **0** |
| `imageCount` | int | 图片总数 | 同上，缺失 → **0** |
| `updateAt` | string | 更新日期 | 优先上游 `adddate` 字符串；否则 epoch `update_at` → 东八区 `%Y-%m-%d`；均无 → `""` |

### 2.2 `UserInfo`（`live._user_info` / `mock.login`）

| 字段 | 类型 | 语义 |
|---|---|---|
| `userId` | string | 用户 id（live `str(uid)`；mock `"mock"+md5(用户名)[:8]`） |
| `username` | string | 用户名 |
| `email` | string | 邮箱（live 缺失 → `""`） |
| `avatarUrl` | string\|null | 头像（经 `/api/image`）；live：绝对 URL 只留路径部分、`nopic*` → `null`、否则 `media/users/{photo}`；无 → `null` |
| `levelName` | string\|null | 等级名称（透传） |
| `level` | int | 等级数值（`_parse_count`，缺省 0） |
| `gender` | string | 性别（缺省 `""`） |
| `coin` | int | 金币 |
| `soulCoin` | int | 灵魂币 |
| `exp` | int | 经验 |
| `nextLevelExp` | int | 升级所需经验 |
| `favorites` | int | 当前收藏数（上游 `album_favorites`） |
| `canFavorites` | int | 收藏上限（上游 `album_favorites_max`） |
| `vip` | bool | 是否 VIP |
| `vipExpire` | any\|null | VIP 到期（**原样透传** `vip_expire`，代码未做格式归一） |

### 2.3 `CommentItem`（`live._map_comment` / mock fixture）

| 字段 | 类型 | 语义 |
|---|---|---|
| `id` | string | 评论 id（live `str(CID)`） |
| `user.name` | string | 评论人 |
| `user.avatarUrl` | string\|null | 头像（同 UserInfo 口径） |
| `content` | string | 内容（live 经 `_strip_html` 剥 HTML） |
| `createdAt` | string | 时间（live epoch → 东八区 `%Y-%m-%d %H:%M`；mock 为 ISO8601 或 `"YYYY-MM-DD HH:MM:SS"`） |
| `likes` | int | 点赞数（缺失 → 0） |
| `replyTo` | string\|null | 被回复人（**live 恒为 `null`**，上游未提供映射；mock 预置数据可为用户名） |
| `replies` | CommentItem[] | 楼中楼（live 取上游 `replys` 递归映射） |

### 2.4 分页约定

- 页码统一 **1 基**（`page: int = Query(1, ge=1)`），除 `/api/comics/latest` 的上游调用换算为 0 基（`page-1`）。
- `hasNext` 判定（`live._has_next`，live 模式）：① 本页不满一页（含空页）→ `false`（覆盖 total 缺失/为 0 的兜底）；② total 有效且 `page*页大小 >= total` → `false`；③ index/search 传 `max_page=120`，`page>=120` → `false`；否则 `true`。
- live 页大小：`comics/index`、`comics/search`（含 `/categories/filter` 路径）= **80**；`comics/latest` = **80**；`comics/serialization` = **40**；`favorites` = **20**；`comments` = **20**；`week/filter` = 上游单期固定约 20 条（无翻页字段）。
- Mock 页大小：**默认 8**（favorites、comments、history 为 20，见 §0.5）。

---

## 3. 逐端点详情

### 3.1 system

#### #1 GET /api/health

- **鉴权**：无。
- **请求**：无参数。
- **响应 `data`**：`{ "status": "ok", "mock": bool, "version": "1.0.0", "upstream": "jmcomic-2.6.17" }`
  - `version`：服务版本，恒 `"1.0.0"`（`config.VERSION`）。
  - `upstream`：mock 恒 `"jmcomic-2.6.17"`；live 读 `jmcomic.__version__`（失败回退 `"jmcomic-2.6.17"`）。
- **错误**：`4000`（兜底，实际难触发）。
- **mock/live 差异**：仅 `upstream` 取值来源不同；`mock` 字段恒反映当前模式。
- **上游映射**：无网络调用。
- **⚠️ 备注**：前端用此端点做连通性探测（settings.js 保存代理后回调验证）。

### 3.2 auth

#### #2 GET /api/auth/captcha

- **鉴权**：无。
- **请求**：无参数。前端经 `captchaURL()` 直接作为 `<img src>`（可加 `?r=<时间戳>` 防缓存，服务端忽略多余 query）。
- **响应**：**图片二进制**（非 JSON 包装）。mock 恒 `image/jpeg`；live 为上游 `content-type`（`image/jpeg` 或 `image/png`），content-type 缺失但魔数合法（JPEG `\xff\xd8\xff`/PNG `\x89PN`）时按 `image/jpeg` 返回。
- **错误**：`2001` 获取验证码失败（data.upstream）；`2002` 无可用上游域名（data.reason）或网络失败。
- **mock/live 差异**：mock 为程序生成的 4 位随机数字 JPEG（带干扰线），答案存入内存 `STATE.captcha_answers`（**deque 上限 10 条**），供 mock 登录校验；live 为上游真实验证码。
- **上游映射**（live）：候选域名 = 匿名客户端 API 域名池第 1 个 → 回退网页端域名（经 `JM_REDIRECT_URL` 重定向解析并进程内缓存）；`GET {PROT}{domain}/captcha`，请求头对齐桌面端 GetCaptchaReq（Chrome UA + `Referer: …/signup`）；校验 HTTP 200 + 魔数/content-type 为图片。
- **⚠️ 备注**：mock 登录用户名为 `captcha` 时，必须携带与最近 10 次生成的验证码答案之一匹配的 `captcha` 字段才能登录成功（P1-3 闭环）。

#### #3 POST /api/auth/login

- **鉴权**：无。
- **请求体**（`LoginRequest`）：`{ "username": "str(≥1字符,必填)", "password": "str(≥1字符,必填)", "captcha": "str?" }`；非法缺字段 → HTTP 422。
- **响应 `data`**：`{ "token": "64hex", "userInfo": <UserInfo> }`
- **错误**：`1001` 凭据错误；`1003` 需要验证码（data.captchaRequired=true）；`2001`/`2002` 上游/网络（live）；`422`。
- **mock/live 差异**：
  - mock：任意非空凭据成功；密码为字面量 `error` → `1001`；用户名为字面量 `captcha` → 未携带有效验证码时 `1003`。mock `UserInfo` 为确定性构造（level=1、coin=520、favorites=3、canFavorites=500、vip=true、vipExpire=`"2027-12-31T00:00:00Z"` 等）。
  - live：上游响应 code≠200 时，msg 含"验证码"/"captcha"（忽略大小写）→ `1003`，否则 → `1001`；登录成功后将返回值 `s` 字段写入 `AVS` cookie（对齐桌面端）。
- **上游映射**（live）：`client.req_api("/login", data={username, password, captcha?})`，由 SDK `JmCryptoTool` 做移动端 AES 加密；响应经 `decoded_data` 解密后映射 `UserInfo`。
- **⚠️ 备注**：创建会话时记录代理快照 `proxy_key`（live）；建会话即清理过期会话。

#### #4 GET /api/auth/profile 🔒

- **鉴权**：Bearer。
- **请求**：无参数。
- **响应 `data`**：`<UserInfo>`。
- **错误**：`1002`/401。
- **mock/live 差异**：mock 返回登录时生成的确定性 UserInfo；live 返回**登录快照**——上游无独立"用户信息查询"端点（桌面端 GetUserInfoReq 已停用），**不回源刷新**。
- **上游映射**：无。
- **⚠️ 备注**：字段以登录时为准；后端设置变更不影响该快照。

#### #5 POST /api/auth/logout 🔒

- **鉴权**：Bearer。
- **请求**：无请求体。
- **响应 `data`**：`{ "ok": true }`。
- **错误**：`1002`/401（token 无效时无法定位会话）。
- **mock/live 差异**：无（均本地销毁）。
- **上游映射**：无。
- **⚠️ 备注**：幂等性仅限 token 有效期内；token 失效后重复登出 → `1002`。

### 3.3 comic（分类 / 发现 / 搜索 / 详情 / 图片 / 评论 / 点赞）

#### #6 GET /api/categories

- **鉴权**：无。
- **请求**：无参数。
- **响应 `data`**：`[ { "id": "0", "name": "全部", "children": [ { "id": "doujin", "name": "同人志" }, … ] }, … ]`
- **错误**：`2001`/`2002`（live）。
- **mock/live 差异**：mock 为固定 6 组 fixture（id=`0`..`5`）；live 为上游实时分类树。
- **上游映射**（live）：`GET /categories?lang=CN`；顶层键 id → str，子分类主键取 **`CID`**（部分上游版本兼容 `id`）→ str。
- **⚠️ 备注**：前端 `Api.categories` 已封装但当前无视图调用（暂无前端消费方）。

#### #7 GET /api/comics/index

- **鉴权**：无。
- **Query**：`page`（int，默认 1，**≥1**，非法 422）；`order`（默认 `"Latest"`，枚举 `Latest|View`，正则约束，非法 422）。
- **响应 `data`**：`{ "page": 1, "hasNext": true, "list": [<ComicItem>] }`（**不返回 total**）。
- **错误**：`422`；live `2001`/`2002`。
- **mock/live 差异**：mock 页大小 8、`View` 按 views 降序 / `Latest` 按 updateAt 降序；live 页大小 80。
- **上游映射**（live）：SDK `client.categories_filter(page, time=全部(TIME_ALL), category=全部(CATEGORY_ALL), order_by=mr|mv)` → 上游 `GET /categories/filter`；**实际 query 由 SDK 生成：`page`、`order`（空串）、`c=0`、`o=mr|mv`，无 `t` 键**（本服务 `order=Latest|View` 参数映射为上游 `o=mr/mv`）；1 基页码原样透传；`hasNext` 受 `max_page=120` 限制。
- **⚠️ 备注**：上游 `/categories/filter` 与 `/search` 的 **total 封顶 10000、第 121 页起返回与第 120 页相同的冻结数据**——不封顶会无限滚动重复追加同一页，故 live `_has_next` 传入 `max_page=JM_LIST_MAX_PAGE=120`。列表接口不返回 likes/views/images_count/tags 时填 0/[]。
- **前端消费**：`Api.comicsIndex` 已封装但当前无视图调用（暂无前端消费方）。

#### #8 GET /api/comics/promote

- **鉴权**：无。
- **请求**：无参数。
- **响应 `data`**：`{ "sections": [ { "key": "s1", "title": "连载更新", "kind": "serialization" }, { "key": "s2", "title": "…", "kind": "static", "list": [<ComicItem>] }, … ] }`
  - `kind="serialization"`：恒为第一个区块，**无 `list` 字段**（前端该 tab 另行调 `/api/comics/serialization`）。
  - `kind="static"`：携带映射后的 `list`。
  - `key` 为 `s1..sN` 顺序编号。
- **错误**：`2001`/`2002`（live）。
- **mock/live 差异**：mock 为固定 fixture（s1 serialization + s2~s4 static）；live 为上游实时区块。
- **上游映射**（live）：`GET /promote?page=0&lang=CN`；data 顶层为 JSON **数组**（超出 SDK `model_data` 的 dict 约束，走 `decoded_data` 手动解密）；**过滤 `type in ('novels','library')`**（小说/书库区块不进首页）；首个保留区块固定 `kind="serialization"`（不使用区块自带书单）；其余区块从 `content` 书单映射 `ComicItem`。
- **⚠️ 备注**：上游 code≠200 → `2001`；解密失败 → `2001`（data.upstream）。

#### #9 GET /api/comics/latest

- **鉴权**：无。
- **Query**：`page`（int，默认 1，**≥1**，非法 422）。
- **响应 `data`**：`{ "page": 1, "hasNext": true, "list": [<ComicItem>] }`（**无 total**）。
- **错误**：`422`；live `2001`/`2002`。
- **mock/live 差异**：mock 按 updateAt 倒序、页大小 **8**；live 页大小 **80**。
- **上游映射**（live）：`GET /latest?page=<page-1>&lang=CN` —— **上游 0 基页码**（服务端做 `page-1` 换算）；data 为纯 book 数组、**无 total**，`hasNext` 按"本页满页（80 条）"判定。
- **⚠️ 备注**：因上游无 total，`hasNext` 仅由本页条数决定——最后一页恰好满 80 条时会多返回一次 `hasNext=true`（下一页为空页后终止）；这是满页判定的固有边界（代码注释明确按契约如此实现）。

#### #10 GET /api/comics/serialization

- **鉴权**：无。
- **Query**：`day`（int，默认 1，**1..7**，1=周一…7=周日，非法 422）；`type`（默认 `"all"`，枚举 `all|manga|hanman`，非法 422）；`page`（int，默认 1，≥1，非法 422）。
- **响应 `data`**：`{ "page": 1, "day": 1, "type": "all", "hasNext": true, "list": [<ComicItem>] }`（无 total）。
- **错误**：`422`；live `2001`/`2002`。
- **mock/live 差异**：mock 按 `day` 轮转 fixture 顺序再按 type 过滤（hanman=韩漫、manga=非韩漫）、页大小 8；live 页大小 40。
- **上游映射**（live）：`GET /serialization?type=<type>&date=<day>&page=<page>&lang=CN`；day/type **原样透传**（对齐桌面端 `date=weekBox.currentIndex()+1`）；上游页码 1 基；data=`{list,total}`（页大小 40），`hasNext` 按 total 判定（无 max_page 限制）。
- **⚠️ 备注**：上游列表项封面路径可能带 `?u=` 缓存参数，映射时剥离（见 §2.1 coverUrl）。

#### #11 GET /api/comics/week

- **鉴权**：无。
- **请求**：无参数。
- **响应 `data`**：`{ "categories": [ { "id": "259", "title": "2026第258期09.25 - 09.18" }, … ] }`
- **错误**：`2001`/`2002`（live）。
- **mock/live 差异**：mock 为固定 3 期 fixture（id=`w1`~`w3`）；live 为上游实时期数。
- **上游映射**（live）：`GET /week?page=0&lang=CN`；data=`{categories:[{id,title,time}],type}`；**契约 `title` 取上游 `time` 字段**（fallback 上游 `title`），丢弃上游 `type` 字段。
- **⚠️ 备注**：仅取 id/title 两字段；按新到旧排列（上游顺序）。

#### #12 GET /api/comics/week/filter

- **鉴权**：无。
- **Query**：`id`（str，**必填且 ≥1 字符**，缺失/空 → 422）；`type`（默认 `"manga"`，枚举 `manga|hanman|another`，非法 422）。
- **响应 `data`**：`{ "list": [<ComicItem>] }`（**无 page/total/hasNext**——单期固定内容，无翻页语义）。
- **错误**：`422`；live `2001`/`2002`。
- **mock/live 差异**：mock 按 `期数×type` 确定性切片，**未知 id 返回空列表**（不报 3001）；live 为上游数据。
- **上游映射**（live）：`GET /week/filter?page=0&id=<id>&type=<type>&lang=CN`（固定 page=0，对齐桌面端）；data=`{total,list}`，服务端仅回传 `list`。
- **⚠️ 备注**：`id` 取 `/api/comics/week` 返回的 `id`。

#### #13 GET /api/comics/search

- **鉴权**：无。
- **Query**（均可选，除 page 外）：
  - `keyword`（str，默认 `""`，无最小长度校验）
  - `page`（int，默认 1，≥1，非法 422）
  - `sort`（默认 `""`，枚举 `""|mr|mv|mv_m|mv_w|mv_t|mp|tf`，非法 422；`mr`=最新、`mv`=最多点击、`mv_m/mv_w/mv_t`=月/周/日排行、`mp`=最多图片、`tf`=最多爱心）
  - `searchType`（可选，枚举 `site|work|author|tag|character`，非法 422；`site` 等价缺省全站）
  - `y`（可选，int，**1..9999**，年份过滤；非法 422）
  - `m`（可选，int，**1..12**，月份过滤；非法 422）
  - `mainCategory`（可选，str，数值 id `1..8` 或分类 slug，大小写不敏感；`0`/空/`all`=不过滤；未知 slug 原样透传）
  - `mainCategoryName`（可选，str，**仅 mock 生效**，按 categoryName 精确匹配；live `search()` 无此参数，直接忽略）
- **响应 `data`**：`{ "page": 1, "total": 1234, "hasNext": true, "list": [<ComicItem>] }`
- **错误**：`422`；live `2001`/`2002`。
- **mock 实现**：keyword 匹配 title/author/tags/categoryName（子串、大小写不敏感）；mainCategory 经 `main_category_slug()` 归一后与 `categoryId` 匹配；`mv*` 按 views 降序、`mp` 按 imageCount、`tf` 按 likes（`""`/`mr` 保持 fixture 顺序）；**`searchType/y/m` 忽略**；页大小 8。
- **上游映射（live，路由规则）**：
  1. `mainCategory` 归一化（`mockdata.main_category_slug` 共用映射：数值 `1..8` → `doujin/single/short/another/hanman/meiman/doujin_cosplay/3d`；`3d` 修正为上游大小写 `3D`；**`doujin_cosplay` → `another_cosplay`**（2026-09 实测当前 Cosplay 真实 slug，旧 slug 上游已与 doujin 同返回））。
  2. **满足任一条件走上游 `GET /categories/filter`**（对齐桌面端 GetSearchCategoryReq2）：`mainCategory` 有效、或 `sort ∈ {mv_m,mv_w,mv_t}`、或 **keyword 为空/全空白**。参数：`page`（1 基）、`o`（排序映射同下）、`c`（归一化 slug，"0"=全部时不传）。**该端点忽略 `search_query`**，携带关键字时按分类/排行返回（关键字被忽略，服务端打日志）。
  3. 否则走上游 `GET /search`：参数 `search_query`+`page`（1 基）+`o`；`searchType≠site` 时附加 `search_type`；`y>0` 时附加 `y`（**实测 2017 年起生效**，更早/未来年份上游忽略）、`m` 仅与 `y` 同传时附加。无高级过滤时走 SDK `client.search_site` 原路径（保留搜车号 `redirect_aid` → 单详情页包装；高级过滤响应含 `redirect_aid` 时也回退该路径）。
  4. `o` 排序映射（SDK `JmMagicConstants`）：`mr`→最新、`mv`→最多点击、`mv_m/mv_w/mv_t`→月/周/日排行、`mp`→最多图片、`tf`→最多爱心、未知/`""`→最新。
  5. 两条路径均回传 `total`，`hasNext` 受 `max_page=120` 限制；页大小 80。
- **⚠️ 备注**：
  - 空关键词 + 任意排序一律落入 `/categories/filter` 分支（对齐桌面端：否则"全部分类+mr/mv/mp/tf"会落入空关键词 `/search` 导致无数据）。
  - **`mv_t`（日排行）上游仅在不指定分类时返回数据，带分类查询结果为空列表**（上游行为，如实透传）。
  - `/search` 关键字搜索仅支持 `mr/mv/mp/tf` 四种排序；月/周/日排行仅 `/categories/filter` 支持。
  - `hasNext` 判定与"total 封顶 10000/页码封顶 120"怪癖同 #7。

#### #14 GET /api/comics/{aid}

- **鉴权**：无。
- **路径参数**：`aid`（str，无格式校验）。
- **响应 `data`**：
  | 字段 | 类型 | 语义 |
  |---|---|---|
  | `aid` | string | 漫画 id（live 取上游 `id`） |
  | `title` | string | 标题 |
  | `author` | string | 作者；live 取上游 `author[0]`（数组转单值），缺失时回退 `"default_author"` |
  | `coverUrl` | string | `media/albums/{aid}_3x4.jpg` 经 `/api/image` |
  | `description` | string | 简介（live 经 `_strip_html` 剥 HTML+实体还原） |
  | `tags` | string[] | 标签 |
  | `category` | object | live **恒为 `{"id":"0","name":"全部","sub":null}`**（上游专辑接口未返回分类）；mock 为 fixture 分类 |
  | `likes` / `views` | int | live 取上游 `likes` / `total_views`（`_parse_count`，缺失 0） |
  | `imageCount` | int | **live 恒 0**（专辑接口不含图片总数，真实数量见 photos）；mock 为章节图片数之和 |
  | `epCount` | int | 章节数 |
  | `updateAt` | string | live 取 `update_at`（epoch → 东八区日期）或 `addtime`；可能为空串 |
  | `liked` | bool | 当前用户是否已赞（上游 `liked`；mock 由点赞状态推导） |
  | `favorited` | bool | 当前用户是否已收藏（上游 `is_favorite`；mock 由收藏状态推导） |
  | `episodes` | array | `[{ "pid": str, "title": str, "order": int }]`；**live 不含 `imageCount` 字段**，mock 额外含 `imageCount`；无 series 时 live 生成单默认章（pid=aid，title=标题，order=1），series 章节按上游 `sort` 排序（缺失按序号兜底，title 缺失 → `"第{n}话"`） |
- **错误**：`3001` 漫画不存在（live：上游 `name` 为空即判定；mock：aid 不在 fixture）；live `2001`/`2002`。
- **mock/live 差异**：见上表括注；mock `episodes[].imageCount` 为 live 所无的附加字段。
- **上游映射**（live）：`GET /album?id={aid}`。
- **⚠️ 备注**：`episodes[].pid` 即阅读单元（photo）id；单章漫画也有一个默认章。docs/API.md §13.3 称 `episodes[].imageCount` 恒为 0，与 live 代码不符（字段整个缺失），见第 5 节差异 #3。路由注册顺序上 `/comics/index`、`/comics/latest` 等静态段先于 `/comics/{aid}` 注册，不会被通配吞掉。

#### #15 GET /api/photos/{pid}

- **鉴权**：无。
- **路径参数**：`pid`（str）。
- **响应 `data`**：
  | 字段 | 类型 | 语义 |
  |---|---|---|
  | `pid` / `aid` | string | 章节 id / 所属漫画 id |
  | `title` | string | 章节标题 |
  | `epIndex` | int | 章节序号（0 基；live 取 `max(album_index-1, 0)`） |
  | `scramble` | string | 乱序分割参数（live 为上游 `scramble_id`，缺省 `"0"`）；前端拼图片 URL 时回传 |
  | `hasNext` | bool | 是否有下一章（live 由所属专辑 `episode_list` 顺序判定） |
  | `nextPid` | string\|null | 下一章 pid |
  | `images` | array | `[{ "index": 1起, "path": "media/photos/{pid}/{文件名}", "width": int, "height": int }]`；**live width/height 恒 0**（上游未提供）；mock 恒 1280×1810 |
- **错误**：`3001` 章节不存在（live `MissingAlbumPhotoException` 映射；mock pid 不在 fixture）；live `2001`/`2002`。
- **mock/live 差异**：mock 图片路径为 `media/photos/{pid}/{i+1:05d}.jpg`；live 为上游真实文件名。
- **上游映射**（live）：SDK `client.get_photo_detail(pid)`，**实际展开为 `GET /chapter?id={pid}`**，默认伴随 **`GET /album?id={album_id}`**（章节顺序来源，供 hasNext/nextPid 判定）与 **`GET /chapter_view_template`**（scramble 专用 token）；`images[].path` 逐张从 `page_arr` 拼装。
- **⚠️ 备注**：前端拼图规则 `imageURL(path, scramble, aid)` → `GET /api/image?path=…&scramble=…&aid=…`（`api.js`）。

#### #16 GET /api/image

- **鉴权**：无。
- **Query**：
  - `path`（str，**必填 ≥1 字符**）：上游图片相对路径（如 `media/photos/{pid}/00001.jpg`、`media/albums/{aid}_3x4.jpg`、`media/users/{photo}`）；**不接受前导斜杠**。
  - `scramble`（str，默认 `"0"`，正则 `^(0|[1-9]\d*)$` 即非负整数字符串，非法 422）。
  - `aid`（str，默认 `""`）：切割数计算的辅助参数。
- **响应**：**图片二进制，统一 `image/jpeg`**（webp/png/gif 源经 PIL 打开，**非 RGB/L 模式才转 RGB**（L 灰度模式保留）后落盘 JPEG，动图仅保留首帧）。
- **错误**：`3001` path 非法（mock/live 双模式生效，data.msg 含原 path 截断 80 字符）；`2001` 图片下载失败/解码失败/缓存写入失败；`2002` 网络；`422` scramble 格式错。
- **mock/live 差异**：mock 返回按 aid/path 程序生成的编号占位 JPEG（封面 300×400、页面 420×560），不走缓存、不受画质影响；live 见下。
- **上游映射（live）**：
  1. path 白名单校验（见备注）；图片 URL **只由后端从 `JmModuleConfig.DOMAIN_IMAGE_LIST` 域名池构建**（取前 3 个域名依次重试），前端不可指定域名。
  2. 下载 → `JmImageTool.open_image` → 按 `_calc_num(scramble, aid, path)` 计算分割数（口径同 SDK `JmImageResp.transfer_to`；scramble=0 不分割；photo_id 优先从 path 的 `media/photos/{photo_id}/…` 解析，兜底用 aid）→ `JmImageTool.decode_and_save` 乱序还原。
  3. 落盘：`mobile/server/cache/{key}.jpg`；quality 与 PIL 默认 75 一致时直接存，否则经无损 PNG 中转后按目标质量重编码一次（避免二次有损）。
- **⚠️ 备注（安全）**：path **白名单正则 `^[A-Za-z0-9_\-./]+\.(jpg|jpeg|png|webp|gif)$`**（忽略大小写，长度 ≤300），并拒绝：绝对 URL（含 `://`，防盲 SSRF）、`..` 穿越、前导 `/`、反斜杠、其余字符——不合法一律 `3001`。这也是列表/详情接口下发 `coverUrl` 前必须剥离封面 `?u=` 缓存参数的原因。
- **⚠️ 备注（缓存）**：缓存键 **`md5("{path}|{scramble}|{quality}")`**（v1.0.3 起画质入键，切画质不命中旧缓存）；命中即刷新 mtime（真 LRU）；写入为 `.tmp` 原子 `os.replace` + 写前二次命中检查（并发同 key 复用）；按文件数 LRU 淘汰（上限 `JM_IMAGE_CACHE_LIMIT`=500，超限删最旧 mtime）。
- **⚠️ 备注（画质）**：落盘质量由 `GET/PUT /api/settings` 的 `imageQuality` 决定：**high/medium/low → JPEG quality 90/75/60**（默认 high）。

#### #17 GET /api/comics/{aid}/comments 🔒

- **鉴权**：Bearer。
- **路径参数**：`aid`；**Query**：`page`（int，默认 1，≥1，非法 422）。
- **响应 `data`**：`{ "page": 1, "total": 50, "hasNext": true, "list": [<CommentItem>] }`（页大小 20）。
- **错误**：`1002`/401；live `2001`/`2002`；`422`。
- **mock/live 差异**：
  - mock：`STATE.comments` 内存态（预置 3 部漫画的评论），新评论插入列表头部；页大小 20；预置数据的 `replies[].replyTo` 为被回复用户名。
  - live：`replyTo` 恒 `null`；`content` 经 `_strip_html`；`createdAt` 为 epoch → 东八区 `%Y-%m-%d %H:%M`；`total` 取上游 `total`（**缺失时按本页条数；上游显式 `null` 时为 0**，`_parse_count(None)`→0）。
- **上游映射**（live）：`GET /forum?mode=manhua&aid={aid}&page={page}`；使用会话客户端（携带登录 cookies；客户端缺失时兜底匿名客户端）。
- **⚠️ 备注**：**评论读取也要求登录**（与契约 🔒 一致）；内容剥 HTML 兼防 XSS（web 端按纯文本渲染）。

#### #18 POST /api/comics/{aid}/comments 🔒

- **鉴权**：Bearer。
- **路径参数**：`aid`；**请求体**（`CommentPostRequest`）：`{ "content": "str(≥1字符,必填)" }`（空 → 422）。
- **响应 `data`**：`{ "ok": true }`。
- **错误**：`1002`/401；live 上游 code≠200 → `2001`（data.upstream）；live `2002`；`422`。
- **mock/live 差异**：mock 立即写内存并回显于后续列表（发布者取会话用户名，id 形如 `c9001`，createdAt 为 ISO8601）；live 仅透传上游，**不回读**。
- **上游映射**（live）：`POST /comment`（表单 `comment`+`aid`）。
- **⚠️ 备注**：无。

#### #19 POST /api/comics/{aid}/like 🔒

- **鉴权**：Bearer。
- **路径参数**：`aid`；无请求体。
- **响应 `data`**：`{ "ok": true, "liked": true | false | null }`
- **错误**：`1002`/401；live 解码后 `status≠"ok"` → `2001`；live `2002`。
- **mock/live 差异**：mock 为 `STATE.likes` 集合按 aid **切换**（再次调用取消），恒返回 true/false；live 见备注。
- **上游映射**（live）：`POST /like`（表单 `aid`）；**上游为切换语义**（已赞则取消）——经解码 `msg` 关键字判定实际结果：含 `"取消"` → `false`；含 `"点赞"/"點讚"/"喜欢"/"喜歡"` → `true`；**均未命中 → `liked=null`**（无法判定）。
- **⚠️ 备注**：切换语义意味着调用方无法仅凭请求确定结果，必须以响应 `liked` 为准；`liked=null` 时前端宜回查详情 `liked` 字段。

### 3.4 favorite

#### #20 GET /api/favorites/folders 🔒

- **鉴权**：Bearer。
- **请求**：无参数。
- **响应 `data`**：`{ "folders": [ { "id": "0", "name": "默认收藏夹", "count": 2 }, { "id": "1", "name": "我的最爱", "count": 1 } ], "total": 3 }`
- **错误**：`1002`/401；live `2001`/`2002`。
- **mock/live 差异**：mock 固定两个收藏夹（`0`=默认、`1`=我的最爱），count 为内存态实时统计；live 为上游收藏夹（`FID`/`name`/`count`，count 经 `_parse_count` 兼容 `"1.2K"` 等）。
- **上游映射**（live）：SDK `client.favorite_folder(page=1)`；`total` 取上游 `total`。
- **⚠️ 备注**：live 的上游移动端收藏夹接口未见显式 folder 维护端点，folders 实为上游返回的收藏夹分页第一页。

#### #21 GET /api/favorites 🔒

- **鉴权**：Bearer。
- **Query**：`folderId`（str，默认 `"0"`）；`page`（int，默认 1，≥1，非法 422）。
- **响应 `data`**：`{ "page": 1, "total": 100, "hasNext": true, "list": [<ComicItem>] }`（页大小 20）。
- **错误**：`1002`/401；live `2001`/`2002`；`422`。
- **mock/live 差异**：mock `folderId="0"` 为**默认收藏夹**（与 `"1"` 平级，未知 folderId → 空列表）；live `folderId="0"` 为**全部收藏夹**（上游语义）。前端按同一参数透传，无需区分。
- **上游映射**（live）：SDK `client.favorite_folder(page=<page>, folder_id=<folderId>)`；`hasNext` 按 total+页大小 20 判定（无 max_page）。
- **⚠️ 备注**：无。

#### #22 POST /api/favorites 🔒

- **鉴权**：Bearer。
- **请求体**（`FavoriteAddRequest`）：`{ "aid": "str(≥1字符,必填)", "folderId": "str?"（缺省/None → 服务端取 "0"） }`。
- **响应 `data`**：`{ "ok": true, "favorited": bool }`
- **错误**：`1002`/401；live 解码 `status≠"ok"` → `2001`；mock aid 不在 fixture → `3001`、folderId 不是 `"0"`/`"1"` → `3001`；live `2002`；`422`。
- **mock/live 差异**：mock 严格校验 aid/folderId 存在性且 `favorited` 恒 `true`；live **忽略 folderId**（上游移动端无独立 folder 参数，对齐桌面端），且为**切换语义**。
- **上游映射**（live）：`POST /favorite`（表单 `aid`）——**切换端点**：已收藏则本次实际是取消。经解码 `msg` 关键字判定：含 `"取消"` → `favorited=false`；含 `"收藏"` → `true`；**无法判定 → `true`**（乐观默认）。
- **⚠️ 备注**：POST 的"添加"在 live 下可能实际执行"取消"；需要精确状态时回查 `GET /api/comics/{aid}` 的 `favorited`。

#### #23 DELETE /api/favorites/{aid} 🔒

- **鉴权**：Bearer。
- **路径参数**：`aid`；无请求体。
- **响应 `data`**：`{ "ok": true, "favorited": bool | null }`
- **错误**：`1002`/401；live 解码 `status≠"ok"` → `2001`；live `2002`。
- **mock/live 差异**：mock **不校验 aid 存在性**（未知 aid 也返回 ok）；`favorited`：此前确有收藏 → `false`，本就没有 → `null`。live：经 `msg` 关键字判定（同 #22），**无法判定 → `false`**。
- **上游映射**（live）：与 #22 完全相同的上游 `POST /favorite` 切换端点。
- **⚠️ 备注**：docs/API.md §6 称 DELETE "无法判定时为 `null`"——**与 live 代码不符**（live 无法判定时为 `false`；`null` 仅出现在 mock），见第 5 节差异 #2。

### 3.5 history（本地持久化，mock/live 共用同一份数据）

#### #24 GET /api/history 🔒

- **鉴权**：Bearer。
- **Query**：`page`（int，默认 1，≥1，非法 422）。
- **响应 `data`**：`{ "list": [ { "aid": "str", "title": "str", "coverUrl": "str", "pid": "str", "epTitle": "str|null", "imageIndex": 1, "updatedAt": "ISO8601" } ], "page": 1, "total": 12, "hasNext": true }`
  - 排序：按 `updatedAt` 倒序；页大小 **20**；记录以 `aid+pid` 为幂等键。
- **错误**：`1002`/401；`422`。
- **mock/live 差异**：无（同一本地存储）。
- **上游映射**：无（`mobile/server/data/history.json`）。
- **⚠️ 备注**：历史为**设备级共享数据**（同一后端实例上所有账号共用一份，不按账号隔离；鉴权仅作准入）。

#### #25 POST /api/history 🔒

- **鉴权**：Bearer。
- **请求体**（`HistoryPostRequest`）：`{ "aid": "str(≥1字符,必填)", "title": "str(默认\"\")", "coverUrl": "str(默认\"\")", "pid": "str(≥1字符,必填)", "epTitle": "str?", "imageIndex": int(默认 1) }`
- **响应 `data`**：`{ "ok": true }`。
- **错误**：`1002`/401；`422`（aid/pid 缺失或空、imageIndex 非整数）。
- **mock/live 差异**：无。
- **上游映射**：无。
- **⚠️ 备注**：**幂等覆盖**——同 `aid+pid` 记录覆盖并刷新 `updatedAt`（本地时区 ISO8601，秒精度）；`imageIndex` 为 1 基页码，服务端不做范围校验。前端 reader.js 阅读中节流上报（1.2s 防抖）。

#### #26 DELETE /api/history 🔒

- **鉴权**：Bearer。
- **请求**：无请求体。
- **响应 `data`**：`{ "ok": true }`（清空全部）。
- **错误**：`1002`/401。
- **mock/live 差异**：无。
- **上游映射**：无。
- **⚠️ 备注**：无单条删除端点（代码未提供）。

### 3.6 settings（本地持久化，mock/live 共用）

#### #27 GET /api/settings 🔒

- **鉴权**：Bearer。
- **请求**：无参数。
- **响应 `data`**：`{ "proxy": "http://127.0.0.1:10809", "imageQuality": "high", "mock": false }`
  - 默认值：`proxy` 取 `JM_PROXY`（未设置 → `http://127.0.0.1:10809`）；`imageQuality` 默认 `high`。
  - **旧版遗留画质值（如 `"original"`）读取时归一化为 `high`**（保证档位恒合法）；`mock` 恒反映当前运行模式。
- **错误**：`1002`/401。
- **mock/live 差异**：无（同一存储；`mock` 字段随模式变化）。
- **上游映射**：无。
- **⚠️ 备注**：仅返回白名单键（`proxy/imageQuality/mock`），其余落盘键不回显。

#### #28 PUT /api/settings 🔒

- **鉴权**：Bearer。
- **请求体**（`SettingsPutRequest`，字段均可选）：`{ "proxy": "str?", "imageQuality": "high|medium|low"? }`
  - `imageQuality` 为 Literal 枚举，其他值 → 422；`proxy` 传 **空串 = 清除代理**（直连）。
  - `threadNum` 等旧字段被**静默忽略**（不报错、不落盘，v1.0.3 移除）。
- **响应 `data`**：同 GET（写入后的完整设置）。
- **错误**：`1002`/401；`422`。
- **mock/live 差异**：live 模式下 `proxy` 变更**即时生效**：`BACKEND.invalidate()` 重建匿名上游客户端；已登录会话客户端在下次请求时惰性重建并搬运 cookies（保持登录态）。mock 模式无上游，无此动作。
- **上游映射**：无直接调用；间接决定 live 所有上游请求与图片下载的代理，以及 `/api/image` 落盘画质（缓存键已含画质）。
- **⚠️ 备注**：设置落盘 `mobile/server/data/settings.json`（tmp 原子写）。

### 3.7 user（每日签到，v1.0.3 追加）

#### #29 GET /api/user/sign 🔒

- **鉴权**：Bearer。
- **请求**：无参数。
- **响应 `data`**：`{ "dailyId": 260901, "todaySigned": false, "days": [ { "date": 1, "signed": true }, … ] }`
  - `dailyId`：签到活动 id（POST 时使用）；live 取上游 `daily_id`（缺失/非法 → 0）。
  - `todaySigned`：今日是否已签。
  - `days`：本月签到记录；`date` 为"本月第几日"（int），上游未返回的日期不出现在列表中。
- **错误**：`1002`/401；live 上游 code≠200/解密失败 → `2001`；live `2002`。
- **mock/live 差异**：mock 的 `dailyId` 为按月确定性构造 `f"{YY:02d}{MM:02d}01"`（如 2026-09 → `260901`），`days` 覆盖 1 日至今日，签到状态取内存 `STATE.sign_days`；live 为上游真实记录。
- **上游映射**（live）：`GET /daily?user_id={uid}`（对齐桌面端 GetDailyReq2；AES 解密 `decoded_data`）；解析 `daily_id` 与 `record=[[{date,signed}],…]`；`todaySigned` 以 `date == 今天.day` 的 `signed` 为准。`uid` 取会话 `userInfo.userId`。
- **⚠️ 备注**：`date` 语义为"当月第几日"而非日期串（对齐桌面端签到日历）。

#### #30 POST /api/user/sign 🔒

- **鉴权**：Bearer。
- **请求**：无请求体。
- **响应 `data`**：`{ "ok": true, "msg": "签到成功" | "今日已签到" | <上游 msg> | "" }`（**空串**：live 解密失败/上游未返回 msg 时，`decoded.get("msg") or ""`）
- **错误**：`1002`/401；live `dailyId<=0` → `2001`（"缺少 daily_id，无法签到"）；live 上游 code≠200 → `2001`；live `2002`。
- **mock/live 差异**：mock 当日首次 → `msg="签到成功"`（写入 `STATE.sign_days`），当日重复 → 幂等成功 `msg="今日已签到"`；live 见下。
- **上游映射（live）**：**先查后签**（保证幂等）：① `GET /daily?user_id={uid}` 取状态；② `todaySigned=true` → 直接返回 `{ok:true, msg:"今日已签到"}`（不调上游签到）；③ 否则取 `dailyId`，`<=0` → `2001`；④ `POST /daily_chk`（表单 `user_id`+`daily_id`，对齐桌面端 SignDailyReq2）→ `msg` 为上游返回信息（解密失败时为空串）。
- **⚠️ 备注**：签到幂等由"先查状态再决定是否调上游"实现，重复调用不会向上游重复提交。

---

## 4. 前端消费对照表（mobile/web/js/api.js ↔ 服务端端点）

`api.js` 共封装 **28 个 Api 方法 + 2 个 URL 拼装助手**。逐一核对视图层调用（`views/*.js`、`main.js`），结果如下：

| api.js 方法/助手 | 端点 | 视图层调用方 | 核对结论 |
|---|---|---|---|
| `Api.health` | #1 GET /api/health | settings.js（×3）、main.js（启动探活） | 一致 |
| `captchaURL(bust)` | #2 GET /api/auth/captcha | login.js（`<img src>`，bust 加 `?r=` 时间戳） | 一致 |
| `Api.login(username,password,captcha)` | #3 POST /api/auth/login | login.js | 一致（无 captcha 时不下发该字段） |
| `Api.profile` | #4 GET /api/auth/profile | settings.js | 一致 |
| `Api.logout` | #5 POST /api/auth/logout | settings.js | 一致 |
| `Api.categories` | #6 GET /api/categories | — | **已封装，当前无视图调用（暂无前端消费方）** |
| `Api.comicsIndex(page,order)` | #7 GET /api/comics/index | — | **已封装，当前无视图调用（暂无前端消费方；首页 tabs 已改用 promote/latest/serialization）** |
| `Api.promote` | #8 GET /api/comics/promote | home.js | 一致 |
| `Api.latest(page)` | #9 GET /api/comics/latest | home.js | 一致 |
| `Api.serialization(day,type,page)` | #10 GET /api/comics/serialization | home.js | 一致 |
| `Api.week` | #11 GET /api/comics/week | week.js | 一致 |
| `Api.weekFilter(id,type)` | #12 GET /api/comics/week/filter | week.js | 一致 |
| `Api.search(params)` | #13 GET /api/comics/search | search.js | 一致（分类模式传 `mainCategory/sort/page`；关键字模式传 `keyword/sort/page`，`searchType≠site` 才下发；`m` 仅与 `y` 同传；空值参数 buildQuery 自动省略） |
| `Api.comicDetail(aid)` | #14 GET /api/comics/{aid} | detail.js | 一致 |
| `Api.photos(pid)` | #15 GET /api/photos/{pid} | reader.js | 一致 |
| `imageURL(path,scramble,aid)` | #16 GET /api/image | reader.js（阅读页逐图） | 一致（**components.js 不调用 `imageURL`**，仅经 `resolveURL` 消费服务端下发的 `coverUrl`） |
| `Api.comments(aid,page)` | #17 GET /api/comics/{aid}/comments | detail.js | 一致 |
| `Api.addComment(aid,content)` | #18 POST /api/comics/{aid}/comments | detail.js | 一致 |
| `Api.like(aid)` | #19 POST /api/comics/{aid}/like | detail.js（按响应 `liked` 翻转 UI） | 一致 |
| `Api.favoriteFolders` | #20 GET /api/favorites/folders | favorites.js | 一致 |
| `Api.favorites(folderId,page)` | #21 GET /api/favorites | favorites.js | 一致 |
| `Api.addFavorite(aid,folderId?)` | #22 POST /api/favorites | detail.js（未传 folderId，服务端补 `"0"`） | 一致 |
| `Api.removeFavorite(aid)` | #23 DELETE /api/favorites/{aid} | detail.js | 一致 |
| `Api.historyList(page)` | #24 GET /api/history | history.js、detail.js（继续阅读） | 一致 |
| `Api.reportHistory(payload)` | #25 POST /api/history | reader.js（节流上报） | 一致 |
| `Api.clearHistory` | #26 DELETE /api/history | history.js、settings.js | 一致 |
| `Api.getSettings` | #27 GET /api/settings | settings.js | 一致 |
| `Api.putSettings(patch)` | #28 PUT /api/settings | settings.js（分别提交 `imageQuality`/`proxy`） | 一致 |
| `Api.signStatus` | #29 GET /api/user/sign | settings.js | 一致 |
| `Api.sign` | #30 POST /api/user/sign | settings.js | 一致 |

**核对结论**：
- 服务端 30 个端点中，28 个有明确的视图层消费方；#6 `categories` 与 #7 `comicsIndex` 仅有 api.js 封装、无视图调用（**暂无前端消费方**，属历史封装保留）。
- 前端无"调用了文档外端点"的情况；`sw.js` 对 `/api` 请求不做缓存拦截，`api.js` 另对 HTTP 401/422 有专门分支（与本契约的 1002→401、参数校验→422 对应）。
- api.js 顶部注释"`mainCategory/mainCategoryName 仅 Mock 模式生效`"已过时：**mainCategory 自 v1.0.4 起 live 模式经上游 `/categories/filter` 真正生效**；仅 `mainCategoryName` 仍为 mock-only（见第 5 节差异 #4）。

---

## 5. 与 docs/API.md 的差异点（以代码为准）

逐条对照中发现的文档与代码不一致（本文均已按代码口径书写）：

| # | 位置 | docs/API.md 表述 | 代码实际行为 | 结论 |
|---|---|---|---|---|
| 1 | docs/API.md §13.2（`/api/image` 参数） | "`path` 接受上游相对路径（…）**或完整 URL**" | `core/imagepath.py` **明确拒绝一切绝对 URL**（含 `://`，防 SSRF），且与该文档 §0/§5 的白名单描述自相矛盾 | **以代码为准**：不接受完整 URL；该句为遗留笔误 |
| 2 | docs/API.md §6（DELETE /api/favorites/{aid}） | 追加字段 `favorited` "无法判定时为 `null`" | live `delete_favorite` 无法判定时返回 **`false`**（`bool(None)→False` 分支）；`null` 仅出现在 **mock** 的"原本未收藏"分支 | **以代码为准**：live 无法判定 → `false`；mock 未收藏 → `null` |
| 3 | docs/API.md §5 / §13.3 | "`episodes[].imageCount`：上游专辑接口不含图片总数，**恒为 0**" | live `detail()` 的 episodes 元素**完全不含 `imageCount` 字段**（只有 `pid/title/order`）；仅 mock 返回该字段 | **以代码为准**：live 下该字段缺失（非 0）；前端应按可缺省处理 |
| 4 | mobile/web/js/api.js 注释 | "`mainCategory/mainCategoryName` 仅 Mock 模式生效" | v1.0.4 起 `mainCategory` 在 live 经上游 `/categories/filter` 生效（`live.search` 消费）；`mainCategoryName` 才是 mock-only（live 无此参数） | **以代码为准**；api.js 注释滞后 |
| 5 | docs/API.md §11（like 关键字） | msg 关键字 "`取消`→false，`点赞/喜歡`→true" | live 代码判定词为 `取消` / `点赞`、`點讚`、`喜欢`、`喜歡`（**含简体 `喜欢`**） | **以代码为准**，文档漏列 `喜欢`；不影响语义理解 |

除上述 5 处外，`docs/API.md` 与代码的字段、校验规则、错误码、上游映射核对一致。

---

## 6. 私有扩展：批量下载（`/api/jm/downloads`，仅内置 Go 服务实现）

本节不属于上游移动端 30 端点契约，是 nowen-reader fork 的**本地扩展能力**：把在线漫画整本(或选定章节)抓取到本地，**打包为 zip 归档到书库目录**，并清理下载临时文件夹。
鉴权只用 nowen 登录（`/api/jm` 组级 `middleware.AuthRequired()`，与 #27/#28 同口径，**不需要 JM token**；游客 JM 会话也能下载）。

### 6.1 端点总览

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/jm/downloads/dirs` | 下载目录候选（书库管理中的漫画/混合书库根路径 + 内置测试目录） |
| GET | `/api/jm/downloads` | 任务列表（新→旧，含已结束；`tempRoot` 为临时沙箱根） |
| POST | `/api/jm/downloads` | 新建下载任务（异步执行，立即返回任务快照） |
| GET | `/api/jm/downloads/:id` | 单任务进度 |
| POST | `/api/jm/downloads/:id/cancel` | 取消任务（排队中/下载中/打包中均可） |
| DELETE | `/api/jm/downloads/:id` | 移除任务记录（不动已归档 zip） |

响应统一 `{code,msg,data}` 包装；参数非法为 **HTTP 422** `{"detail": ...}`；无目录管理权限为 **HTTP 403**；任务 id 不存在为 `code=3001`。

### 6.2 `GET /api/jm/downloads/dirs`

```json
{
  "dirs": [
    {
      "label": "漫画库",
      "path": "D:\comics",
      "kind": "library",
      "libraryId": "…",
      "libraryType": "comic",
      "canManage": true,
      "isDefault": false,
      "exists": true
    },
    { "label": "测试目录(不入库)", "path": "<DataDir>\jm\download-test", "kind": "test", "canManage": true, "isDefault": true, "exists": false }
  ],
  "testDir": "<DataDir>\jm\download-test"
}
```

- 候选来源：`书库管理` 中 `enabled=true` 且 `type` 为 `comic`/`mixed` 的书库根路径（多根路径逐个列出，标签带序号）；末尾固定追加内置**测试目录**（`kind=test`，不参与入库扫描，便于先验证效果）。
- `canManage` 为当前用户对该书库的管理权限（管理员恒为 true），**false 时前端置灰、后端 403 拒绝**。
- 默认项：`#28` 设置里的 `downloadDir` 命中候选则用其，否则测试目录。

### 6.3 `POST /api/jm/downloads`

请求体：

```json
{ "aid": "1477967", "title": "作品名", "author": "作者", "pids": ["123", "124"], "destDir": "D:\comics" }
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `aid` | 与 `pids` 至少一者 | 漫画 id；提供时后端会拉一次 `#14 详情` 以获得标题/作者/章节名与顺序 |
| `pids` | 与 `aid` 至少一者 | 指定章节（按给定顺序下载）；缺省 = 全部章节 |
| `title`/`author` | 否 | 展示与 zip 命名用；缺省用详情返回值 |
| `destDir` | 是 | **必须在 `dirs` 候选白名单内且 `canManage=true`**，否则 422/403；绝对路径 |

成功返回任务快照（同 6.4）。提交成功后会把 `destDir` 记入 `#28` 设置的 `downloadDir`（下次默认选中）。

### 6.4 任务快照

```json
{
  "id": "0b45878e8e6e866d",
  "aid": "1477967",
  "title": "作品名",
  "author": "作者",
  "tags": ["巨乳", "JK", "校园"],
  "destDir": "D:\comics",
  "destLabel": "漫画库",
  "libraryId": "…",
  "status": "running",
  "error": "",
  "warning": "",
  "chapters": [
    { "pid": "123", "title": "第01话 序章", "order": 1, "state": "done", "total": 42, "done": 42 }
  ],
  "totalImages": 308,
  "doneImages": 120,
  "zipName": "",
  "zipPath": "",
  "zipSize": 0,
  "createdAt": "2026-09-30T22:40:02",
  "updatedAt": "2026-09-30T22:41:10"
}
```

- `status`：`queued`（排队）→ `running`（抓图）→ `packing`（打包/归档）→ `done` / `failed` / `canceled`。
- `chapters[].state`：`pending` / `running` / `done` / `failed`；`done` 章为成功页数，`failed` 章附 `error`。
- 部分章节失败但至少一章成功 → `status=done` 且 `warning` 列出失败章节（zip 已生成）；全部失败 → `failed` + `error`。
- 完成后 `zipName` / `zipPath` / `zipSize` 有值；前端按 1.5s（有活动任务）或 15s（空闲）轮询 `GET /api/jm/downloads`。
- `tags`：任务启动抓详情时捕获的 JM 标签（trim/去重/上限 30），入库自动打标用（见 6.5⑨）；详情抓取失败或按 pids 下载无详情时缺省省略。
- **空值字段按 `omitempty` 省略**（`error`/`warning`/`tags`/`zipName`/`zipPath`/`zipSize`/`chapters[].error` 等）；`totalImages` 在收尾统一汇总，抓图过程中始终以 `chapters[].total/done` 为准。

### 6.5 归档与清理规则

1. **抓图**：复用 `#15 章节 + #16 图片管线`（域名池下载 → 乱序还原 → 统一 JPEG），单页失败重试 3 次，整章失败页再补抓 1 轮。
2. **落盘**：全部中间产物写在自有沙箱 `<DataDir>/jm/download-tmp/<taskId>/`（与目标目录无关）。
3. **命名**（移植 JMComic-qt `ToolUtil.GetCanSaveName`）：删除 `\ / : * ? " < > |` 与控制字符 → 去尾部 `.` → 去首尾空格 → **截断 83 字符**（`254//3-1`，CJK 3 字节 × 83 ≈ 249 ≤ 255）→ 再去尾部 `.`/空格；清洗后为空回退 `aid`/`untitled`；并按目标目录收紧**全路径 ≤ 240 字节**（Windows MAX_PATH 兜底）。
4. **目录结构**：章节目录 `第NN话[_标题]`（NN 按总章数补零，便于自然排序），页面 `0001.jpg`…（4 位零填充，阅读顺序）。
5. **打包**：`zip` + `ZIP_DEFLATED`，条目为 `第NN话/0001.jpg`（相对路径，正斜杠）。
6. **归档**（仅对库目录）：`<destDir>/<清洗后的标题>.zip`；同名自动追加 ` (2)`…` (99)`，**绝不覆盖或删除目标目录中的既有文件**。
7. **清理**：任务结束（成功/失败/取消）后整体删除沙箱 `<taskId>/` 目录 —— 即“打包成 zip 后清理下载文件夹”。
8. **入库**：`destDir` 属于某个书库时，归档后异步触发该书库扫描（全局同时只允许一个扫描，冲突时最多退避重试 3 次 × 15s）。
9. **自动标签**（入库后增强，`#28` `downloadTags` 开关控制，默认开）：扫描触发后轮询等待归档 zip 对应的 Comic 记录产生（`PathToID(libraryID, zipName)` 确定性定位，2s 间隔、最长 90s），然后 `AddTagsToComic` 把任务快照的 `tags` 挂到书库漫画（标签不存在自动创建、幂等）。测试目录（非书库）不入库也不打标；轮询超时仅记日志放弃，不影响下载结果；分类**不**自动写。

### 6.6 并发与限流

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `JM_DOWNLOAD_TASK_CONCURRENCY` | `2` | 同时下载的漫画数（批量下载时排队） |
| `JM_DOWNLOAD_CONCURRENCY` | `3` | 单章内并发抓图数（章节之间串行） |

任务表为**进程内存态**（保留最近 60 条），服务重启即清空；已归档 zip 不受影响。

### 6.7 前端消费对照

| 入口 | 位置 | 行为 |
|---|---|---|
| 漫画详情页「下载」按钮 | `app/jm/comic/[aid]/page.tsx` | 打开对话框：章节多选（默认全选）+ 归档目录下拉；并有「下载中 N」快捷入口 |
| 在线阅读页左下浮动按钮 | `app/jm/reader/[pid]/page.tsx` | 「下载整本 / 下载本章」+ 任务面板（右下角仍是章节导航，互不遮挡） |
| 列表页多选批量下载 | `app/jm/page.tsx`、`search`、`week`、`favorites` | 网格包裹 `JmBatchSelectionProvider` 后出现「多选下载」：点封面勾选 → 一次为每部漫画建一个任务（全部章节） |
| 在线漫画页页头「下载任务」按钮 | `app/jm/page.tsx`、`search`、`week`、`favorites`、`history` 的 `PageHeader actions` | 角标显示进行中任务数,点击打开任务面板查看下载队列 |
| 任务面板 | `components/jm/download/DownloadTasks.tsx` | 进度条 / 章节明细 / 自动标签预告 / 取消 / 移除 / 归档路径(页头按钮、详情页、阅读页、设置页共用同一份任务状态) |
| 设置 · 在线漫画源 · 漫画下载 | `components/settings/JmSourcePanel.tsx` | 默认下载目录下拉（书库管理目录 + 测试目录）与任务面板入口；`#27/#28` 增加 `downloadDir`、`downloadTags`（下载后自动添加标签开关，默认开）字段 |

### 6.8 测试

```bash
# 单元测试(名称清洗/打包/归档去重)
go test ./internal/jm/

# 真实上游端到端(需本地代理可用):抓图 → 打包 → 清理沙箱 → 校验 zip
JM_LIVE_TEST=1 go test ./internal/jm/ -run TestLiveDownloadPipeline -v -timeout 30m

# HTTP 层端到端(独立 DATABASE_URL/DATA_DIR,不触碰开发库):注册→建库→批量下载→入库扫描
bash scripts/jm-download-e2e.sh
```

---

## 7. 私有扩展：标签收藏（`/api/jm/tag-favorites`，仅内置 Go 服务实现）

本节不属于上游移动端 30 端点契约，是 nowen-reader fork 的**本地扩展能力**：把详情页看到的标签收藏起来，
在搜索页/标签收藏页**一键按标签搜索**（复用 #13 `searchType=tag&keyword=<标签>`，不新增上游端点）。
鉴权只用 nowen 登录（`/api/jm` 组级 `middleware.AuthRequired()`，与 #27/#28 同口径，**不需要 JM token**）：
浏览与搜索本就匿名可用，收藏标签跟随；数据为**设备级共享**（同 #24 阅读历史口径），持久化于
`<DataDir>/jm/tag-favorites.json`。

### 7.1 端点总览

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/jm/tag-favorites` | 已收藏标签列表（全量，createdAt 倒序） |
| POST | `/api/jm/tag-favorites` | 收藏标签（同 tag 幂等，不刷新收藏时间） |
| DELETE | `/api/jm/tag-favorites?tag=` | 取消收藏（tag 不存在幂等成功） |

### 7.2 GET /api/jm/tag-favorites — 列表

**响应 data**

```json
{
  "list": [{ "tag": "巨乳", "createdAt": "2026-10-02T23:30:00" }],
  "total": 1
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| list | array | `JmTagFavorite`，按 `createdAt` 倒序（同秒并列保持入库顺序，稳定排序） |
| list[].tag | string | 标签文本（后端 trim，无首尾空白） |
| list[].createdAt | string | 本地时区 ISO8601 秒精度（同 #24 updatedAt 口径） |
| total | int | = list.length |

### 7.3 POST /api/jm/tag-favorites — 收藏

**请求体** `{ "tag": "巨乳" }`

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| tag | string | ✅ | trim 后 1..64 字符，否则 422 |

**响应 data** `{"ok": true}`；重复收藏幂等成功（列表不重复、时间不刷新）。

**错误**：`422 {"detail": "tag 必填" | "tag 过长(上限 64 字符)"}`。

### 7.4 DELETE /api/jm/tag-favorites?tag= — 取消收藏

**Query** `tag`（必填，URL 编码；走 query 而非路径参数，规避中文/特殊字符路径编码问题）。

**响应 data** `{"ok": true}`；tag 不存在幂等成功。
**错误**：`422 {"detail": "tag 必填"}`。

### 7.5 前端消费对照

| 入口 | 位置 | 行为 |
|---|---|---|
| 详情页标签 | `app/jm/comic/[aid]/page.tsx` | 标签可点选中 → 「搜索」跳 `/jm/search?keyword&searchType=tag`、「收藏/已收藏」切换（乐观更新，失败回滚）；已收藏标签带 ★ 角标 |
| 搜索页「我的标签」 | `app/jm/search/page.tsx` | 表单上方 chips：点击即按该标签搜索、× 就地取消收藏（失败静默）、「管理」进 /jm/tags；URL 参数初始化表单并自动首搜 |
| 标签收藏页 | `app/jm/tags/page.tsx`（路由 `/jm/tags`） | chips 管理：点击跳搜索、× 取消收藏（失败回滚+toast）；空态引导到详情页收藏 |
| 在线首页入口 | `app/jm/page.tsx` UserCard 快捷区 | 「标签收藏」入口（Tag 图标） |

### 7.6 测试

```bash
# 存储层单测(增查删幂等/trim/倒序/重开重读)
go test ./internal/jm/ -run TestTagFavorite -v
```
