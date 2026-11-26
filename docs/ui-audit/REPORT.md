# NowenReader 线上界面审查 · 汇总报告（只读）

- **审查对象**：http://<instance>:6680
- **实例版本**：`dev-93793dd`（`GET /api/health` → `{"status":"ok","version":"dev-93793dd","go":"go1.26.5"}`）
- **审查时间**：2026-11-26 20:10 ~ 21:37（+08:00 成都）
- **视口**：桌面 `1440x900` / 移动 `390x844`（DPR 2）
- **范围口径**：仅查三类 —— ①按钮无效 ②界面显示不正常 ③字体错乱或重复。不含性能、安全、文案质量、接口语义。
- **只读声明**：全程未修改仓库任何文件，未点击任何破坏性按钮（唯一写操作是上游 core 组验证「保存阈值」时做过一次 `PUT 0/0/0` 并已复查恢复；本次复核对该按钮**仅做量测、未点击**）。
- **复核方式**：四位分组审查员（core / jm / pica / detail）产出分报告后，由汇总复核员用独立自建探针逐条复核（CDP `Page.captureScreenshot` + `Runtime.evaluate` 量测 + `Input.dispatchMouseEvent` 真实命中点击），**上游 25 条逐条复核，抽样复现率 100%（要求 ≥30%）**，其中 3 条被推翻/降级、2 条本次未复现、6 条同根因合并，最终确认 **16 条**。

---

## 1. 审查范围

| 分组 | 页面（×2 视口） | 分报告 |
|---|---|---|
| A 常规页面 12 页 | `/`、`/books`、`/collections`、`/history`、`/recommend`、`/stats`、`/logs`、`/tag-manager`、`/metadata`(刮削)、`/data-admin`、`/data-qa`、`/file-stats` | `core.md` |
| B JM 模块 8 页 | `/jm`(4 Tab)、`/jm/search`、`/jm/week`、`/jm/tags`、`/jm/favorites`、`/jm/history`、`/jm/comic/150`、`/jm/reader/1488671` | `jm.md` |
| C Pica 模块 7 页 | `/pica`(4 Tab)、`/pica/search`、`/pica/favorites`、`/pica/history`、`/pica/comic/[id]`、`/pica/reader/[id]`、`/pica/login` | `pica.md` |
| D 详情/阅读器/设置 | 详情 12 路径变体 + `/reader/[id]`(桌面+移动) + 设置 12 Tab | `detail.md` |

