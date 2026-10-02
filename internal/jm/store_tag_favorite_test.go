package jm

// 标签收藏存储单测:增查删幂等、trim、倒序、重开重读(纯本地,不触网)。

import (
	"path/filepath"
	"testing"
)

func newTagFavStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func TestTagFavoriteAddListIdempotent(t *testing.T) {
	s := newTagFavStore(t)

	if err := s.AddTagFavorite(" 巨乳 "); err != nil {
		t.Fatalf("收藏失败: %v", err)
	}
	if err := s.AddTagFavorite("JK"); err != nil {
		t.Fatalf("收藏失败: %v", err)
	}
	// 重复收藏同 tag:幂等,不新增、不报错
	if err := s.AddTagFavorite("巨乳"); err != nil {
		t.Fatalf("重复收藏应幂等: %v", err)
	}

	list, err := s.ListTagFavorites()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应恰有 2 条(幂等去重), got %d: %#v", len(list), list)
	}
	// createdAt 秒级精度:同秒并列按稳定排序保持入库顺序(先收藏在前)
	if list[0].Tag != "巨乳" || list[1].Tag != "JK" {
		t.Fatalf("同秒并列应保持入库顺序: %#v", list)
	}
	// trim:收藏与查询都不带首尾空白
	if list[0].Tag != "巨乳" {
		t.Fatalf("tag 应 trim: %q", list[0].Tag)
	}
	if list[0].CreatedAt == "" {
		t.Fatalf("createdAt 应记录: %#v", list[0])
	}
}

func TestTagFavoriteRemoveAndEmpty(t *testing.T) {
	s := newTagFavStore(t)

	// 删除不存在的 tag:幂等成功
	if err := s.RemoveTagFavorite("不存在"); err != nil {
		t.Fatalf("删除不存在 tag 应幂等: %v", err)
	}
	_ = s.AddTagFavorite("女仆")
	if err := s.RemoveTagFavorite(" 女仆 "); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	list, err := s.ListTagFavorites()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("删除后应为空: %#v", list)
	}
}

func TestTagFavoritePersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	_ = s1.AddTagFavorite("校园")
	_ = s1.AddTagFavorite("奇幻")

	// 重新打开(模拟进程重启)后数据仍在
	s2 := NewStore(dir)
	list, err := s2.ListTagFavorites()
	if err != nil {
		t.Fatalf("重读失败: %v", err)
	}
	if len(list) != 2 || list[0].Tag != "校园" || list[1].Tag != "奇幻" {
		t.Fatalf("重开重读不符: %#v", list)
	}

	// 落盘文件确为 tag-favorites.json 且 schema 为 {"list":[...]}
	check := NewStore(filepath.Join(dir))
	_ = check.AddTagFavorite("第三条")
	list2, _ := check.ListTagFavorites()
	if len(list2) != 3 {
		t.Fatalf("追加后应 3 条: %#v", list2)
	}
}

func TestTagFavoriteEmptyTagIgnored(t *testing.T) {
	s := newTagFavStore(t)
	if err := s.AddTagFavorite("   "); err != nil {
		t.Fatalf("空 tag 应静默忽略: %v", err)
	}
	list, _ := s.ListTagFavorites()
	if len(list) != 0 {
		t.Fatalf("空 tag 不应入库: %#v", list)
	}
}
