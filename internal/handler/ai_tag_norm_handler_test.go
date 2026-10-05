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