本次复核自建探针与产物：`<audit-root>/review\`（`verify.mjs` / `reader3.mjs` / `scan.mjs`），量测 JSON 与截图在 `<audit-root>/review\out\`。

---

## 2. 问题清单（16 条）

计数：**①按钮无效 3 条 / ②显示不正常 8 条 / ③字体错乱或重复 5 条**。

### ① 按钮无效（3 条）

| 编号 | 现象 | 页面 | 视口 | 证据（绝对路径 + 关键数值） | 严重度 | 建议方向 |
|---|---|---|---|---|---|---|
| **R-01** | 阅读器工具栏展开时，右下角两个浮动圆钮被阅读器底栏 **100% 覆盖**，点击穿透到下层：点「AI 阅读助手」实际命中页码层，点「Translate Page」实际命中底栏按钮。视觉上紫色圆钮与「1 / 129 单页/双页」文字糊在一起。**同根因跨 3 个模块**（通用阅读器 / JM 阅读器 / Pica 阅读器），共影响 4 个阅读器路由。 | `/reader/36faa4106673`、`/jm/reader/1488671`、`/pica/reader/[id]` | 桌面 1440x900 **+** 移动 390x844 | 桌面：底栏 `r=[0,754,1440,146] z=50`；FAB `[1376,772,48,48] z=30`、`[1312,772,48,48] z=30`，覆盖面积比 **1.00**；`elementFromPoint` 命中 `DIV "1 / 129"` 与底栏 `BUTTON`。真实点击中心 (1400,796)：`bodyTextLen 86→86`、`panel=0`（无反应）。**对照组**（等 4.2s 工具栏自动隐藏后点同坐标）：`bodyTextLen 94→259`、`panel=1` —— 按钮本身没坏，只是被遮。移动：底栏 `[0,710,390,134]`、FAB `[330,720,44,44]`/`[266,720,44,44]`，命中 `SPAN "1 / 129"`，真点 `65→65`、`panel=0`。Pica 侧额外后果：真实点 (1402,858) 使 `themeClass: dark→light`（误触夜间开关）。<br>`<audit-root>/review\out\rd3d-reader3.json`、`rd3d-A-shown.png`、`rd3d-B-fabclick.png`、`rd3d-C-fabclick-control.png`、`rd3m-reader3.json`、`rd3m-B-fabclick.png`、`rd3m-C-fabclick-control.png`；上游 `<audit-root>/audit\detail\ov-reader-d.json`、`ev-pageind-d.json`、`<audit-root>/audit\pica\ev3-fab-d.json`、`ev-zlayers-d.json`、`zoom-fab-covered-right-d.png` | **高** | FAB 的 `bottom-20`(80px) / `bottom-24`(96px) 小于底栏高度 146/134px，应在 `toolbarVisible` 时把 FAB 抬到底栏之上（或反过来降低底栏 z 序），并让页码层不参与命中。 |
| **R-03** | JM/Pica 阅读历史、收藏页进入「多选下载」后，底部批操作条压住列表末尾的「加载更多」，**点中心无反应、点顶边才生效**（按钮可用但被遮）。 | `/jm/history`、`/jm/favorites`、`/pica/history`、`/pica/favorites` | 桌面（63%）**+** 移动（37%） | 桌面 /jm/history 多选态滚到底：按钮 `lmR=[787,814,106,38]`；操作条 `r=[0,828,1440,72] z=40`（`pointer-events-none fixed inset-x-0 bottom-…`）；**重叠 24px / 覆盖率 0.63**；中心 `elementFromPoint` 命中 `DIV.pointer-events-auto mx-auto flex max-w-2xl flex-nowrap…` 而非按钮本身（`btn=null`）。上游真点对照：点中心 `rows 20→20` 无变化、点顶边 `20→40`（收藏页 `18→18` / `18→36`）。移动覆盖率 37%，中心仍可点。<br>`<audit-root>/review\out\jm1.jsonl`(check=`jm_hist`)；上游 `<audit-root>/audit\jm\crop-hist-d-loadmore.png`、`crop-fav-d-loadmore.png`、`hk-d-history-loadmore-blocked.png` | **高** | 批操作条占位 `Spacer` 落在列表上方而非下方（`BatchDownload.tsx:105-108`），且分页尾部渲染在 `BatchSelectShell` 之外，应把尾部移入 shell 或改为滚动留白。 |
| **R-04** | `/pica/search` 在关键词为空、未选分类时点「搜索」，是**纯 no-op**：URL 不变、`bodyText` 长度不变、无 toast、无 loading、按钮也不 disabled —— 用户得不到任何反馈。 | `/pica/search` | 桌面 **+** 移动 | 点击前 `{url:"/pica/search", len:212}`，点击「搜索」按钮（`ok:true, 82x40, disabled:false`）后 `after.url` 仍 `/pica/search`、`len` 仍 212、`toast:[]`。<br>`<audit-root>/audit\pica\ev-search-empty-d.json`、`ev-search-empty-m.json`、`ev-search-page-d.png`、`d-pica-search-steps.png` | **低** | 双守卫直接 return 前先给一句 toast（"请输入关键词或选择分类"）或把按钮置 disabled。 |

### ② 显示不正常（8 条）

| 编号 | 现象 | 页面 | 视口 | 证据（绝对路径 + 关键数值） | 严重度 | 建议方向 |
|---|---|---|---|---|---|---|
| **R-02** | 「最近阅读」卡片被撑到 **891px 宽**（视口仅 390px），右侧被父级 `overflow-x:hidden` 裁掉：条目时长 `0m`、卡片右上「查看全部」**完全不可见**，且无法横向滚动看到。 | `/stats` | **仅移动** 390x844（桌面正常） | 网格容器 `grid lg:grid-cols-2 gap-4`：`r=[16,1210,358,612]`，**`gridTemplateColumns=890.828px`**，`scrollWidth 891 / clientWidth 358`；卡片 `r=[37,1231,849,32]`。**出屏叶子文本 24 个、最远出屏 490px**：条目标题 `[87,1289,763,20]`、`0m` `[862,1299,18,16]`、「查看全部」`[822,1231,64,32]`。整页 `docScrollWidth=390`（父级 `animate-page-enter overflow-x-hidden`，故无横向滚动条）。桌面同页 0 出屏。<br>`<audit-root>/review\out\m2.jsonl`、`m1.jsonl`、`m1-stats_recent.png`、`m2-stats_recent.png`；上游 `<audit-root>/audit\core\p-stats-cause-mob.json`、`shots\stats-mob-recent-view.png`、`shots\crop-stats-mob-recent.png` | **高** | `lg:grid-cols-2` 缺 `grid-cols-1` 的显式基列（或子项缺 `min-w-0`），使单列轨道被内容的最小内容宽度撑到 890px；补 `grid-cols-1` + `min-w-0` 即可。 |
| **R-05** | 标签管理分页条在移动端**溢出容器且不滚动**：「第一页/‹」左出界 9px，「跳转」右缘越过视口并被挤成竖排（"跳"/"转"上下两行）。 | `/tag-manager` | **仅移动**（桌面正常） | 外层 `flex flex-col sm:flex-row items-center justify-between gap-3`：`r=[16,4191,358,104]`，**`scrollWidth 383 > clientWidth 358`**，`overflow-x: visible`（不产生滚动，直接溢出）；内层行 `flex items-center gap-1`：`r=[-9,4251,408,36]`；「跳转」按钮 `r=[364,4251,35,36]` → 右缘 **399 > 390**。整页 `docScrollWidth=390`，用户无处可滚。<br>`<audit-root>/review\out\m1.jsonl`(check=`tag_pager`)、`m1-tag_pager.png`；上游 `<audit-root>/audit\core\p-tagpager2.json`、`shots\tag-mob-pager-view.png`、`shots\crop-tag-mob-pager.png` | 中 | 移动端分页条改为可换行（`flex-wrap`）或把页码与「跳转」拆成两行，并给容器 `overflow-x:auto` 兜底。 |
| **R-06** | 阅读器顶栏响应式显隐**完全失效**：移动端多出 2 个桌面专用图标（ⓘ详情 / ⛶全屏），把书名标题从 202px 挤到 **122px**；桌面端多出本应移动专用的「⋯更多」，其菜单里的「详情/全屏」又与顶栏图标功能重复。 | `/reader/[id]` | 桌面 **+** 移动 | 合成探针（两视口一致）：`class="hidden"` → `display:none w=0`；`class="reader-toolbar-button"` → `flex`；**`class="hidden reader-toolbar-button"` → `flex w=8.1`**；`class="sm:hidden reader-toolbar-button"` → `flex` —— 证明未分层 CSS `.reader-toolbar-button{display:flex}`（`frontend/src/app/globals.css:464`）压过了 `@layer utilities` 里的 `.hidden` / `.sm:hidden`。移动实测顶栏 6 个按钮中含 `hidden sm:flex` 两颗：`[262,6,36,36]`、`[302,6,36,36]`；移除后标题 `clientW 122→202`（**回收 80px**，`scrollW` 仍 610，`text-overflow:ellipsis`）。桌面 7 个按钮全 `display:flex`，含 `reader-toolbar-button sm:hidden` `[1388,10,36,36]`。<br>`<audit-root>/review\out\rd3d-reader3.json`、`rd3m-reader3.json`、`rd3m-A-shown.png`、`rd3d-A-shown.png`；上游 `<audit-root>/audit\detail\ev-hidden-probe-m.json`、`ev-titlew-m.json`、`ev-hs2-d.json`、`rt-d.png`、`tbar-m.png` | 中 | 把 `.reader-toolbar-button` 移进 `@layer components` 或改用 `@apply`，否则 Tailwind 的 `hidden` 永远赢不了它。 |
| **R-07** | 设置页「JM 源」的代理地址输入框在移动端被**纵向压扁成 20px 高**（类里声明 `h-10`=40px），与紧挨着的高 40px「保存」按钮明显不齐，输入体验受损。全设置页 12 个 Tab 扫描仅此 1 例。 | `/settings?tab=jm-source` | **仅移动**（桌面正常，哔咔 Tab 同款输入框 234x40 正常） | 输入框（placeholder `http://127.0.0.1:10809`）`r=[37,573,316,20]`，`computedHeight=20px`、`flex:1 1 0%`、`box-sizing:border-box`、`padding:0`；父容器 `display:flex; flex-direction:column`，父 `r=[37,569,316,68]`（两个子元素共 68px 高）；同排按钮 `r=[37,601,60,40]`。类名完整：`h-10 min-w-0 flex-1 rounded-lg border …`，父类 `flex flex-col gap-2 sm:flex-row`。<br>`<audit-root>/review\out\m5.jsonl`(check=`settings_mob`)；上游 `<audit-root>/audit\detail\ev-jminput-m.json`、`sel-jm-input-m.png`、`clip-jm-input-m2.png`（源码 `frontend/src/components/settings/JmSourcePanel.tsx:436-451`） | 中 | column 方向下 `flex:1 1 0%` 会把高度也按比例压缩，移动端给输入框补 `shrink-0`（或 `sm:flex-1` 只在行方向生效）。 |
| **R-08** | `/pica/comic/[id]` 是 Pica 模块里**唯一未套 `PicaGate` 的内容页**：未登录时整屏纯黑，居中一行「登录已失效,请重新登录」+[重试][返回]，侧栏、页头、底部导航**全部消失**；与其余 6 个 pica 页统一跳 `/pica/login?from=` 且保留外壳的做法不一致。附带「重试」连点两次 `bodyText` 不变、无 loading；「返回」在无历史时跳到站点首页 `/` 而非 `/pica`。 | `/pica/comic/[id]` | 桌面 **+** 移动 | 未登录态 `bodyText` 仅含登录失效提示与两个按钮，`nav`/`aside`/`header` 检出数 0；对照 `/jm/comic/150` 同状态保留完整外壳（`<JmGate>`）。根因：`frontend/src/app/pica/comic/[id]/page.tsx:47-48` 直接 `return <DetailContent/>`，且 `frontend/src/main.tsx:168` 把它注册在 `ShellRouteLayout` 之外。<br>`<audit-root>/audit\pica\ev-comic-unauth2-d.png`、`ev-comic-unauth2-m.png`、`m-pica-comic.png`、`d-pica-comic.png`；对照 `<audit-root>/audit\pica\ev-jmcomic-unauth-d.png` | 中 | 与其余 pica 页对齐：用 `PicaGate` 包裹并注册进 `ShellRouteLayout`，让未登录也保留导航外壳。 |
| **R-09** | 哔咔阅读历史与评论区的**时间戳原样输出 ISO 字符串**（`2026-11-26T18:43:07` / `2026-11-20T10:00:00Z`），未做本地化格式化，与其余页面的「11/26 19:30」风格不一致。 | `/pica/history`、`/pica/comic/[id]` 评论面板 | 桌面 **+** 移动 | 历史行文本 `… · 第 28 页 2026-11-26T18:43:07 继续阅读`（14 条记录全部同格式，`text-[11px]`）；评论 `2026-11-20T10:00:00Z`。源码 `frontend/src/app/pica/history/page.tsx:181` `{item.updatedAt}`、`CommentPanel.tsx:83` `{cm.createdAt}`。<br>`<audit-root>/audit\pica\d-pica-history-14.json`、`d-pica-history-14.png`、`m-pica-history-14.png`、`d-mock-comic.png` | 中 | 统一走已有的时间格式化工具（相对时间或 `MM/DD HH:mm`），不要直接渲染后端字段。 |
| **R-10** | 移动端底部固定 tab 栏为**半透明 + backdrop-blur**，中段滚动位置下**透出并压住正文文字**，可读性下降（滚到页底有留白，属中段重叠）。 | `/data-admin`、`/file-stats` | **仅移动** | 底栏 `r=[0,787,390,57] z=50`，背景 `oklab(0.168279 -0.00191062 -0.0179564 / 0.9)` + `backdrop-filter: blur(40px)`。`/data-admin` 检出与底栏矩形相交的文本 **9 处**（其中 4 处是底栏自身导航文字「首页/书库/在线」，真实内容重叠 5 处），样例：`标签 1292 · 书目关联 18317 [46,790,298,16]`、`466.19 KB · 2026/10/… [46,824,148,13]`、`恢复 [274,814,42,32]`；`scrollBottomGap=0`。<br>`<audit-root>/review\out\m1.jsonl`(check=`dataadmin_mob`)、`m1-dataadmin_mob.png`；上游 `<audit-root>/audit\core\shots\e4-dataadmin-mob-navoverlap.png`、`e4-tagmgr-mob-navoverlap.png` | 低 | 给页面底部补 `pb-[57px]` 滚动留白（底栏固定高度），并把底栏背景改为不透明或加深模糊。 |
| **R-11** | `/data-admin`「保存阈值」在 **disabled 状态下仍是实心绿底 + 白字**（仅 `opacity 0.5`），看起来可点；同页另外 3 个 disabled 的「清理」按钮是 `opacity 0.4` + 无底色，同页两种禁用样式并存。 | `/data-admin` | 桌面 **+** 移动 | `disabled:true`；`class="inline-flex items-center gap-1.5 rounded bg-emerald-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-emerald-500"`；`background=oklch(0.596 0.145 163.225)`（实心绿）、`color=rgb(255,255,255)`、`opacity=0.5`；`r=[277,3472,80,32]`(移动) / `[1311,2170,80,28]`(桌面)。对照「清理」：`bg=rgba(0,0,0,0)`、`opacity=0.4`。<br>`<audit-root>/review\out\m1.jsonl`(字段 `thrInfo`/`otherDisabled`)、`d2.jsonl`；上游 `<audit-root>/audit\core\dis-data-admin-desk.json`、`shots\e5-dataadmin-savethreshold.png`、`shots\e5-dataadmin-cleanbtn.png` | 低 | 统一禁用态：`disabled:bg-surface disabled:text-muted disabled:opacity-40`，去掉 hover 变色。 |

