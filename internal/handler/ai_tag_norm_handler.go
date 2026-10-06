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
// DeepSeek V4 等大上下文模型下，2000 可覆盖数千标签的库；
// 超出部分等后续轮次收敛（每轮合并后重新排序切分）。
const aiNormMaxCandidates = 2000

// aiNormTargetPoolSize 规范池大小：用量降序头部的这些标签作为优先合并目标；
// 候选不足时取一半，保证留有变体。由 service 提示词完整带入每个变体批。
const aiNormTargetPoolSize = 150

// aiNormTagItem 响应中的标签条目。
type aiNormTagItem struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	ComicCount int    `json:"comicCount"`
}

// aiNormGroupItem 一组归并建议（已严格校验、映射到真实标签）。
// NewTarget=true 表示目标不在既有标签中，应用时按名称新建。
type aiNormGroupItem struct {
	Target     aiNormTagItem   `json:"target"`
	Sources    []aiNormTagItem `json:"sources"`
	NewTarget  bool            `json:"newTarget"`
	Applied    bool            `json:"applied"`
	ComicCount int             `json:"comicCount"`
	Error      string          `json:"error,omitempty"`
}

// validateTagMergeGroups 对 AI 建议做严格校验（纯函数，便于单测）：
//   - 名称 trim 后必须命中候选清单；未知来源名直接丢弃，不整组否决；
//   - target 未知或已被其他组占用 → 整组丢弃；
//   - New 组：目标允许不在候选清单中，但不得与任何候选同 normKey
//     （同键差异应由既有标签/机械预览处理），来源与目标同 normKey 的剔除；
//   - 来源与 target 相同、来源重复去重；去重后来源为空 → 丢弃；
//   - 任一来源已被其他组占用 → 整组丢弃（保证组间互不重叠，落库顺序无关）。
func validateTagMergeGroups(suggestions []service.TagMergeSuggestion, candidates []store.TagVariant) []aiNormGroupItem {
	byName := make(map[string]store.TagVariant, len(candidates))
	normKeys := make(map[string]string, len(candidates)) // normKey → 首个候选名
	for _, t := range candidates {
		byName[t.Name] = t
		k := store.TagNormKey(t.Name)
		if _, ok := normKeys[k]; !ok {
			normKeys[k] = t.Name
		}
	}

	used := make(map[string]bool)
	newUsed := make(map[string]bool) // 已采用的新建目标名
	groups := []aiNormGroupItem{}
	for _, s := range suggestions {
		targetName := strings.TrimSpace(s.Target)
		target, targetExists := byName[targetName]
		isNew := s.New && !targetExists
		if !targetExists && !s.New {
			continue // target 未知且未声明新建
		}
		if targetExists && used[targetName] {
			continue
		}
		if isNew && newUsed[targetName] {
			continue
		}
		newKey := store.TagNormKey(targetName)
		if isNew && normKeys[newKey] != "" {
			// 声明新建却与既有候选同键（仅繁简/大小写差异）→ 交由既有标签处理，丢弃
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
			if isNew && store.TagNormKey(name) == newKey {
				continue // 同键来源并入新建目标会空转，剔除
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

		item := aiNormGroupItem{Sources: sources}
		if isNew {
			newUsed[targetName] = true
			item.NewTarget = true
			item.Target = aiNormTagItem{Name: targetName}
		} else {
			used[targetName] = true
			item.Target = aiNormTagItem{ID: target.ID, Name: target.Name, ComicCount: target.ComicCount}
		}
		for name := range seen {
			if _, ok := byName[name]; ok {
				used[name] = true
			}
		}
		groups = append(groups, item)
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

	// 两段式切分：用量降序头部为规范池（优先目标），其余为待归一变体
	poolSize := len(candidates) / 2
	if poolSize > aiNormTargetPoolSize {
		poolSize = aiNormTargetPoolSize
	}
	pool := aiCandidates[:poolSize]
	variants := aiCandidates[poolSize:]

	suggestions, err := service.SuggestTagMerges(cfg, pool, variants)
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
			targetID := groups[i].Target.ID
			if groups[i].NewTarget {
				id, _, err := store.ResolveOrCreateCanonicalTag(groups[i].Target.Name)
				if err != nil {
					groups[i].Error = err.Error()
					continue
				}
				targetID = id
			}
			sourceIDs := make([]int, 0, len(groups[i].Sources))
			for _, s := range groups[i].Sources {
				if s.ID == targetID {
					continue // 新建目标解析可能命中既有/别名标签而与来源重合
				}
				sourceIDs = append(sourceIDs, s.ID)
			}
			if len(sourceIDs) == 0 {
				groups[i].Error = "合并后无有效来源标签"
				continue
			}
			moved, err := store.ApplyTagMerge(targetID, sourceIDs)
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
