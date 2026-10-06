package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/service"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// TestSnapshotEndpointCRUDAndGuards 路由守卫 + 手动创建/列表/删除。
func TestSnapshotEndpointCRUDAndGuards(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)

	// 未登录 → 401
	if w := performRequest(r, "GET", "/api/snapshots", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d, want 401", w.Code)
	}

	// 手动创建（无名 → 默认名）
	w := performAuthedRequest(r, "POST", "/api/snapshots", map[string]string{}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d %s, want 200", w.Code, w.Body.String())
	}
	var created struct {
		OK       bool `json:"ok"`
		Snapshot struct {
			ID   int64  `json:"id"`
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || !created.OK || created.Snapshot.ID <= 0 {
		t.Fatalf("create response = %s (err=%v)", w.Body.String(), err)
	}
	if created.Snapshot.Kind != "manual" {
		t.Fatalf("kind = %s, want manual", created.Snapshot.Kind)
	}

	// 列表可见
	w = performAuthedRequest(r, "GET", "/api/snapshots", nil, cookie)
	var list struct {
		List []struct {
			ID   int64  `json:"id"`
			Kind string `json:"kind"`
		} `json:"list"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.List) != 1 {
		t.Fatalf("list = %s (err=%v)", w.Body.String(), err)
	}

	// 删除
	if w := performAuthedRequest(r, "DELETE", "/api/snapshots/"+itoa(int(created.Snapshot.ID)), nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("delete = %d %s, want 200", w.Code, w.Body.String())
	}
	w = performAuthedRequest(r, "GET", "/api/snapshots", nil, cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.List) != 0 {
		t.Fatalf("list after delete = %s (err=%v)", w.Body.String(), err)
	}

	// 恢复不存在的快照 → 404
	if w := performAuthedRequest(r, "POST", "/api/snapshots/9999/restore", nil, cookie); w.Code != http.StatusNotFound {
		t.Fatalf("restore missing = %d, want 404", w.Code)
	}
}

// TestAISuggestTagMergesCreatesAutoSnapshot 用 mock LLM 验证：
// AI 归并 apply=true 时自动创建域快照，恢复该快照可把合并后的状态复原。
func TestAISuggestTagMergesCreatesAutoSnapshot(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)
	t.Setenv("DATA_DIR", t.TempDir())

	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"snap-ai-1", "snapai1.cbz", "Snap AI 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := store.AddTagsToComic("snap-ai-1", []string{"巨乳", "besar"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	aiReply := `[{"target":"巨乳","sources":["besar"]}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + strings.ReplaceAll(aiReply, `"`, `\"`) + `"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	if err := service.SaveAIConfig(testAIConfigWithURL(server.URL + "/v1")); err != nil {
		t.Fatalf("SaveAIConfig failed: %v", err)
	}

	// AI 归并（apply=true）→ 自动快照 + 标签合并
	w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{"apply": true}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("suggest-tag-merges = %d %s, want 200", w.Code, w.Body.String())
	}

	// 源标签已删除
	var n int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "name" = 'besar'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("besar should be merged away (n=%d, err=%v)", n, err)
	}

	// 自动快照已创建
	var list struct {
		List []struct {
			ID     int64  `json:"id"`
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
			Name   string `json:"name"`
		} `json:"list"`
	}
	w = performAuthedRequest(r, "GET", "/api/snapshots", nil, cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse list failed: %v", err)
	}
	if len(list.List) != 1 || list.List[0].Kind != "auto" || list.List[0].Reason != "AI 归并应用前" {
		t.Fatalf("snapshots = %#v, want 1 auto with reason AI 归并应用前", list.List)
	}
	snapID := list.List[0].ID

	// 恢复快照 → besar 复活，别名恢复，书目关联恢复
	w = performAuthedRequest(r, "POST", "/api/snapshots/"+itoa(int(snapID))+"/restore", nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("restore = %d %s, want 200", w.Code, w.Body.String())
	}
	var restoreResp struct {
		OK       bool `json:"ok"`
		Restored struct {
			SafetySnapshot int64 `json:"safetySnapshotId"`
		} `json:"restored"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &restoreResp); err != nil || !restoreResp.OK {
		t.Fatalf("restore response = %s (err=%v)", w.Body.String(), err)
	}
	if restoreResp.Restored.SafetySnapshot <= 0 {
		t.Fatalf("safety snapshot id missing: %s", w.Body.String())
	}

	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "name" = 'besar'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("besar should be restored (n=%d, err=%v)", n, err)
	}
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM "ComicTag" ct JOIN "Tag" t ON t."id" = ct."tagId" WHERE ct."comicId" = 'snap-ai-1' AND t."name" = 'besar'`,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("snap-ai-1 should have besar after restore (n=%d, err=%v)", n, err)
	}
}
