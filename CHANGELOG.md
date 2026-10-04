# Changelog

## Unreleased
### Added (feat/jm-backfill-tags 书库补标签)

- 书库补标签(私有扩展,MOBILE_API.md §8):用 JM 在线搜索给书库中无标签的旧书批量补标。无状态三端点 `GET /api/jm/backfill/candidates`(无标签清单 + 标题清洗出的默认搜索词)、`POST match`(全站搜索 + 标题打分,不改库;归一全等 1.0/包含 0.75/CJK bigram Jaccard×0.7,作者一致 +0.1,分 high/medium/low 三档)、`POST apply`(后端自行拉详情写标签,不信任客户端透传;与下载入库打标同口径:标签上限 30、作者占位符过滤、author/metadataSource 仅空缺回填)
- 标题清洗:剥噪声括号(汉化组/搬运/raw 等)→ 卷话后缀(第N卷/vol.N)→ 尾部数字 id(4~7 位,"漫画名-125734" 形态);纯数字(车号)保留走上游直达
- 上游纪律:match/apply 共用全局限速器 ≥1.2s/次,批量循环打不穿上游
- 新页面「书库补标签」`/jm/backfill`(在线首页快捷区入口):行内可编辑搜索词、单行匹配/应用/备选/跳过、「自动匹配未处理」顺序循环(可停止)、「应用全部高置信」批量写入、待处理/已处理/全部筛选;无标签清单即进度源,刷新天然续跑

### Added (tag-dev 下载作者同步到书库)

- 下载入库自动标签扩展作者同步:有效作者名(剔除 N/A/未知 等占位符)并入自动标签清单(书库详情页标签区可点可筛选),并在书库记录 `author` 字段为空时回填(不覆盖刮削/手动结果);作者同步受同一 `downloadTags` 开关控制;占位符作者(如 aid=1475046 的 "N/A")不同步

### Added (tag-dev 作者标签)

- 详情页作者可点:作者行改为作者 chips(圆角+👤+紫色系,与标签明显区分);选中后与标签共用操作条——「搜索」按最近点选项类型自动选择 `searchType=tag/author` 跳搜索,「收藏/取消收藏」对标签与作者统一批量切换(乐观更新、失败精确回滚),已收藏作者带 ★ 角标
- 标签收藏数据模型加 `type`(tag/author,幂等键 type+tag,旧数据无 type 读入归一为 tag 零迁移);同一名值可同时以标签和作者收藏,互不影响
- 标签收藏页 /jm/tags 分「标签/作者」两组管理;搜索页「我的收藏」chips 同样分组,作者点击即按 `searchType=author` 就地搜索
- 详情接口新增 `authors: string[]` 字段(上游 author 归一:string→单元素,数组→逐个 trim/去重;`author` 字符串字段保持不变)
- 端点:POST/DELETE /api/jm/tag-favorites 增加 `type` 参数(缺省 tag,非法 422);GET 列表项透传 type

### Added (tag-dev 下载入库自动标签)

- JM 批量下载入库后自动把该漫画的 JM 标签挂到书库记录:任务启动抓详情时捕获标签(trim/去重/上限 30),归档扫描后轮询等待 Comic 入库(确定性 ID,2s×45 次兜底)再 `AddTagsToComic` 写入(标签不存在自动建、幂等);测试目录不入库不打标;分类不自动写
- 下载任务快照新增 `tags` 字段,任务面板展示「标签(N)」预告
- JM 设置新增「下载后自动添加标签」开关(`settings.downloadTags`,默认开)

### Added (tag-dev 标签搜索/标签收藏)

