package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ============================================================
// 标签归一管理 M1
//
// 三张表（迁移 v37）：
//   TagAlias      别名 → 规范标签（alias PRIMARY KEY → tagId）
//   TagOperation  合并操作日志（支持撤销）
//   TagNormIgnore 归一预览忽略（normKey → 忽略整簇）
//
// 归一键 TagNormKey(name) = trim → 小写 → 简繁折叠。
// 写入口 NormalizeTagName：别名精确命中 → 同 normKey 既有标签（最早创建）
// → 都没有才新建（展示名保留写入原名）。
// ============================================================

// tagNormSimpTradPairs 内置高频简/繁字对，每对为「简体+繁体」两个相邻 rune，
// 以空格分隔便于阅读。只收标签场景常见字，宁缺毋滥。
// 折叠方向：繁 → 简（简体字为归一后的规范形）。
const tagNormSimpTradPairs = `
汉漢 后後 发發 发髮 门門 见見 车車 东東 画畫 图圖 体體 风風 云雲 龙龍 马馬 鱼魚 长長 书書 园園 广廣
庆慶 应應 现現 爱愛 学學 实實 时時 间間 开開 关關 问問 话話 语語 说說 读讀 写寫 记記 让讓 谢謝
请請 讲講 谈談 论論 议議 训訓 设設 访訪 证證 评評 词詞 诗詩 试試 资資 质質 费費 卖賣 买買 贵貴 货貨
财財 贝貝 负負 责責 赛賽 赠贈 赋賦 赢贏 红紅 绿綠 蓝藍 黄黃 银銀 铁鐵 钱錢 组組 织織 细細 线線 练練
结結 绝絕 继繼 续續 级級 纪紀 纯純 约約 经經 绑綁 绳繩 维維 缩縮 网網 轨軌 转轉 轮輪 软軟 轻輕 载載
较較 辅輔 辆輛 轴軸 输輸 针針 钢鋼 钮鈕 铃鈴 铜銅 锁鎖 错錯 键鍵 镜鏡 铺鋪 录錄 闪閃 闭閉 闲閒 闷悶
闹鬧 闻聞 闺閨 阁閣 阅閱 阔闊 队隊 阶階 阳陽 阴陰 陆陸 陈陳 险險 随隨 隐隱 难難 电電 雾霧 飞飛 饭飯
饮飲 饱飽 饲飼 饼餅 馆館 驱驅 驻駐 驾駕 骂罵 骑騎 骗騙 鲜鮮 鸟鳥 鸡雞 鸣鳴 鹰鷹 麦麥 听聽 员員 别別
剂劑 办辦 务務 动動 勋勳 胜勝 势勢 匀勻 医醫 华華 协協 单單 卫衛 卷捲 厂廠 厅廳 历歷 历曆 厉厲 压壓
县縣 参參 双雙 变變 台臺 号號 叹嘆 吓嚇 呜嗚 咏詠 唤喚 启啟 启啓 团團 圆圓 国國 处處 备備 复復 复複
个個 内內 军軍 农農 冲衝 决決 况況 冻凍 净淨 凉涼 减減 凤鳳 凭憑 击擊 划劃 刘劉 则則 刚剛 创創 删刪
剑劍 剥剝 剧劇 劝勸 劳勞 励勵 娱娛 婴嬰 娇嬌 孙孫 宁寧 宝寶 宠寵 审審 对對 寻尋 导導 将將 层層 属屬
屡屢 帅帥 师師 带帶 帮幫 干幹 庄莊 库庫 废廢 弃棄 弹彈 强強 归歸 当當 偿償 忆憶 志誌 怀懷 态態 总總
恋戀 恶惡 悦悅 悬懸 惊驚 惧懼 愿願 战戰 户戶 执執 扩擴 扫掃 扬揚 拥擁 拨撥 择擇 挂掛 挤擠 挥揮 损損
捡撿 换換 舍捨 据據 采採 携攜 摄攝 摆擺 摇搖 撑撐 敌敵 数數 斗鬥 斗鬦 断斷 旧舊 昼晝 显顯 暂暫 畅暢
杀殺 杂雜 权權 条條 来來 极極 构構 枢樞 标標 栏欄 树樹 样樣 桥橋 检檢 楼樓 荣榮 横橫 樱櫻 欢歡 欧歐
残殘 毕畢 气氣 汇匯 没沒 泄洩 泪淚 洁潔 浅淺 测測 济濟 浑渾 浓濃 涂塗 涛濤 涨漲 渐漸 渔漁 渗滲 温溫
湿濕 满滿 滤濾 滥濫 滨濱 滚滾 滞滯 灭滅 潜潛 濒瀕 灯燈 灵靈 灾災 灿燦 为為 炼煉 炽熾 烂爛 烟煙 烦煩
烧燒 热熱 状狀 犹猶 独獨 狮獅 猎獵 猪豬 献獻 环環 玛瑪 疗療 稳穩 穷窮 系係 系繫 竞競 节節 笔筆 笼籠
篮籃 简簡 签簽 类類 紧緊 联聯 职職 脑腦 脚腳 脸臉 兴興 术術 众眾 众衆 订訂 讯訊 询詢 该該 详詳 夸誇
诚誠 误誤 谁誰 谐諧 诺諾 谣謠 识識 丰豐 郑鄭 虚虛 虽雖 虾蝦 蚀蝕 蚂螞 袜襪 装裝 里裏 里裡 观觀 页頁
顶頂 顺順 须須 头頭 频頻 颗顆 题題 颜顏 顾顧 齐齊 龟龜 鲁魯 胡鬍 静靜 严嚴 优優 伞傘 伟偉 传傳 伤傷
伦倫 侦偵 侧側 侨僑 俩倆 俭儉 债債 倾傾 储儲 儿兒 亿億 从從 义義 乌烏 乐樂 乔喬 习習 乱亂 争爭 亏虧
产產 亩畝 亲親 价價 临臨 举舉 蛮蠻 万萬 与與 几幾 只隻 码碼 宫宮 丽麗 梦夢 声聲 药藥 兽獸 触觸 猫貓
丝絲 萝蘿 戏戲 游遊 制製 周週 面麵
`

