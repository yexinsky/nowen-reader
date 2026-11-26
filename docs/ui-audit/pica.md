# NowenReader 哔咔 Pica 模块 只读界面审查报告

- 目标站点:<http://<instance>:6680> 版本 `dev-93793dd`
- 审查范围:`/pica`(4 个 Tab)、`/pica/search`、`/pica/favorites`、`/pica/history`、`/pica/comic/[id]`、`/pica/reader/[id]`、`/pica/login`
- 视口:桌面 `1440x900`、移动 `390x844`(DPR 2,等价 CSS 390 宽)
- 审查口径:只记录三类 —— **①按钮无效**、**②PC/移动端界面显示不正常**、**③字体错乱或重复性显示**
- 方式:CDP 无头 Chrome 真实鼠标点击(`Input.dispatchMouseEvent`)+ `elementFromPoint` 命中测试 + 几何量测 + 截图;批量指标(`overflowX` / `overflowOffenders` / `brokenImages` / `mojibake` / `tinyFonts` / `clipped` / `duplicateAdjacent` / `outOfViewportFixed` / `exceptions` / `consoleErrors`)。**只读**,未修改任何代码,未点任何破坏性按钮(原生 `confirm` 一律取消)
- 证据目录:`<audit-root>/audit\pica\` —— 共 **217 张截图 + 131 份原始量测 JSON**;本报告引用的 89 个证据文件均已核对存在

---

## 零、必须先读的环境前提(决定本报告的边界)

1. **容器内没有哔咔会话**,`/api/pica/*` 内容接口统一 401 `{"code":1002,"msg":"登录已失效,请重新登录"}`;`PicaGate` 会把内容页拦到「登录哔咔漫画」卡片。这是**未登录态**,不是缺陷,详见第二章。
2. **即使有 token 也拿不到内容**:服务端哔咔上游代理 `127.0.0.1:10809` 未监听,任何 token 调内容接口都返回 `{"code":2002,"msg":"网络不可达,请检查代理设置"}`(`/api/pica/settings` 里 `proxy=http://127.0.0.1:10809`、`proxyRequired=true`)。因此「真实内容列表 / 真实详情 / 真实阅读页」在本环境**不可达**。
3. 为了让**列表、详情、阅读器的正常态布局**也能被审查,本次用 CDP `Fetch.fulfillRequest` 在浏览器内存里拦截 `*/api/pica/*` 返回**形状一致的内存 mock**(`code:0` + 真实字段名,封面为内联 SVG),不写服务端、不改代码。凡是用 mock 的结论均在证据列注明。
4. `/api/pica/history` 是**本地持久化**(与上游无关),因此历史页可以造真实行数据审查。本次曾 POST 14 条 `【审计临时】…` 种子行用于排版量测,**已全部 `DELETE /api/pica/history` 清理并复核 `total=0`**。

---

## 一、问题清单(共 5 条)

| # | 页面 / 路由 | 视口 | 类型 | 现象 | 严重度 | 关键证据 |
|---|---|---|---|---|---|---|
| P1 | `/pica/reader/[id]` 阅读器 | 桌面 1440x900 + 移动 390x844 | **① + ②** | 工具栏展开时,左下「批量下载」、右下「章节导航」两个浮动按钮**完全落在工具栏底栏矩形内**(z=30 < 底栏 z=50)。点「章节导航」实际点亮了工具栏的**夜间/日间**开关(theme 由 `dark` → `light`,`chapterPanelOpen` 仍为 false);点「批量下载」落到工具栏的**单页/模式段**按钮。等 4.2s 工具栏自动隐藏后同一坐标才正常(`chapterPanelOpen=true`)。视觉上圆图标与「夜间」「单页」「自动翻页」文字糊在一起 | 高 | `ev3-fab-d.json`、`ev-zlayers-d.json`、`ev-zlayers-m.json`、`ev2-fab-m.json`、`zoom-fab-covered-right-d.png`、`zoom-fab-covered-left-d.png`、`zoom-fab-right-m.png`、`zoom-reader-bottomright-m.png` |
| P2 | `/pica/comic/[id]` 详情 | 桌面 + 移动 | **②**(含 ① 的次要项) | 该页**唯一没有套 `PicaGate`** 的 pica 内容页:未登录时不跳登录卡,而是整屏 `min-h-dvh` 居中一行灰字「登录已失效,请重新登录」+ [重试][返回],**整个 App 外壳(侧栏/页头/底部导航)全部消失**,纯黑背景孤立文字 —— 与其余 pica 页统一跳 `/pica/login?from=` 的口径不一致,且把「未登录」说成「登录已失效」。另外:连点「重试」无任何状态变化(文案不变、无 loading);点「返回」在直接打开(无历史)时跳到**首页 `/`** 而不是 `/pica` | 高 | `d-pica-comic.png`、`m-pica-comic.png`、`ev-comic-unauth2-d.json`、`ev-comic-unauth2-m.json`、`ev-jmcomic-unauth-d.json`(JM 同位置会跳登录页,作对照) |
| P3 | `/pica/history` 行内时间、`/pica/comic/[id]` 评论区时间 | 桌面 + 移动 | **③** | 时间戳**原样输出 ISO 字符串**,未做任何本地化/格式化:历史行显示 `2026-11-26T18:43:07`(带 `T`、无时区)、评论显示 `2026-11-20T10:00:00Z`(带 `T` 和 `Z`)。两处字号都是 `text-[11px]`,与同排中文混排时 `T`/`Z` 裸露,观感即「字体错乱」 | 中 | `d-pica-history-14.png`、`m-pica-history-14.png`、`d-mock-comic.png`、`m-mock-comic.png` |
| P4 | `/pica/comic/[id]` 下载弹层标题(移动端) | 移动 390x844 | **② + ③** | 标题 `下载《…》` 所在 `<h3>` 同时带 `flex` 与 `truncate`;`text-overflow:ellipsis` 对 flex 容器不生效,长标题被**硬切**(仅容器 316px、内容 370px),末尾既无省略号也切在「是否正」的半个字上,直接顶到右侧关闭 × | 低 | `ev-trunc-comic-m.json`、`ev-dlsheet-m.png`、`zoom-dlsheet-title-m.png` |
| P5 | `/pica/search` 搜索按钮 | 桌面 + 移动 | **①** | 关键词为空且未选分类时点「搜索」是一个**纯 no-op**:URL 不变、无 toast、无提示、输入框下方无任何反馈,用户无法判断按钮是否生效(需先输入关键词或选分类才有效果) | 低 | `ev-search-empty-d.json`、`ev-search-empty-m.json` |

### P1 阅读器浮动按钮被工具栏压住并触发错误功能(详细)

- 几何(工具栏展开,**桌面 1440x900**):

  | 元素 | 位置 | z-index | 结果 |
  |---|---|---|---|
  | 工具栏底栏 `.reader-toolbar-surface` | `l=0 t=754 w=1440 h=146` `position:fixed` | **50** | — |
  | 「章节导航」FAB | `l=1380 t=836 44x44` `fixed` | **30** | 落在底栏矩形内 |
  | 「批量下载」FAB | `l=16 t=836 44x44` `fixed` | **30** | 落在底栏矩形内 |

- 几何(工具栏展开,**移动 390x844**):底栏 `l=0 t=710 w=390 h=134 z=50`;两个 FAB 均 `t=780 44x44 z=30` ⇒ 44×44 **全部**落在底栏内。
- 命中测试 `elementFromPoint(FAB 中心)`:
  - 桌面:`hitNav = "夜间"`、`hitDl = "单页"`(命中工具栏按钮,不是 FAB)
  - 移动:`hitNav = "svg"`(属于工具栏模式段的图标)、`hitDl = "path"`
- 真实点击后果(桌面 `ev3-fab-d.json`):
  1. `tap:0.5:0.5` 展开工具栏 → `toolbarOpacity=1, toolbarVisible=true, hitAtNav="夜间", themeClass="dark"`
  2. `tapPx:1402:858`(章节导航中心)→ **`themeClass: dark → light`**,`chapterPanelOpen` 仍 `false` ⇒ 点「章节导航」变成了切换日/夜主题
  3. `wait:4200`(工具栏自动隐藏,`TOOLBAR_AUTO_HIDE_MS=4000`)→ `hitAtNav="章节导航"`
  4. 同一坐标再点 → `chapterPanelOpen: true` ✔ ⇒ **按钮本身没坏,是工具栏展开时的层级/位移问题**
- 移动端(`ev2-fab-m.json`)同型:工具栏展开时点 `(352,802)` 同样 `dark → light` 且 `chapterPanelOpen:false`;4.2s 后再点 → `chapterPanelOpen:true`。
- 视觉证据:`zoom-fab-covered-right-d.png`(「夜间」两字被章节导航的层叠图标压住,文字与图标糊成一坨)、`zoom-fab-covered-left-d.png`(「单页」按钮上叠着下载圆图标)、`zoom-fab-right-m.png` / `zoom-reader-bottomright-m.png`(移动端章节目录图标压在「自动翻页」的三角上)。
- 代码位置:`app\pica\reader\[id]\page.tsx:514`(`className={`fixed bottom-5 right-4 z-30 ...`}`)与 `:592`(同款 `fixed bottom-5 left-4 z-30`),**没有任何「工具栏可见时上移」的分支**;对照 JM 阅读器 `app\jm\reader\[pid]\page.tsx:559` 至少有 `toolbarVisible ? "bottom-24" : "bottom-5"`。工具栏底栏 `ReaderToolbar.tsx:339-390` 为 `fixed bottom-0 z-50`,层级天然高于 FAB。
- 影响:点「章节导航」会**误改夜间模式**(白天模式下突然切白/黑),点「批量下载」会**误切单页/双页/长条模式**,两个功能在工具栏可见期间完全不可用。

### P2 `/pica/comic/[id]` 未登录态丢失整个 App 外壳(详细)

- 实测(清空 `pica_token` 后直接打开 `/pica/comic/58266`):
  - 桌面 `d-pica-comic.png`(1440x900,仅 10KB):**整屏纯黑**,画面中央一行灰字「登录已失效,请重新登录」,其下蓝色「重试」+ 描边「返回」。侧栏、页头、底部导航**全部不存在**。
  - 移动 `m-pica-comic.png`:同样整屏、同样只有那一行字 + 两个按钮。
  - 同视口对照:`/pica`、`/pica/search`、`/pica/favorites`、`/pica/history`、`/pica/reader/*` 未登录时**统一重定向**到 `/pica/login?from=<原路径>`,并保留完整侧栏/页头(登录卡居中)。
- 为什么只有它:全量扫描 `app/pica/**/page.tsx` 的 `PicaGate|PicaAuthGuard` 覆盖

  | 路由 | 守卫 | 未登录表现 |
  |---|---|---|
  | `/pica` | ✔ GATED | 跳 `/pica/login?from=%2Fpica` |
  | `/pica/search` | ✔ GATED | 跳登录页 |
  | `/pica/favorites` | ✔ GATED | 跳登录页 |
  | `/pica/history` | ✔ GATED | 跳登录页 |
  | `/pica/reader/[id]` | ✔ GATED | 跳登录页 |
  | `/pica/comic/[id]` | ✘ **NO-GATE** | **整屏错误卡,无外壳** |
  | `/pica/login` | ✘(设计如此) | — |

- 代码:`app\pica\comic\[id]\page.tsx:47-48` 的 `export default function PicaComicDetailPage() { return <DetailContent />; }` 直接透传,没有守卫;错误分支在 `:138-162`(`min-h-dvh flex flex-col items-center justify-center`),并且该路由在 `main.tsx:168` **注册在 `ShellRouteLayout` 之外**(`:159-170` 的 `.pica/comic/:id` 与 `pica/reader/:id` 同组),所以一旦进入错误分支就彻底没有侧栏壳。对照 JM 同位置 `app\jm\comic\[aid]\page.tsx:124-126` 是 `<JmGate>…</JmGate>` 包裹 ⇒ **JM 会跳登录页,pica 不会**,属口径不一致的实现缺口。
- 顺带(同页次要项):
  - 「重试」连点两次:`document.body.innerText` 两次都还是 `"登录已失效,请重新登录\n\n重试\n返回"`,过程中**没有任何 loading / 禁用 / 提示变化**(`ev-comic-unauth2-d.json`)。请求确实重新发出,但 401 会立刻原样失败,所以从用户视角这个按钮在未登录态是**点了没反应**(① 的次要项;正确做法应是像其它页那样直接跳登录页)。
  - 「返回」= `navigate(-1)`(`:154`);直接打开该 URL 时无历史,实测跳到 `/`(首页),不是 `/pica`(`:154` 与正常态顶栏 `:171-180` 的 `history.state.idx` 判断不一致 —— 正常态会兜底到 `/pica`,错误态没有)。

### P3 时间戳原样输出 ISO 串(详细)

| 位置 | 源码 | 页面实际显示 | 证据 |
|---|---|---|---|
| 历史行第三行 | `app\pica\history\page.tsx:181` `<p className="mt-0.5 text-[11px] text-muted/70">{item.updatedAt}</p>` | `2026-11-26T18:43:07`(原文带 `T`、无时区) | `d-pica-history-14.png`、`m-pica-history-14.png` |
| 详情页评论时间 | `components\pica\CommentPanel.tsx:83` `<p className="text-[11px] text-muted">{cm.createdAt}</p>` | `2026-11-20T10:00:00Z`(带 `T` 与 `Z`) | `d-mock-comic.png`、`m-mock-comic.png` |

- 两处都直接渲染后端原串,全站其它位置(如 `读到第 N 话 · 第 M 页`)是中文语义化文案,因此这两行在列表里非常突兀 —— 属「字体/文本显示不正常」类。
- 修复方向(仅建议,本次不改代码):统一走一个 `formatDateTime()`(如 `2026-11-26 18:43` / 相对时间)。

### P4 下载弹层标题硬切(详细)

- 结构:`components\Sheet.tsx` 的 `SheetHeader` 里
  `<h3 className="flex min-w-0 items-center gap-2 truncate text-sm font-semibold ...">`
  —— `flex` 与 `truncate`(`overflow:hidden;text-overflow:ellipsis;white-space:nowrap`)同时存在时,**省略号不会渲染**(`text-overflow` 只对块级容器里的行内内容生效)。
- 实测(移动 390x844,comic 详情点「下载」):`scrollWidth=370` vs `clientWidth=316`,`computedStyle.textOverflow` 确实是 `ellipsis`,但截图上末尾是**硬切**在「是否正」处,后面直接跟关闭 ×(`zoom-dlsheet-title-m.png`)。
- 同一探针在桌面 1440x900 与「批量下载 12 部漫画」弹层返回 `count:0`(短标题不触发),说明只在长标题 + 窄视口下出现。

### P5 搜索按钮空提交无反馈(详细)

- `app\pica\search\page.tsx:151-154`:`handleSubmit` → `pushParams(keyword, cats, sort, 1)` → `:141-146` 里 `if (kw.trim()) params.set("keyword", …)`,空值时生成**空参数集**,`setSearchParams({})` 不改变 URL;`:125-137` 的 effect 有 `if (kw.trim() || c.length > 0)` 守卫,所以也不会发起搜索。
- 实测(`ev-search-empty-d.json` / `ev-search-empty-m.json`):点击「搜索」(`ok:true, n:1, w:82, h:40, disabled:false`)后 `location = /pica/search` 不变,`body` 仍是「输入关键词或选择分类开始浏览」,无 toast、无错误提示。
- 判定为低严重度:不是「按钮坏了」,而是「点了没有任何反馈」,用户会反复点。

---

## 二、未登录态实测(既定前提,不是缺陷)

> 容器内无哔咔会话,`PicaGate` 拦截属**正常行为**。本节只如实记录「未登录时页面渲染成什么样」。

| 页面 | 桌面 1440x900 | 移动 390x844 | 登录卡本身是否正常 |
|---|---|---|---|
| `/pica` → `/pica/login?from=%2Fpica` | `d-pica-home.png` | `m-pica-home.png`、`m-pica-home-full.png` | ✔ 卡片居中、图标/标题/文案/两个输入框/「登录」按钮排列正常;`overflowX delta=0`、`overflowOffenders=[]`、`brokenImages=[]`、`mojibake=[]`、`tinyFonts=[]`、`clipped=[]`、`duplicateAdjacent=[]`;桌面按钮 `返回 40x40` / `显示密码 32x32` / `登录 382x44`,移动 `登录 308x44` |
| `/pica/search` → 登录页 | `d-pica-search.png` | `m-pica-search.png` | ✔ 同上,无溢出 |
| `/pica/favorites` → 登录页 | `d-pica-favorites.png` | `m-pica-favorites.png` | ✔ 同上 |
| `/pica/history` → `/pica/login?from=%2Fpica%2Fhistory` | `d-pica-history.png` | `m-pica-history.png` | ✔ 同上 |
| `/pica/reader/58266?ep=1` → 登录页 | `d-pica-reader.png` | `m-pica-reader.png` | ✔ 同上 |
| `/pica/login` | `d-pica-login-unauth.png`(`bodyTextLen=174`) | `m-pica-login-unauth.png`(`bodyTextLen=61`) | ✔ 登录卡正常;桌面多出的文字是左侧固定侧栏(`hidden lg:flex fixed … w-[220px]`),移动端无 fixed 层 |
| `/pica/comic/58266` | `d-pica-comic.png` | `m-pica-comic.png` | ✘ **未跳登录页,整屏错误卡无外壳 —— 见 P2** |

登录卡按钮在未登录态的实测(均 PASS):空提交 → 「请输入账号和密码」;填账号密码后点「显示密码」→ `input.type` 由 `password` 变 `text`、aria-label 变「隐藏密码」,再点回 `password`;点「登录」→ 真实请求打到上游失败,显示「网络不可达,请检查代理设置」+「请检查网络连接,查看哔咔源设置」;齿轮「在线源设置」→ `/settings?tab=pica`(显示内置服务 `运行中 · v1.0.0`、代理 `http://127.0.0.1:10809`);「返回」→ 从 `/` 进 `/pica` 再返回能回到 `/`(有历史时正确)。证据:`d-pica-login-steps*.json`、`ev-login-pw.json`、`ev-login-backflow-d.json`、`clk-login-gear2-m.json`、`ev-login-whenlogged-d.json`(已登录访问 `/pica/login` 会自动跳 `/pica`)。