- 在线漫画详情页标签可点(支持多选):「搜索」跳转在线搜索页并自动按最近点选的标签执行 `searchType=tag` 搜索;「收藏/取消收藏」对选中集合批量切换(逐 tag 幂等调用,乐观更新、失败精确回滚),已收藏标签带 ★ 角标
- 标签收藏(私有扩展):服务端持久化 `<DataDir>/jm/tag-favorites.json`(设备级共享,仅 nowen 登录、不要求 JM 登录);新增端点 `GET/POST/DELETE /api/jm/tag-favorites`(同 tag 幂等)
- 新页面「标签收藏」`/jm/tags`:点击标签即搜索、× 取消收藏、空态引导;在线首页快捷区加入口
- 搜索页:支持 `?keyword=&searchType=` URL 参数初始化并自动首搜(详情页标签搜索跳入);表单上方「我的标签」chips(点击即搜、× 就地取消)

### Added (阅读器体验增强 READER-UX-01)

- 阅读器交互小修：鼠标滚轮翻页（单页/双页模式）、页码输入跳页、快捷键帮助面板（`?` 键）、移动端 double 自动降级 single（<768px）
- 移动端阅读体验优化：顶部工具栏压缩（h-12）、底部安全区适配（env(safe-area-inset-bottom)）、横屏检测与页码指示器隐藏、Webtoon 图片 block display
- 图片加载/错误重试优化：修正 SinglePageView 误导性 PDF 错误文案、新增 retry 计数器、DoublePageView/WebtoonView retry 按钮触控友好化（min-h-[44px]）
- 沉浸模式：`I` 键或 More 菜单切换，cursor-none，工具栏自动隐藏，ESC 退出
- 缩略图导航：`T` 键或 More 菜单打开，4/5 列网格，当前页高亮，点击跳页，lazy loading，移动端底部 sheet 布局
- 快捷键新增：`I` 沉浸模式、`T` 缩略图导航、`?` 快捷键帮助


### Added

- 只读数据一致性巡检 API（`GET /api/admin/data-qa/summary` / `GET /api/admin/data-qa/issues`）：检测 8 类数据问题
- Dry-run 修复预览 API（`POST /api/admin/data-qa/fix-preview`）：返回修复计划，不修改数据库
- 安全修复执行 API（`POST /api/admin/data-qa/fix`）：支持 4 类安全 issue 的真实修复，需 `confirm: true`
- Data QA 管理页面（`/data-qa`）：summary 卡片、issue 列表、筛选、dry-run 预览、执行修复、二次确认
- 支持真实修复的 issue 类型：TOTAL_TIME_ZERO、UCS_TOTAL_TIME_ZERO、ORPHAN_TAG、ORPHAN_CATEGORY
- PAGE_COUNT_ZERO / PAGE_COUNT_NEGATIVE 仅支持 dry-run 预览和重扫触发，不直接修改 pageCount
- 修复执行安全机制：admin 权限、事务保护、幂等校验、孤儿确认后再删除、totalReadTime 仅更新 ≤ 0 的行
- SESSION_ORPHAN 安全关闭策略：标记 endedAt=startedAt / duration=0，不累加阅读时长，保留审计记录
- SESSION_ZERO_DURATION 反算修复：从时间戳计算 duration，安全范围校验（2s-12h），重聚合 totalReadTime
- PAGE_COUNT 重扫触发 API（`POST /api/admin/data-qa/pagecount-rescan`）：列出需要重扫的漫画，复用已有 scanner
- `/data-qa` 管理页面集成高风险操作按钮（PageCount Rescan）
- 所有 8 类 issue 现在均有 dry-run 支持，6 类支持真实修复
- DATA-QA-02 补充测试覆盖：SESSION_ZERO_DURATION 边界测试（<=2s / >12h skipped）、幂等测试、handler 层 8 个 DataQA 端点测试（auth / confirm / fix-preview / fix / rescan）

- 上传功能支持选择目标书库：管理员可在首页上传区域选择目标书库，文件直接写入该书库的 rootPath
- `POST /api/upload` 新增可选参数 `libraryId`，传入时上传到对应书库，不传时兼容旧 `comicsDir` / `novelsDir` 逻辑
- 上传文件类型按书库类型自动校验（comic 书库只允许归档、novel 书库只允许电子书、mixed 允许全部）
- 系统诊断和 PDF renderer 接口（`/api/system/*`）添加认证要求
- Library.defaultAccess 防御性数据库迁移（migration 25），兼容中间版本升级的老数据库