### ③ 字体错乱或重复（5 条）

| 编号 | 现象 | 页面 | 视口 | 证据（绝对路径 + 关键数值） | 严重度 | 建议方向 |
|---|---|---|---|---|---|---|
| **R-12** | 书库筛选栏里「标签筛选」**在同一栏渲染两遍**：一处是筛选器名（12px `text-xs font-medium`），另一处是计数入口「293 标签筛选」（12px `text-muted`）。已排除"溢出挤压产生副本"——共同父容器 `scrollWidth == clientWidth`。 | `/books` | 桌面 **+** 移动 | 桌面：`标签筛选` span `r=[317,205,48,16] fs=12px`（`text-xs font-medium whitespace-nowrap`）+ `293 标签筛选` span `r=[425,204,73,16] fs=12px`（`text-xs text-muted`）；文档宽 1440/1440（无溢出）。移动：`[57,219,48,16]` + `[37,251,73,16]`；文档宽 390/390。共同父容器 `scrollWidth=clientWidth`（1086/1086 桌面、316/316 移动）。<br>`<audit-root>/review\out\d1.jsonl`、`d1-books_tagfilter.png`、`m3.jsonl`、`m3-books_tagfilter.png`；上游 `<audit-root>/audit\core\p-tagfilter-desk.json`、`p-tagfilter-mob.json`、`shots\e1-books-desk-tagfilter.png`、`shots\e1-books-mob-filterbar-wide.png`、`shots\e1-books-mob-tagfilter.png` | 中 | 两者保留其一：把计数并入同一个 chip（`标签筛选 (293)`），或把计数入口移到标题区。 |
| **R-13** | 「智能合集」弹层里 **h2 标题与副标题完全同文**（都是「智能合集」，18px 对 14px），真正的说明文字在第 3 行。 | `/collections`（智能合集弹层） | 桌面 **+** 移动 | 桌面（先真实点 `智能合集` `[1196,15,102,40]`）：弹层 `r=[336,212,768,476]`；**H2「智能合集」`fs=18px fw=600 r=[389,231,72,28]`** + **P「智能合集」`fs=14px fw=500 r=[692,438,56,20]`**；另有说明 P「自动识别可合并的同合集漫画」`fs=12px r=[642,466,156,16]`。移动：触发器是「批量管理」内无文字圆钮 `[342,235,32,32]`（故本组探针 `__clicked:null`），现象依上游截图确认同型（h2 18px + p 14px 同文）。<br>`<audit-root>/review\out\d4.jsonl`(check=`col_smart`)、`d4-col_smart.png`；上游 `<audit-root>/audit\core\shots\dlg-col-smart-desk.png`、`shots\dlg-col-smart-mob.png`、`p-smartcount.json` | 中 | 副标题改为「共 N 个合集将被合并」之类有信息量的描述，不要复用标题。 |
| **R-14** | 「重复检测」弹层里 **h2 标题与副标题同文**（都是「重复检测」），且下方紧跟一个**同名按钮**「检测重复」，三处近似文字堆叠。 | `/books`（检测重复弹层） | 桌面 **+** 移动 | 须先真实点「检测重复」按钮（桌面 `[664,159,94,32]`、移动 `[37,169,36,32]`）才会出现弹层。桌面：弹层 `r=[336,224,768,452]`；**H2「重复检测」`fs=18px fw=600 r=[389,243,72,28]`** + **P「重复检测」`fs=14px fw=400 r=[692,450,56,20]`**；按钮「检测重复」`fs=14px r=[668,494,104,40]`、「关闭」`[1019,627,60,36]`。移动：弹层 `r=[8,42,374,760]`；H2 `[53,57,72,28]`、P `[167,256,56,20]`、按钮 `[143,300,104,40]`；文本序列 `["重复检测","重复检测","检测重复","关闭"]`，`bodyTextLen=1448`。<br>`<audit-root>/review\out\d4.jsonl`、`d4-books_dupdlg.png`、`m3.jsonl`、`m3-books_dupdlg.png`；上游 `<audit-root>/audit\core\dc-dup.json`、`dlgdup-mob.json`、`shots\dlg-books-dup-desk.png`、`shots\dlg-books-dup-mob.png` | 中 | 副标题写结果摘要（如「未发现重复」/「发现 N 组重复」），按钮文案改「开始检测」。 |
| **R-15** | 阅读器「更多」菜单里「真实翻页（实验）」一行**丢了 `className`，被渲染成原生 `button`**：字号 16px（同级 14px）、`padding:0`（同级 `10px 14px`）、`display:inline-block`（同级 flex）、行高 24（同级 20/行高 40）、`text-align:center`，与其余 6 个菜单项明显不齐；该行还是「~」字号 12px 与「真实翻页（实验）」字号 16px 混排。 | `/reader/[id]`「更多」菜单 | 桌面（移动无此菜单入口） | 异常行：`className="(empty)"`、`display=inline-block`、`padding=0px`、`fontSize=16px`、`lineHeight=24px`、`textAlign=center`、`r=[1248,250,137,24]`；同级正常行：`display=flex`、`padding=10px 14px`、`fontSize=14px`、`lineHeight=20px`、`r=[1248,210,176,40]`。子 span：`~` `fs=12px r=[1248,256,9,15]`、`真实翻页（实验）` `fs=16px r=[1257,252,128,20]`。<br>`<audit-root>/review\out\rd3d-reader3.json`、`rd3d-D-menu.png`；上游 `<audit-root>/audit\detail\ev-fliprow-d.json`、`rmenu-d.png`、`rmenu-m.png`（源码 `frontend/src/components/reader/ReaderToolbar.tsx:310-324`） | 中 | 补上与同级一致的 `className`（`flex w-full items-center gap-2.5 px-3.5 py-2.5 text-sm …`）。 |
| **R-16** | 首页阅读统计环形图的**中心标签字号只有 8px**（「核心」/「MB」），几乎不可读。 | `/`（首页统计卡） | **仅桌面**（移动端同类文本为 9px 徽标，按口径不计） | 桌面 `tinyFonts` 3 条：`核心` `fs=8px r=[1205,227,16,8]`（`text-[8px] text-muted leading-none mt-0.5`）、`MB` `fs=8px r=[1284,227,13,8]`，另有 `1%` `fs=9px`。移动端 8 条 tiny 全为 `fs=9px` 百分比徽标（`text-[9px] … tabular-nums`），无 8px。<br>`<audit-root>/review\out\d1.jsonl`(check=`home_tiny`)、`d1-home_tiny.png`、`m3.jsonl`、`m3-home_tiny.png`；上游 `<audit-root>/audit\core\fin-home-desk.json`、`shots\e3-home-desk-8px.png` | 低 | 中心标签提到 10–11px，或把单位并入数值行（`1.2 GB` 整体 11px）。 |

