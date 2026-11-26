# NowenReader 界面审查报告 —— 详情页 / 阅读器 / 设置页

- 目标：http://<instance>:6680 （版本 `dev-93793dd`，`/api/health` 返回 `{"status":"ok","version":"dev-93793dd","go":"go1.26.5"}`）
- 审查范围（本报告）：`/comic/[id]`、`/novel/[id]`、`/series/[id]`、`/group/[id]`、`/reader/[id]`，以及 `/settings` 全部 12 个 Tab
- 视口：桌面 **1440x900**、移动 **390x844**（DPR=1，`Emulation.setDeviceMetricsOverride`，移动端 `mobile:true`）
- 方式：只读。用自建 CDP 探针（`shot2.mjs` / `navProbe.mjs` / `readerOverlap.mjs` / `readerClick*.mjs` / `deadBtn.mjs` / `hiddenScan.mjs` 等）采集 DOM 几何、计算样式、`elementFromPoint` 命中链、React fiber props，并做真实鼠标点击验证。**未修改任何代码，未保存任何设置项**（设置页全部用 URL 直达切换 Tab，未点「保存 / 保存代理 / 改下载目录 / 清空历史 / 退出登录」）。
- 产物目录：`<audit-root>/audit\detail\`（截图 `*.png` + 原始度量 `*.json`）

## 结论摘要

| 统计 | 数量 |
| --- | --- |
| 覆盖范围 | 5 类详情/阅读路由的 **12 个路径变体**（正常/无进度/有进度/404/降级）+ 设置页 **12 个 Tab**，桌面 1440x900 与移动 390x844 各测一遍（404 与降级页部分只取单视口）；另有工具栏展开前后、更多菜单、阅读设置面板等交互态补充采样 |
| 确认缺陷 | **6 条**（中 4 / 低 2，无高危） |
| 按主类归并 | ①按钮无效 **1** 条（D-01）、②界面显示不正常 **4** 条（D-02/D-04/D-05/D-06）、③字体错乱或重复显示 **1** 条（D-03）；D-01 同时构成②的视觉重叠，D-02 同时构成③的功能重复，故交叉计数为 1/5/2 |
| 高危（阻断功能） | 0 条 |
| 死按钮（绑事件缺失） | **0** 个（13 个页面×视口组合、共 **301** 个可点元素全扫，`dead=0`） |

---

## 缺陷清单

| 编号 | 类别 | 页面 | 视口 | 现象 | 证据（截图 + 关键数值） | 严重度 |
| --- | --- | --- | --- | --- | --- | --- |
| **D-01** | ①按钮无效（+②视觉重叠） | `/reader/[id]` 阅读器（工具栏展开时） | 桌面 1440x900 + 移动 390x844 | 点击页面唤出工具栏后，右下两个浮动圆钮（**AI 阅读助手**、**Translate Page**）被 `z-50` 底栏整块压住，`elementFromPoint` 命中的是底栏内容而非按钮本身，真实点击无任何反应；同时底栏里的页码文字 `1 / 129` 直接压在紫色 AI 圆钮左半边 | `rov-d.json` / `rov-m.json`：两钮 `clickable:false`，桌面命中 `div.flex items-center gap-3...`（底栏 rect `[0,754,1440,146]`）、移动命中 `span.shrink-0 min-w-[60px] ... reader-t...`（页码文本）；两钮 48x48 被覆盖面积比 **1.0**（area 2304）。`rc-d.json`/`rc-m.json`：真点击后 `bodyLen` 仍为 86/65、`hasAI=false`、`hasTranslatePanel=false`。对照组 `rc2-d.json`/`rc3-d.json`/`rc3-m.json`（不唤出工具栏点同坐标）：`bodyLen` 145/255/234，出现 “Page Translation …”“AI Reading Assistant …”。截图 `rov-d.png`、`rov-m.png`、`clip-reader-br-d.png`（5x）、`clip-reader-br-m.png`（4x） | **中** |
| **D-02** | ②界面显示不正常（+③重复性显示） | `/reader/[id]` 阅读器顶栏 | 桌面 1440x900 + 移动 390x844 | 顶栏按钮的响应式显隐完全失效：移动端多出 2 个**本应桌面专用**的按钮（「详情 ⓘ」「全屏 ⛶」），把标题挤到 **122px**（显示为「1-10[母印堂 (シ…」），移除这两个按钮后标题可回到 **202px**（+80px）；桌面端反过来多出 1 个**本应仅移动端**的「更多 ⋯」按钮，点开后的菜单里「详情」「全屏」与顶栏 ⓘ / ⛶ 图标**功能完全重复**（图标指纹一致） | 根因：`frontend/src/app/globals.css:464` 的 `.reader-toolbar-button { display:flex }` 写在**未分层（unlayered）CSS** 里，而 Tailwind 的 `.hidden` / `sm:hidden` 在 `@layer utilities` 内 —— 未分层规则优先，`display:flex` 反压 `display:none`。实测探针 `ev-hidden-probe-m.json`：合成 `class="hidden reader-toolbar-button"` → `display:flex`（w=8.1，未被隐藏），而单独 `class="hidden"` → `display:none`。泄漏清单 `ev-hs2-m.json`（移动 2 个：rect `[262,6,36,36]`,`[302,6,36,36]`）/ `ev-hs2-d.json`（桌面含 `sm:hidden` 的「更多」`[1388,10,36,36]`）。标题宽度 `ev-titlew-m.json`：`before.clientW=122` → `after.clientW=202`（`reclaimedPx=80`）。截图 `tbar-m.png`（移动顶栏 6 个图标）、`rmenu-d.png` / `mrow-d.png`（桌面「更多」菜单）、`morecmp-d.log`（`menu.items` 含 详情/书签列表/快捷键/缩略图/沉浸模式/真实翻页/全屏，`duplicatedByIcon:["详情","全屏"]`） | **中** |
| **D-03** | ③字体错乱或重复显示 | `/reader/[id]` 阅读器「更多」菜单 | 桌面 1440x900 + 移动 390x844 | 菜单里「**真实翻页（实验）**」一行没有样式类，被渲染成**原生 button**：字号 **16px**（同级各行 14px）、`padding:0`（同级 `10px 14px`）、无 hover/无 `w-full`、行高 48px（同级 40px）；移动端还把 10px 的「小屏设备暂不支持」硬塞进同一行，文字明显比其它行大一号、对齐也不一致 | 源码 `frontend/src/components/reader/ReaderToolbar.tsx:310-324` 的 `<button>` 缺 `className`。实测 `ev-fliprow-m.json`：`row.className="(empty)"`、`fontSize:"16px"`、`padding:"0px"`、`display:"inline-block"`、`textAlign:"center"`、`disabled:true`、rect `[202,246,176,48]`；同级「沉浸模式」`className="flex w-full items-center gap-2.5 px-3.5 py-2.5 text-sm …"`、`fontSize:"14px"`、`padding:"10px 14px"`、rect `[202,206,176,40]`。桌面 `ev-fliprow-d.json` 同现象（rect `[1248,250,137,24]` vs 同级 `[1248,210,176,40]`）。截图 `mrow-m.png`、`mrow-d.png` | **中** |
| **D-04** | ②界面显示不正常 | `/settings?tab=jm-source` 在线漫画源（移动端） | 移动 390x844 | 代理输入框被压扁成 **20px 高**（class 声明 `h-10` 应为 40px），旁边「保存」按钮 40px —— 输入框看起来像一条细边，与按钮高度明显不齐 | 源码 `frontend/src/components/settings/JmSourcePanel.tsx:436-451`：父容器 `flex flex-col gap-2 sm:flex-row`，input 为 `h-10 min-w-0 flex-1`；移动端父容器是**纵向** flex，`flex:1 1 0%` 让 input 在主轴被压缩到 line-height。实测 `ev-jminput-m.json`：`rect [37,569,316,20]`、`offsetH 20`、`computedH "20px"`、`flex "1 1 0%"`、`padding 0px/0px`、`parentDir "column"`、`parentRect [37,569,316,68]`、`btnH 40`。对照：哔咔 Tab 的代理输入框父容器是 `flex gap-2`（`PicaSourcePanel.tsx:293`），实测 `234x40` 正常。全设置页扫描（`e-squash.js`，声明 h-8~h-12 但渲染矮 ≥8px 的控件）**仅此 1 例**。截图 `clip-jm-input-m2.png`（4x）、`sel-jm-input-m.png`、`m-jm-source.png` | **中** |
| **D-05** | ②界面显示不正常（信息展示不全） | `/settings?tab=diagnostics` 系统诊断（移动端，展开「分类诊断→存储空间」） | 移动 390x844 | 「磁盘空间」行的值被省略号截断成 `已用 89% (836.5 GB / 93…`，看不到总容量与「磁盘空间紧张」提示 | 源码 `frontend/src/components/NASDiagnosticsPanel.tsx:539`：`<span className="truncate text-xs text-muted max-w-[50%]">`。实测 `sel-diag-trunc.json`：`rect [195,414,150,16]`、`scrollWidth 270` > `clientWidth 150`、`textOverflow:ellipsis`。同一信息在页面顶部「需要处理」卡片里是完整显示的（`at-diag-storage.png`）。截图 `sel-diag-trunc.png`（含整行上下文）、`ev-diag-row.png`（4x 局部） | **低** |
| **D-06** | ②界面显示不正常（内容被裁切） | `/settings?tab=about` 关于（移动端） | 移动 390x844 | 「技术栈 → Comics」的取值 `ZIP/CBZ/RAR/CBR/7Z/PDF/AZW3` 横向溢出卡片右边界 **13px**，被卡片的 `overflow-hidden` 裁掉尾部字符（自然滚动位置下末尾「3」被切） | 源码 `frontend/src/app/settings/page.tsx:825-830`：`flex items-center gap-3.5 px-5 py-3` 行内 `w-20 shrink-0` 标签 + 无 `min-w-0`/无换行的值 `span`。实测 `e-aboutovf.js`（`ev-aboutovf-m.json`）：值节点 `rect [161,665,225,20]`、`right=385.5`；最近裁切祖先 `overflow-hidden rounded-lg border border-border bg-card` 为 `[16,374]`，`scrollWidth 369 > clientWidth 356`、**`hiddenOverflowPx=13`**、`scrollLeft=0`。截图 `ev-about-natural.png`（未滚动、6x：可见末尾被卡片右边缘切掉）、`ev-about-row.png`（整行 4x）、`m-about.png` | **低** |

---

## 逐条详情与修复线索（仅建议，未改代码）

### D-01 阅读器工具栏展开时浮动按钮被底栏遮挡

- 复现：打开 `/reader/36faa4106673` → 点击页面中央唤出工具栏 → 点击右下角紫色（AI 阅读助手）或蓝色（Translate Page）圆钮。
- 几何：浮动钮 `fixed bottom-20 right-4 z-30` / `fixed bottom-20 right-20 z-30`（`frontend/src/app/reader/[id]/page.tsx`），底栏 `fixed bottom-0 left-0 right-0 z-50`（`frontend/src/components/reader/ReaderToolbar.tsx:341`）。桌面底栏高 146px、从 y=754 起；浮动钮 y=772~820 —— 完全落入底栏区域，且 `z=30 < z=50`。桌面精确命中：AI 钮中心被 `DIV.flex items-center gap-3 sm:gap-4 py-2 sm:py-3` 拿到、Translate 钮中心被 `BUTTON.reader-toolbar-button h-9 w-9 sm:h-8 sm:w-8 sh`（翻页箭头）拿到（`ev-pageind-d.json`）。
- 移动端底栏 `[0,710,390,134]`，浮动钮 `y=720~764`，同样整块被压住，且底栏页码文本 `1 / 129`（`rect [318,729,60,16]`）与 AI 钮（`rect [330,720,44,44]`）在视觉上直接叠加。
- 影响：每次唤出工具栏（工具栏 4 秒后自动隐藏，`frontend/src/app/reader/[id]/page.tsx:247-255`）期间这两个功能不可用，且页码与圆钮图形互相污染。
- 建议方向：工具栏可见时把浮动钮上移/淡出，或提高浮动钮层级并给底栏留出避让区；页码指示器与浮动钮不要落在同一水平带。

### D-02 阅读器顶栏响应式显隐失效（未分层 CSS 压过 Tailwind utilities）

- 复现：移动端 390x844 打开 `/reader/[id]`，唤出工具栏 → 顶栏出现 6 个图标（返回 / 标题 / 书签 / 设置 / **ⓘ** / **⛶** / **⋯**），其中 ⓘ 与 ⛶ 本应只在桌面出现（`hidden sm:flex`）。
- 桌面 1440x900 相反：出现「⋯ 更多」（`sm:hidden`），菜单里「详情 / 全屏」与顶栏 ⓘ / ⛶ 重复。
- 根因（已用 CSSOM + 合成元素探针证实）：`globals.css` 用 `@import "tailwindcss"` 把 utilities 放进 `@layer utilities`（线上 CSS `index-BQogjRAk.css` 中 `.hidden{display:none}` 位于 `@layer utilities`，区间 12875–205013；而 `.reader-toolbar-button{...display:flex}` 在 211509，**不在任何 @layer 内**）。CSS 层叠规则：未分层样式优先于任何层。因此凡是带 `reader-toolbar-button` 的按钮，`hidden` / `sm:hidden` 一律失效。
- 受影响元素：移动端 2 个（ⓘ / ⛶）、桌面端 1 个（⋯ 更多）。底栏另有 6 个 `hidden sm:inline` 的文字标签（单页/双页/长条/左→右/自动翻页/夜间），因为只在 `sm` 及以上显示、桌面本就该显示，不构成泄漏。
- 其它观察：泄漏出来的 ⓘ / ⛶ 点击是**有效**的（`e-leakbtn-m.js`：点 ⓘ 后 body 文本 65→238 且出现详情抽屉；点 ⛶ 后 `document.fullscreenElement=true`），所以不是「死按钮」，而是「重复 + 挤占」。
- 建议方向：把 `.reader-toolbar-button` 等组件的 `display` 移入 `@layer components`（或改用 `@utility`），或至少在需要显隐的按钮上使用 `!hidden` / `sm:!flex`。

### D-03 「更多」菜单里「真实翻页（实验）」一行无样式

- 现象：该行字号 16px（其余 14px）、无内边距、无左对齐图标缩进、无 hover，移动端还把「小屏设备暂不支持」10px 提示塞进同一行 → 视觉上像一段裸文本而非菜单项。
- 源码：`ReaderToolbar.tsx:310-324` 的 `<button>` 只有 `onClick/disabled/title`，**缺 `className`**（对比 `:277/:286/:295/:304/:327` 均有 `flex w-full items-center gap-2.5 px-3.5 py-2.5 text-sm …`）。
- 建议方向：补上与同级一致的 className，并把「小屏设备暂不支持」放进独立的说明行。

### D-04 移动端 jm-source 代理输入框被压扁

- 现象：`input` 高 20px（声明 `h-10`=40px），与右侧「保存」按钮 40px 不齐；实测量到的 `padding:0`、`flex:1 1 0%`、父容器 `flex-col`。
- 建议方向：给 input 加 `shrink-0`，或在移动端改为 `h-10 w-full`（父容器 `flex-col` 时不参与纵向 flex 压缩）。

### D-05 / D-06 两个移动端信息展示不全

- D-05：`max-w-[50%]` + `truncate` 在 390px 下只给 150px，而文本需要 270px。建议改为 `break-words` 或允许换行/两行截断。
- D-06：技术栈行的值缺 `min-w-0` 且未换行，卡片 `overflow-hidden` 裁掉 13px。建议值 `span` 加 `min-w-0 break-all`，或该行改为可换行布局。

---

## 重点核查项结论（本会话改动相关）

| 核查项 | 结论 | 证据 |
| --- | --- | --- |
| 设置页「阅读 → 进章节从第 1 页开始」开关是否正常渲染 | **正常**。桌面 `44x24 @ (1207,960)`、`aria-checked=false`；移动 `44x32`；标题 14px/20px、说明 3 行完整换行，`scrollWidth==clientWidth`、`outOfParentX=false`、`coveredBy=null` | `at-jmswitch.png` / `at-jmswitch.json`、`s-jm-source-d.json`（`switches`）、`m-jm-source.json` |
| 代理输入框是否必填 | **是（前端守卫）**。DOM 上无 `required` 属性，但空值保存会被拦：`JmSourcePanel.tsx:179-197` `toast.error("代理不能为空:在线漫画必须经代理访问,不支持直连")`；哔咔侧 `PicaSourcePanel.tsx:135-137` 同。输入框存在且带当前值 `<proxy>` | `s-jm-source-d.json`（`inputs`）、`JmSourcePanel.tsx:179-197/440-471` |
| 是否还有「清除(直连)」按钮 | **已不存在**，与改造预期一致。按钮清单只有「保存」60x40；`lib/jm/client.ts:345` 仅剩代码注释里的 “proxy 空串 = 清除直连” 字样，不是 UI | `s-jm-source-d.json`（`buttons` 全文，无该文案）、`m-jm-source.json` |
| 手机设置页长说明文字是否溢出 / 开关行是否错位 | 12 个 Tab 全查：说明文字 3 行完整换行、`textOverflow` 仅 D-06 一处（且属技术栈取值不是说明文字）；开关行未错位（`squash` 扫描仅 D-04 一处 height 异常） | `s-*/m-*.json` 汇总（见下「覆盖矩阵」） |
| 详情页操作条在 390px 下是否被挤成省略号 | **没有**。进度态 `/comic/61696feb8c4a`：「继续阅读 (第 35 页)」`240x44`、`scrollWidth==clientWidth`；「删除漫画」`240x42`，两按钮各占整行 | `m-comic-prog.json`、`e-detailactions.js` 结果（39 个可点元素 `truncated=false`）、`m-comic-prog.png` |
| 阅读器工具栏 / 页码指示器 / 浮动按钮是否互相遮挡 | **是**，见 D-01、D-02、D-03 | 同上 |
| 详情页操作条里有没有「下载」按钮（是否会被挤成省略号） | **本次范围内没有这个按钮，不适用**。本地详情页 `/comic/[id]`（`frontend/src/app/comic/[id]/page.tsx`，1464 行）中搜索 `下载` **零命中**，实际只有「开始阅读」/「继续阅读 (第 N 页)」「删除漫画」「更换封面」；下载入口属于**在线源详情页** `jm/comic/[aid]`（`frontend/src/app/jm/comic/[aid]/page.tsx:792` 用 `components/jm/download/DownloadButton.tsx`「下载/下载中 N%/已归档」）与 `pica/comic/[id]`，以及设置页 `jm-source` 的「下载任务(10)」按钮（130x36，实测可点）。这些在线源页面不在本任务范围，已记入移交说明 | `s-comic-d.json` / `m-comic*.json` 的 `buttons` 全量（无「下载」文案）、`frontend/src/app/comic/[id]/page.tsx` grep 零命中 |

---

## 覆盖矩阵（全部实测，指标全 0 表示无该类问题）

指标缩写：`ox`=横向滚动 delta，`off`=越界元素，`btn`=按钮数，`tru`=真截断（scrollWidth>clientWidth+2），`dup`=相邻重复文本，`cov`=控件被遮挡，`txtOv`=文本溢出父容器，`h`=scrollHeight。

| 页面 | 桌面 1440x900 | 移动 390x844 |
| --- | --- | --- |
| `/comic/36faa4106673`（无进度） | ox0 off1* tru0 dup0 cov0 h1337 | ox0 off1* tru0 dup0 cov0 h2632 |
| `/comic/61696feb8c4a`（进度 16%，第 35 页） | ox0 off1* tru0 dup0 cov0 h1303 | ox0 off1* tru0 dup0 cov0 h2600 |
| `/comic/does-not-exist` | — | 「无权访问」+ 返回/返回书架，无缺陷 |
| `/novel/36faa4106673` | 自动 302→`/reader/36faa4106673`（无小说资源） | 同上 |
| `/series/does-not-exist` | 「作品加载失败 + 返回书架(88x36)」，无缺陷 | 同（无 series 数据，环境限制） |
| `/group/1`、`/group/2` | ox0 off0 tru0 dup0 cov0 h920 | ox0 off0 tru0 dup0 cov0 h1358 |
| `/group/999` | — | 「漫画不存在 返回书库」 |
| `/reader/36faa4106673` | **D-01 / D-02 / D-03** | **D-01 / D-02 / D-03** |
| `/settings?tab=account` | ox0 off0 tru0 dup0 cov0 btn18 h1070 | ox0 off0 tru0 dup0 cov0 btn7 h1190 |
| `/settings?tab=reader` | ox0 off0 tru0 dup0 cov0 btn15 h907 | ox0 off0 tru0 dup0 cov0 btn4 h1234 |
| `/settings?tab=jm-source` | ox0 off0 tru0 dup0 cov0 btn22 h1712 | ox0 off0 tru0 dup0 cov0 btn11 h2074；**D-04** |
| `/settings?tab=pica` | ox0 off0 tru0 dup0 cov0 btn21 h993 | ox0 off0 tru0 dup0 cov0 btn10 h1132 |
| `/settings?tab=site` | ox0 off0 tru0 dup0 cov0 btn16 h1354 | ox0 off0 tru0 dup0 cov0 btn5 h1574 |
| `/settings?tab=ai` | ox0 off0 tru0 dup0 cov0 btn23 h907 | ox0 off0 tru0 dup0 cov0(1 误报) btn12 h1045 |
| `/settings?tab=scan-rules` | ox0 off0 tru0 dup0 cov0 btn30 h2007 | ox0 off0 tru0 dup0 cov0 btn19 h2586 |
| `/settings?tab=users` | ox0 off0 tru0 dup0 cov0 btn17 h907 | ox0 off0 tru0 dup0 cov0 btn6 h900 |
| `/settings?tab=libraries` | ox0 off0 tru0 dup0 cov0 btn17 h907 | ox0 off0 tru0 dup0 cov0 btn6 h1022 |
| `/settings?tab=user-groups` | ox0 off0 tru0 dup0 cov0 btn13 h907 | ox0 off0 tru0 dup0 cov0 btn2 h900 |
| `/settings?tab=diagnostics` | ox0 off0 tru0 dup0 cov0 btn22 h1008 | tru1 → **D-05**；btn11 h1208 |
| `/settings?tab=about` | ox0 off0 tru0 dup0 cov0 btn13 h1684 | txtOv1 → **D-06**；btn2 h1922 |

`*` `/comic/*` 的 `off=1` / `txtOv=1` 均为详情页背景模糊封面装饰层，见「已排除误报」。

### 死按钮扫描（React fiber props + 可见性）

13 个「页面×视口」组合（`/comic/…` d+m、`/reader/…` d+m、`/group/1` d+m、`settings` 的 reader/jm-source/pica/about/diagnostics 各取 d+m）共扫 **301** 个可点元素（39/40/17/16/19/29/30/10/38/18/16/12/17），**`dead=0`**：不存在「有按钮但没绑事件」的情况。

### 按钮不可点性复核

- 阅读器工具栏 17 个按钮：唤起工具栏后 `elementFromPoint` 全部命中自身，`clickable=true`（`top-d.json` 顶栏 7 个 / `top-m.json` 顶栏 6 个均 `clickable:true`）。
- 详情页 39 个可点元素：`truncated` 全 false。
- 设置页唯一 `disabled` 的合理按钮：`scan-rules` 未配规则时的「执行（仅新增）/执行（全库）」、`pica` 未改动时的「保存」（`proxyInput.trim() === settings.proxy` 时禁用，`PicaSourcePanel.tsx:305`）。

---

## 环境噪声（非缺陷，但影响本次可测范围）

1. **没有小说文件**：`/api/comics` 全量 2317 本，扩展名只有 `zip 2275` / `rar 42`，`epub/txt/mobi/azw3` **为 0**（`/api/comics?contentType=novel` 返回 `total:0`）。因此 `/novel/[id]` 只能用漫画 id 验证降级行为：桌面与移动都自动 `replace` 到 `/reader/[id]`，页面表现即阅读器（无报错、无空白）。小说阅读器的专属 UI（字号/行距/分栏设置等）**本次无法覆盖**。
2. **没有 series 数据**：`/api/series` 返回 `{"series":[]}`，`/series/[id]` 只能用不存在的 id 验证错误页（「作品加载失败 + 返回书架」，`88x36`，无越界/截断）。
3. **阅读器 AI/翻译面板为英文文案**（“AI Reading Assistant”“Page Translation / Detecting and translating text...”），与全站中文界面不一致；这是文案本地化问题，不属本次三类缺陷，仅记录。
4. 移动端底部导航（`fixed bottom-0 h=57 z-50`）不遮挡内容：`AppShell` 有 `pb-[calc(3.5rem+env(safe-area-inset-bottom,0px))] sm:pb-0`（`frontend/src/components/AppShell.tsx:15`），实测滚到底 `coveredAtBottom=[]`。

## 已排除误报（复核实为设计行为或探针假阳性）

1. **详情页 `div.absolute inset-0 scale-110 … opacity-25`**（桌面 `left=-72 right=1512`、移动 `left=-19 right=410`）：这是详情页背景模糊封面的装饰层，父容器 `overflow-hidden`，不产生横向滚动（`overflowX.delta=0`）。`s-comic-*.json` / `m-comic*.json` 里的 `off` / `txtOv` 各 1 条均指它。
2. **`/settings?tab=ai` 移动端 `cov=1`**（「使用量统计」326x32 中心点被 `DIV.relative flex h-14 items-center justify-around` 命中）：折叠面板 header 与其同层容器的 `elementFromPoint` 抖动；`bottomCheck.mjs` 实测滚到底 `coveredAtBottom=[]`、`lastContentClearOfNav=true`，点击「使用量统计」可正常展开。判为探针假阳性。
3. **桌面端 `moreBtn=null`（`readerToolbarTest.mjs`）**：脚本按 `sm:hidden` 语义假设桌面上没有「更多」；实际因 D-02 该按钮在桌面也存在，扫描脚本按 `display` 过滤才拿到。这不是「按钮缺失」，而是 D-02 的一部分。
4. **`/settings?tab=pica` 的「保存」为 disabled**：因未改动输入框时 `proxyInput === settings.proxy`，属正确的防误提交行为。
5. **`/settings?tab=scan-rules` 桌面 3 个 disabled 按钮**：未配置规则时「执行（仅新增）/执行（全库）」禁用，符合预期。
6. **详情页移动端「更换封面」半透明浮层压在封面底部**：`92x32`，位于封面下缘、不完全遮挡任何文字/图形，点击可工作 —— 设计如此。
7. **`shot2.mjs` 早期的 `coveredButtons=6`**：因把折叠线以下（`y>899`）的元素也纳入检测；改为只测「完全在视口内」的控件后归零（`s-jm-source-d.json` 复跑为 0）。同理早期 `truncated=20` 是 `truncate` 类但 `scrollWidth==clientWidth` 的元素，按 `sw>cw+2` 复核后为 0。

---

## 证据文件索引（`<audit-root>/audit\detail\`）

- D-01：`rov-d.png` `rov-m.png` `clip-reader-br-d.png` `clip-reader-br-m.png` `rc-d.png` `rc-m.png` `rc2-d.png` `rc3-d.png` `rc3-m.png`；`rov-d.json` `rov-m.json` `ov-reader-d.json` `ov-reader-m.json` `ev-pageind-d.json` `ev-pageind-m.json` `rc-d.json` `rc-m.json` `rc2-d.json` `rc3-d.json` `rc3-m.json`
- D-02：`tbar-m.png` `rmenu-d.png` `mrow-d.png` `top-d.png`；`ev-hs2-m.json` `ev-hs2-d.json` `ev-hidden-probe-m.json` `ev-titlew-m.json` `top-m.json` `top-d.json` `morecmp-d.log` `mrow-m.log` `mrow-d.log`
- D-03：`mrow-m.png` `mrow-d.png` `rmenu-m.png`；`ev-fliprow-m.json` `ev-fliprow-d.json`
- D-04：`clip-jm-input-m2.png` `sel-jm-input-m.png` `m-jm-source.png`；`ev-jminput-m.json` `m-jm-source.json`
- D-05：`sel-diag-trunc.png` `ev-diag-row.png` `at-diag-storage.png`（对照：同一信息完整显示）；`sel-diag-trunc.json`
- D-06：`ev-about-natural.png` `ev-about-row.png` `m-about.png`；`ev-aboutovf-m.json`
- 正向核查：`at-jmswitch.png` `at-jmswitch.json`（开关）、`s-jm-source-d.json`（无「清除(直连)」、input 列表）、`m-comic-prog.png` `m-comic-prog.json`（操作条 240x44 未截断）、`dead.log`（死按钮 0）
- 全量页面截图/度量：`s-*.png|json`（桌面 12 Tab + 详情/合集/阅读器）、`m-*.png|json`（移动同套）

## 本次范围外的相邻线索（移交，未取证）

1. `/jm/comic/[aid]`、`/pica/comic/[id]` 的下载按钮（`components/jm/download/DownloadButton.tsx`、`components/pica/download/DownloadButton.tsx`）与批量下载浮动条（`BatchSelectShell.tsx` / `BatchActionBar.tsx`，源码注释提到「操作条会跟着滚出可视区」的历史问题）—— 属在线源页面，未在本任务分配范围内，建议由负责 `jm/*`、`pica/*` 的同事重点核查 390px 下的浮动条与底部导航是否重叠。
2. 阅读器 D-01 的同类风险：`/jm/reader/[pid]/page.tsx:547-557` 与 `/pica/reader/[id]/page.tsx:583-591` 也有**左下**浮动「批量下载」按钮（注释称「避开右下角章节导航」），建议同样验证工具栏展开时是否被底栏遮挡。
3. `frontend/src/components/reader/ReaderToolbar.tsx:310-324` 缺 className（D-03）与 `frontend/src/app/globals.css:464` 未分层 `display:flex`（D-02）是同一文件族的两个独立问题，修复时注意不要互相掩盖。