- 漫画阅读器图片滤镜：亮度、对比度、灰度，通过 CSS filter 实现，支持单页/双页/Webtoon 模式
- 图片滤镜预设：默认、夜间护眼、老漫画增强、黑白增强，一键切换常用阅读效果
- Webtoon 模式双击缩放：双击放大到 200%，再次双击还原，缩放状态下支持单指拖拽平移
- 漫画本地书签：工具栏添加/取消书签、书签列表面板、点击跳转、localStorage 按漫画分组持久化
- 用户级阅读状态（想读/在读/已读完）：每个用户独立管理，不再使用全局 Comic.readingStatus
- 首页按阅读状态筛选：下拉选择器支持全部状态/想读/在读/已读完，与搜索、标签、分类等条件自由组合
- 漫画详情页阅读状态选择器：点击即保存，支持 403/401 错误提示
- Webtoon 缩放状态指示器（右下角百分比提示）
- 漫画阅读器设置面板新增图片滤镜区域（3 个 slider + 预设按钮 + 重置按钮）

- 多用户书库权限能力（Library / UserLibraryAccess / UserGroup / GroupLibraryAccess）
- `UserCanViewLibrary()` 统一授权语义：admin→enabled / public→自动 / 直接授权 / 用户组继承
- `UserCanViewComic()` 处理 NULL libraryId 和不存在书库的向后兼容
- `checkComicAccess()` 被所有资源接口共用的权限校验中间件
- `GetReadingSessionComicID()` 用于阅读会话权限前置校验
- `ForbiddenPage` 前端无权限友好页面组件
- `FolderBrowser` 从 SiteSettingsPanel 抽取为公共目录选择组件
- `calculateReadingProgress()` / `isReadingFinished()` 集中进度计算工具函数
- 书库管理面板支持 `defaultAccess`（公开/私有）、`scanEnabled`（自动扫描开关）、目录浏览选择
- 站点设置中"目录配置已迁移到书库管理"引导卡片
- 管理员 API：`/api/admin/libraries` CRUD + `/api/admin/users/:id/library-access`
- 移动端阅读器首次手势提示：首次进入移动端漫画阅读器时显示双击/双指缩放提示，localStorage 持久化，仅显示一次
- 设置面板滤镜 slider 数值实时显示：亮度/对比度/灰度旁边显示当前百分比
- 移动端顶部工具栏更多菜单：低频操作（详情、书签列表、全屏）收进 overflow 菜单，主栏只保留核心按钮


- 首页 Hero 区和背景视觉层：产品标题、内容统计、上传/扫描入口，毛玻璃容器 + radial-gradient 光晕背景
- 首页媒体横架容器视觉升级：ContinueReading 和 RecommendationStrip 采用 surface-card 圆角容器，卡片封面动效（motion-cover + interactive-scale）
- 首页筛选栏媒体库控制台：StatsBar、筛选、分类、标签、收藏、阅读状态、排序统一包裹在 surface-glass-panel 毛玻璃容器中
- 首页内容区 Section Header：显示"全部漫画/全部小说"标题、内容数量、当前页信息
- 首页空状态视觉升级：surface-card 圆角容器，更紧凑的内边距
- 首页分页控件 motion-button 动效：所有分页按钮统一 hover/active 交互，当前页 accent 阴影
- 筛选按钮统一视觉：收藏、阅读状态、分类、合集、批量、重复检测、排序按钮均加 motion-button 和统一边框
- 清除筛选按钮：仅在有活跃筛选条件时显示，一键清除收藏/阅读状态/分类/标签筛选