---

## 三、需登录 + 上游代理可用后才能验证的页面(本环境不可达)

> 下列内容只有在「真 token + `127.0.0.1:10809` 代理可用」时才能看到。本次用内存 mock 只验证了**布局与交互**,未验证**真实数据下的极端文案长度**(如超长作者名、超多分类、超长评论区)。

| 页面 | 只能用 mock 验证的部分 | 未能验证的部分 |
|---|---|---|
| `/pica` 四个 Tab | 每周热门/日榜/月榜/神魔推荐的切换与网格布局(12 卡 / 合集卡片) | 真实榜单数据量、真实长篇标题的折行、真实封面比例异常时的表现 |
| `/pica/search` | 结果网格、分页条、热词 chips、排序 pill、分类多选 | 真实 `total` 下的翻页(12 条 mock 只有 1 页,「上一页/下一页」不渲染);真实分类数量下的 chips 换行 |
| `/pica/favorites` | 12 卡网格、排序 pill、多选条、下载弹层 | 真实收藏数(数百)下的分页与性能;真实封面尺寸异常 |
| `/pica/history` | 14 条**真实**行数据(本地持久化,已清理)、行内下载按钮、清空历史 confirm | 真实 `epTitle`/`imageIndex` 组合下的文案长度 |
| `/pica/comic/[id]` | 封面/信息/操作条/章节网格/评论区布局、点赞·收藏·下载·开始阅读 | 真实简介长度、真实章节数(数百话)下的展开、真实评论区高度 |
| `/pica/reader/[id]` | 单页/双页/长条/夜间/自动翻页/章节导航面板/批量下载 FAB、页码指示器 | 真实图片尺寸差异、真实长条模式的滚动性能、真实章节切换 |

