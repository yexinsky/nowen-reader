package jm

// 下载入库自动标签:标签清洗纯函数 + settings.downloadTags 默认值/持久化单测。

import (
	"path/filepath"
	"strconv"
	"testing"
)

func TestNormalizeJmTags(t *testing.T) {
	got := normalizeJmTags([]any{
		" 巨乳 ", "JK", "", "  ", "巨乳", // 空值剔除 + trim + 去重
		float64(123), // 数值转串(stringify 语义)
		"校园",
	})
	if len(got) != 4 {
		t.Fatalf("应得 4 个标签, got %v", got)
	}
	if got[0] != "巨乳" || got[1] != "JK" || got[2] != "123" || got[3] != "校园" {
		t.Fatalf("顺序/去重/trim 不符: %v", got)
	}

	// 上限截断
	many := make([]any, maxDownloadTags+10)
	for i := range many {
		many[i] = "tag" + strconv.Itoa(i)
	}
	if got := normalizeJmTags(many); len(got) != maxDownloadTags {
		t.Fatalf("应截断到 %d, got %d", maxDownloadTags, len(got))
	}

	// 空输入
	if got := normalizeJmTags(nil); len(got) != 0 {
		t.Fatalf("空输入应为空: %v", got)
	}
}

// TestDownloadManagerTagFilter 下载快照过滤钩子:注册后上游标签先经钩子裁剪再入快照
// (任务列表展示的即"入库后会挂上的标签");未注册时原样通过。
func TestDownloadManagerTagFilter(t *testing.T) {
	m := NewDownloadManager(nil, t.TempDir())

	// 未注册钩子:原样通过
	in := []string{"巨乳", "DL版"}
	if got := m.filterTags(in); len(got) != 2 {
		t.Fatalf("未注册钩子应原样返回: %v", got)
	}

	m.SetTagFilter(func(tags []string) []string {
		kept := make([]string, 0, len(tags))
		for _, tag := range tags {
			if tag != "DL版" {
				kept = append(kept, tag)
			}
		}
		return kept
	})
	if got := m.filterTags(in); len(got) != 1 || got[0] != "巨乳" {
		t.Fatalf("钩子未生效: %v", got)
	}
	if in[0] != "巨乳" || in[1] != "DL版" {
		t.Fatalf("钩子不得就地改写入参: %v", in)
	}
}

func TestSettingsDownloadTagsDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	// 旧 settings.json(无 downloadTags 键)/文件不存在 → 默认 true
	if got := s.LoadSettings("").DownloadTags; !got {
		t.Fatalf("downloadTags 缺省应为 true")
	}

	// 显式关闭 → 落盘 → 重开仍为 false
	if err := s.SaveSettings(Settings{
		Proxy:        "",
		ImageQuality: "high",
		DownloadTags: false,
	}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if got := NewStore(filepath.Join(dir)).LoadSettings("").DownloadTags; got {
		t.Fatalf("downloadTags=false 应持久化")
	}

	// 再显式打开 → 重开仍为 true
	if err := s.SaveSettings(Settings{ImageQuality: "high", DownloadTags: true}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if got := s.LoadSettings("").DownloadTags; !got {
		t.Fatalf("downloadTags=true 应持久化")
	}
}