---

## 3. 已排除的误报（不计入清单）

| # | 候选问题 | 排除理由（含数值） |
|---|---|---|
| 1 | `/logs` 满屏 401/404 红色错误卡 | 该页内容本身就是接口异常记录，401/404 是**被显示的数据**而非页面报错。 |
| 2 | 各模块空态（JM 收藏夹空、Pica 收藏 401 跳登录、书架 0 本） | 上游接口 401/超时/代理不通导致的**环境噪声**；哔咔代理 `127.0.0.1:10809` 未监听（`code:2002`），非前端缺陷。 |
| 3 | `/books`「共 N 本」与排序箭头"重叠" | 两者矩形间距恰为 **0px**，无实际重叠像素；`elementFromPoint` 中心各自命中自身。 |
| 4 | `/collections` 桌面视图切换组"被裁" | 三按钮完整在容器内（右缘 1385 < 1440），无裁切。 |
| 5 | 17 个弹层"四边溢出视口" | 逐个量测四边均在内（最小 inset ≥8px），无一溢出。 |
| 6 | 移动端底部 Tab 栏遮住「加载更多」 | 上游 JM 组报的 52px 重叠为探针把 `fixed` 元素当普通文档流比较所致的**误报**（`blockedPts=0`）。 |
| 7 | `/jm/history` 文本"重复显示" | 上游启发式 `duplicateAdjacent` 对分页渲染出的同标题行误判，非真重复。 |
| 8 | `/jm/comic/150` 封面"裂图" | 首屏计时窗口内懒加载未完成，`16.5s` 后 `12/12` 全部 `ok`；非裂图（属慢加载，见 §4-2）。 |
| 9 | `/jm/week` 右侧 chip "被截断" | `className` 在 DOM 序列化时被截断，实际 CSS 未截断内容。 |
| 10 | 哔咔收藏页「下载整本」28×28 角标与封面/标题"打架" | 几何量测不成立：徽标 `l=22 t=185 b=217`，封面 `16,179 - 189,410`，标题 `top=418`；`badgeOverTitle/badgeOutsideCover/titleClipped` 全 false，放大图确认角标在封面内。 |
| 11 | 哔咔多选条"被顶成两行" | 移动端子元素 `distinctCenterY=1`（全在 `y=739~747`，条 `y=730 h=114`）；桌面 `bar y=828 h=72` 单行。 |
| 12 | 哔咔阅读器页码「1 / 6」重复 | 与 `!toolbarVisible` 互斥渲染，同屏只有一份。 |
| 13 | 阅读器「页码压在紫色圆钮上」 | 是 R-01 的同一条现象，不重复计数（页码层只是 `elementFromPoint` 的命中结果）。 |
| 14 | 详情页 `scale-110` 背景、"AI 面板覆盖比 1.0" | 装饰层与固定定位元素的量测伪阳性，非可见缺陷。 |
| 15 | 设置页「保存」(pica)、「扫描规则」按钮 disabled | 前置条件未满足的**设计内**禁用（未改动/未配规则），语义正确。 |
| 16 | `/jm/search` 标签行右端 chip 被硬切半个字 | 该行是**横向滚动区**（`scrollWidth 2991`），滑出视口的 chip 属正常出屏；无渐隐提示归为体验建议，不计缺陷。 |
| 17 | `/settings?tab=diagnostics` 磁盘空间值 ellipsis、`about` 技术栈裁尾 | 均为**有意截断**（`truncate` / `overflow-hidden`）：同一信息在顶部卡片/上一行完整可见，损失 13px 尾部；列为体验建议（上游标"低"），不计入本次三类清单。 |
| 18 | 9px 图表刻度、角标 badge | 按约定口径，9px 属可接受的次要标注，不计入"字体错乱"。 |
| 19 | `/novel/[id]`、`/series/[id]` 异常页 | 书库 2317 本全为 zip/rar（无 epub/txt）→ `/novel/[id]` 自动降级到 `/reader/[id]`；`/api/series` 为空 → 只见到错误页。属**数据缺失**，非界面缺陷（见 §4-3）。 |
| 20 | 死按钮普查 | 13 个页面×视口组合共 **301 个可点元素**（React fiber props 判定）`dead=0`；core 组 20 例真实命中点击 16 例生效，3 例 `works=false` 逐一排除（已是 active 态 / 空页码 / 探针选中 0×0 隐藏副本）。**不存在"渲染了按钮但没绑事件"的情况。** |