---

## 四、已核查正常项(含任务卡指定重点,逐条给出结论)

### 4.1 任务卡点名的三个「疑似」——**实测全部不成立**

| 疑似问题 | 实测结论 | 数值证据 | 证据文件 |
|---|---|---|---|
| 收藏页卡片左上「下载整本」28×28 角标与封面/标题打架 | **不成立**。角标落在封面内左上角,不越出封面、不压标题 | 移动:badge `l=22 t=185 r=50 b=217 (28x32)`;cover `l=16 t=179 r=189 b=410`;title `top=418` ⇒ `badgeOverTitle=false`、`badgeOutsideCover=false`、`titleClipped=false`。桌面:badge `28x28 @278,181`,cover `272,175-448,410`,title `t=418` | `m-mock-favorites.json`、`d-mock-favorites.json`、`zoom-fav-badge-m2.png` |
| 多选模式底部操作条被顶成两行 / 压住列表 | **不成立**。四个子元素中心 Y 只有一个值 ⇒ 严格单行 | 收藏页移动 bar `{y:730 h:114}`、子元素 `已选 0/12 @23,747`、`全选 @186,739`、`下载(0) @252,739`、`退出多选 @335,739` ⇒ `distinctCenterY=1`;桌面 bar `{y:828 h:72}` 同样 1 行;搜索页移动 `ev-batchbar-search-m.json` 亦 `distinctCenterY=1` | `m-mock-fav-selbottom.json`、`ev-batchbar-search-m.json`、`m-mock-fav-selbottom-s2.png` |
| 历史页行内下载按钮与「继续阅读」标签挤在一起 | **不成立**。桌面两者间距 12px 且都右对齐;移动「继续阅读」按 `sm:inline` 隐藏,只剩 32×32 下载按钮,与 Link 间距 12px | 桌面 `dl r=1323` / `cont l=1335` ⇒ `gapDlToCont=12`;`link r=1279` ⇒ `gapLinkToDl=12`。移动 `contVisible=false`(display:none),`gapLinkToDl=12` | `ev-histgap-d.json`、`ev-histgap-m.json`、`d-pica-history-14.png`、`m-pica-history-14.png` |

