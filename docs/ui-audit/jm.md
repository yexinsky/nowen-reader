# NowenReader 在线漫画(JM)模块 只读界面审查报告

- 目标站点:<http://<instance>:6680>,版本 `dev-93793dd`
- 审查范围:JM 模块 8 个页面 —— `/jm`(推荐 / 最新 / 每周热门 / 连载表)、`/jm/search`、`/jm/week`、`/jm/tags`、`/jm/favorites`、`/jm/history`、`/jm/comic/150`、`/jm/reader/1488671`
- 视口:桌面 `1440x900`、移动 `390x844`(DPR 2),双端均跑
- 审查口径:只记录三类 —— **①按钮无效**、**②PC/移动端界面显示不正常**、**③字体错乱或重复性显示**
- 方法:CDP 无头 Chrome 真实鼠标点击(`Input.dispatchMouseEvent`)+ `elementFromPoint` 命中链 + `getBoundingClientRect` 几何量测 + 截图;全程只读,未修改任何代码,未点破坏性按钮(清空历史 / 取消收藏 / 退出登录 / 删除任务)
- 证据目录:`<audit-root>/audit\jm\`(截图 `*.png`、量测 `*.json`)

---

## 一、问题清单(共 6 条)

| # | 页面 / 路由 | 视口 | 类型 | 现象 | 严重度 |
|---|---|---|---|---|---|
| **P1** | `/jm/reader/1488671?page=37&aid=1488671` 阅读器 | 桌面 + 移动 | **① + ②** | 工具栏展开时,左下「下载」/ 右下「章节」两个浮动按钮被工具栏底栏与页码压住;**点它们实际触发翻页(37→36)**,下载菜单与章节面板都打不开 | 高 |
| **P2** | `/jm/history` 阅读历史(多选态) | 桌面 | **①**(+ 移动 **②**) | 固定底部操作条压住「加载更多」按钮 63%;**点按钮中心完全没反应**(20 行 → 20 行),只有点顶端未遮挡的 14px 才生效(20 → 40) | 高 |
| **P3** | `/jm/favorites` 我的收藏(多选态) | 桌面 + 移动 | **① + ②** | 同型缺陷:桌面「加载更多」被压 63%、点中心无反应(18 卡 → 18 卡),点顶边才 18 → 36;移动压 37%,按钮中心仍可点但视觉上一半藏在操作条后面 | 中 |
| **P4** | `/jm` 首页「最新」Tab | 桌面(移动间歇) | **②** | 首屏封面 10~16 秒内是**整屏深色空框**,只剩日期角标与标题;桌面曲线 `6.5s ok=0 / 11.5s ok=0 / 16.5s 才 12/12` | 中 |
| **P5** | `/jm` 首页「每日签到」抽屉 | 桌面 + 移动 | **③** | 抽屉内「每日签到」标题**重复出现两次**,间隔 55px,字号与颜色完全一致 | 中 |
| **P6** | `/jm/search` 顶部「我的收藏」标签行 | 桌面 + 移动 | **②** | 标签行单行横滑,右端 chip 被容器边缘**硬切半个字**(桌面切在「強」,移动切在「中」),无渐隐、无「更多」提示 | 低 |

> 只在桌面复现或只在移动复现的,已在各条明细里注明;P2 的移动端仅构成遮挡观感(按钮中心仍可点)。

---

### P1 阅读器浮动按钮被工具栏压住且点不动(① + ②)

- 几何(桌面 1440x900,工具栏展开):
  - 左下 FAB `44x44 @ l=16 t=760`;右下 FAB `44x44 @ l=1380 t=760`,两者 `z-30`
  - 工具栏底栏 `fixed bottom-0 z-50 @ top=754 height=146`
  - 进度滑条 `input[type=range] @ l=64 t=785 w=1236 h=4`;页码 `@ l=1364 t=775 w=60 h=16`
  - 相交面积:`overlapFabLeftBar=1936`(= 44×44,**完全落在底栏矩形内**)、`overlapFabRightBar=1936`、`overlapFabRightCounter=704`
- 命中链(`elementFromPoint` 于 FAB 中心):
  - 桌面 `fabL.fabReachable=false`:`svg` → `button.reader-toolbar-button`(上一页按钮)
  - 桌面 `fabR.fabReachable=false`:`span.shrink-0 min-w-[60px] ... font-mono`(页码 `37 / 38`)
  - 移动 `fabL` 命中 `button.reader-toolbar-button`;`fabR` 命中 `div.flex items-center gap-3 py-2 sm:py-3`
- 点击后果:点左下 → `page 37/38 → 36/38`、`menuOpen=false`;点右下 → `page 36/38`、`chapterPanel=false`
- **对照组**(工具栏隐藏,`fab3-*`):两个 FAB 均 `fabReachable=true`,点左下 `menuOpen=true`、点右下 `chapterPanel=true` ⇒ 按钮本身没坏,是工具栏展开时的层级/位移问题
- 视觉:`crop-fab-left-d.png`(下载 FAB 圆钮压在滑条左端、角标 1)、`crop-fab-right-d.png`(页码「37 / 38」被圆钮压住、数字叠在按钮上)、`crop-fab-left-m.png`(移动端同型)
- 截图:`fab2-d-1-toolbar.png`、`fab2-m-1-toolbar.png`、`rd-d-bars-shown.png`、`rd-m-bars-shown.png`
- 量测:`fab2-d.json`、`fab2-m.json`、`fab3-d.json`、`fab3-m.json`、`rd-d.json`、`rd-m.json`、`tb-d.json`、`tb-m.json`
- 代码位置:`app\jm\reader\[pid]\page.tsx:558-559`(`toolbarVisible ? "bottom-24" : "bottom-5"`)、`components\jm\reader\JmChapterNav.tsx:114` 同款 —— `bottom-24` = 96px,小于底栏实际高度 146px(移动 134px),上移量不足

### P2 阅读历史「加载更多」被多选底部操作条吃掉(①)

- 状态:264 条记录、每页 20 行;进入多选后滚到 `scrollY=2007/2007`(`atBottom=true`)
- 几何:「加载更多」`l=787 t=814 w=106 h=38`;底部操作条 `fixed @ l=0 t=828 w=1440 h=72`
- 遮挡量:`overlap = min(852,900) - max(814,828) = 24px` ⇒ **按钮 63% 被盖住,只剩顶端 14px 露在外面**
- 命中测试:`hitLmTop`(t+3px)= `button 加载更多` ✔;`hitLmMid`(中心)= `div.pointer-events-auto mx-auto flex max-w-2xl ...` 文本「已选 0/20 全选本页 下载(0)」✘
- 点击实测:点中心 → `rows 20 → 20`(**无变化**);再点顶边 → `rows 20 → 40` ✔
- 移动 390x844:`overlap = 14px`(37%),`hitLmMid` 仍是「加载更多」,点中心 `rows 20 → 40` ⇒ 移动端功能未失效,只是按钮底部 14px 被压(观感归 ②)
- 视觉:`crop-hist-d-loadmore.png`(按钮下半截明显被半透明操作条覆盖)、`hr-d-bottom.png`、`hr-m-bottom.png`、`hm-d-4-bottom.png`、`hk-d-history-loadmore-blocked.png`
- 量测:`hr-d.json`、`hr-m.json`、`hl-d.json`、`hl-m.json`、`hm-d.json`
- 代码位置:`components\jm\download\BatchDownload.tsx:105-108` 的 `<BatchActionBarSpacer />` + `<BatchActionBar />` 渲染在列表**上方**父级末尾,而 `app\jm\history\page.tsx:192` 的 `<BatchSelectShell>`(`components\BatchSelectShell.tsx:100-102` 同构)只包住列表,分页尾部「加载更多」(`app\jm\history\page.tsx:228-236`)在壳外 ⇒ 96px 占位没落到列表末尾

### P3 收藏页多选底部操作条同样压住「加载更多」(① + ②)

> 线上 `/jm/favorites` 因上游 401 会跳登录页(见「环境噪声」),故用 Fetch 拦截伪造 18 张卡验证布局(`jmfavlm.mjs`,`code:0 + data.list/data.total/data.hasNext` 真实响应形状)

- 桌面 1440x900:`loadMore @ l=787 t=814 w=106 h=38`、`bar @ t=828 h=72` ⇒ **覆盖 24px / 63%**;`hitMid` = 操作条内层 div,点中心 `cards 18 → 18` ✘;点顶边 `18 → 36` ✔
- 移动 390x844:`loadMore @ t=706 h=38`、`bar @ t=730 h=114` ⇒ **覆盖 14px / 37%**;`hitMid` = 加载更多(可点,`18 → 36`),但 `crop-fav-m-loadmore.png` 可见按钮下半截藏在操作条后面
- 卡片本身**没有**被压:桌面最后一行卡片 `b=658` 远高于 `bar.t=828`(`overlapLastCardBar=0`);移动最后一行 `b=506` vs `bar.t=730`,同样为 0
- 视觉:`crop-fav-d-loadmore.png`、`crop-fav-m-loadmore.png`、`lc-fav-d-bottom.png`、`lc-fav-m-bottom.png`、`fb-d-3-bottom.png`
- 量测:`fl-d.json`、`fl-m.json`、`fb-d.json`、`fb-m.json`、`lc-fav-d.json`、`lc-fav-m.json`
- 卡片角标**正常**:桌面「下载整本」`28x28 @ l=278 t=193`(static)、「取消收藏」`28x28 @ l=414 t=193`(absolute z-10),`downloadRemoveOverlap=0` 互不打架;进入多选后两角标按设计全部隐藏、只剩勾选圈(`fb-d-2-selected.png`)

### P4 「最新」Tab 封面长时间空框(②)

- 桌面曲线(`hk-d.json`,点击「最新」后按 5s 采样可见图片数):
  `t=1500ms visible=0` → `t=6500 visible=12 ok=0` → `t=11500 visible=12 ok=0` → **`t=16500 ok=12/12`** → `t=21500 ok=12` → `t=26500 ok=12`
- 视觉:`hk-d-latest-covers.png`(整屏 12 张卡片只有深色空框 + 日期角标 + 标题)、`jmdeep-d-1b.png`
- 移动:较早的一次采集 `t-m-tab-最新.png` 同为整屏空框(该次 `okImgs=21/230`);但补测(`hk-m.json`,2.5s/5s/7.5s/10s/12.5s/15s 六次采样)`visible=4 ok=4` 全部已加载 ⇒ **移动端为间歇性**,不作确定性缺陷记录
- 反证(排除「接口坏/裂图」):稍后同一路由截图 `jmimg-d-最新.png` 全部正常;`/jm/comic/150` 封面 `t=4s complete=false nw=0` → `t=15s complete=true nw=400 nh=533`(`cm-d.json`);`jmimg-d.json` 显示图片接口 200 `image/jpeg`
- 成因:`components\jm\LetterCover.tsx:87-93` 是 `<img loading="lazy">` + 仅 `onError` 才切字母封面,**无骨架/占位**;卡片容器 `components\jm\ComicCard.tsx:60` 为 `bg-muted/10` 纯色框;`/api/jm/image` 单请求最慢 40.8s

### P5 签到抽屉「每日签到」重复显示(③)

- `dupsign-d.json`(只统计**直接文本节点**等于「每日签到」的元素,排除嵌套):
  1. `<h3 class="flex min-w-0 items-center gap-2 truncate text-sm f...">每日签到</h3>` `l=481 t=169 w=80`(抽屉标题,带图标)
  2. `<h3 class="text-sm font-semibold text-foreground">每日签到</h3>` `l=505 t=224 w=56`(日历自身标题)
  - 两者 `font-size: 14px`、`color: rgb(248, 250, 252)` 完全一致,垂直间隔 55px
- 移动 `dupsign-m.json`:`l=17 t=368` 与 `l=41 t=423`,同样两条
- 视觉:`crop-sign-dup.png`(同屏两行「每日签到」)、`dupsign-d-sign-dup.png`、`dupsign-m-sign-dup.png`、`crop-sign-sheet.png`(整张抽屉)
- 抽屉其余部分**正常**:日历 7 列 36 格、已签到日蓝底、今日有描边、底部「✓ 已签到」、`/api/jm/user/sign` HTTP 200、无骨架(`.animate-pulse=0`)、`[aria-label="本月签到日历"]` 36 个子节点(`sg3-d.json`)
- 代码位置:`app\jm\page.tsx:277-287` 抽屉标题写「每日签到」且内部渲染 `<JmSignCalendar bare .../>`;`components\jm\SignCalendar.tsx:115-123` 自身又渲染一次标题 + 年月 ⇒ 二选一即可修

### P6 搜索页标签行右端硬切(②)

- 桌面 1440x900:`crop-search-d-tags.png` 可见标签行最后一个 chip「強…」被容器右边缘切断;量测 `scrollWidth=2991 / clientWidth=1106`
- 移动 390x844:`crop-search-m-top.png` 可见「我的收藏 38 ⌄」下一行 chip 依次 `壁尻× 新娘× 日式小學書包× 拍攝× 中`,「中」只剩半个字;量测 `scrollWidth=2991 / clientWidth=328`,`scrollable=true`,`scrollbar-width:none`(无可见滚动条)
- 横向滚动**功能可用**:`jmscroll.mjs` 把 `scrollLeft` 拉到 `scrollWidth` 后,末位 chip「MM」`visibleRatio 100%`(`sc-m-2-scrolled.png`);问题仅在首屏观感缺渐隐/指示
- 代码 `app\jm\search\page.tsx:575-580`:折叠态 `flex items-center gap-1.5 overflow-x-auto` 单行横滑

---

## 二、环境噪声(非 UI 缺陷,单列)

> JM 上游 AVS 会话已失效,以下现象由后端/上游引起,不计入上表。

| 现象 | 证据 | 说明 |
|---|---|---|
| `/jm/favorites` 直接跳 `/jm/login?from=%2Fjm%2Ffavorites` | `jm-fav-d.png`、`jm-fav-d.json`、`m-d-1-jm_favorites.png`、`m-d-2-jm_favorites.png` | `/api/jm/favorites/folders` 返回 **401**(JM 会话失效)→ `JmAuthGuard` 重定向;收藏页布局改用 Fetch 伪造数据验证(见 P3) |
| 首页快捷入口「收藏」chip 也跳登录 | `btn-d.json`(`landed="/jm/login?from=%2Fjm%2Ffavorites"`) | 同上;其余 4 个 chip 正常(见第三章) |
| `/api/jm/favorites?page=1` HTTP 200 但 `code:2002` | `jmapi.json` | 前端显示「在线源服务不可达」,与 UI 实现无关 |
| `/api/jm/image` 单请求最慢 40.8s;不带 `X-JM-Token` 时 401 | `jmapi.json`、`jmdeep-d.json` | 是 P4 长时间空框的成因之一,但接口本身可用(带 token 时 200 `image/jpeg` 70KB) |
| 搜索结果「共 10,000 条」为上游封顶值 | `jmapi.json`(`searchAll.total=10000`、`searchLoli.total=10000`);页面 `countText="共 10,000 条"`(`s-d.json`、`t-m-search.json`) | 直查 API 同样 10000,前端只是如实显示;`weekTop.total=453` 正常 ⇒ 非前端计算问题 |
| 签到接口 200 正常 | `sg3-d.json`、`dupsign-d.json` | SignCalendar 数据链正常,排除「重复标题 = 错误态」的猜测 |

---

## 三、已核查正常项(避免重复排查)

### 按钮有效性(真实点击 + 落地/状态校验)

| 位置 | 操作 | 结果 | 证据 |
|---|---|---|---|
| `/jm` 5 个快捷入口 | 逐个点击 | 搜索→`/jm/search`、每周必看→`/jm/week`、标签→`/jm/tags`、历史→`/jm/history`;收藏→登录页(环境噪声) | `btn-d.json` |
| `/jm` 4 个 Tab | 推荐 / 最新 / 每周热门 / 连载表 | 4 个均可切换,`selected` 态与内容同步(桌面 `jmt-d.json`,移动 `t-m.json` steps) | `jmt-d-tab-*.png`、`t-m-tab-*.png` |
| `/jm/tags` 标签 | 点「壁尻」 | 跳 `/jm/search?keyword=壁尻&searchType=tag`,结果「共 368 条」80 卡 | `tg-d.json`、`tg-d-tags-click.png` |
| `/jm/week` 期数下拉 | 267 → 266 | `value` 切换生效、列表刷新 | `btn-d.json`、`btn-d-week-select.png` |
| `/jm/week` 类型 chips | 漫画 → 韩漫 | active 样式正确转移(`bg-accent text-white` 从「漫画」转到「韩漫」,全 class 比对) | `tg-d.json`、`tg-d-week-chip.png` |
| `/jm/search` 排序下拉 | 最新 → 最多爱心 | URL 变 `?keyword=萝莉&sort=tf`,select 值 `tf`,`sortNow="最多爱心\|value=tf"` | `s-d.json`、`t-m-search.json` |
| `/jm/search` 重置条件 | 点击 | `selects ["tf","tag","","",""] → ["mr","site","","",""]`、URL 回 `/jm/search`、输入框清空 | `btn-d.json`、`btn-d-search-reset.png` |
| `/jm/reader` 翻页热区 | 右 85% / 左 8% | `37/38 → 38/38 → 37/38` 正常 | `rd-d.json` `pageClicks` |
| `/jm/reader` 工具栏开合 | 点屏幕中央 | 正常显示 / 隐藏 | `rd-d.json`、`rd-m.json` |
| `/jm/reader` 两个 FAB(工具栏隐藏时) | 点击 | `menuOpen=true`、`chapterPanel=true` | `fab3-d.json`、`fab3-m.json` |
| `/jm/history`「加载更多」 | 移动端点中心 | `rows 20 → 40` 正常 | `hr-m.json`、`hl-m.json` |

### 布局 / 字体

| 位置 | 核查项 | 结论 | 证据 |
|---|---|---|---|
| `/jm` 推荐 Tab 桌面 | 用户卡单行、5 chips 等分一行、4 Tab 一行、无横向溢出 | 正常(`sw=1440 cw=1440 delta=0`),无破图 / 乱码 / 极小字号 / 越界 fixed | `jm-home-d.png`、`jm-home-d.json`、`jm-home-d-full.png` |
| `/jm` 推荐 Tab 移动 | 用户卡三行折行、5 chips 均分 358px 单行、底部导航 fixed 57px | 正常(`sw=390 cw=390 delta=0`) | `jm-home-m.png`、`jm-home-m.json`、`jm-home-m-full.png` |
| `/jm` 四个 Tab 桌面 | 每 Tab 溢出 / 破图 / 截断 / 极小字 / 重叠 / 乱码 | 全部为 0(仅推荐 Tab 6 处 `line-clamp-2`/`truncate` 设计内省略,见下) | `jmt-d.json` |
| `/jm/history` 双视口 | 264 条列表、「下载整本」32x32、「清空历史」94x30、「多选下载」90x30 | 正常;无封面行显示**首字渐变块**属正常设计,不是裂图 | `jm-history-d.png/.json`、`jm-history-m.png/.json`、`hm-d-1-list.png` |
| `/jm/week` 双视口 | 期数下拉「2026第266期」+ 类型 chips + 6 列 / 2 列网格 | 正常,无溢出、无破图 | `m-d-2-jm_week.png`、`m-m-0-jm_week.png`、`tg-d-week-chip.png` |
| `/jm/tags` 双视口 | 共 38 项(标签 27 · 作者 11) | 正常 | `m-d-3-jm_tags.png`、`m-m-1-jm_tags.png` |
| `/jm/search` 桌面 | 搜索框 + 4 个筛选 select 常驻首屏、「共 N 条 · 重置条件」行在结果区上方 | 正常,不随结果骨架被替换 | `jm-search-d.png`、`s-d.json` |
| `/jm/search` 移动 | 表单常驻、排序/范围/年份/月份/分类分行 | 正常(仅标签行见 P6) | `crop-search-m-top.png` |
| `/jm/comic/150` 双视口 | 标题 / 作者 / 标签 / 4 个按钮 / 章节 / 评论 | 正常;移动端 4 按钮排 2 行不截断;封面 15s 内加载完(慢加载,非裂图) | `m-d-0-jm_comic_150.png/.json`、`m-m-2-jm_comic_150.png`、`cm-d.json`、`cm-m.json` |
| 移动端底部导航 | 是否压住页面内容末元素 | 全部未遮挡(history `covered=-43`、week `-19`、tags `-20~-27`、search `-87`、jm `-27`) | `mbn2-m.json`、`mbn2-m-*.png` |

### 已排除的误报(重要,避免下游重复排查)

| 曾疑似问题 | 结论 | 依据 |
|---|---|---|
| `/jm/history` 标题与副标题「重复文本」10 条 | **启发式误报**:标题被 `truncate` 后与章节名前缀相同,正文并未真重复两遍 | `jm-history-d.json` `duplicateAdjacent`(全部落在 `min-w-0 flex-1` 内的两个 `<p>`) |
| 移动端底部导航「压住内容 52px」 | **探针误报**:首版把导航自身的 `<a>首页</a>` 计入被遮挡元素;排除 fixed 容器内元素后全部为负值 | `mbn-m.json` → `mbn2-m.json` |
| `/jm/comic/150` 封面「裂图」 | 非裂图:`<img>` 最终 `complete=true nw=400 nh=533`,`/api/jm/image` 200 `image/jpeg` | `cm-d.json`、`jmapi.json` |
| 搜索页排序「切了没反应」 | 首版探针点的是 `<option>` 不触发 `change`;用 value setter + `dispatchEvent('change')` 后排序确实生效 | `jms-d.json` → `s-d.json` / `t-m-search.json` |
| `/jm/week` chips 点后「看不出变化」 | 首版把 class 截断到 60 字符;取完整 class 后确认 active 正确转移 | `btn-d.json` → `tg-d.json` |
| 关键词模式下「月/周/日排行」置灰 +(不可用) | **设计如此** | `app\jm\search\page.tsx:704-707` + `t-m-search.json` `selects` |
| `/jm` 推荐 Tab 6 处「文本截断」 | 均为 `line-clamp-2`(标题两行)/ `truncate`(作者行)的设计内省略,有 `title` 或无溢出破损 | `tr-d.json`、`crop-search-d-tags.png` 对照 |

---

## 四、复现命令

```powershell
. <audit-root>/env.ps1