- 漫画详情页 Hero 背景模糊视觉：封面图作为 blurred backdrop，blur-2xl + opacity-25 + 渐变遮罩，pointer-events-none 不影响点击
- 详情页封面卡片动效：motion-cover + shadow-black/20，阅读按钮 motion-button 增强
- 详情页主信息区视觉层级：标题升级为 text-2xl sm:text-3xl tracking-tight，收藏/评分/阅读状态按钮统一 motion-button
- 详情页元数据 / 标签 / 分类 / 描述卡片化：统一 surface-card rounded-xl 容器，tag/category pill 加 motion-button
- SimilarComics 相似推荐容器视觉：surface-card rounded-2xl 容器，section header + 副标题，卡片 interactive-scale
- SimilarComics loading 骨架屏可见状态：不再 return null，显示 5 个封面骨架 + 标题骨架
- 详情页 not-found 状态 surface-card 容器美化
- detail-hero-bg utility：radial-gradient 光晕 + reduced-motion 降级

- 阅读器顶部工具栏和底部进度栏统一 motion 交互：所有按钮加 `.motion-button`，容器加 `border-white/5`
- ReaderOptionsPanel 设置面板毛玻璃视觉：`bg-zinc-900/95 backdrop-blur-xl`，分组容器 hover 阴影，slider / 预设按钮统一 pill 风格
- BookmarkPanel 书签面板毛玻璃视觉：`backdrop-blur-xl` 容器，列表 item `interactive-scale`，删除按钮 `motion-button`，空状态紧凑
- 阅读器 loading / error / 404 状态视觉增强：ambient glow + 毛玻璃卡片容器 + `.motion-button` 操作按钮
- Webtoon 缩放指示器毛玻璃 pill：`bg-zinc-900/80 backdrop-blur-xl border-white/[0.08] shadow-lg`
- PDF canvas 纸张质感：`shadow-2xl shadow-black/40 rounded-sm`，容器 `bg-[#080808]` 深色背景
- PDF 缩放指示器升级为毛玻璃 pill，日/暗色模式统一
- 小说阅读器容器升级：`max-w-prose` 行宽、`leading-[1.85] tracking-wide` 排版、`text-2xl tracking-tight` 章节标题
- 小说页面 loading / error / 404 状态和漫画 reader 页面风格统一
### Changed

- 站点目录配置入口迁移到书库管理，原入口改为引导提示

- 阅读状态从全局 Comic.readingStatus 切换为 UserComicState 用户级状态，多用户之间互不影响
- GET /api/comics/:id 返回当前用户自己的 readingStatus，不再返回全局值
- PUT /api/comics/:id/reading-status 写入当前用户的 UserComicState，不再更新 Comic 表
- GET /api/comics?readingStatus=reading 按当前用户的阅读状态过滤
- Webtoon 缩放不破坏虚拟化逻辑、书签跳转、图片滤镜
- 修复详情页阅读状态选择器被插入到 loading skeleton 分支，导致加载完成后的真实详情页不显示
- OPDS All / Recent / Favorites / Search 全部接入 `opdsLibraryFilter()` 书库权限过滤
- OPDS `opdsLibraryFilter()` 对无用户/无权限返回 `WHERE 1 = 0`，不再放行全库
- 漫画详情、阅读器、小说页面接入 403 ForbiddenPage 友好展示
- 漫画列表按用户可访问书库过滤（`GetUserAccessibleLibraryIDs`）
- 阅读进度计算统一为 0-based index 的展示语义
- 系统诊断面板适配日间模式（33 处硬编码颜色替换为 CSS 变量）
- 移动端底部工具栏、设置面板、书签面板 safe-area 适配，避开 iPhone home indicator 和 Android 底部导航栏
- 小屏（< 640px）双页模式自动临时退化为单页，用户保存的设置不变
- 移动端顶部工具栏按钮布局精简：书签切换 + 设置始终显示，更多菜单收纳低频操作
- 设置面板 slider 数值可读性提升（12px 等宽字体、70% 不透明度）和预设按钮触控区域增大


- 首页整体从普通列表页升级为私人媒体库首页（Plex / Apple TV 风格）
- ContinueReading / RecommendationStrip 采用统一 surface-card 容器和 section header
- 筛选、分类、收藏、阅读状态、排序按钮统一 pill 风格和 motion-button 交互
- 分页控件统一 motion-button 交互，当前页按钮增加 shadow-accent/25
- 移动端布局增加 overflow-x-hidden 防护，筛选控件支持横向滚动
- 设计系统 utility 首次大范围接入：surface-card、surface-glass-panel、motion-button、motion-cover、interactive-scale