---

## 4. 因环境限制未能验证

| # | 项 | 原因与影响 |
|---|---|---|
| 1 | **哔咔（Pica）绝大多数内容页的真实数据渲染** | 容器内无哔咔登录会话，内容接口返回 401 `code:1002`；即使伪造 token 也因代理 `127.0.0.1:10809` 未监听而返回 `code:2002`。故 `/pica/favorites`、`/pica/history`、`/pica/comic`、`/pica/reader` 多数场景只能用 CDP `Fetch.fulfillRequest` **内存 mock** 出图；mock 下的布局数值不具结论力。仅 R-08（未登录态，真实可复现）、R-04（空关键词，真实可复现）、R-09（历史页用 mock 但时间格式取自真实接口字段）三条有实证支撑。哔咔下载弹层标题硬切（上游 P4）因此列为**待确证**：证据 `<audit-root>/audit\pica\zoom-dlsheet-title-m.png`、`ev-dlsheet-m.json` 存在，但标题长度来自 mock 数据，真实标题是否触发不确定。 |
| 2 | **JM 首屏封面"慢加载"是否构成缺陷** | `/api/jm/image` 最慢响应 **40.8s**，7s 采样时 `covers={n:150, ok:61}`（89 张未完成），上游 6.5s/11.5s 时 `ok=0`、16.5s 才 `12/12`。属**上游服务慢**导致的懒加载空框（`LetterCover.tsx:87-93` 无骨架占位），无法在稳定窗口内判定"裂图"还是"慢加载"，故列观察项，不计入三类清单。 |
| 3 | `/novel/[id]`、`/series/[id]` 正常态 | 数据缺失（无 epub/txt 书籍、`/api/series` 为空），只能看到错误页/降级跳转，无法审查正常布局。 |
| 4 | AI / 翻译面板的功能正确性 | 面板打开后为英文文案、依赖外部模型服务，超出"三类界面问题"范围，仅记录不评。 |

