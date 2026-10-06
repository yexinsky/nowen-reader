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

// TestValidateTagMapping 校验纯函数：命中/去重/词表 normKey 对齐/繁简漂移回填。
func TestValidateTagMapping(t *testing.T) {
	candidates := []store.TagVariant{
		{ID: 3, Name: "黑白絲", ComicCount: 5},
		{ID: 4, Name: "垃圾站名", ComicCount: 2},
	}
	vocab := []store.TagVariant{
		{ID: 1, Name: "巨乳", ComicCount: 100},
		{ID: 2, Name: "過膝襪", ComicCount: 40},
	}

	suggestions := []service.TagMappingSuggestion{
		// 合法：AI 目标写成简体（词表是繁体）→ normKey 对齐回词表项，名称回填词表原文
		{Tag: "黑白絲", Action: "map", Target: "过膝袜", Reason: "同义"},
		// 目标不在词表（normKey 也对不上）→ 丢弃（视为保留）
		{Tag: "垃圾站名", Action: "map", Target: "不存在词"},
		// delete 保留
		{Tag: "垃圾站名", Action: "delete", Reason: "站名"},
		// 未知标签 → 丢弃
		{Tag: "不存在", Action: "delete"},
		// keep/未知动作 → 不输出
		{Tag: "黑白絲", Action: "keep"},
	}

	items := validateTagMapping(suggestions, candidates, vocab)
	if len(items) != 2 {
		t.Fatalf("items = %#v, want exactly 2", items)
	}
	if items[0].Action != "map" || items[0].Tag != "黑白絲" || items[0].Target == nil ||
		items[0].Target.ID != 2 || items[0].Target.Name != "過膝襪" {
		t.Fatalf("item[0] = %#v, want map 黑白絲 -> 過膝襪 (繁简对齐)", items[0])
	}
	if items[1].Action != "delete" || items[1].Tag != "垃圾站名" || items[1].Target != nil {
		t.Fatalf("item[1] = %#v, want delete 垃圾站名", items[1])
	}
}

// TestTagVocabularyEndpoint Vocabulary 路由：守卫、增删、繁简冲突 422。
func TestTagVocabularyEndpoint(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)

	if w := performRequest(r, "GET", "/api/tags/vocabulary", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d, want 401", w.Code)
	}

	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"vocab-api-1", "vapi1.cbz", "Vocab API 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := store.AddTagsToComic("vocab-api-1", []string{"巨乳", "過膝襪"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	juruID, guoxiID := 0, 0
	_ = store.DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '巨乳'`).Scan(&juruID)
	_ = store.DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '過膝襪'`).Scan(&guoxiID)

	// 空 body → 400
	if w := performAuthedRequest(r, "POST", "/api/tags/vocabulary", map[string]interface{}{}, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("empty body = %d, want 400", w.Code)
	}

	// 添加两项
	if w := performAuthedRequest(r, "POST", "/api/tags/vocabulary", map[string]interface{}{"add": []int{juruID, guoxiID}}, cookie); w.Code != http.StatusOK {
		t.Fatalf("add = %d %s, want 200", w.Code, w.Body.String())
	}

	w := performAuthedRequest(r, "GET", "/api/tags/vocabulary", nil, cookie)
	var resp struct {
		List []struct {
			TagID int    `json:"tagId"`
			Name  string `json:"name"`
		} `json:"list"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.List) != 2 {
		t.Fatalf("list = %s (err=%v)", w.Body.String(), err)
	}

	// 同 normKey 变体（绕过写入口直接入库）→ 422
	if _, err := store.DB().Exec(`INSERT INTO "Tag" ("name") VALUES ('过膝袜')`); err != nil {
		t.Fatalf("insert variant failed: %v", err)
	}
	simplifiedID := 0
	_ = store.DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '过膝袜'`).Scan(&simplifiedID)
	if w := performAuthedRequest(r, "POST", "/api/tags/vocabulary", map[string]interface{}{"add": []int{simplifiedID}}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("normKey conflict = %d %s, want 422", w.Code, w.Body.String())
	}

	// 移除 → 200 且列表收缩
	if w := performAuthedRequest(r, "POST", "/api/tags/vocabulary", map[string]interface{}{"remove": []int{juruID}}, cookie); w.Code != http.StatusOK {
		t.Fatalf("remove = %d, want 200", w.Code)
	}
	w = performAuthedRequest(r, "GET", "/api/tags/vocabulary", nil, cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.List) != 1 || resp.List[0].Name != "過膝襪" {
		t.Fatalf("list after remove = %s (err=%v)", w.Body.String(), err)
	}
}