- 详情页从普通信息页升级为媒体详情页风格（Plex / Apple TV 详情页质感）
- 收藏按钮 active 状态增加 shadow-rose-500/20，未收藏时增加 border-border/40
- 评分星星 motion-button 增强 hover 弹性
- 阅读状态按钮统一 motion-button + border-border/40 边框
- 元数据卡片统一 surface-card 风格，标签/分类区域包裹 surface-card 容器
- 合集卡片和 AI 结果容器统一 surface-card rounded-xl

- 阅读器整体背景从纯黑 `bg-black` 升级为更沉浸的深灰黑 `bg-[#0a0a0a]`
- ReaderToolbar 按钮统一 `.motion-button`，阅读模式切换 active 状态增加 `shadow-accent/25`
- 设置面板 overlay 增加 `backdrop-blur-sm`，关闭按钮加 `.focus-ring`
- 书签列表 item hover 从 `bg-white/8` 升级为 `bg-white/[0.06]`，当前页高亮加 `ring-1 ring-accent/20`
- PDF / 小说 loading spinner 从 `h-8 w-8` 增大为 `h-10 w-10`，边框弱化 `border-white/10`
- 描述文字升级为 text-foreground/80 leading-relaxed 增强可读性
### Fixed

- JM 在线源会员操作（签到/收藏/点赞）全部报「JM 服务端返回错误」：登录后只保存裸 AVS cookie，丢失 `/setting` 引导 cookies（`__cflb`/`ipm5`/`ipcountry` 等），会话客户端裸带 AVS 请求会员端点被上游按未登录拒绝（HTTP 401「請先登入會員」）。现登录时合并完整 cookie jar（对齐 jmcomic SDK 持久 Session 语义）；上游 401/未登录文案映射为 1002「登录已失效」提示重新登录，不再笼统报 2001
- FolderBrowser 目录浏览器 API 路径错误（`/api/admin/browse` → `/api/browse-dirs`），导致书库管理中目录选择不可用
- AI 语义搜索未做书库权限过滤，普通用户可搜索到无权限书库内容
- `.gitignore` 缺少 `site-config.json` 和 `storage-history.json` 规则

- 普通用户可通过直接请求 comicId 越权访问漫画详情的问题
- OPDS 列表接口未过滤无权限书库、下载接口可下载无权限文件的问题
- `UserCanViewLibrary()` 缺少 `defaultAccess=public` 和用户组继承检查
- 收藏、评分、阅读进度、阅读状态、阅读会话、AI 推荐原因缺少权限校验的问题

#### UI-R3 详情页重构验证

- 验证收藏后仍刷新首页缓存和详情数据（handleToggleFavorite → invalidateComicsCache + refetch）
- 验证评分后仍刷新首页缓存和详情数据（handleRating → invalidateComicsCache + refetch）
- 验证阅读状态仍保持用户级状态（UserComicState，非全局 Comic.readingStatus）
- 验证 SimilarComics loading / error / empty 不静默消失（loading 改为骨架屏，error/empty 有 surface-card 容器）
- 验证 SimilarComics fallback 推荐未被破坏（后端 fallback 逻辑未改动）
- 验证阅读入口（getReaderUrl）和管理员操作未受影响
- 验证移动端无横向溢出（375px / 390px / 430px / iPad）
- 验证 prefers-reduced-motion 覆盖所有详情页新 utility class

#### UI-R4 阅读器视觉优化验证

- 验证单页 / 双页 / Webtoon / PDF / 小说模式均正常
- 验证 Webtoon pinch / pan / double tap 未受影响（touchAction、passive:false、snap-to-1 逻辑未改动）
- 验证小屏双页退化单页逻辑未受影响（effectiveMode = isSmallScreen && mode === "double" ? "single" : mode）
- 验证 PDF retry 逻辑未受影响（setError(null) + retryCount++）
- 验证书签 localStorage、跳转、删除逻辑未受影响
- 验证图片滤镜只影响漫画图片，不影响 PDF / 小说
- 验证 safe-area 未回退（ReaderToolbar 1 处、ReaderOptionsPanel 3 处、BookmarkPanel 2 处）
- 验证 reduced-motion 支持正常
- 验证移动端和横屏无明显溢出问题

