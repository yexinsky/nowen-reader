package handler

// 下载入库作者同步:作者名归一(占位符剔除)单测。

import "testing"

func TestJmSyncAuthorName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{" 山本ティナ ", "山本ティナ"}, // 正常:trim
		{"N/A", ""},            // 占位符(1475046 实测形态,不区分大小写)
		{"n/a", ""},
		{"-", ""},
		{"未知作者", ""},
		{"default_author", ""},
		{"Unknown", ""},
		{"", ""},
		{"   ", ""},
		{"千叶リョウコ", "千叶リョウコ"},
	}
	for _, c := range cases {
		if got := jmSyncAuthorName(c.in); got != c.want {
			t.Fatalf("jmSyncAuthorName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
