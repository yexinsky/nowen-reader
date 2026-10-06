package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/service"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// TagScenarioHandler handles tag scenario API endpoints (标签情景).
type TagScenarioHandler struct{}

// NewTagScenarioHandler creates a new TagScenarioHandler.
func NewTagScenarioHandler() *TagScenarioHandler {
	return &TagScenarioHandler{}
}

// writeTagScenarioError 将 store 哨兵错误映射为 HTTP 响应（422/404/500）。
// 返回 true 表示已写入响应。
func writeTagScenarioError(c *gin.Context, err error, action string) bool {
	switch {
	case errors.Is(err, store.ErrTagScenarioNameInvalid):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "情景名称需为 1~50 个字符"})
	case errors.Is(err, store.ErrTagScenarioNameConflict):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "情景名称已存在"})
	case errors.Is(err, store.ErrTagScenarioNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Scenario not found"})
	case errors.Is(err, store.ErrTagNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": action})
	}
	return true
}

// GET /api/tags/scenarios — 按情景分组的标签清单 + 未分配标签
func (h *TagScenarioHandler) List(c *gin.Context) {
	groups, unassigned, err := store.ListTagScenariosWithTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tag scenarios"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"list":       groups,
		"unassigned": gin.H{"tags": unassigned},
	})
}

// POST /api/tags/scenarios — 新建情景
func (h *TagScenarioHandler) Create(c *gin.Context) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	id, err := store.CreateTagScenario(body.Name, body.Color)
	if err != nil {
		writeTagScenarioError(c, err, "Failed to create tag scenario")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

// PUT /api/tags/scenarios/:id — 编辑情景（字段缺省不改）
func (h *TagScenarioHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scenario id"})
		return
	}

	var body struct {
		Name      *string `json:"name"`
		Color     *string `json:"color"`
		SortOrder *int    `json:"sortOrder"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := store.UpdateTagScenario(id, body.Name, body.Color, body.SortOrder); err != nil {
		writeTagScenarioError(c, err, "Failed to update tag scenario")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DELETE /api/tags/scenarios/:id — 删除情景（标签回到未分配）
func (h *TagScenarioHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scenario id"})
		return
	}

	if err := store.DeleteTagScenario(id); err != nil {
		writeTagScenarioError(c, err, "Failed to delete tag scenario")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// POST /api/tags/scenarios/assign — 批量设置标签情景（scenarioId=null 移出情景）
func (h *TagScenarioHandler) Assign(c *gin.Context) {
	var body struct {
		TagIDs     []int `json:"tagIds"`
		ScenarioID *int  `json:"scenarioId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.TagIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tagIds required"})
		return
	}

	assigned, err := store.AssignTagScenario(body.TagIDs, body.ScenarioID)
	if err != nil {
		writeTagScenarioError(c, err, "Failed to assign tag scenario")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "assigned": assigned})
}

// ============================================================
// POST /api/ai/assign-tag-scenarios — AI 批量分配标签情景
// ============================================================

// AssignTagScenarios 由 AI 为标签挑选既有情景并直接写入 Tag.scenarioId。
// 只允许已有情景（名字严格相等才采用）；分配低风险可逆。
func (h *AIHandler) AssignTagScenarios(c *gin.Context) {
	var body struct {
		OnlyUnassigned *bool `json:"onlyUnassigned"`
	}
	_ = c.ShouldBindJSON(&body) // body 可省略，缺省只处理未分配标签
	onlyUnassigned := true
	if body.OnlyUnassigned != nil {
		onlyUnassigned = *body.OnlyUnassigned
	}

	cfg := service.LoadAIConfig()
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "AI 未配置，请先在系统设置中启用云端 AI 并填写 API Key"})
		return
	}

	scenarios, err := store.ListTagScenarios()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tag scenarios"})
		return
	}
	if len(scenarios) == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "请先创建情景"})
		return
	}

	tags, err := store.ListTagsWithScenarioState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags"})
		return
	}
	if onlyUnassigned {
		filtered := tags[:0]
		for _, t := range tags {
			if t.ScenarioID == 0 {
				filtered = append(filtered, t)
			}
		}
		tags = filtered
	}
	if len(tags) == 0 {
		c.JSON(http.StatusOK, gin.H{"assignments": []gin.H{}, "skipped": []string{}})
		return
	}

	scenarioNames := make([]string, len(scenarios))
	scenarioByName := make(map[string]store.TagScenario, len(scenarios))
	scenarioNameByID := make(map[int]string, len(scenarios))
	for i, s := range scenarios {
		scenarioNames[i] = s.Name
		scenarioByName[s.Name] = s
		scenarioNameByID[s.ID] = s.Name
	}
	tagNames := make([]string, len(tags))
	tagByName := make(map[string]store.TagScenarioState, len(tags))
	tagNameByID := make(map[int]string, len(tags))
	for i, t := range tags {
		tagNames[i] = t.Name
		tagByName[t.Name] = t
		tagNameByID[t.ID] = t.Name
	}

	suggestions, err := service.SuggestTagScenarios(cfg, tagNames, scenarioNames)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 严格匹配：tag/scenario 名字完全相等才采用；逐情景批量写库
	pending := make(map[int][]int) // scenarioId → tagIds
	for _, s := range suggestions {
		tag, ok := tagByName[strings.TrimSpace(s.Tag)]
		if !ok {
			continue // 未知标签名
		}
		name := strings.TrimSpace(s.Scenario)
		if name == "" {
			continue // AI 建议跳过
		}
		scenario, ok := scenarioByName[name]
		if !ok || tag.ScenarioID == scenario.ID {
			continue // 未知情景名（严格匹配）或与现值一致
		}
		pending[scenario.ID] = append(pending[scenario.ID], tag.ID)
	}

	// 将要写 Tag.scenarioId → 先落一份可恢复的域快照（失败则拒绝执行）
	if len(pending) > 0 {
		if err := autoSnapshotForAI("AI 情景分配前"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "自动快照失败，已取消本次 AI 写操作: " + err.Error()})
			return
		}
	}

	assignments := []gin.H{}
	assignedTagIDs := make(map[int]bool)
	for scenarioID, ids := range pending {
		if _, err := store.AssignTagScenario(ids, &scenarioID); err != nil {
			continue // 单组写库失败不阻断其余分配
		}
		for _, id := range ids {
			assignedTagIDs[id] = true
			assignments = append(assignments, gin.H{
				"tagId":        id,
				"tagName":      tagNameByID[id],
				"scenarioId":   scenarioID,
				"scenarioName": scenarioNameByID[scenarioID],
			})
		}
	}

	// 未写入的标签（AI 跳过/名字模糊不匹配）进 skipped
	skipped := []string{}
	for _, t := range tags {
		if !assignedTagIDs[t.ID] {
			skipped = append(skipped, t.Name)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"assignments": assignments,
		"skipped":     skipped,
	})
}
