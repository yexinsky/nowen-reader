package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/store"
)

// TestTagFilterEndpoint 过滤名单路由：守卫（读需登录、写需管理员）+ 增删查 + 幂等计数。
func TestTagFilterEndpoint(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)

	// 未登录：读/写都 401
	if w := performRequest(r, "GET", "/api/tags/filters", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d, want 401", w.Code)
	}
	if w := performRequest(r, "POST", "/api/tags/filters", map[string]interface{}{"names": []string{"DL版"}}); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated add = %d, want 401", w.Code)
	}

	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"filter-api-1", "filterapi1.cbz", "Filter API 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := store.AddTagsToComic("filter-api-1", []string{"巨乳", "過膝襪"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	// 空 names → 400
	if w := performAuthedRequest(r, "POST", "/api/tags/filters", map[string]interface{}{"names": []string{"  "}}, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("blank names = %d, want 400", w.Code)
	}

	// 添加：已入库标签 + 繁简变体写法 + 库中不存在的预防性拉黑（名单内互不重复 → 全部入库）
	w := performAuthedRequest(r, "POST", "/api/tags/filters", map[string]interface{}{
		"names": []string{"巨乳", "过膝袜", "DL版"},
	}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("add = %d %s, want 200", w.Code, w.Body.String())
	}
	var addResp struct {
		Added   int `json:"added"`
		Skipped int `json:"skipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &addResp); err != nil {
		t.Fatalf("add resp parse failed: %v", err)
	}
	if addResp.Added != 3 || addResp.Skipped != 0 {
		t.Fatalf("added=%d skipped=%d, want 3/0", addResp.Added, addResp.Skipped)
	}

	// 列表：巨乳/过膝袜（简体写法）都关联到既有标签；DL版 尚未入库
	w = performAuthedRequest(r, "GET", "/api/tags/filters", nil, cookie)
	var listResp struct {
		List []store.TagFilterItem `json:"list"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil || len(listResp.List) != 3 {
		t.Fatalf("list = %s (err=%v)", w.Body.String(), err)
	}
	byName := map[string]store.TagFilterItem{}
	for _, item := range listResp.List {
		byName[item.Name] = item
	}
	if byName["巨乳"].TagID == 0 || byName["巨乳"].ComicCount != 1 {
		t.Fatalf("巨乳 should resolve to existing tag: %#v", byName["巨乳"])
	}
	if byName["过膝袜"].TagID == 0 || byName["过膝袜"].ComicCount != 1 {
		t.Fatalf("过膝袜 should resolve to existing 過膝襪 tag: %#v", byName["过膝袜"])
	}
	if byName["DL版"].TagID != 0 {
		t.Fatalf("DL版 should stay unmatched: %#v", byName["DL版"])
	}

	// 幂等重加（含繁简/大小写变体）→ 全部跳过
	w = performAuthedRequest(r, "POST", "/api/tags/filters", map[string]interface{}{
		"names": []string{"巨乳", "過膝襪", "dl版"},
	}, cookie)
	_ = json.Unmarshal(w.Body.Bytes(), &addResp)
	if w.Code != http.StatusOK || addResp.Added != 0 || addResp.Skipped != 3 {
		t.Fatalf("idempotent add = %d added=%d skipped=%d, want 200 0/3", w.Code, addResp.Added, addResp.Skipped)
	}

	// 移除（幂等）
	target := byName["DL版"].ID
	if w := performAuthedRequest(r, "DELETE", "/api/tags/filters/"+strconv.Itoa(target), nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200", w.Code)
	}
	if w := performAuthedRequest(r, "DELETE", "/api/tags/filters/"+strconv.Itoa(target), nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("delete idempotent = %d, want 200", w.Code)
	}
	if w := performAuthedRequest(r, "DELETE", "/api/tags/filters/abc", nil, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("delete invalid id = %d, want 400", w.Code)
	}

	w = performAuthedRequest(r, "GET", "/api/tags/filters", nil, cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil || len(listResp.List) != 2 || listResp.List[0].Name != "巨乳" {
		t.Fatalf("list after delete = %s (err=%v)", w.Body.String(), err)
	}
}

// TestJmFilterTagsByBlocklist 下载/补全共用写入闸门：命中名单丢弃、繁简折叠命中、
// 名单为空原样放行（供下载入库自动打标与标签补全两条写入路径共用）。
func TestJmFilterTagsByBlocklist(t *testing.T) {
	setupTestRouter(t)

	if kept, dropped := jmFilterTagsByBlocklist([]string{"巨乳", "剧情"}); len(kept) != 2 || len(dropped) != 0 {
		t.Fatalf("empty blocklist kept=%v dropped=%v", kept, dropped)
	}

	if _, _, err := store.AddTagFilters([]string{"巨乳", "過膝襪", "DL版"}); err != nil {
		t.Fatalf("AddTagFilters failed: %v", err)
	}
	kept, dropped := jmFilterTagsByBlocklist([]string{"巨乳", "剧情", "过膝袜", "dl版"})
	if len(kept) != 1 || kept[0] != "剧情" {
		t.Fatalf("kept = %v, want [剧情]", kept)
	}
	if len(dropped) != 3 {
		t.Fatalf("dropped = %v, want 3 items", dropped)
	}
}