#### BUG-UI-HomeFilter-01 首页筛选控制台修复

- 修复首页筛选控制台在列表模式下过高、阴影过重（`surface-glass-panel` 的 `shadow-float` 太强）、标签空状态占用空间过大的视觉问题
- 将筛选控制台从大浮层视觉压缩为轻量工具栏：`rounded-xl bg-card/60 backdrop-blur-md border-border/20`
- 标签空状态改为更紧凑的 inline muted 提示（`text-[11px] text-muted/60 italic`，文案缩短为"暂无标签"）
- 内容区标题与筛选控制台间距从 `mb-4` 增加为 `mt-6 mb-5`，视觉层级更清晰

- `UserCanViewComic()` 对 NULL libraryId 和不存在书库返回异常的问题
- 最后一页/最后章节显示 99% 的问题（漫画详情、漫画阅读器、小说阅读页、小说底栏）
- 系统诊断面板日间模式文字不可读的问题
- `frontend/tsconfig.tsbuildinfo` 被 Git 跟踪导致构建后误报"有未推送改动"
- 前端收藏/评分/阅读状态 403 错误被静默吞掉的问题
- iPhone home indicator 遮挡阅读器底部进度条、设置面板底部内容、书签面板最后一项的问题
- 小屏手机双页模式内容过小看不清的问题
- 移动端顶部工具栏按钮拥挤、标题和按钮互相挤压的问题


#### UI-R2 首页重构验证

- 验证移动端无横向溢出（375px / 390px / 430px / iPad）
- 验证收藏筛选 / 阅读状态筛选仍扁平显示具体漫画（isFlatComicFiltering 未被改动）
- 验证合集 + 散本统一视图未被破坏（unifiedItems 逻辑未变）
- 验证批量操作、拖拽排序、右键菜单、分页未受影响
- 验证 prefers-reduced-motion 覆盖所有新 utility class
- 验证亮色 / 暗色模式无明显坏样式

### Tests

- 新增 `internal/store/library_access_test.go`，覆盖 8 个回归场景：
  - admin 可访问所有 enabled 书库
  - 普通用户可访问 public 书库
  - 普通用户不可访问 disabled 书库
  - 直接授权可访问
  - 用户组授权可访问
  - 未授权 private 不可访问
  - NULL libraryId 兼容
  - 不存在书库兼容
- P7 集成验收：后端 build + 权限测试 + handler 测试 + 前端 lint/build 均通过

### Chore

- 停止跟踪 TypeScript 构建缓存文件 `frontend/tsconfig.tsbuildinfo`（已从 Git 索引移除）

### Known Issues

- `internal/store/db_test.go` 的 `TestComicCRUD` 存在历史 FTS5 搜索结果数量问题（返回 3 条而非 1 条），不是本轮改动引入

---

## 关键 Commit 列表（本轮 P0-P9）

