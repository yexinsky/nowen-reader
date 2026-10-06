package handler

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nowen-reader/nowen-reader/internal/service"
	"github.com/nowen-reader/nowen-reader/internal/store"
)

// ============================================================
// POST /api/ai/suggest-tag-merges — AI 标签归一建议
//
// 把内容标签清单交给 LLM 找出「同一概念的不同写法」分组；
// 返回前对建议做严格校验（名称逐字匹配既有标签、组间不重叠），
// apply=true 时逐组走 store.ApplyTagMerge 落库（迁移关联、写别名、
// 记操作日志，可在归一工作台撤销）。
// ============================================================

// aiNormMaxCandidates 单次交给 AI 分析的最大标签数（按用量降序截取）。
const aiNormMaxCandidates = 800

// aiNormTagItem 响应中的标签条目。
type aiNormTagItem struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	ComicCount int    `json:"comicCount"`
}

// aiNormGroupItem 一组归并建议（已严格校验、映射到真实标签）。
type aiNormGroupItem struct {
	Target     aiNormTagItem   `json:"target"`
	Sources    []aiNormTagItem `json:"sources"`
	Applied    bool            `json:"applied"`
	ComicCount int             `json:"comicCount"`
	Error      string          `json:"error,omitempty"`
}

// validateTagMergeGroups 对 AI 建议做严格校验（纯函数，便于单测）：
//   - 名称 trim 后必须命中候选清单；未知来源名直接丢弃，不整组否决；
//   - target 未知或已被其他组占用 → 整组丢弃；
//   - 来源与 target 相同、来源重复去重；去重后来源为空 → 丢弃；
//   - 任一来源已被其他组占用 → 整组丢弃（保证组间互不重叠，落库顺序无关）。
func validateTagMergeGroups(suggestions []service.TagMergeSuggestion, candidates []store.TagVariant) []aiNormGroupItem {
	byName := make(map[string]store.TagVariant, len(candidates))
	for _, t := range candidates {
		byName[t.Name] = t
	}

	used := make(map[string]bool)
	groups := []aiNormGroupItem{}
	for _, s := range suggestions {
		targetName := strings.TrimSpace(s.Target)
		target, ok := byName[targetName]
		if !ok || used[targetName] {
			continue
		}

		seen := make(map[string]bool)
		sources := []aiNormTagItem{}
		conflict := false
		for _, raw := range s.Sources {
			name := strings.TrimSpace(raw)
			if name == targetName || seen[name] {
				continue
			}
			seen[name] = true
			v, ok := byName[name]
			if !ok {
				continue
			}
			if used[name] {
				conflict = true
				break
			}
			sources = append(sources, aiNormTagItem{ID: v.ID, Name: v.Name, ComicCount: v.ComicCount})
		}
		if conflict || len(sources) == 0 {
			continue
		}

		used[targetName] = true
		for name := range seen {
			if _, ok := byName[name]; ok {
				used[name] = true
			}
		}
		groups = append(groups, aiNormGroupItem{
			Target:  aiNormTagItem{ID: target.ID, Name: target.Name, ComicCount: target.ComicCount},
			Sources: sources,
		})
	}
	return groups
}

// SuggestTagMerges POST /api/ai/suggest-tag-merges
// body: { apply?: bool }。
func (h *AIHandler) SuggestTagMerges(c *gin.Context) {
	var body struct {
		Apply bool `json:"apply"`
	}
	_ = c.ShouldBindJSON(&body)

	cfg := service.LoadAIConfig()
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "AI 未配置，请先在系统设置中启用云端 AI 并填写 API Key"})
		return
	}

	tags, err := store.ListContentTagsWithCounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags"})
		return
	}
	if len(tags) == 0 {
		c.JSON(http.StatusOK, gin.H{"analyzed": 0, "groups": []aiNormGroupItem{}})
		return
	}

	// 排除已忽略 normKey 的标签（用户在归一工作台明确忽略过的簇不再打扰）
	ignored := make(map[string]bool)
	if ignoreRows, err := store.ListTagNormIgnores(); err == nil {
		for _, item := range ignoreRows {
			ignored[item.NormKey] = true
		}
	}
	candidates := make([]store.TagVariant, 0, len(tags))
	for _, t := range tags {
		if ignored[store.TagNormKey(t.Name)] {
			continue
		}
		candidates = append(candidates, t)
	}

	// 按用量降序截取（同量按 id 升序，保证确定性）
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ComicCount != candidates[j].ComicCount {
			return candidates[i].ComicCount > candidates[j].ComicCount
		}
		return candidates[i].ID < candidates[j].ID
	})
	if len(candidates) > aiNormMaxCandidates {
		candidates = candidates[:aiNormMaxCandidates]
	}

	aiCandidates := make([]service.TagNormCandidate, len(candidates))
	for i, t := range candidates {
		aiCandidates[i] = service.TagNormCandidate{Name: t.Name, Count: t.ComicCount}
	}
	suggestions, err := service.SuggestTagMerges(cfg, aiCandidates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	groups := validateTagMergeGroups(suggestions, candidates)

	if body.Apply && len(groups) > 0 {
		// 将要合并标签 → 先落一份可恢复的域快照（失败则拒绝执行）
		if err := autoSnapshotForAI("AI 归并应用前"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "自动快照失败，已取消本次 AI 写操作: " + err.Error()})
			return
		}
		for i := range groups {
			sourceIDs := make([]int, len(groups[i].Sources))
			for j, s := range groups[i].Sources {
				sourceIDs[j] = s.ID
			}
			moved, err := store.ApplyTagMerge(groups[i].Target.ID, sourceIDs)
			if err != nil {
				groups[i].Error = err.Error()
				continue
			}
			groups[i].Applied = true
			groups[i].ComicCount = moved
		}
	}

	c.JSON(http.StatusOK, gin.H{"analyzed": len(candidates), "groups": groups})
}