# P1 阅读器 FAB:工具栏隐藏(对照) / 显示(缺陷)
node <audit-root>/jmfab3.mjs 1440 900 fab3-d
node <audit-root>/jmfab2.mjs 1440 900 fab2-d
node <audit-root>/jmfab2.mjs 390 844 fab2-m

# P2 历史页「加载更多」精确遮挡 + 命中链
node <audit-root>/jmhistrow.mjs 1440 900 hr-d
node <audit-root>/jmhistrow.mjs 390 844 hr-m
node <audit-root>/jmhl.mjs 1440 900 hl-d            # 点击验证 rows 20→20
node <audit-root>/jmclick.mjs 1440 900 hk-d         # 同上 + P4 封面曲线

# P3 收藏页(伪造数据)
node <audit-root>/jmfavlm.mjs 1440 900 fl-d
node <audit-root>/jmfavlm.mjs 390 844 fl-m
node <audit-root>/jmlastcard.mjs fav 1440 900 lc-fav-d

# P4 最新 Tab 封面加载曲线(移动对照)
node <audit-root>/jmlatestm.mjs

# P5 签到抽屉重复标题(精确到直接文本节点)
node <audit-root>/jmdupsign.mjs 1440 900 dupsign-d
node <audit-root>/jmdupsign.mjs 390 844 dupsign-m