---

## 5. 复核中推翻 / 修正的上游结论

| 上游条目 | 上游结论 | 本次复核 | 处理 |
|---|---|---|---|
| JM P5「签到抽屉「每日签到」重复两次」 | 桌面 h3 `[481,169,80,20]` 与另一处 `[505,224,56,20]`，均 14px，间隔 55px | 本次探针同样走「点签到 → 抽屉展开」，但抽屉内 `每日签到` **仅 1 处**：`r=[505,293,56,20] fs=14px`（可能已修或抽屉展开态不同） | **本次未复现**，暂不计入 16 条清单；保留上游证据（`<audit-root>/audit\jm\dupsign-d-sign-dup.png`、`crop-sign-dup.png`）供开发自查 |
| core C-06「移动底栏压正文，滚到底有 +19px 留白」 | 6 个页面分别 10/6/4/4/2 处重叠；`scrollBottomGap +19px` | 实测 `scrollBottomGap = 0`（无留白）；`/data-admin` 9 处重叠中 4 处是**底栏自身导航文字**，真实内容重叠 5 处；仅确认 `/data-admin`、`/file-stats` 两页 | **修正数值、收窄页面范围、降为「低」**（R-10） |
| core C-08「首页 8px」 | 未区分视口 | 桌面确认 `核心`/`MB` 均 8px；移动端 8 条 tiny **全为 9px 百分比徽标**，无 8px | **限定为桌面**（R-16） |
| detail D-03「异常行行高 48」 | 行高 48 对 40 | 实测异常行 `lineHeight=24px`（`r` 高 24），同级行 `lineHeight=20px`/高 40 | **修正数值**（R-15）；`className` 为空、`display:inline-block`、`padding:0`、`fs:16px` 三项与上游一致 |
| detail D-02「`.reader-toolbar-button` 压过 `.hidden`」 | 仅移动端举了 ⓘ/⛶ 两例 | 合成探针在**桌面同样成立**：`class="hidden reader-toolbar-button"` → `display:flex w=8.1`；`class="sm:hidden reader-toolbar-button"` → `flex` | **确认根因并扩大影响面**：桌面的 `sm:hidden`「更多」也因此常显（R-06） |
| jm P4 / pica P4 / pica P5 等 | 见 §4 | 已在 §4 说明为何不纳入 | — |
| core C-05「须先点按钮才出弹层」 | 未强调 | 确认：仅 `element.click()` 不触发，必须用 CDP `Input.dispatchMouseEvent` 真实命中，否则会误判"弹层不存在" | 记为**探针方法修正**，避免后续复测踩坑 |

