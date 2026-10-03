package jm

// 标签/作者收藏存储单测:增查删幂等、type 维度、旧数据归一、重开重读(纯本地,不触网)。

import (
	"os"
	"testing"
)

func newTagFavStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func TestTagFavoriteAddListIdempotent(t *testing.T) {
	s := newTagFavStore(t)

	if err := s.AddTagFavorite(TagFavoriteTypeTag, " 巨乳 "); err != nil {
		t.Fatalf("收藏失败: %v", err)
	}
	if err := s.AddTagFavorite(TagFavoriteTypeTag, "JK"); err != nil {
		t.Fatalf("收藏失败: %v", err)
	}
	// 重复收藏同 (type,tag):幂等,不新增、不报错
	if err := s.AddTagFavorite(TagFavoriteTypeTag, "巨乳"); err != nil {
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
	if list[0].Type != TagFavoriteTypeTag {
		t.Fatalf("type 应归一为 tag: %#v", list[0])
	}
}

func TestTagFavoriteTypeDimensions(t *testing.T) {
	s := newTagFavStore(t)

	// 同名值可同时以标签与作者两种类型收藏(幂等键 = type+tag)
	if err := s.AddTagFavorite(TagFavoriteTypeTag, "山本ティナ"); err != nil {
		t.Fatalf("收藏标签失败: %v", err)
	}
	if err := s.AddTagFavorite(TagFavoriteTypeAuthor, "山本ティナ"); err != nil {
		t.Fatalf("收藏作者失败: %v", err)
	}
	list, _ := s.ListTagFavorites()
	if len(list) != 2 {
		t.Fatalf("同名 tag+author 应共存 2 条: %#v", list)
	}

	// 重复添加同类型幂等
	_ = s.AddTagFavorite(TagFavoriteTypeAuthor, "山本ティナ")
	list, _ = s.ListTagFavorites()
	if len(list) != 2 {
		t.Fatalf("同类型重复收藏应幂等: %#v", list)
	}

	// 按类型删除互不影响
	if err := s.RemoveTagFavorite(TagFavoriteTypeAuthor, "山本ティナ"); err != nil {
		t.Fatalf("删除作者失败: %v", err)
	}
	list, _ = s.ListTagFavorites()
	if len(list) != 1 || list[0].Type != TagFavoriteTypeTag {
		t.Fatalf("删除作者后应仅剩标签: %#v", list)
	}

	// normalize:未知 type 归一为 tag,与显式 tag 幂等
	if err := s.AddTagFavorite("whatever", "怪类型"); err != nil {
		t.Fatalf("未知 type 收藏失败: %v", err)
	}
	list, _ = s.ListTagFavorites()
	if len(list) != 2 || list[1].Type != TagFavoriteTypeTag {
		t.Fatalf("未知 type 应归一为 tag: %#v", list)
	}
}

func TestTagFavoriteLegacyDataNormalized(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	_ = s.AddTagFavorite(TagFavoriteTypeTag, "旧收藏")

	// 模拟升级前旧文件:去掉 type 字段
	p := s.tagFavoritesPath()
	legacy := `{"list":[{"tag":"旧收藏","createdAt":"2026-01-01T00:00:00"}]}`
	if err := os.WriteFile(p, []byte(legacy), 0o644); err != nil {
		t.Fatalf("写旧格式失败: %v", err)
	}

	list, err := s.ListTagFavorites()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(list) != 1 || list[0].Type != TagFavoriteTypeTag || list[0].Tag != "旧收藏" {
		t.Fatalf("旧数据 type 应归一为 tag: %#v", list)
	}

	// 归一后的 (tag,旧收藏) 幂等:不会重复追加
	_ = s.AddTagFavorite(TagFavoriteTypeTag, "旧收藏")
	list, _ = s.ListTagFavorites()
	if len(list) != 1 {
		t.Fatalf("归一后重复收藏应幂等: %#v", list)
	}
}

func TestTagFavoriteRemoveAndEmpty(t *testing.T) {
	s := newTagFavStore(t)

	// 删除不存在的 tag:幂等成功
	if err := s.RemoveTagFavorite(TagFavoriteTypeTag, "不存在"); err != nil {
		t.Fatalf("删除不存在 tag 应幂等: %v", err)
	}
	_ = s.AddTagFavorite(TagFavoriteTypeTag, "女仆")
	if err := s.RemoveTagFavorite(TagFavoriteTypeTag, " 女仆 "); err != nil {
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
	_ = s1.AddTagFavorite(TagFavoriteTypeTag, "校园")
	_ = s1.AddTagFavorite(TagFavoriteTypeAuthor, "千叶リョウコ")

	// 重新打开(模拟进程重启)后数据仍在,type 保留
	s2 := NewStore(dir)
	list, err := s2.ListTagFavorites()
	if err != nil {
		t.Fatalf("重读失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("重开重读条数不符: %#v", list)
	}
	byKey := map[string]string{}
	for _, it := range list {
		byKey[it.Type+"\x1f"+it.Tag] = it.Tag
	}
	if byKey[TagFavoriteTypeTag+"\x1f校园"] != "校园" || byKey[TagFavoriteTypeAuthor+"\x1f千叶リョウコ"] != "千叶リョウコ" {
		t.Fatalf("重开重读 type/tag 不符: %#v", list)
	}

	// 落盘文件确为 tag-favorites.json 且 schema 为 {"list":[...]}
	check := NewStore(dir)
	_ = check.AddTagFavorite(TagFavoriteTypeTag, "第三条")
	list2, _ := check.ListTagFavorites()
	if len(list2) != 3 {
		t.Fatalf("追加后应 3 条: %#v", list2)
	}
}

func TestTagFavoriteEmptyTagIgnored(t *testing.T) {
	s := newTagFavStore(t)
	if err := s.AddTagFavorite(TagFavoriteTypeAuthor, "   "); err != nil {
		t.Fatalf("空 tag 应静默忽略: %v", err)
	}
	list, _ := s.ListTagFavorites()
	if len(list) != 0 {
		t.Fatalf("空 tag 不应入库: %#v", list)
	}
}
