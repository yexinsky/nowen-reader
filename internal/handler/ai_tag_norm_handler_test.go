package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/service"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// TestValidateTagMergeGroups 校验纯函数：严格匹配、去重、组间不重叠。
func TestValidateTagMergeGroups(t *testing.T) {
	candidates := []store.TagVariant{
		{ID: 1, Name: "巨乳", ComicCount: 100},
		{ID: 2, Name: "besar", ComicCount: 30},
		{ID: 3, Name: "少女", ComicCount: 50},
		{ID: 4, Name: "SHOUJO", ComicCount: 5},
	}

	suggestions := []service.TagMergeSuggestion{
		// 有效：未知来源名丢弃
		{Target: "巨乳", Sources: []string{"besar", "不存在的标签"}},
		// 有效：来源带空白会 trim，target 重复出现在 sources 中剔除
		{Target: " 少女 ", Sources: []string{" SHOUJO ", "少女", "SHOUJO"}},
		// 组间重叠：少女已被上一组占用 → 整组丢弃
		{Target: "SHOUJO", Sources: []string{"少女"}},
		// target 未知 → 丢弃
		{Target: "不存在", Sources: []string{"besar"}},
		// 来源去重后为空 → 丢弃
		{Target: "巨乳", Sources: []string{"巨乳"}},
	}

	groups := validateTagMergeGroups(suggestions, candidates)
	if len(groups) != 2 {
		t.Fatalf("groups = %#v, want exactly 2", groups)
	}
	if groups[0].Target.Name != "巨乳" || groups[0].Target.ID != 1 || len(groups[0].Sources) != 1 || groups[0].Sources[0].Name != "besar" {
		t.Fatalf("group[0] = %#v", groups[0])
	}
	if groups[1].Target.Name != "少女" || len(groups[1].Sources) != 1 || groups[1].Sources[0].Name != "SHOUJO" {
		t.Fatalf("group[1] = %#v", groups[1])
	}
	if groups[0].Applied || groups[0].ComicCount != 0 || groups[0].Error != "" {
		t.Fatalf("group[0] should be unapplied by default: %#v", groups[0])
	}
}

// TestAISuggestTagMergesGuards 路由守卫：未登录 401、AI 未配置 422、无标签 200 空结果。
func TestAISuggestTagMergesGuards(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)

	// 隔离 DATA_DIR：让「AI 未配置」判定与本地开发机的 ai-config.json 无关
	t.Setenv("DATA_DIR", t.TempDir())

	if w := performRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{}); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d, want 401", w.Code)
	}

	if w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("AI not configured = %d %s, want 422", w.Code, w.Body.String())
	}

	if err := service.SaveAIConfig(testAIConfig()); err != nil {
		t.Fatalf("SaveAIConfig failed: %v", err)
	}

	// 无任何内容标签 → 200 空结果（不触发 LLM 调用）
	w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{"apply": true}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("zero tags = %d %s, want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Analyzed int              `json:"analyzed"`
		Groups   []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse zero-tags response failed: %v", err)
	}
	if resp.Analyzed != 0 || len(resp.Groups) != 0 {
		t.Fatalf("zero tags response = %#v, want empty", resp)
	}
}

