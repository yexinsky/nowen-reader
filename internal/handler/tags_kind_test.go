package handler

// GET /api/tags 的 kind 参数契约:缺省 tag(书库筛选向后语义,排除作者标签)、
// author(作者标签)、all(全部,tag-manager 用);响应条目带 kind 字段。

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/model"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

func TestListTagsKindParam(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)

	// 一本漫画:挂内容标签 + 作者标签(走作者专用写入口)
	lib := &model.Library{ID: "lib-tags-kind", Name: "kind 参数测试库", Type: "comic", RootPath: "/test/tags-kind", Enabled: true}
	if err := store.CreateLibrary(lib); err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	comics := []struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{ID: "tags-kind-1", Filename: "tags-kind-1.zip", Title: "kind 测试书", FileSize: 10},
	}
	if err := store.BulkCreateComicsWithSource(comics,
		map[string]string{"tags-kind-1": ""},
		map[string]string{"tags-kind-1": lib.ID},
	); err != nil {
		t.Fatalf("BulkCreateComicsWithSource failed: %v", err)
	}
	if err := store.AddTagsToComic("tags-kind-1", []string{"萝莉"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := store.AddAuthorTagToComic("tags-kind-1", "山本ティナ"); err != nil {
		t.Fatalf("AddAuthorTagToComic failed: %v", err)
	}

	type tagEntry struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Count int    `json:"count"`
		Kind  string `json:"kind"`
	}
	fetch := func(path string) []tagEntry {
		t.Helper()
		w := performAuthedRequest(r, "GET", path, nil, cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, w.Code, w.Body.String())
		}
		var resp struct {
			Tags []tagEntry `json:"tags"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %s failed: %v", path, err)
		}
		return resp.Tags
	}
	find := func(tags []tagEntry, name string) *tagEntry {
		t.Helper()
		for i := range tags {
			if tags[i].Name == name {
				return &tags[i]
			}
		}
		return nil
	}

	// 缺省(无 kind)= tag:只有内容标签(书库筛选向后语义)
	def := fetch("/api/tags")
	if got := find(def, "萝莉"); got == nil || got.Kind != store.TagKindTag {
		t.Fatalf("default listing must contain content tag with kind=tag, got %+v", def)
	}
	if got := find(def, "山本ティナ"); got != nil {
		t.Fatalf("default listing must exclude author tags, got %+v", def)
	}

	// kind=author:只有作者标签
	authors := fetch("/api/tags?kind=author")
	if got := find(authors, "山本ティナ"); got == nil || got.Kind != store.TagKindAuthor {
		t.Fatalf("kind=author must return author tags with kind field, got %+v", authors)
	}
	if got := find(authors, "萝莉"); got != nil {
		t.Fatalf("kind=author must exclude content tags, got %+v", authors)
	}

	// kind=all:全部,各自带 kind
	all := fetch("/api/tags?kind=all")
	c := find(all, "萝莉")
	a := find(all, "山本ティナ")
	if c == nil || c.Kind != store.TagKindTag || a == nil || a.Kind != store.TagKindAuthor {
		t.Fatalf("kind=all must return both kinds with kind field, got %+v", all)
	}

	// 非法 kind → 400
	if w := performAuthedRequest(r, "GET", "/api/tags?kind=bogus", nil, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind should 400, got %d", w.Code)
	}
}
