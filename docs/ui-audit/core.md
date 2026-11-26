# NowenReader 常规页面界面审查报告（auditor-core / t1）

- 目标：`http://<instance>:6680`，版本 `dev-93793dd`（线上只读审查，未修改任何代码、未提交任何写操作）
- 视口：桌面 **1440×900**、移动网页端 **390×844**（`mobile:true`，DPR=1）
- 负责页面（12 个，每页两视口，共 24 次采集）：`/`、`/books`、`/collections`、`/history`、`/recommendations`、`/stats`、`/logs`、`/file-stats`、`/tag-manager`、`/scraper`、`/data-admin`、`/data-qa`
- 只查三类：①按钮无效 ②PC/移动界面显示不正常 ③字体错乱或重复性显示
- 证据目录：`<audit-root>/audit\core\shots\`（截图）与 `<audit-root>/audit\core\`（JSON 探针结果）

## 结论摘要

- 共 **8 条缺陷**：高 1 条、中 4 条、低 3 条。
- **未发现「按钮无效」**：本次共对 20 个功能按钮做了真实鼠标点击（`Input.dispatchMouseEvent`，含浏览器命中测试，可发现「被遮挡收不到指针事件」），16 个直接产生 DOM/网络/路由变化，其余 3 个为「已是当前态」或「空输入无动作」的预期行为，1 个为探针选中 0×0 隐藏副本的误报（详见 §4）。
- 24 个视图的整页横向溢出（`documentElement.scrollWidth - clientWidth`）**全部为 0**；无裂图（`brokenImages=0`）、无乱码（`mojibake=0`）、无失效链接（`badLinks=0`）、无超视口固定弹层（17 个弹层 `overflowRight/Left/Top/Bottom` 全为负）。
- 出屏内容集中于两处：`/stats` 移动端「最近阅读」卡片（16 个文本节点出屏，最远超出 490px）、`/tag-manager` 移动端分页条（左右各出界 9px）。其余 22 个视图出屏文本数为 0。

## 1. 问题清单

| 编号 | 类别 | 页面 | 视口 | 现象 | 证据（截图名 + 关键数值） | 严重度 |
|---|---|---|---|---|---|---|
| C-01 | ③重复性显示 | /books | 桌面 + 移动 | 「标签筛选」在同一筛选栏内被渲染两遍：一遍是「图标 + 标签筛选 + 数字按钮」，紧邻又输出一遍「N 标签筛选」 | `shots\e1-books-desk-tagfilter.png`(7×)、`shots\e1-books-mob-filterbar-wide.png`(3×)、`shots\books-desk.png`、`shots\books-mob.png`；`p-tagfilter-desk.json`：两处 span 分别为 r=[317,205,48,16] 与 r=[425,204,73,16]（桌面），r=[57,219,48,16] 与 r=[37,251,73,16]（移动）；共同父容器 `div.flex.gap-2.flex-col.sm:flex-row.sm:items-center` 的 `scrollWidth=clientWidth`（桌面 1086/1086、移动 316/316）→ 不是溢出挤压，是同一文案被输出两遍 | 中 |
| C-02 | ②显示不正常 | /stats | 移动 390×844 | 「最近阅读」卡片被撑到 891px 宽（页面仅 390px），内容右侧整体出屏被裁：「查看全部」链接与每条记录的阅读时长完全不可见 | `shots\stats-mob-recent-view.png`、`shots\crop-stats-mob-recent.png`、`shots\stats-mob-full.png`；`p-stats-cause-mob.json`：容器 `div.grid.lg\\:grid-cols-2.gap-4` 自身 r=[16,1210,**358**,612]，但 `gridTemplateColumns` 计算值 = **890.828px**（移动端单列却取了两列宽度），子卡片 r=[16,1210,**891**,612]；出屏文本 16 个，`0m` r=[862,1299,18,16] 右侧超出 **490px**、`11/26 19:30 · p1→3` 超出 **460px**、「查看全部」a r=[822,1231,64,32] 整块在视口外；裁切者 `div.animate-page-enter.overflow-x-hidden`（`p-stats3-mob.json` 祖先链）；`outscreen.txt`：stats-mob leafOverflow=16 / wideContainers=27，其余 23 视图为 0 | 高 |
| C-03 | ②显示不正常 | /tag-manager | 移动 390×844 | 分页条横向溢出：最左「第一页」按钮左半出界，最右「跳转」按钮右缘超出视口，「跳转」二字被挤成竖排两行 | `shots\tag-mob-pager-view.png`；`p-tagpager2.json`：父条 r=[16,4191,358,104] 但 `scrollWidth=383 > clientWidth=358`；内层行 r=[-9,4251,**408**,36]；「第一页」button r=[**-9**,652,26,32]（左出界 9px）、「跳转」button r=[364,650,35,36]（右缘 399 > 390，出界 9px）；`outscreen.txt`：tag-manager-mob leafOverflow=1（"跳转" overR=9）/ wide=1；`fin-tag-manager-mob.json` hClip=4，裁切者同为 `animate-page-enter.overflow-x-hidden` | 中 |
| C-04 | ③重复性显示 | /collections（智能合集弹层） | 桌面 + 移动 | 弹层标题「智能合集」与弹层中部副标题「智能合集」完全同文，中间只隔一行说明文字 | `shots\dlg-col-smart-desk.png`(1440×900)、`shots\dlg-col-smart-mob.png`(390×844)；`p-smartcount.json`：`h2`「智能合集」fs=18/fw=600 r=[389,233,72,23] 与 `p`「智能合集」fs=14/fw=500 r=[692,439,56,18]，同文命中 3 处（第 3 处为操作按钮「智能合集」）；`smart-mob.json`：移动弹层 r=[8,42,374,760]，同文重复项 `"智能合集" x2` | 中 |
| C-05 | ③重复性显示 | /books（检测重复弹层） | 桌面 + 移动 | 弹层标题「重复检测」与副标题「重复检测」完全同文，正下方紧接同名操作按钮「检测重复」 | `shots\dlg-books-dup-desk.png`(1440×900)、`shots\dlg-books-dup-mob.png`(390×844)；`dc-dup.json`：`h2`「重复检测」fs=18/fw=600 r=[389,243,72,28] 与 `p`「重复检测」fs=14/fw=400 r=[692,450,56,20]；`dg-books-dup-mob.json` 文本序列 = `["重复检测","重复检测","检测重复","关闭"]`；`dlgdup-mob.json` dups=`"重复检测" x2`（移动弹层 r=[8,42,374,760]） | 中 |
| C-06 | ②显示不正常 | /data-admin、/tag-manager（移动端，另 /data-qa、/file-stats、/history 同现象） | 移动 390×844 | 底部固定 tab 栏半透明压在正文之上：未滚动到底时，页面末尾的记录文字与图标透过底栏显示并与 tab 文字重叠 | `shots\e4-dataadmin-mob-navoverlap.png`(2×)、`shots\e4-tagmgr-mob-navoverlap.png`(2×)、`shots\data-admin-mob.png`、`shots\tag-manager-mob.png`；底栏 r=[0,787,390,57] class `fixed bottom-0 left-0 right-0 z-50 ... bg-surface/90 backdrop-blur-2`；`dc-data-admin-mob.json` pairCount=5：`span.shrink-0.text-[10px]`「466.19 KB · 2026/10/7 00:47:00」r=[101.6,823.7,92.6,13] × `span`「在线」r=[153.2,820.5,20,13] 重叠 20×9.8px；`button`「恢复」r=[283,822.3,24,15] ×「合集」r=[280.5,820.5,20,13] 重叠 17.5×11.2px；`p-layout-data-admin-mob.json` textOverlaps 同源。滚到页底时 `lastContent.bottom=768` vs `navTop=787` gap=+19px（`p-navverify-da-mob.json`），故为「滚动中段重叠」而非永久遮挡。命中页与条数（`p-layout-*-mob.json` 的 textOverlaps）：/file-stats 10、/data-qa 6、/data-admin 4、/history 4、/tag-manager 2；其余 6 个移动页为 0 | 低 |
| C-07 | ①按钮（disabled 但看起来可点） | /data-admin | 桌面 + 移动 | 「保存阈值」为 disabled 态时仍保留实心绿底 + 白字的主行动按钮外观，仅靠 50% 整体透明度区分，与同页其他禁用按钮（0.4 透明 + 无底色描边）不一致 | `shots\e5-dataadmin-savethreshold.png`、`shots\e5-dataadmin-threshold-ctx.png`；`dis-data-admin-desk.json`：`disabled=true / opacity=0.5 / bg=emerald-600 / color=text-white`，r=[1311,2170,80,28]；`dis-data-admin-mob.json` r=[277,3472,80,32]；同页 3 个「清理」按钮 opacity=0.4 且 `bg=rgba(0,0,0,0)`（对照） | 低 |
| C-08 | ③极小字号 | / | 桌面 1440×900 | 首页环形图中心数字下的说明文字为 8px，4× 最近邻放大后「核心」二字已糊成一团无法辨认 | `shots\e3-home-desk-8px.png`(4×)；`fin-home-desk.json` tinyText：`核心` fs=**8px** r=[1204.8,227,16,8]、`MB` fs=**8px** r=[1284.1,227,12.8,8]，class `text-[8px] text-muted leading-none mt-0.5` | 低 |

## 2. 逐页核查结论（12 页 × 2 视口）

| 页面 | 桌面 1440×900 | 移动 390×844 |
|---|---|---|
| `/` | 仅 C-08（8px 环形图标签）；其余正常 | 正常（无出屏、无重叠、无 8/9px 正文） |
| `/books` | C-01；另「共 N 本」与排序箭头为 0px 间距紧贴（见 §4）非缺陷 | C-01 |
| `/collections` | C-04（智能合集弹层，弹层本身也出现在移动端） | C-04；页面本体正常 |
| `/history` | 正常 | C-06（底栏重叠）；列表 599 项本体无重复、无出屏 |
| `/recommendations` | 正常 | 正常 |
| `/stats` | 正常 | **C-02** |
| `/logs` | 正常（内容即接口错误记录，见 §5） | 正常 |
| `/file-stats` | 正常 | C-06（底栏重叠） |
| `/tag-manager` | 正常 | **C-03**、C-06 |
| `/scraper` | 正常（功能关闭态说明页） | 正常 |
| `/data-admin` | C-07 | C-06、C-07 |
| `/data-qa` | 正常 | C-06（底栏重叠） |

## 3. 三类问题的覆盖方式

1. **按钮无效**：`realclick.mjs` 用 CDP `Input.dispatchMouseEvent` 走浏览器真实命中测试点击（序列 mouseMoved → mousePressed → mouseReleased），点击前用 `elementFromPoint` 记录命中元素与是否被遮挡，点击后对比 `body.innerHTML` 长度、`innerText` 长度、路由、`Network.requestWillBeSent` 请求数、`[role=status/alert]` 提示，并对同组 tab 比对 active 态。共 20 例（桌面 12 例 + 移动 4 例 + 早期批量 4 例），结果见 §4。另外单独验证了「需点两次才生效」「输入后按钮不 enable」两类时序问题（`twoclick.mjs`）。
2. **界面显示不正常**：`e-outscreen.mjs` 逐元素量叶子文本的 `getBoundingClientRect`（排除 `overflow-x:auto/scroll` 可滚动容器内元素、排除自身 `truncate` 的元素），同时统计「宽于视口且 `overflow-x:visible` 的容器」；`e-layout.mjs` 统计 `offenders / hardClip / textOverlaps / bottomFixed`；`audit4.mjs` 全页 `overflowX / fixedLayers / outOfViewportFixed / clipped`；`e-dialog.mjs` 对 17 个弹层做四边溢出量测。
3. **字体错乱或重复性显示**：`mojibake`（乱码正则 + `textContent` 逐字符校验）24 视图全为 0；`e-final.mjs` 的 `tinyText`（<9.5px，排除 svg 内部与纯数字刻度）；`e-dup2.mjs` 的 `pairRepeats`（相邻兄弟同文）与 `h1`↔副标题同文；`e-dlgcard.mjs` 对弹层内部做同文配对。**口径**：图表坐标轴刻度数字、角标 badge（如 /recommendations 的「标签匹配」9px 角标、/stats 的 9px 横轴日期、/file-stats 的 8–9px 图表刻度）不计入缺陷。

## 4. 已核查但判定「不是缺陷」的项（避免凑数与误报）

- **按钮点击全部有效（20 例明细）**：`/` 换一个、查看全部（路由跳转 + 19 请求）、浏览全部书库；`/books` 无标签、检测重复、标签补全（弹层 + toast「未打标 73 本 · 待处理 7」）、批量（`Δhtml=23712`）、表头排序；`/collections` 按名称（排序态切换）、创建合集（`Δhtml=12778`、13 请求）、智能合集（`Δtext=47`）；`/tag-manager` 分类(24)/情景(7) tab、新建标签（`Δhtml=1183`）、全选当页（`Δhtml=13926`）、页码「3」+ 跳转（`activePage` 由 1 → 3）；`/stats` 7天/30天（active 切换、`Δhtml=10124`）、设定每日目标（输入框展开、`mutations=1116`）；`/history` 已完成/阅读时长/全部/在读/阅读进度、详情（路由 → `/comic/36faa4106673`，37 请求）；`/file-stats` 文件夹统计（`Δhtml=-25793`，切到文件夹视图）；`/logs` 统计概览、刷新（`GET /api/logs?page=1&pageSize=20` + `/api/logs/stats`）；`/data-admin` 刷新（`GET /api/snapshots`）、WAL Checkpoint（`POST /api/admin/storage/db/checkpoint`）、完整性检查（`POST .../db/integrity`）、保存阈值（`PUT /api/admin/storage/threshold` + `GET storage?fresh=1`）；`/recommendations` 刷新、AI 推荐理由（`POST /api/recommendations/ai-reasons`）；`/data-qa` 页数重扫（`POST .../pagecount-rescan`）、Dry-run 预览（`Δtext=4495`）；`/scraper` 前往设置 / 返回书库（路由跳转）。
- **3 例 works=false 已逐一排除为预期行为**：① `/file-stats`「总览统计」点击前即为 active 视图，无变化属正确；② `/tag-manager` 移动端「跳转」在页码输入框为空时 `Δhtml=0`（空页码不动作是合理设计），填入 3 后即生效；③ `/collections` 桌面「批量管理」探针选中了 `rect=[0,0,0,0]` 的不可见副本（`elementFromPoint` 命中其下方容器），属探针误报，非真实遮挡。
- **`/books` 桌面「共 2319 本」与排序箭头**：字符级 Range 测量得「本」右缘 390.2、`svg.lucide-arrow-up-down` 左缘 390.2，**间距恰为 0px 而非重叠**（`shots\e6-books-header-12x.png` 放大可见「本↓」紧贴）。属排版拥挤，不按「文字压住图标」上报。
- **`/collections` 桌面视图切换按钮组**：8× 放大（`shots\v-col-toggle.png`）确认两个 26×26 图标按钮完整可见、未被裁切。
- **禁用按钮**：`/books` 首页时的「«」「‹」、`/logs` 第 1 页时的「上一页」、`/tag-manager` 的「第一页/上一页」均为 `opacity:0.3` 且同组兄弟为 1.0，是标准置灰，正常。全站仅 `/data-admin` 的「保存阈值」存在 C-07 所述的样式不一致。
- **弹层超出视口**：17 个弹层（创建合集、智能合集、检测重复、标签补全、新建标签、创建快照、Dry-run 预览、每日/每周目标等）四边 `overflowRight/Left/Top/Bottom` 全部 ≤ 0，`innerOverflow=0`，无「弹层超出视口」问题。
- **`/books` 移动端工具栏与 `/` 桌面封面行** 的 `hardClip` 分别来自 `overflow-x-auto` 横向可滚动条与封面图 `overflow-hidden` 裁切，属设计。
- **`/stats` 桌面与 `/file-stats` 的 9px**、`/recommendations` 两视口各 50 处 9px 均为图表刻度或角标 badge，按 §3 口径不计入。

## 5. 环境噪声（不算缺陷）

- **`/logs` 页面内容本身即「接口异常记录」**：列表中的 `401 GET /api/jm/favorites/folders`、`404 GET /api/comics/*/thumbnail` 是该页面要展示的日志数据，不是页面报错。
- **部分卡片/统计为 0**：`/file-stats`「小说 0 / 平均大小 0 B」、`/stats` 若干 `0m`、`/data-admin` 缓存 0 / 备份 0 —— 与库内实际数据一致（2319 本全部为漫画），属真实数据而非渲染缺陷。
- **`/scraper` 整页为功能关闭空态**：文案「内容刮削功能当前已关闭。请在「设置 → 站点设置」中启用「内容刮削」开关后再使用。」属产品设计。
- **未见 401/网络不可达导致的整页错误卡**：24 视图 `badLinks=0`、`brokenImages=0`；缩略图缺失仅表现为日志条目（见上条），页面上无裂图占位。
- 未点击任何破坏性按钮（清理缓存、删除标签、回滚快照、执行修复等一律未触发）。唯一产生的写操作是「保存阈值」验证：验证前读取服务器原值为空（即 0/0/0，与首次基线 `p-threshold.json` 三输入框均为 `0` 一致），验证后已 `PUT {cacheMaxMB:0,dbMaxMB:0,diskFreeMinMB:0}` 恢复，复查接口 `threshold` 返回 `{}`，与初始状态一致（`thr-before.json` / `thr-restore.json` / `thr-final.json`）。

## 6. 证据文件索引

- 基线整页截图 / JSON：`shots\<page>-<desk|mob>.png`、`-full.png`、`.json`（24 组）；汇总 `summary.txt`
- C-01：`p-tagfilter-desk.json`、`p-tagfilter-mob.json`、`shots\e1-books-desk-tagfilter.png`、`shots\e1-books-mob-filterbar-wide.png`、`shots\books-desk.png`、`shots\books-mob.png`
- C-02：`p-stats-cause-mob.json`、`p-stats3-mob.json`、`p-stats2-mob.json`、`p-layout-stats-mob.json`、`outscreen.txt`、`shots\stats-mob-recent-view.png`、`shots\crop-stats-mob-recent.png`、`shots\stats-mob-full.png`
- C-03：`p-tagpager2.json`、`fin-tag-manager-mob.json`、`outscreen.txt`、`shots\tag-mob-pager-view.png`
- C-04：`p-smartcount.json`、`dc-smart.json`、`smart-mob.json`、`shots\dlg-col-smart-desk.png`、`shots\dlg-col-smart-mob.png`
- C-05：`dc-dup.json`、`dlgdup-mob.json`、`dg-books-dup-mob.json`、`shots\dlg-books-dup-desk.png`、`shots\dlg-books-dup-mob.png`
- C-06：`dc-data-admin-mob.json`、`p-layout-data-admin-mob.json`、`p-navverify-da-mob.json`、`shots\e4-dataadmin-mob-navoverlap.png`、`shots\e4-tagmgr-mob-navoverlap.png`
- C-07：`dis-data-admin-desk.json`、`dis-data-admin-mob.json`、`p-thrpoll.json`、`p-thrlong.json`、`twoclick1.json`、`shots\e5-dataadmin-savethreshold.png`、`shots\e5-dataadmin-threshold-ctx.png`
- C-08：`fin-home-desk.json`、`shots\e3-home-desk-8px.png`
- 按钮证据：`rc-*.json`（含 `-before.png` / `-after.png`）、`real2.json`、`real-mob.json`