// TestAISuggestTagMergesStrictValidationAndApply 用 httptest 假 LLM 验证
// 严格校验 + apply=true 落库（迁移关联、写别名、删源标签），无真实外呼。
func TestAISuggestTagMergesStrictValidationAndApply(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)
	t.Setenv("DATA_DIR", t.TempDir())

	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-norm-1", "tsnorm1.cbz", "TS Norm 1", 1000},
		{"ts-norm-2", "tsnorm2.cbz", "TS Norm 2", 2000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := store.AddTagsToComic("ts-norm-1", []string{"巨乳", "besar", "少女", "SHOUJO", "汉化"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	if err := store.AddTagsToComic("ts-norm-2", []string{"besar", "SHOUJO"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	// 预先忽略「汉化」簇 → 汉化不进候选，AI 提及也会被丢弃
	if err := store.IgnoreTagNormKey("汉化"); err != nil {
		t.Fatalf("IgnoreTagNormKey failed: %v", err)
	}

	// 假 LLM：
	// - 巨乳←besar+未知名+汉化(已忽略) → 有效组，仅 besar 入组
	// - 少女←SHOUJO → 有效组
	// - SHOUJO←少女（少女已被占用）→ 丢弃
	// - 未知 target → 丢弃
	// - 汉化（不在候选）→ 丢弃
	aiReply := `[` +
		`{"target":"巨乳","sources":["besar","不存在的标签","汉化"]},` +
		`{"target":"少女","sources":["SHOUJO"]},` +
		`{"target":"SHOUJO","sources":["少女"]},` +
		`{"target":"不存在","sources":["besar"]},` +
		`{"target":"汉化","sources":[]}]`
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

	w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{"apply": true}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("suggest-tag-merges = %d %s, want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Analyzed int `json:"analyzed"`
		Groups   []struct {
			Target struct {
				ID         int    `json:"id"`
				Name       string `json:"name"`
				ComicCount int    `json:"comicCount"`
			} `json:"target"`
			Sources []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"sources"`
			Applied    bool   `json:"applied"`
			ComicCount int    `json:"comicCount"`
			Error      string `json:"error"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}

	// 候选排除已忽略的汉化 → analyzed = 4
	if resp.Analyzed != 4 {
		t.Fatalf("analyzed = %d, want 4", resp.Analyzed)
	}
	if len(resp.Groups) != 2 {
		t.Fatalf("groups = %#v, want exactly 2", resp.Groups)
	}
	g0, g1 := resp.Groups[0], resp.Groups[1]
	if g0.Target.Name != "巨乳" || len(g0.Sources) != 1 || g0.Sources[0].Name != "besar" {
		t.Fatalf("group[0] = %#v", g0)
	}
	// comic1 已有巨乳，只有 comic2 实际新增关联 → moved = 1
	if !g0.Applied || g0.ComicCount != 1 || g0.Error != "" {
		t.Fatalf("group[0] apply result = %#v", g0)
	}
	if g1.Target.Name != "少女" || len(g1.Sources) != 1 || g1.Sources[0].Name != "SHOUJO" {
		t.Fatalf("group[1] = %#v", g1)
	}
	// comic1 本就带少女标签，只有 comic2 实际新增关联 → moved = 1
	if !g1.Applied || g1.ComicCount != 1 || g1.Error != "" {
		t.Fatalf("group[1] apply result = %#v", g1)
	}

	// 落库验证：源标签删除、别名写入、书目关联迁移
	var n int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM "Tag" WHERE "name" IN ('besar', 'SHOUJO')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("source tags should be deleted (n=%d, err=%v)", n, err)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM "TagAlias" WHERE "alias" = 'besar'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("alias besar missing (n=%d, err=%v)", n, err)
	}
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM "TagAlias" WHERE "alias" = 'SHOUJO'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("alias SHOUJO missing (n=%d, err=%v)", n, err)
	}
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM "ComicTag" ct JOIN "Tag" t ON t."id" = ct."tagId" WHERE ct."comicId" = 'ts-norm-2' AND t."name" = '巨乳'`,
	).Scan(&n); err != nil || n != 1 {
		t.Fatalf("comic2 should have 巨乳 (n=%d, err=%v)", n, err)
	}
	if err := store.DB().QueryRow(
		`SELECT COUNT(*) FROM "ComicTag" ct JOIN "Tag" t ON t."id" = ct."tagId" WHERE ct."comicId" = 'ts-norm-1' AND t."name" IN ('巨乳','少女','汉化')`,
	).Scan(&n); err != nil || n != 3 {
		t.Fatalf("comic1 tags after merge = %d, want 3 (err=%v)", n, err)
	}
}

// TestAISuggestTagMergesBatching 验证超过 250 个标签时分批调用 LLM 并合并结果。
func TestAISuggestTagMergesBatching(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)
	t.Setenv("DATA_DIR", t.TempDir())

	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-batch-1", "tsbatch1.cbz", "TS Batch 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	// 510 个标签 → 应分 3 批（250+250+10）
	names := make([]string, 510)
	for i := range names {
		names[i] = fmt.Sprintf("t%03d", i)
	}
	if err := store.AddTagsToComic("ts-batch-1", names); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		// 每批都返回同一建议：校验层应去重，最终只留一组
		aiReply := `[{"target":"t000","sources":["t001"]}]`
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + strings.ReplaceAll(aiReply, `"`, `\"`) + `"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	if err := service.SaveAIConfig(testAIConfigWithURL(server.URL + "/v1")); err != nil {
		t.Fatalf("SaveAIConfig failed: %v", err)
	}

	w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("suggest-tag-merges = %d %s, want 200", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("LLM calls = %d, want 3 (510 tags / batch 250)", got)
	}
	var resp struct {
		Analyzed int `json:"analyzed"`
		Groups   []struct {
			Target struct {
				Name string `json:"name"`
			} `json:"target"`
			Sources []struct {
				Name string `json:"name"`
			} `json:"sources"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Analyzed != 510 {
		t.Fatalf("analyzed = %d, want 510", resp.Analyzed)
	}
	if len(resp.Groups) != 1 || resp.Groups[0].Target.Name != "t000" || len(resp.Groups[0].Sources) != 1 || resp.Groups[0].Sources[0].Name != "t001" {
		t.Fatalf("groups = %#v, want exactly 1 (t000 <- t001)", resp.Groups)
	}
}

// TestAISuggestTagMergesBatchSizeConfig 验证 AI 设置里的 tagNormBatchSize 生效：
// 批大小 600 时 510 个标签应单批完成（1 次 LLM 调用）。
func TestAISuggestTagMergesBatchSizeConfig(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)
	t.Setenv("DATA_DIR", t.TempDir())

	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-bcfg-1", "tsbcfg1.cbz", "TS BCfg 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	names := make([]string, 510)
	for i := range names {
		names[i] = fmt.Sprintf("b%03d", i)
	}
	if err := store.AddTagsToComic("ts-bcfg-1", names); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		aiReply := `[{"target":"b000","sources":["b001"]}]`
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + strings.ReplaceAll(aiReply, `"`, `\"`) + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	cfg := testAIConfigWithURL(server.URL + "/v1")
	cfg.TagNormBatchSize = 600
	if err := service.SaveAIConfig(cfg); err != nil {
		t.Fatalf("SaveAIConfig failed: %v", err)
	}

	w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("suggest-tag-merges = %d %s, want 200", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("LLM calls = %d, want 1 (510 tags / batch 600)", got)
	}
	var resp struct {
		Analyzed int `json:"analyzed"`
		Groups   []struct {
			Target struct {
				Name string `json:"name"`
			} `json:"target"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}
	if resp.Analyzed != 510 || len(resp.Groups) != 1 || resp.Groups[0].Target.Name != "b000" {
		t.Fatalf("analyzed=%d groups=%#v, want 510 + 1 group (b000)", resp.Analyzed, resp.Groups)
	}
}

// TestAISuggestTagMergesTruncationError 输出被 max_tokens 截断时应报明确的「截断」错误，
// 而不是让半截 JSON 流入解析层。
func TestAISuggestTagMergesTruncationError(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)
	t.Setenv("DATA_DIR", t.TempDir())

	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-trunc-1", "tstrunc1.cbz", "TS Trunc 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := store.AddTagsToComic("ts-trunc-1", []string{"巨乳", "besar", "少女", "SHOUJO"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}

	// 半截 JSON：内层数组闭合但整体未结束（复现 "unexpected end of JSON input" 的形态）
	truncated := `[{"target":"巨乳","sources":["besar"]},{"target":"少女","sources":["SHOU`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + strings.ReplaceAll(truncated, `"`, `\"`) + `"},"finish_reason":"length"}],"usage":{"prompt_tokens":1,"completion_tokens":4096,"total_tokens":4097}}`))
	}))
	defer server.Close()

	if err := service.SaveAIConfig(testAIConfigWithURL(server.URL + "/v1")); err != nil {
		t.Fatalf("SaveAIConfig failed: %v", err)
	}

	w := performAuthedRequest(r, "POST", "/api/ai/suggest-tag-merges", map[string]bool{}, cookie)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("truncated response = %d %s, want 500", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "截断") {
		t.Fatalf("error should mention truncation: %s", w.Body.String())
	}
}
