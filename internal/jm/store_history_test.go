package jm

import (
	"testing"
)

// newHistoryTestStore 建一个临时目录的 Store,测试结束自动清理。
func newHistoryTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

// TestReportHistoryKeepsExistingMeta 覆盖「缺省不覆盖」兜底:
// 阅读器只报页码(标题/封面为空)时,不能把服务端已存的标题与封面清空。
func TestReportHistoryKeepsExistingMeta(t *testing.T) {
	s := newHistoryTestStore(t)

	first := HistoryItem{
		Aid: "100", Pid: "100", Title: "作品名", CoverURL: "/p/1.jpg",
		EpTitle: "第1话", ImageIndex: 5,
	}
	if err := s.ReportHistory(first); err != nil {
		t.Fatalf("首次上报失败: %v", err)
	}

	// 只报页码:标题/封面/epTitle 全部留空
	if err := s.ReportHistory(HistoryItem{Aid: "100", Pid: "100", ImageIndex: 42}); err != nil {
		t.Fatalf("二次上报失败: %v", err)
	}

	list, _, _ := s.ListHistory(1)
	if len(list) != 1 {
		t.Fatalf("同 aid+pid 应只有一条记录,实际 %d 条", len(list))
	}
	got := list[0]
	if got.ImageIndex != 42 {
		t.Errorf("页码应更新为 42,实际 %d", got.ImageIndex)
	}
	if got.Title != "作品名" {
		t.Errorf("标题应保留原值「作品名」,实际 %q", got.Title)
	}
	if got.CoverURL != "/p/1.jpg" {
		t.Errorf("封面应保留原值,实际 %q", got.CoverURL)
	}
	if got.EpTitle != "第1话" {
		t.Errorf("章节名应保留原值「第1话」,实际 %v", got.EpTitle)
	}
}

// TestReportHistoryOverwritesWithNewMeta 显式给出非空值时应正常覆盖。
func TestReportHistoryOverwritesWithNewMeta(t *testing.T) {
	s := newHistoryTestStore(t)

	if err := s.ReportHistory(HistoryItem{
		Aid: "200", Pid: "200", Title: "旧标题", CoverURL: "/old.jpg", ImageIndex: 1,
	}); err != nil {
		t.Fatalf("首次上报失败: %v", err)
	}
	if err := s.ReportHistory(HistoryItem{
		Aid: "200", Pid: "200", Title: "新标题", CoverURL: "/new.jpg", ImageIndex: 3,
	}); err != nil {
		t.Fatalf("二次上报失败: %v", err)
	}

	list, _, _ := s.ListHistory(1)
	if len(list) != 1 {
		t.Fatalf("同 aid+pid 应只有一条记录,实际 %d 条", len(list))
	}
	if list[0].Title != "新标题" || list[0].CoverURL != "/new.jpg" {
		t.Errorf("显式给出的新值应覆盖,实际 title=%q cover=%q", list[0].Title, list[0].CoverURL)
	}
}

// TestReportHistoryBlankOnlySpaces 只含空白的字符串也视为「未提供」。
func TestReportHistoryBlankOnlySpaces(t *testing.T) {
	s := newHistoryTestStore(t)

	if err := s.ReportHistory(HistoryItem{Aid: "300", Pid: "300", Title: "作品", CoverURL: "/c.jpg"}); err != nil {
		t.Fatalf("首次上报失败: %v", err)
	}
	if err := s.ReportHistory(HistoryItem{Aid: "300", Pid: "300", Title: "   ", CoverURL: " \t "}); err != nil {
		t.Fatalf("二次上报失败: %v", err)
	}

	list, _, _ := s.ListHistory(1)
	if list[0].Title != "作品" || list[0].CoverURL != "/c.jpg" {
		t.Errorf("空白串不应覆盖既有值,实际 title=%q cover=%q", list[0].Title, list[0].CoverURL)
	}
}

// TestStartFromFirstPageSetting 覆盖新设置项的默认值与读写往返。
func TestStartFromFirstPageSetting(t *testing.T) {
	s := newHistoryTestStore(t)

	got := s.LoadSettings("")
	if got.StartFromFirstPage {
		t.Error("默认值应为 false(保持按记录续读)")
	}

	got.StartFromFirstPage = true
	if err := s.SaveSettings(got); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	if again := s.LoadSettings(""); !again.StartFromFirstPage {
		t.Error("保存后重新读取应为 true")
	}

	got.StartFromFirstPage = false
	if err := s.SaveSettings(got); err != nil {
		t.Fatalf("再次保存失败: %v", err)
	}
	if again := s.LoadSettings(""); again.StartFromFirstPage {
		t.Error("关闭后重新读取应为 false")
	}
}