### 4.2 另外两条曾被怀疑、实测推翻的

| 曾疑似问题 | 结论 | 依据 |
|---|---|---|
| 移动端底部 Tab 栏遮挡列表最后一行 | **不成立**。`AppShell` 为 Tab 栏预留了 `pb-[calc(3.5rem+env(safe-area-inset-bottom,0px))]`(56px),滚到底后最后一行 9 点命中 `blockedPts=0` | `/pica` 首页 `sy=1643 sh=2487`、lastCard `y=450`、`blocked=0`;`/pica/search` 结果页 `sy=1553 sh=2397`、`blocked=0`;`/pica/favorites` 多选态 `sy=1433 sh=2277`、`blocked=0`;`/pica/history` 多选态 `sy=831 sh=1675`、lastRow `y=582 h=90`、`blocked=0`。证据 `ev-lastrow-pica-m.json`、`ev-lastrow-search2-m.json`、`probe-lastrow-fav-m.json`、`ev-hist-lastrow-m.json` |
| 阅读器页码「1 / 6」重复显示两个 | **不成立**。页码指示器(`page.tsx:467`)带 `!toolbarVisible` 条件,与工具栏内页码互斥;另一处始终处在 `translate-y-full opacity-0` 的隐藏层里 | 工具栏关闭:`1 / 6 @707,859`(可见)+ `@1364,921`(`hiddenAncestor: fixed bottom-0 … z-50`);工具栏打开:仅 `1 / 6 @1364,775` 可见。证据 `ev-pageind-d0.json`、`ev-pageind-d1.json` |