# P6 标签行可横滑性 + 末位可见比例
node <audit-root>/jmscroll.mjs

# 按钮有效性回归
node <audit-root>/jmbtn.mjs 1440 900 btn-d
node <audit-root>/jmtags.mjs 1440 900 tg-d
```

---

## 五、结论

1. **P1 阅读器浮动按钮**是最严重的一条:既是显示重叠(②),也直接造成按钮功能错乱(①,点下载变翻页),桌面与移动双端稳定复现,建议优先修。
2. **P2 / P3 是同一个根因**:多选态 `BatchActionBarSpacer` 的位置使「加载更多」仍贴底,被 `z-40` 的固定操作条覆盖 63%(桌面),导致历史页该按钮**中心点不动**;收藏页同型。建议按「把占位高度放到分页尾部之后」一次修两页。
3. **P4 / P6** 属显示质量问题(慢加载无占位、移动端与桌面标签行硬切),优先级低于前三条。
4. **P5** 是本次唯一确定的「重复性显示」问题,抽屉标题与日历标题二选一即可。
5. 除上述 6 条,双视口下未发现其它横向溢出、字号错乱、乱码、文本重叠或按钮失效;`/jm/week`、`/jm/tags`、`/jm/comic/150`、`/jm/history` 列表与 `/jm/search` 表单均正常。
