package handler

import "testing"

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
}