// tagTradToSimp 繁 → 简 折叠表（运行时从 tagNormSimpTradPairs 构建）。
var tagTradToSimp = buildTagTradToSimp()

func buildTagTradToSimp() map[rune]rune {
	m := make(map[rune]rune)
	for _, pair := range strings.Fields(tagNormSimpTradPairs) {
		r := []rune(pair)
		if len(r) != 2 || r[0] == r[1] {
			continue // 非法对在测试中兜底断言，运行时跳过
		}
		if _, exists := m[r[1]]; !exists {
			m[r[1]] = r[0]
		}
	}
	return m
}

// TagNormKey 计算标签的归一键：TrimSpace → 小写（casefold）→ 简繁折叠。
// 归一键只用于匹配，不改变标签的展示名。
func TagNormKey(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if simp, ok := tagTradToSimp[r]; ok {
			b.WriteRune(simp)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ============================================================
// 写入口归一
// ============================================================

// tagNormIndex 一次载入别名表与全部标签，按 normKey 分组供写入口解析。
// 组内 tagId 升序排列（最早创建的在前）。
type tagNormIndex struct {
	aliases map[string]int   // alias(已 trim) -> tagId
	groups  map[string][]int // normKey -> tagIds (升序)
}

func loadTagNormIndex() (*tagNormIndex, error) {
	ix := &tagNormIndex{
		aliases: make(map[string]int),
		groups:  make(map[string][]int),
	}

	rows, err := db.Query(`SELECT "alias", "tagId" FROM "TagAlias"`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var alias string
		var tagID int
		if err := rows.Scan(&alias, &tagID); err != nil {
			rows.Close()
			return nil, err
		}
		ix.aliases[strings.TrimSpace(alias)] = tagID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT "id", "name" FROM "Tag" ORDER BY "id" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		key := TagNormKey(name)
		ix.groups[key] = append(ix.groups[key], id)
	}
	return ix, rows.Err()
}

// resolve 将写入的原始标签名解析为 tagId：
//  1. trim 后查 TagAlias 精确命中 → 用其 tagId；
//  2. 未命中按 normKey 找已有标签（同 normKey 取最早创建的）；
//  3. 都没有才新建，展示名保留写入时的原名。
func (ix *tagNormIndex) resolve(rawName string) (int, error) {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return 0, errors.New("tag name is empty")
	}

	if tagID, ok := ix.aliases[name]; ok {
		return tagID, nil
	}

	key := TagNormKey(name)
	if group := ix.groups[key]; len(group) > 0 {
		return group[0], nil
	}

	// 新建（并发下同名冲突则取既有行）
	if _, err := db.Exec(`INSERT INTO "Tag" ("name") VALUES (?) ON CONFLICT("name") DO NOTHING`, name); err != nil {
		return 0, err
	}
	var tagID int
	if err := db.QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = ?`, name).Scan(&tagID); err != nil {
		return 0, err
	}
	ix.groups[key] = append(ix.groups[key], tagID)
	return tagID, nil
}

// NormalizeTagName 单次解析入口（供非循环场景使用）。
func NormalizeTagName(rawName string) (int, error) {
	ix, err := loadTagNormIndex()
	if err != nil {
		return 0, err
	}
	return ix.resolve(rawName)
}

// ============================================================
// 合并 apply / 撤销 undo
// ============================================================

var (
	// ErrTagNotFound 标签不存在（映射 404）。
	ErrTagNotFound = errors.New("tag not found")
	// ErrTagOperationNotFound 操作日志不存在（映射 404）。
	ErrTagOperationNotFound = errors.New("tag operation not found")
	// ErrTagOperationUndone 操作已撤销过（映射 422）。
	ErrTagOperationUndone = errors.New("tag operation already undone")
	// ErrAliasConflictsWithTagName 别名与现有标签名冲突（映射 422）。
	ErrAliasConflictsWithTagName = errors.New("alias conflicts with existing tag name")
)

// tagMergeEntry 记录一次合并中实际发生迁移的书目及其来源标签名。
// TagOperation.comicIds 以该结构的 JSON 数组存储（迁移 v37，TEXT 列）。
type tagMergeEntry struct {
	ComicID  string `json:"comicId"`
	FromName string `json:"fromName"`
}

// ApplyTagMerge 将 sourceTagIds 逐个并入 targetTagId：
// 迁移 ComicTag（去重，仅记录实际新建立的关联）、写别名（源名→目标）、删源标签，
// 并写一行 TagOperation。返回实际迁移的书目数（去重后）。
func ApplyTagMerge(targetTagID int, sourceTagIDs []int) (int, error) {
	if targetTagID <= 0 {
		return 0, ErrTagNotFound
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var targetName string
	err = tx.QueryRow(`SELECT "name" FROM "Tag" WHERE "id" = ?`, targetTagID).Scan(&targetName)
	if err == sql.ErrNoRows {
		return 0, ErrTagNotFound
	}
	if err != nil {
		return 0, err
	}

	var fromNames []string
	var moved []tagMergeEntry
	seenMoved := make(map[string]bool)

	for _, srcID := range sourceTagIDs {
		if srcID == targetTagID {
			continue // 目标自身不处理
		}
		var srcName string
		err := tx.QueryRow(`SELECT "name" FROM "Tag" WHERE "id" = ?`, srcID).Scan(&srcName)
		if err == sql.ErrNoRows {
			return 0, ErrTagNotFound
		}
		if err != nil {
			return 0, err
		}
		fromNames = append(fromNames, srcName)

		// 迁移该源标签的全部书目关联
		rows, err := tx.Query(`SELECT "comicId" FROM "ComicTag" WHERE "tagId" = ?`, srcID)
		if err != nil {
			return 0, err
		}
		var comicIDs []string
		for rows.Next() {
			var cid string
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				return 0, err
			}
			comicIDs = append(comicIDs, cid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}

		for _, cid := range comicIDs {
			res, err := tx.Exec(
				`INSERT INTO "ComicTag" ("comicId", "tagId") VALUES (?, ?) ON CONFLICT DO NOTHING`,
				cid, targetTagID,
			)
			if err != nil {
				return 0, err
			}
			// 仅记录实际新建立的目标关联（undo 时只回退这些关联）
			if n, _ := res.RowsAffected(); n > 0 && !seenMoved[cid] {
				seenMoved[cid] = true
				moved = append(moved, tagMergeEntry{ComicID: cid, FromName: srcName})
			}
		}
		if _, err := tx.Exec(`DELETE FROM "ComicTag" WHERE "tagId" = ?`, srcID); err != nil {
			return 0, err
		}

		// 写别名：源名 → 目标 tagId（已存在别名保持不变）
		if _, err := tx.Exec(
			`INSERT INTO "TagAlias" ("alias", "tagId", "createdAt") VALUES (?, ?, ?) ON CONFLICT("alias") DO NOTHING`,
			srcName, targetTagID, time.Now().UTC(),
		); err != nil {
			return 0, err
		}

		// 删除源标签
		if _, err := tx.Exec(`DELETE FROM "Tag" WHERE "id" = ?`, srcID); err != nil {
			return 0, err
		}
	}

	fromJSON, err := json.Marshal(fromNames)
	if err != nil {
		return 0, err
	}
	movedJSON, err := json.Marshal(moved)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(
		`INSERT INTO "TagOperation" ("kind", "fromNames", "toTagId", "comicIds", "undone", "createdAt")
		 VALUES (?, ?, ?, ?, 0, ?)`,
		"merge", string(fromJSON), targetTagID, string(movedJSON), time.Now().UTC(),
	); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(moved), nil
}

// UndoTagOperation 撤销一次合并：
// 重建 fromNames 对应的源标签（重名则复用），把本操作建立的 (comicId, toTagId)
// 关联改挂回源标签，删除本操作写入的别名行，标记 undone=1。
func UndoTagOperation(operationID int64) error {
	var kind, fromNamesJSON, comicIDsJSON string
	var toTagID, undone int
	err := db.QueryRow(
		`SELECT "kind", "fromNames", "toTagId", "comicIds", "undone" FROM "TagOperation" WHERE "id" = ?`,
		operationID,
	).Scan(&kind, &fromNamesJSON, &toTagID, &comicIDsJSON, &undone)
	if err == sql.ErrNoRows {
		return ErrTagOperationNotFound
	}
	if err != nil {
		return err
	}
	if undone != 0 {
		return ErrTagOperationUndone
	}

	var fromNames []string
	if err := json.Unmarshal([]byte(fromNamesJSON), &fromNames); err != nil {
		return fmt.Errorf("invalid fromNames in operation %d: %w", operationID, err)
	}
	var moved []tagMergeEntry
	if err := json.Unmarshal([]byte(comicIDsJSON), &moved); err != nil {
		return fmt.Errorf("invalid comicIds in operation %d: %w", operationID, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 重建源标签（重名复用既有标签）
	srcIDByName := make(map[string]int, len(fromNames))
	for _, name := range fromNames {
		var tagID int
		err := tx.QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = ?`, name).Scan(&tagID)
		if err == sql.ErrNoRows {
			if _, e := tx.Exec(`INSERT INTO "Tag" ("name") VALUES (?) ON CONFLICT("name") DO NOTHING`, name); e != nil {
				return e
			}
			if e := tx.QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = ?`, name).Scan(&tagID); e != nil {
				return e
			}
		} else if err != nil {
			return err
		}
		srcIDByName[name] = tagID
	}

	// 回退书目关联：改挂回源标签，移除本操作建立的目标关联
	for _, entry := range moved {
		if entry.ComicID == "" {
			continue
		}
		if entry.FromName != "" {
			if srcID, ok := srcIDByName[entry.FromName]; ok {
				if _, err := tx.Exec(
					`INSERT INTO "ComicTag" ("comicId", "tagId") VALUES (?, ?) ON CONFLICT DO NOTHING`,
					entry.ComicID, srcID,
				); err != nil {
					return err
				}
			}
		} else {
			// 兜底：缺少来源信息时挂回全部源标签
			for _, srcID := range srcIDByName {
				if _, err := tx.Exec(
					`INSERT INTO "ComicTag" ("comicId", "tagId") VALUES (?, ?) ON CONFLICT DO NOTHING`,
					entry.ComicID, srcID,
				); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(
			`DELETE FROM "ComicTag" WHERE "comicId" = ? AND "tagId" = ?`,
			entry.ComicID, toTagID,
		); err != nil {
			return err
		}
	}

	// 删除本操作产生的别名行
	for _, name := range fromNames {
		if _, err := tx.Exec(`DELETE FROM "TagAlias" WHERE "alias" = ?`, name); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`UPDATE "TagOperation" SET "undone" = 1 WHERE "id" = ?`, operationID); err != nil {
		return err
	}
	return tx.Commit()
}

// ============================================================
// 操作日志列表
// ============================================================

// TagOperationItem 操作日志条目（HTTP 契约字段，camelCase）。
type TagOperationItem struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	FromNames  []string  `json:"fromNames"`
	ToTagID    int       `json:"toTagId"`
	ToTagName  string    `json:"toTagName"`
	ComicCount int       `json:"comicCount"`
	Undone     bool      `json:"undone"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ListTagOperations 分页返回合并操作日志（最新在前）。
func ListTagOperations(page, pageSize int) ([]TagOperationItem, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "TagOperation"`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := db.Query(
		`SELECT o."id", o."kind", o."fromNames", o."toTagId", COALESCE(t."name", ''), o."comicIds", o."undone", o."createdAt"
		 FROM "TagOperation" o
		 LEFT JOIN "Tag" t ON t."id" = o."toTagId"
		 ORDER BY o."id" DESC
		 LIMIT ? OFFSET ?`,
		pageSize, (page-1)*pageSize,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := []TagOperationItem{}
	for rows.Next() {
		var item TagOperationItem
		var fromNamesJSON, comicIDsJSON string
		var undone int
		if err := rows.Scan(&item.ID, &item.Kind, &fromNamesJSON, &item.ToTagID, &item.ToTagName,
			&comicIDsJSON, &undone, &item.CreatedAt); err != nil {
			continue
		}
		item.FromNames = []string{}
		_ = json.Unmarshal([]byte(fromNamesJSON), &item.FromNames)
		var rawIDs []json.RawMessage
		_ = json.Unmarshal([]byte(comicIDsJSON), &rawIDs)
		item.ComicCount = len(rawIDs)
		item.Undone = undone != 0
		list = append(list, item)
	}
	return list, total, rows.Err()
}

// ============================================================
// 归一预览
// ============================================================

// TagVariant 预览簇中的一个变体标签。
type TagVariant struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	ComicCount int    `json:"comicCount"`
}

// TagNormCluster 按 normKey 聚合的标签簇。
type TagNormCluster struct {
	NormKey     string       `json:"normKey"`
	Variants    []TagVariant `json:"variants"`
	TotalComics int          `json:"totalComics"`
}

// PreviewTagNormalization 返回同 normKey 且变体数 > 1 的标签簇，
// 排除已忽略的 normKey，按 totalComics 降序，内存分页。
func PreviewTagNormalization(page, pageSize int) ([]TagNormCluster, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	// 标签量级千级：一条 JOIN 聚合后内存分组
	type tagRow struct {
		id, count int
		name      string
	}
	var tags []tagRow
	rows, err := db.Query(
		`SELECT t."id", t."name", COUNT(ct."comicId")
		 FROM "Tag" t
		 LEFT JOIN "ComicTag" ct ON ct."tagId" = t."id"
		 GROUP BY t."id"
		 ORDER BY t."id" ASC`,
	)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var r tagRow
		if err := rows.Scan(&r.id, &r.name, &r.count); err != nil {
			rows.Close()
			return nil, 0, err
		}
		tags = append(tags, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()

	ignored := make(map[string]bool)
	irows, err := db.Query(`SELECT "normKey" FROM "TagNormIgnore"`)
	if err != nil {
		return nil, 0, err
	}
	for irows.Next() {
		var key string
		if err := irows.Scan(&key); err == nil {
			ignored[key] = true
		}
	}
	irows.Close()
	if err := irows.Err(); err != nil {
		return nil, 0, err
	}

	grouped := make(map[string][]TagVariant)
	order := []string{} // 保持首次出现顺序，保证输出确定
	for _, t := range tags {
		key := TagNormKey(t.name)
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], TagVariant{ID: t.id, Name: t.name, ComicCount: t.count})
	}

	clusters := []TagNormCluster{}
	for _, key := range order {
		variants := grouped[key]
		if len(variants) < 2 || ignored[key] {
			continue
		}
		total := 0
		for _, v := range variants {
			total += v.ComicCount
		}
		clusters = append(clusters, TagNormCluster{NormKey: key, Variants: variants, TotalComics: total})
	}

	sort.SliceStable(clusters, func(i, j int) bool {
		if clusters[i].TotalComics != clusters[j].TotalComics {
			return clusters[i].TotalComics > clusters[j].TotalComics
		}
		return clusters[i].NormKey < clusters[j].NormKey
	})

	total := len(clusters)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return clusters[start:end], total, nil
}

// ============================================================
// 忽略管理
// ============================================================

// TagNormIgnoreItem 忽略条目。
type TagNormIgnoreItem struct {
	NormKey   string    `json:"normKey"`
	CreatedAt time.Time `json:"createdAt"`
}

// IgnoreTagNormKey 将 normKey 加入预览忽略表（幂等）。
func IgnoreTagNormKey(normKey string) error {
	key := TagNormKey(normKey)
	if key == "" {
		return errors.New("normKey is empty")
	}
	_, err := db.Exec(
		`INSERT INTO "TagNormIgnore" ("normKey", "createdAt") VALUES (?, ?) ON CONFLICT("normKey") DO NOTHING`,
		key, time.Now().UTC(),
	)
	return err
}

// UnignoreTagNormKey 移除忽略（幂等，不存在时不报错）。
func UnignoreTagNormKey(normKey string) error {
	key := TagNormKey(normKey)
	if key == "" {
		return errors.New("normKey is empty")
	}
	_, err := db.Exec(`DELETE FROM "TagNormIgnore" WHERE "normKey" = ?`, key)
	return err
}

// ListTagNormIgnores 返回全部忽略条目。
func ListTagNormIgnores() ([]TagNormIgnoreItem, error) {
	rows, err := db.Query(`SELECT "normKey", "createdAt" FROM "TagNormIgnore" ORDER BY "createdAt" DESC, "normKey" ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []TagNormIgnoreItem{}
	for rows.Next() {
		var item TagNormIgnoreItem
		if err := rows.Scan(&item.NormKey, &item.CreatedAt); err != nil {
			continue
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// ============================================================
// 别名管理
// ============================================================

// TagAliasItem 别名条目（HTTP 契约字段）。
type TagAliasItem struct {
	Alias     string    `json:"alias"`
	TagID     int       `json:"tagId"`
	TagName   string    `json:"tagName"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListTagAliases 返回全部别名。
func ListTagAliases() ([]TagAliasItem, error) {
	rows, err := db.Query(
		`SELECT a."alias", a."tagId", COALESCE(t."name", ''), a."createdAt"
		 FROM "TagAlias" a
		 LEFT JOIN "Tag" t ON t."id" = a."tagId"
		 ORDER BY a."createdAt" DESC, a."alias" ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []TagAliasItem{}
	for rows.Next() {
		var item TagAliasItem
		if err := rows.Scan(&item.Alias, &item.TagID, &item.TagName, &item.CreatedAt); err != nil {
			continue
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// AddTagAlias 新增（或更新指向）别名。别名与现有标签名重复 → ErrAliasConflictsWithTagName。
func AddTagAlias(alias string, tagID int) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return errors.New("alias is empty")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "id" = ?`, tagID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrTagNotFound
	}

	var m int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "name" = ?`, alias).Scan(&m); err != nil {
		return err
	}
	if m > 0 {
		return ErrAliasConflictsWithTagName
	}

	_, err := db.Exec(
		`INSERT INTO "TagAlias" ("alias", "tagId", "createdAt") VALUES (?, ?, ?)
		 ON CONFLICT("alias") DO UPDATE SET "tagId" = excluded."tagId"`,
		alias, tagID, time.Now().UTC(),
	)
	return err
}

// DeleteTagAlias 删除别名（幂等，不存在时不报错）。
func DeleteTagAlias(alias string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return errors.New("alias is empty")
	}
	_, err := db.Exec(`DELETE FROM "TagAlias" WHERE "alias" = ?`, alias)
	return err
}
