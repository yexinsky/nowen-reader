package handler

import (
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/jm"
)

// 旧库命名两主形态(用户实测):"漫画名-数字id" 与 纯漫画名,
// 外加卷话后缀/汉化组括号等噪声。纯数字(车号)保留,短数字结尾不误杀。
func TestJmCleanSearchKeyword(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"呑噬万物-125734", "呑噬万物"},
		{"海贼王", "海贼王"},
		{"海贼王-123456 第01卷", "海贼王"},
		{"海贼王 第100卷", "海贼王"},
		{"海贼王 vol.3", "海贼王"},
		{"进击的巨人 Chapter 12", "进击的巨人"},
		{"【汉化组】海贼王", "海贼王"},
		{"海贼王(汉化)-123456", "海贼王"},
		{"海贼王 [RAW] (1)", "海贼王"},
		{"【搬运】鬼灭之刃 【汉化】", "鬼灭之刃"},
		{"【汉化】", ""},             // 全噪声 → 空词,调用方按"无效"跳过
		{"", ""},                 // 空标题
		{"   ", ""},              // 纯空白
		{"125734", "125734"},     // 纯数字:车号直达,保留
		{"一拳超人 2", "一拳超人 2"},    // 短数字结尾不是 id,不误杀
		{"一拳超人-2", "一拳超人-2"},    // 短数字尾缀同样保留
		{"東京喰種：re 第3巻", "東京喰種：re"}, // 中文卷号 + 冒号标题
		// id 夹在中间 + 前后重复(用户实测 bug 标题形态)
		{"[黒青郎君]永世流転-224406-[黒青郎君]永世流転", "永世流転"}, // 拆半 + 剥括号
		{"海贼王-海贼王-125734", "海贼王"},
		{"海贼王 - 123456", "海贼王"},    // 空格夹分隔符
		{"1234-5678", "1234-5678"},   // 全数字段:剔除后为空,原样保留
		{"test-comic", "test-comic"}, // 合法连字符名不动
		// 爬虫拼接标题(实测库主流形态):拆半取干净一半,再剥括号留标题主体——
		// 上游对带括号的长关键词失效(返回默认列表),纯主体才搜得到
		{"(C97) [サークルフィオレ (えかきびと)] りゅうおうのまとめぼん (りゅうおうのおしごと!)-262147-(C97) [サークルフィオレ (えかきびと)] りゅうおうのまとめぼん (りゅうおうのおしごと!)",
			"りゅうおうのまとめぼん"},
		{"[Cathriell Rue] Mrs. Graves (The Coffin of Andy and Leyley)-1230237-[路小茜个人汉化] [Cathriell Rue] Mrs. Graves (The Coffin of Andy and Leyley)",
			"Mrs. Graves"},
		{"[TWILIGHT DUSK (藍夜)] 勤め先の娘さんをおいしく頂く本 それから…&amp;nbsp;&amp;nbsp;[中国翻訳]-1228837-[TWILIGHT DUSK (藍夜)] 勤め先の娘さんをおいしく頂く本 それから…&amp;nbsp;&amp;nbsp;[中国翻訳]",
			"勤め先の娘さんをおいしく頂く本 それから…"}, // HTML 实体 + 翻訳噪声括号
	}
	for _, tc := range cases {
		if got := jmCleanSearchKeyword(tc.in); got != tc.want {
			t.Errorf("jmCleanSearchKeyword(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJmScoreMatch(t *testing.T) {
	cases := []struct {
		name         string
		local, lAuth string
		remote, rAuth string
		wantConf     string
	}{
		{"全等", "呑噬万物", "", "呑噬万物", "", "high"},
		{"全等+作者加分仍封顶", "呑噬万物", "田中", "呑噬万物", "田中", "high"},
		{"包含", "海贼王 第100卷", "", "海贼王", "", "medium"},
		{"包含+作者到high", "海贼王", "尾田", "海贼王学院", "尾田", "high"},
		{"bigram部分重叠", "航海王物语", "", "海贼王传说", "", "low"},
		{"无关", "一个男人", "", "关于咖啡的一切", "", "low"},
	}
	for _, tc := range cases {
		score := jmScoreMatch(tc.local, tc.lAuth, tc.remote, tc.rAuth)
		if got := jmMatchConfidence(score); got != tc.wantConf {
			t.Errorf("%s: score=%.2f confidence=%s, want %s", tc.name, score, got, tc.wantConf)
		}
	}

	if s := jmScoreMatch("", "", "呑噬万物", ""); s != 0 {
		t.Errorf("empty local title should score 0, got %.2f", s)
	}
	// 包含关系 0.75 基底,加作者 0.1 后应落在 [0.85, 1] 内
	s := jmScoreMatch("海贼王", "尾田", "海贼王学院", "尾田")
	if s < jmMatchConfHigh || s > 1 {
		t.Errorf("containment+author score out of range: %.2f", s)
	}

	// 实质子串(≥8 字符)→ 0.9 高置信:爬虫噪声超集 vs JM 原版标题的典型形态
	doubled := "(C97) [サークルフィオレ (えかきびと)] りゅうおうのまとめぼん (りゅうおうのおしごと!)-262147-(C97) [サークルフィオレ (えかきびと)] りゅうおうのまとめぼん (りゅうおうのおしごと!)"
	canonical := "(C97) [サークルフィオレ (えかきびと)] りゅうおうのまとめぼん (りゅうおうのおしごと!)"
	if s := jmScoreMatch(doubled, "", canonical, "えかきびと"); jmMatchConfidence(s) != "high" {
		t.Errorf("scraper-superset vs canonical should be high, got %.2f", s)
	}
	// 高置信档"作者加分封顶"不受实质子串档影响
	if s := jmScoreMatch("海贼王", "尾田", "海贼王 第100卷", "尾田"); s > 1 {
		t.Errorf("author bonus should cap at 1, got %.2f", s)
	}
}

// 标题内嵌 JM aid(爬虫落库痕迹):命中即可车号直达,不再依赖标题相似度。
func TestJmExtractEmbeddedAid(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"[Cathriell Rue] Mrs. Graves (The Coffin of Andy and Leyley)-1230237-[路小茜个人汉化] [Cathriell Rue] Mrs. Graves (The Coffin of Andy and Leyley)", "1230237"},
		{"[個人機翻][AKAIMELON] [Full color] CRAZY SEX LIFE OF THE FORGER FAMILY (SPY X FAMILY)-1292135-[個人機翻][AKAIMELON] [Full color] CRAZY SEX LIFE OF THE FORGER FAMILY (SPY X FAMILY)", "1292135"},
		{"人類的最後放送 [路小茜個人漢化] Humanity's Last Broadcast - A Fubuki Doujin-1204748", "1204748"}, // 尾部
		{"([临月汉化] [呆然乙女R (Anago)] 桐生キキョウは孕みたい (ブルーアーカイブ) [DL版]-1054639)-[临月汉化] [呆然乙女R (Anago)] 桐生キキョウは孕みたい (ブルーアーカイブ) [DL版]", "1054639"}, // 右括号边界
		{"125734", "125734"},        // 整标题即车号
		{"1234-5678", ""},           // 4 位数字段不是 aid
		{"海贼王 第100卷", ""},           // 卷号
		{"一拳超人-2", ""},             // 短数字
		{"海贼王", ""},                 // 无数字
	}
	for _, tc := range cases {
		if got := jmExtractEmbeddedAid(tc.in); got != tc.want {
			t.Errorf("jmExtractEmbeddedAid(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 尾部卷话标记提取(漫画名补全改名防呆):只认锚定结尾的卷话形态,
// 裸数字尾缀不提取(可能是标题本体),标题中部不算。
func TestJmExtractVolumeMarker(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"海贼王 第100卷", "第100卷"},
		{"海贼王 vol.3", "vol.3"},
		{"海贼王 Vol 12", "Vol 12"},
		{"进击的巨人 ch.5", "ch.5"},
		{"进击的巨人 Ch 7", "Ch 7"},
		{"进击的巨人 chapter 9", "chapter 9"},
		{"東京喰種 第3話", "第3話"},
		{"東京喰種 第12话", "第12话"},
		{"某作品 第2季", "第2季"},
		{"某作品 第12.5话", "第12.5话"},   // 小数话数
		{"海贼王 第100卷  ", "第100卷"},    // 尾随空白
		{"进击的巨人 04", ""},             // 裸数字尾缀:可能是标题本体,不提取
		{"海贼王", ""},                   // 无标记
		{"", ""},                       // 空标题
		{"第3话的爱情", ""},              // 标题本体含"第3话"但不在尾部
		{"某作品 第3话 前篇", ""},         // 标记后还有别的内容,不算尾部
	}
	for _, tc := range cases {
		if got := jmExtractVolumeMarker(tc.in); got != tc.want {
			t.Errorf("jmExtractVolumeMarker(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 新名合成:无标记直接用 JM 原题;有标记且 JM 题未含则拼接保住多卷区分度;
// JM 题已含标记不重复;裸数字尾缀不参与拼接。
func TestJmComposeRenamedTitle(t *testing.T) {
	cases := []struct {
		name    string
		orig    string
		jmTitle string
		want    string
	}{
		{"无标记→JM原题", "海贼王-125734", "ONE PIECE", "ONE PIECE"},
		{"有标记且JM不含→拼接", "海贼王 第100卷", "海贼王", "海贼王 第100卷"},
		{"JM已含标记→不重复", "海贼王 第100卷", "海贼王 第100卷", "海贼王 第100卷"},
		{"vol标记拼接", "海贼王 vol.3", "海贼王", "海贼王 vol.3"},
		{"裸数字→不拼接", "进击的巨人 04", "进击的巨人", "进击的巨人"},
		{"话数拼接", "東京喰種 第12话", "東京喰種:re", "東京喰種:re 第12话"},
	}
	for _, tc := range cases {
		if got := jmComposeRenamedTitle(tc.orig, tc.jmTitle); got != tc.want {
			t.Errorf("%s: jmComposeRenamedTitle(%q, %q) = %q, want %q",
				tc.name, tc.orig, tc.jmTitle, got, tc.want)
		}
	}
}

// 详情标题提取(mapAlbumDetail 映射结果取 "title"):异常响应返回空串。
func TestJmExtractDetailTitle(t *testing.T) {
	// mapped 详情:mapAlbumDetail 把上游 name 映射为 "title"
	if got := jm.ExtractDetailTitle(map[string]any{
		"aid": "125734", "title": "呑噬万物", "author": "N/A",
	}); got != "呑噬万物" {
		t.Errorf("ExtractDetailTitle(mapped) = %q, want %q", got, "呑噬万物")
	}
	if got := jm.ExtractDetailTitle(map[string]any{"aid": "125734"}); got != "" {
		t.Errorf("ExtractDetailTitle(no title) = %q, want empty", got)
	}
	// 异常形态:非 map / nil → 空串
	if got := jm.ExtractDetailTitle("not a map"); got != "" {
		t.Errorf("ExtractDetailTitle(string) = %q, want empty", got)
	}
	if got := jm.ExtractDetailTitle(nil); got != "" {
		t.Errorf("ExtractDetailTitle(nil) = %q, want empty", got)
	}
}