### 4.3 按钮有效性(真实点击 + 落地校验)

| 位置 | 操作 | 结果 | 证据 |
|---|---|---|---|
| `/pica` 4 个 Tab | 逐个点击 | 全部生效:`activePills` 分别变 `日榜`/`月榜`/`神魔推荐`/`每周热门`,内容同步(神魔推荐正文 551 字、热门 859 字) | `clk-pica-tabs.json`、`d-mock-pica-tabs-s*.png`、`clk-coll-m.png` |
| `/pica` UserCard 5 入口 | 逐个点击 | 每日打卡 → 出现「打卡成功,经验 +10」;随机一本 → `/pica/comic/mock-0001`;搜索 → `/pica/search`;我的收藏 → `/pica/favorites`;阅读历史 → `/pica/history` | `clk-pica-d.json`、`clk-pica-fav.json`、`clk-pica-hist2.json` |
| `/pica` 右上「下载任务」 | 点击 | 弹层「下载任务 / 暂无下载任务」 | `clk-dltask-m.json` |
| `/pica/search` 搜索 / 热词 / 排序 / 分类 | 逐个点击 | 填 `mock` 点搜索 → 「共 12 条结果 · 第 1 页」+ 12 张卡;热词「关键词一」→ `?keyword=关键词一` 且回填;排序「爱心多」→ `&s=ld` 且 pill 高亮;分类「测试分类A」→ `?c=测试分类A` 且出现 pill | `d-mock-search.json`、`d-mock-search-s2.png`、`clk-search-cat.json`、`clk-search-d.json`、`ev-search-page-d.json` |
| `/pica/favorites` 排序 / 多选 / 全选 / 下载 / 退出 | 逐个点击 | 「旧→新」→ pill 切换;「多选下载」→ 出条;「全选」→ `已选 12/12`、变「全不选」、`下载(12)` 解除 disabled;点「下载(12)」→ 弹层「批量下载 12 部漫画 / Default Library · /app/comics」、按钮变「开始下载 12 部」;「退出多选」→ 回到 12 卡普通态 | `clk-fav-m.json`、`clk-fav-exit.json`、`d-mock-favorites-sel.json` |
| `/pica/favorites` 卡片「下载整本」(28×28) | 点击 | 弹出下载弹层(标题为卡片标题)⇒ 角标可用 | `clk-fav-qdl-d.json`、`clk-fav-qdl-m.json` |
| `/pica/favorites` 卡片本体 | 点击 | → `/pica/comic/mock-0001` | `clk-fav-card.json` |
| `/pica/history` 行内「下载整本」(32×32) | 点击 | 弹层「下载《【审计临时】批量行 4》/ 下载目录 Default Library (1) · /app/comics」 | `clk-hist-qdl-d.json`、`clk-hist-qdl-m.json` |
| `/pica/history` 清空历史 | 点击 | 原生 confirm「确定清空全部哔咔阅读历史?此操作不可撤销。」(探针取消,14 行未删)⇒ 交互正常;正式清理已用 DELETE 完成 | `clk-hist-clear-d.json`(`nativeDialogs`) |
| `/pica/comic/[id]` 点赞/收藏/下载/章节/开始阅读 | 逐个点击 | 点赞 → 「已点赞」;收藏 → 「已加入收藏」;下载 → 弹层 + 13 个章节 checkbox;章节「第2话」→ `/pica/reader/mock-0001?ep=2`;「开始阅读」→ 阅读器 | `clk-comic-d2.json` |
| `/pica/reader/[id]` 双页/长条/夜间/更多/设置/工具栏开合 | 逐个点击 | 双页 → 移动端告警「⚠️ 双页模式在小屏幕上体验不佳…」;长条 → 「已到达最后一页 下一章 →」;夜间 → 主题切换;「更多」→ 顶栏出现「全屏」;「设置」→ 阅读器选项面板(显示/适应/容器/页面渲染/阅读方向/页码指示器/图片滤镜/预加载/自动翻页间隔);点屏幕中央 → 工具栏开合 | `clk-reader-m.json`、`clk-reader-more-m.json`、`clk-reader-set-m.json`、`ev-pageind-d0.json` |
| `/pica/reader` 两个 FAB(工具栏**隐藏**时) | 点击 | 章节导航 → `chapterPanelOpen=true`;批量下载 → `downloadPanelOpen=true` ⇒ 按钮本身正常 | `ev3-fab-d.json`(第 3-4 步)、`ev2-fab-m.json` |