// TestAISuggestTagMappingGuardsAndFlow AI 映射端点：守卫 + mock LLM 全流程（含繁简对齐）。
func TestAISuggestTagMappingGuardsAndFlow(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)
	t.Setenv("DATA_DIR", t.TempDir())

	// AI 未配置 → 422
	if w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-mapping", map[string]string{}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("AI not configured = %d, want 422", w.Code)
	}

	if err := service.SaveAIConfig(testAIConfig()); err != nil {
		t.Fatalf("SaveAIConfig failed: %v", err)
	}

	// 词表为空 → 422（提示先建词表）
	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"map-1", "map1.cbz", "Map 1", 1000},
		{"map-2", "map2.cbz", "Map 2", 2000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := store.AddTagsToComic("map-1", []string{"巨乳", "過膝襪"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := store.AddTagsToComic("map-2", []string{"黑白絲", "垃圾站名"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-mapping", map[string]string{}, cookie)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "词表") {
		t.Fatalf("empty vocab = %d %s, want 422 with 词表 hint", w.Code, w.Body.String())
	}

	// 构建词表
	juruID, guoxiID := 0, 0
	_ = store.DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '巨乳'`).Scan(&juruID)
	_ = store.DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '過膝襪'`).Scan(&guoxiID)
	if err := store.AddTagsToVocabulary([]int{juruID, guoxiID}); err != nil {
		t.Fatalf("AddTagsToVocabulary failed: %v", err)
	}

	// 假 LLM：AI 对「黑白絲」给简体目标「过膝袜」→ 应被 normKey 对齐回词表「過膝襪」
	aiReply := `[{"tag":"黑白絲","action":"map","target":"过膝袜","reason":"同义"},{"tag":"垃圾站名","action":"delete","reason":"站名"},{"tag":"巨乳","action":"map","target":"巨乳","reason":"词表项"}]`
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

	w = performAuthedRequest(r, "POST", "/api/ai/suggest-tag-mapping", map[string]string{}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("suggest-tag-mapping = %d %s, want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Analyzed  int `json:"analyzed"`
		VocabSize int `json:"vocabSize"`
		Mappings  []struct {
			Tag    string `json:"tag"`
			Action string `json:"action"`
			Target *struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"target"`
			Reason string `json:"reason"`
		} `json:"mappings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Analyzed != 2 || resp.VocabSize != 2 {
		t.Fatalf("analyzed=%d vocabSize=%d, want 2/2", resp.Analyzed, resp.VocabSize)
	}
	if len(resp.Mappings) != 2 {
		t.Fatalf("mappings = %#v, want 2 (vocab 自引用应被丢弃)", resp.Mappings)
	}
	m0 := resp.Mappings[0]
	if m0.Action != "map" || m0.Tag != "黑白絲" || m0.Target == nil || m0.Target.ID != guoxiID || m0.Target.Name != "過膝襪" {
		t.Fatalf("mappings[0] = %#v, want 黑白絲 -> 過膝襪(繁简对齐回填)", m0)
	}
	if resp.Mappings[1].Action != "delete" || resp.Mappings[1].Tag != "垃圾站名" {
		t.Fatalf("mappings[1] = %#v, want delete 垃圾站名", resp.Mappings[1])
	}
}