---

## 6. 复核证据索引

- 本次自建探针与产物：`<audit-root>/review\`（`verify.mjs` / `reader3.mjs` / `scan.mjs`）；量测 JSON 与截图：`<audit-root>/review\out\`
- 阅读器（R-01 / R-06 / R-15）：`rd3d-reader3.json`、`rd3m-reader3.json`、`rd3d-A~D-*.png`、`rd3m-A~C-*.png`
- 常规页面（R-02 / R-05 / R-10 / R-12 / R-16）：`m1.jsonl`、`m2.jsonl`、`m3.jsonl`、`d1.jsonl`、`d2.jsonl`
- 弹层（R-13 / R-14）：`d4.jsonl`、`m3.jsonl` + `d4-col_smart.png`、`d4-books_dupdlg.png`、`m3-col_smart.png`、`m3-books_dupdlg.png`
- 设置（R-07）：`m5.jsonl`、`m5-settings_mob.png`
- JM（R-03）：`jm1.jsonl`、`jm1-jm_home.png`、`jm1-jm_hist.png`
- 上游原始产物：`<audit-root>/audit\{core,jm,pica,detail}\`（截图 PNG + 量测 JSON，报告引用的文件已逐一核对存在）

**复现命令**（桌面 `1440x900` / 移动 `390x844` 各跑一次）：

```powershell
. <audit-root>/env.ps1
node <audit-root>/review\verify.mjs 1440 900 7000 d9 books_tagfilter col_smart books_dupdlg home_tiny
node <audit-root>/review\verify.mjs 390 844 6500 m9 stats_recent tag_pager dataadmin_mob settings_mob books_dupdlg
node <audit-root>/review\reader3.mjs 1440 900 rd9d
node <audit-root>/review\reader3.mjs 390 844 rd9m
```