### 4.4 布局 / 字体(双视口)

| 页面 | 桌面 1440x900 | 移动 390x844 |
|---|---|---|
| `/pica` | 侧栏 220px + UserCard 一行 5 按钮(y=125)+ 4 Tab 单行 + 6 列网格 | UserCard 按钮折成 3 行(`y=180 / 226 / 272`,每行 2+2+1)、4 Tab 单行、2 列网格;无溢出(`sw=cw=390`) |
| `/pica/search` | 搜索框 + 热词 chips + 4 排序 pill + 分类 select 一行 + 结果网格(6 列) | 表单常驻、chips 折行、结果 2 列;无溢出 |
| `/pica/favorites` | 6 列 × 2 行 = 12 卡 + 分页区 | 2 列;角标 28×32 落在封面内 |
| `/pica/history` | 14 行,行高 90px,封面 48×64,「下载整本 32×32」+「继续阅读」右对齐 | 行高 90px,「继续阅读」隐藏,标题 `truncate` 正确(`【审计临时】这是一个特别长的哔咔…`) |
| `/pica/comic/[id]` | 封面 208 宽 + 右侧信息 + 操作条一行(点赞/已收藏/下载/开始阅读)+ 章节 4 列 + 评论 | 单列堆叠,操作条「点赞/已收藏/下载」一行 +「开始阅读」独占一行 |
| `/pica/reader/[id]` | 单页居中、页码底部居中、两个 44×44 FAB 在左下/右下 | 同款;无底部 Tab 栏(`MobileBottomNav` 对 `/pica/reader/` 与 `/pica/comic/` 主动 `return null`,见 `MobileBottomNav.tsx:48-60`) |
| 下载弹层(移动) | — | `sheet-layer` 高 844 = 视口高,内部 `overflow-y-auto` 区 `y=348 h=432`,底部「取消 / 开始下载」均在视口内(`bottom=830 ~ 831 < 844`)⇒ 按钮不被顶出屏幕 |