| Commit | 描述 |
|:---|:---|
| `48e207a` | feat(frontend): 书库管理面板支持扫描状态展示和手动扫描 |
| `92fd8d8` | feat: 新增单书库扫描接口，扫描器支持按书库独立扫描 |
| `42f10de` | feat(store): 书库CRUD读写扫描状态字段，新增单书库漫画查询 |
| `46e084d` | feat(store): 书库表新增扫描状态字段，支持单书库扫描管理 |
| `5a179bc` | feat: 扫描器自动创建书库并写入libraryId/relativePath |
| `1061b4c` | fix(store,handler): 修复书库权限语义并为漫画详情加入访问校验 |
| `ed86220` | fix(handler): 修复 OPDS 下载接口权限校验并保留书库过滤入口 |
| `e77f657` | fix(handler): 修复 OPDS 列表接口书库权限漏洞 |
| `7855b61` | fix(auth): 统计、用户操作、阅读会话、AI 推荐等接口权限补漏 |
| `5ef2a65` | test(auth): 新增权限回归测试覆盖 |
| `e18b0f1` | fix(ui): 前端 403 无权限友好页面 + 403 错误透传 |
| `30a9d23` | refactor(settings): 站点目录配置入口迁移到书库管理 |
| `f75ce7c` | fix(ui): 修复日间模式下系统诊断面板样式问题 |
| `66fa6be` | fix(ui): 修复阅读进度显示 99% 问题，统一使用 calculateReadingProgress |
| `da6bd53` | feat(ui): 视觉 + 动效设计系统 token（globals.css） |
| `a0767e9` | feat(ui): ComicCard / Recommendations / Toast / ReaderToolbar 设计系统试点接入 |
| `a168745` | feat(ui): 首页 Hero 区 + 背景视觉层 |
| `ae8b4ba` | feat(ui): ContinueReading / RecommendationStrip 媒体横架容器视觉升级 |
| `481b591` | feat(ui): 首页筛选栏媒体库控制台视觉重做 |
| `6c5ffb6` | feat(ui): 首页内容区 / 空状态 / 分页视觉升级 |
| `e53ea9a` | feat(ui): 漫画详情页 Hero 背景模糊视觉 + detail-hero-bg utility |
| `237ce66` | feat(ui): 详情页封面/主信息/操作按钮视觉升级 |
| `3112abc` | feat(ui): 详情页元数据/标签/描述区域视觉升级 |
| `7f2f7dc` | feat(ui): SimilarComics 相似推荐容器视觉升级 |
| `789ed5a` | chore(git): 停止跟踪 TypeScript 构建缓存文件 |
| `4b3b28d` | fix(ui): 统一小说阅读页及阅读器进度计算，消除最后章节 99% 问题 |

### Key Commits (P13-P16)

| Commit | Description |
|:---|:---|
| `2575109` | feat(reader): 漫画阅读器图片滤镜功能 |
| `18087e1` | feat(reader): 图片滤镜预设功能 |
| `385d098` | feat(reader): Webtoon 双击缩放和平移 |
| `314d637` | feat(reader): 漫画阅读器本地书签功能 |
| `df05842` | feat(store): 用户级阅读状态 Store 层改造 |
| `8ad26cb` | fix(handler): 用户级阅读状态 Handler 改造 |
| `62834d0` | feat(comic): 漫画详情页阅读状态选择器 |
| `878d7c9` | feat(comic): 首页列表页阅读状态筛选 |
| `8ecb7aa` | fix(comic): 修复详情页阅读状态选择器在 loading skeleton 中未显示 |

### Key Commits (P18 Mobile Reader)

| Commit | Description |
|:---|:---|
| `2e56b20` | fix(reader): 移动端底部安全区适配 |
| `1776bf5` | feat(reader): 移动端顶部工具栏按钮精简 |
| `ec647a0` | fix(reader): 小屏双页模式自动退化为单页 |
| `e632336` | feat(reader): 移动端缩放手势首次提示 |
| `f0b722b` | feat(reader): 设置面板滤镜 slider 数值显示和触控优化 |

### Key Commits (UI-R4 Reader Visual)

| Commit | Description |
|:---|:---|
| `b31a2c3` | feat(ui): ReaderToolbar / 底部栏视觉统一 |
| `bb5007d` | feat(ui): ReaderOptionsPanel 设置面板毛玻璃视觉 |
| `39f9f14` | feat(ui): BookmarkPanel 书签面板毛玻璃视觉 |
| `700a5da` | feat(ui): 阅读区背景 / loading / error / Webtoon 缩放指示器视觉升级 |
| `dc2d5e8` | feat(ui): PDF canvas 纸张质感 + 小说阅读容器行宽/行高优化 |
| `32a58b9` | fix(ui): 首页筛选控制台展示异常修复（BUG-UI-HomeFilter-01） |
