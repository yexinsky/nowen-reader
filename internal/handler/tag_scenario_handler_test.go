package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/nowen-reader/nowen-reader/internal/middleware"
	"github.com/nowen-reader/nowen-reader/internal/service"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

func TestTagScenarioEndpointParamValidation(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)

	// 未登录 → 401
	if w := performRequest(r, "GET", "/api/tags/scenarios", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401", w.Code)
	}

	// 创建：name 必填（trim 后非空 ≤50 rune），否则 422
	if w := performAuthedRequest(r, "POST", "/api/tags/scenarios", map[string]string{"name": "   "}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create blank name = %d, want 422 (%s)", w.Code, w.Body.String())
	}
	if w := performAuthedRequest(r, "POST", "/api/tags/scenarios", map[string]string{"name": strings.Repeat("标", 51)}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create 51-rune name = %d, want 422", w.Code)
	}

	// 创建成功 → {"ok":true,"id":N}
	w := performAuthedRequest(r, "POST", "/api/tags/scenarios", map[string]string{"name": "剧情", "color": "#6366f1"}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("create = %d %s, want 200", w.Code, w.Body.String())
	}
	var created struct {
		OK bool `json:"ok"`
		ID int  `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || !created.OK || created.ID <= 0 {
		t.Fatalf("create response = %s (err=%v)", w.Body.String(), err)
	}

	// 重名 → 422
	if w := performAuthedRequest(r, "POST", "/api/tags/scenarios", map[string]string{"name": "剧情"}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate create = %d, want 422", w.Code)
	}

	// PUT 部分更新：只改 color；非法名称 → 422；不存在 → 404
	if w := performAuthedRequest(r, "PUT", "/api/tags/scenarios/"+itoa(created.ID), map[string]string{"color": "#ff0000"}, cookie); w.Code != http.StatusOK {
		t.Fatalf("update color = %d %s, want 200", w.Code, w.Body.String())
	}
	if w := performAuthedRequest(r, "PUT", "/api/tags/scenarios/"+itoa(created.ID), map[string]string{"name": "  "}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("update blank name = %d, want 422", w.Code)
	}
	if w := performAuthedRequest(r, "PUT", "/api/tags/scenarios/9999", map[string]int{"sortOrder": 3}, cookie); w.Code != http.StatusNotFound {
		t.Fatalf("update missing = %d, want 404", w.Code)
	}

	// GET list：list + unassigned 结构
	w = performAuthedRequest(r, "GET", "/api/tags/scenarios", nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", w.Code)
	}
	var listResp struct {
		List []struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			Color     string `json:"color"`
			SortOrder int    `json:"sortOrder"`
			Tags      []struct {
				ID         int    `json:"id"`
				Name       string `json:"name"`
				ComicCount int    `json:"comicCount"`
			} `json:"tags"`
		} `json:"list"`
		Unassigned struct {
			Tags []struct {
				ID         int    `json:"id"`
				Name       string `json:"name"`
				ComicCount int    `json:"comicCount"`
			} `json:"tags"`
		} `json:"unassigned"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("parse list response failed: %v", err)
	}
	if len(listResp.List) != 1 || listResp.List[0].Name != "剧情" || listResp.List[0].Color != "#ff0000" || listResp.List[0].Tags == nil {
		t.Fatalf("unexpected list: %#v", listResp.List)
	}
	if listResp.Unassigned.Tags == nil {
		t.Fatalf("unassigned.tags should be non-nil array: %#v", listResp.Unassigned)
	}

	// assign：tagIds 必填（400）；不存在 tagId/scenarioId（404）
	if w := performAuthedRequest(r, "POST", "/api/tags/scenarios/assign", map[string][]int{"tagIds": {}}, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("assign empty tagIds = %d, want 400", w.Code)
	}
	if w := performAuthedRequest(r, "POST", "/api/tags/scenarios/assign", map[string]interface{}{"tagIds": []int{9999}, "scenarioId": created.ID}, cookie); w.Code != http.StatusNotFound {
		t.Fatalf("assign missing tag = %d, want 404", w.Code)
	}
	if w := performAuthedRequest(r, "POST", "/api/tags/scenarios/assign", map[string]interface{}{"tagIds": []int{1}, "scenarioId": 9999}, cookie); w.Code != http.StatusNotFound {
		t.Fatalf("assign missing scenario = %d, want 404", w.Code)
	}

	// DELETE：不存在 → 404；存在 → 200
	if w := performAuthedRequest(r, "DELETE", "/api/tags/scenarios/9999", nil, cookie); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", w.Code)
	}
	if w := performAuthedRequest(r, "DELETE", "/api/tags/scenarios/"+itoa(created.ID), nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200", w.Code)
	}
	if w := performAuthedRequest(r, "GET", "/api/tags/scenarios", nil, cookie); w.Code != http.StatusOK {
		t.Fatalf("list after delete = %d, want 200", w.Code)
	}
}