**批量指标(15 个页面 × 双视口)** —— 全部为 `0`:
`overflowX.delta` / `overflowOffenders` / `brokenImages` / `mojibake` / `tinyFonts` / `clipped` / `duplicateAdjacent` / `outOfViewportFixed` / `exceptions` / `consoleErrors` / `apiFails`。
覆盖:`d-mock-pica`、`m-mock-pica`、`d-mock-pica-tabs`、`d-mock-search`、`m-mock-search`、`d-mock-favorites`、`m-mock-favorites`、`m-mock-fav-selbottom`、`d-mock-comic`、`m-mock-comic`、`d-mock-reader`、`m-mock-reader`、`d-pica-history-14`、`m-pica-history-14`、`d-pica-login-unauth`、`m-pica-login-unauth`。
(唯一非零:`m-pica-history-14` 的 `coveredClickables=2`,是**滚动中途**采样的结果;滚到底后 9 点命中 `blockedPts=0`,故不记为缺陷 —— 见 4.2。)

---

## 五、环境噪声(不是 UI 缺陷,单列)

| 现象 | 证据 | 说明 |
|---|---|---|
| 内容接口统一 401 `code:1002` | `curl /api/pica/leaderboard?tt=D7` | 无 `X-Pica-Token` 时后端直接拒绝;`PicaGate` 据此跳登录页,属正常 |
| 带任意 token 仍返回 `code:2002` | `curl -H "X-Pica-Token: …"` | 服务端上游代理 `127.0.0.1:10809` 未监听,`data.reason` 为 `dial tcp 127.0.0.1:10809: connect: connection refused` |
| 错误卡标题「在线源服务不可达」 | `d-pica-favorites-tok.json`、`d-pica-search-tok.json` | `PicaErrorCard` 对 `code===2002` 的正常分支,非缺陷 |
| `/pica/comic/[id]` 错误文案随环境变化 | `ev-comic-unauth2-d.json`(`登录已失效…`)、`d-pica-comic-tok.json`(`网络不可达…`) | 文案取自 `error.message`,环境不同显示不同;P2 讨论的是**外壳缺失**,与文案内容无关 |
| 历史页种子数据 | 已 `DELETE /api/pica/history` 并复核 `total=0` | 审计写入的 14 + 3 条 `audit-seed-*` 全部清理,原库本来即为 0 条 |

---

## 六、复现方式

```powershell
. <audit-root>/env.ps1
$O = "<audit-root>/audit\pica"

# P1 阅读器 FAB:展开工具栏 -> 命中测试 -> 点 FAB -> 等自动隐藏 -> 再点(对照)
node <audit-root>/probe_clicks.mjs /pica/reader/mock-0001 1440 900 10000 $O\ev3-fab-d `
  "tap:0.5:0.5" "qfile:<audit-root>/q_fab.js" "tapPx:1402:858" `
  "qfile:<audit-root>/q_fab.js" "wait:4200" "qfile:<audit-root>/q_fab.js" `
  "tapPx:1402:858" "qfile:<audit-root>/q_fab.js"

# P1 层级/几何(桌面 + 移动)
node <audit-root>/probe_clicks.mjs /pica/reader/mock-0001 390 844 10000 $O\ev-zlayers-m `
  "tap:0.5:0.5" "qfile:<audit-root>/q_zlayers.js"

# P2 未登录详情页(必须清 pica 会话)
$env:NAV_MOCK="0"; $env:NAV_NO_PICA_TOKEN="1"
node <audit-root>/probe_clicks.mjs /pica/comic/58266 1440 900 9000 $O\ev-comic-unauth2-d `
  "q:document.body.innerText.trim().slice(0,120)" "q:[...document.querySelectorAll('nav,aside')].length" `
  "click:重试" "q:document.body.innerText.trim().slice(0,120)" "click:返回" "q:location.pathname"

# P3 时间戳(历史页需要先 POST 造行, 见 <audit-root>/audit\pica\seed\h*.json)
node <audit-root>/audit3.mjs /pica/history 1440 900 9000 $O\d-pica-history-14

# P4 弹层标题硬切
node <audit-root>/probe_clicks.mjs /pica/comic/mock-0001 390 844 10000 $O\ev-trunc-comic-m `
  "click:下载" "qfile:<audit-root>/q_trunc.js"

# P5 空搜索
node <audit-root>/probe_clicks.mjs /pica/search 1440 900 10000 $O\ev-search-empty-d `
  "click:搜索" "wait:600" "q:location.pathname+location.search"
```

> `probe_clicks.mjs` 默认拦截 `*/api/pica/*` 注入内存 mock;`NAV_MOCK=0` 关闭 mock(走真实后端),`NAV_NO_PICA_TOKEN=1` 清空 `localStorage.pica_token`。原生 `confirm/alert` 一律以 `accept:false` 关闭,并记录在输出 JSON 的 `nativeDialogs`。

---

## 七、结论

- **P1(阅读器两个浮动按钮)** 是本次最硬的问题:**双端必现**,既是层级重叠(②),也直接导致「点章节导航 = 切夜间」「点批量下载 = 切单页」(①)。根因单一 —— `app\pica\reader\[id]\page.tsx:514 / :592` 的 FAB 固定在 `bottom-5 z-30`,没有像 JM 那样在 `toolbarVisible` 时上移,而工具栏底栏是 `bottom-0 z-50`。
- **P2(详情页无 `PicaGate`)** 是口径不一致造成的「未登录即丢壳」:其余 6 个 pica 内容页都会跳登录卡并保留侧栏,pica 详情页直接整屏一行灰字。对照 JM 已用 `<JmGate>` 包住,建议对齐。
- **P3(时间戳原样输出)** 是本次唯一确定的「文本/字体显示不规范」问题,两处 `text-[11px]` 直接渲染 ISO 串,修复成本极低。
- **P4/P5** 属低优先级的截断与反馈缺失。
- 任务卡点名要复核的三项(收藏角标打架、多选条两行、历史页下载按钮与「继续阅读」挤压)**在双视口下全部不成立**,已给出几何数值与放大截图;另外两项历史怀疑(移动端 Tab 栏遮挡最后一行、阅读器页码重复)也已被精确命中测试推翻 —— 这些请勿再当作缺陷排期。
- 除上述 5 条外,`/pica` 四个 Tab、搜索、收藏、历史、详情、阅读器、登录页在 1440x900 与 390x844 下的横向溢出、破图、乱码、极小字号、文本重复、fixed 元素越界、JS 异常、控制台报错均为 **0**。