func TestAIAssignTagScenariosGuards(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)

	// 隔离 DATA_DIR：让「AI 未配置」判定与本地开发机的 ai-config.json 无关
	t.Setenv("DATA_DIR", t.TempDir())

	// 未登录 → 401
	if w := performRequest(r, "POST", "/api/ai/assign-tag-scenarios", map[string]bool{}); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated AI assign = %d, want 401", w.Code)
	}

	// 未配置 AI → 422
	if w := performAuthedRequest(r, "POST", "/api/ai/assign-tag-scenarios", map[string]bool{}, cookie); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("AI not configured = %d %s, want 422", w.Code, w.Body.String())
	}

	// 配置 AI（指向不可达地址即可——被测分支都不会真的发起 LLM 调用）
	if err := service.SaveAIConfig(testAIConfig()); err != nil {
		t.Fatalf("SaveAIConfig failed: %v", err)
	}

	// 无任何情景 → 422「请先创建情景」
	w := performAuthedRequest(r, "POST", "/api/ai/assign-tag-scenarios", map[string]bool{}, cookie)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "请先创建情景") {
		t.Fatalf("no scenarios = %d %s, want 422 with 请先创建情景", w.Code, w.Body.String())
	}

	// 有情景、无标签 → 200 空结果（不触发 LLM 调用）
	if _, err := store.CreateTagScenario("剧情", ""); err != nil {
		t.Fatalf("CreateTagScenario failed: %v", err)
	}
	w = performAuthedRequest(r, "POST", "/api/ai/assign-tag-scenarios", map[string]bool{}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("zero tags = %d %s, want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Assignments []map[string]interface{} `json:"assignments"`
		Skipped     []string                 `json:"skipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse zero-tags response failed: %v", err)
	}
	if len(resp.Assignments) != 0 || len(resp.Skipped) != 0 {
		t.Fatalf("zero tags response = %#v, want empty", resp)
	}

	// 非管理员且未开 AI → 403（AIRequired 门控）
	regBody := map[string]string{"username": "noai", "password": "password123", "nickname": "No AI"}
	if w := performRequest(r, "POST", "/api/auth/register", regBody); w.Code != http.StatusOK {
		t.Fatalf("register second user failed: %d %s", w.Code, w.Body.String())
	}
	w2 := performRequest(r, "POST", "/api/auth/login", regBody)
	userCookie := ""
	for _, c := range w2.Result().Cookies() {
		if c.Name == middleware.SessionCookie {
			userCookie = c.Value
		}
	}
	if userCookie == "" {
		t.Fatalf("no session cookie after second login: %d %s", w2.Code, w2.Body.String())
	}
	if w := performAuthedRequest(r, "POST", "/api/ai/assign-tag-scenarios", map[string]bool{}, userCookie); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin AI assign = %d, want 403", w.Code)
	}
}

// testAIConfig 返回启用云端 AI 的配置（测试用，指向不可达地址避免真实外呼）。
func testAIConfig() service.AIConfig {
	return service.AIConfig{
		EnableCloudAI: true,
		CloudProvider: "compatible",
		CloudAPIKey:   "test-key",
		CloudAPIURL:   "http://127.0.0.1:1/v1", // 不可达端口；被测分支不发起请求
		CloudModel:    "test-model",
		MaxTokens:     2000,
		MaxRetries:    0,
	}
}

// itoa 简单 int → string（测试用）。
func itoa(v int) string {
	return strconv.Itoa(v)
}

// TestAIAssignTagScenariosStrictMatching 用 httptest 假 LLM 服务验证
// AI 分配的严格匹配与写库行为（仓库既有 AI 测试同款 mock 方式，无真实外呼）。
func TestAIAssignTagScenariosStrictMatching(t *testing.T) {
	r := setupTestRouter(t)
	cookie := registerAndLogin(t, r)
	t.Setenv("DATA_DIR", t.TempDir())

	// 情景 + 标签数据
	toolID, err := store.CreateTagScenario("工具", "")
	if err != nil {
		t.Fatalf("CreateTagScenario failed: %v", err)
	}
	if err := store.BulkCreateComics([]struct {
		ID       string
		Filename string
		Title    string
		FileSize int64
	}{
		{"ts-ai-1", "tsai1.cbz", "TS AI 1", 1000},
	}); err != nil {
		t.Fatalf("BulkCreateComics failed: %v", err)
	}
	if err := store.AddTagsToComic("ts-ai-1", []string{"汉化", "巨乳", "BBD"}); err != nil {
		t.Fatalf("AddTagsToComic failed: %v", err)
	}
	hanhuaID := 0
	if err := store.DB().QueryRow(`SELECT "id" FROM "Tag" WHERE "name" = '汉化'`).Scan(&hanhuaID); err != nil {
		t.Fatalf("tag 汉化 missing: %v", err)
	}

	// 假 LLM：AI 返回中
	// - 汉化→工具（有效，应写入）
	// - 巨乳→不存在的情景（严格匹配失败 → skipped）
	// - BBD→空情景（AI 跳过 → skipped）
	// - 未知标签名（忽略）
	aiReply := `[{"tag":"汉化","scenario":"工具"},{"tag":"巨乳","scenario":"不存在情景"},{"tag":"BBD","scenario":""},{"tag":"未知标签","scenario":"工具"}]`
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

	w := performAuthedRequest(r, "POST", "/api/ai/assign-tag-scenarios", map[string]interface{}{"onlyUnassigned": true}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("AI assign = %d %s, want 200", w.Code, w.Body.String())
	}
	var resp struct {
		Assignments []struct {
			TagID        int    `json:"tagId"`
			TagName      string `json:"tagName"`
			ScenarioID   int    `json:"scenarioId"`
			ScenarioName string `json:"scenarioName"`
		} `json:"assignments"`
		Skipped []string `json:"skipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response failed: %v", err)
	}

	// assignments 只含实际写入的汉化→工具
	if len(resp.Assignments) != 1 {
		t.Fatalf("assignments = %#v, want exactly 1", resp.Assignments)
	}
	a := resp.Assignments[0]
	if a.TagID != hanhuaID || a.TagName != "汉化" || a.ScenarioID != toolID || a.ScenarioName != "工具" {
		t.Fatalf("unexpected assignment: %#v", a)
	}

	// skipped：未写入的标签按输入顺序（name ASC）返回
	if len(resp.Skipped) != 2 || resp.Skipped[0] != "BBD" || resp.Skipped[1] != "巨乳" {
		t.Fatalf("skipped = %#v, want [BBD 巨乳]", resp.Skipped)
	}

	// 写库验证：汉化挂到工具，其余保持未分配
	scenarios, unassigned, err := store.ListTagScenariosWithTags()
	if err != nil {
		t.Fatalf("ListTagScenariosWithTags failed: %v", err)
	}
	if len(scenarios) != 1 || len(scenarios[0].Tags) != 1 || scenarios[0].Tags[0].Name != "汉化" {
		t.Fatalf("scenario groups after AI assign = %#v", scenarios)
	}
	if len(unassigned) != 2 {
		t.Fatalf("unassigned after AI assign = %#v, want 2 tags", unassigned)
	}
}

// testAIConfigWithURL 返回指向 mock 服务的启用云端 AI 配置。
func testAIConfigWithURL(apiURL string) service.AIConfig {
	cfg := testAIConfig()
	cfg.CloudAPIURL = apiURL
	return cfg
}
