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
// POST /api/ai/suggest-tag-mapping — AI 词表映射建议
//
// 词表驱动式标签重构：AI 为词表外的长尾标签给出 map/delete 建议，
// keep 隐式（不出现在响应中）。应用由前端走既有归并/删除端点
// （可撤销、有快照），本端点只读不改数据。
//
// 简繁处理：校验时 AI 的 target 按 TagNormKey（繁→简折叠）对齐
// 词表项——写法差异不构成"目标不存在"，展示名回填词表原文。
// ============================================================

// aiMappingMaxVariants 单次映射分析的最大长尾标签数。
const aiMappingMaxVariants = 3000

// aiMappingItem 校验后的映射建议条目。
type aiMappingItem struct {
	TagID  int            `json:"tagId"`
	Tag    string         `json:"tag"`
	Action string         `json:"action"` // map | delete
	Target *aiNormTagItem `json:"target,omitempty"`
	Reason string         `json:"reason,omitempty"`
}

// validateTagMapping 对 AI 映射建议做严格校验（纯函数，便于单测）：
//   - tag trim 后必须命中长尾候选集；未知名丢弃；同一标签先到先得；
//   - action=map：target 按 normKey 对齐词表项（繁简差异容错），
//     未命中词表 → 降级为不输出（该标签视为保留）；
//     目标与来源同 normKey（繁简变体并入规范写法）→ 合法且有用；
//   - action=delete：原样保留（应用端逐条删除，前端默认不勾选）；
//   - action 未知或 keep → 不输出。
func validateTagMapping(suggestions []service.TagMappingSuggestion, candidates, vocab []store.TagVariant) []aiMappingItem {
	byName := make(map[string]store.TagVariant, len(candidates))
	for _, t := range candidates {
		byName[t.Name] = t
	}
	vocabByNormKey := make(map[string]store.TagVariant, len(vocab))
	for _, v := range vocab {
		k := store.TagNormKey(v.Name)
		if _, ok := vocabByNormKey[k]; !ok {
			vocabByNormKey[k] = v
		}
	}

	seen := make(map[string]bool)
	items := []aiMappingItem{}
	for _, s := range suggestions {
		tagName := strings.TrimSpace(s.Tag)
		if tagName == "" || seen[tagName] {
			continue
		}
		tag, ok := byName[tagName]
		if !ok {
			continue // 未知标签名
		}

		action := strings.ToLower(strings.TrimSpace(s.Action))
		switch action {
		case "map":
			targetName := strings.TrimSpace(s.Target)
			v, ok := vocabByNormKey[store.TagNormKey(targetName)]
			if !ok {
				continue // 目标不在词表（含繁简对齐后仍未命中）→ 视为保留
			}
			seen[tagName] = true
			items = append(items, aiMappingItem{
				TagID:  tag.ID,
				Tag:    tag.Name,
				Action: "map",
				Target: &aiNormTagItem{ID: v.ID, Name: v.Name, ComicCount: v.ComicCount},
				Reason: strings.TrimSpace(s.Reason),
			})
		case "delete":
			seen[tagName] = true
			items = append(items, aiMappingItem{
				TagID:  tag.ID,
				Tag:    tag.Name,
				Action: "delete",
				Reason: strings.TrimSpace(s.Reason),
			})
		default:
			// keep / 未知动作 → 不输出
		}
	}
	return items
}

// SuggestTagMapping POST /api/ai/suggest-tag-mapping
func (h *AIHandler) SuggestTagMapping(c *gin.Context) {
	var body struct {
		TargetLang string `json:"targetLang"`
	}
	_ = c.ShouldBindJSON(&body)

	cfg := service.LoadAIConfig()
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "AI 未配置，请先在系统设置中启用云端 AI 并填写 API Key"})
		return
	}

	vocabItems, err := store.ListTagVocabulary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch vocabulary"})
		return
	}
	if len(vocabItems) == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "词表为空，请先在标签归一工作台的「词表」页签中构建目标词表"})
		return
	}

	tags, err := store.ListContentTagsWithCounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tags"})
		return
	}

	// 词表条目 → TagVariant；长尾 = 内容标签中不在词表的（按用量降序截取）
	vocabByID := make(map[int]store.TagVariant, len(vocabItems))
	for _, v := range vocabItems {
		vocabByID[v.TagID] = store.TagVariant{ID: v.TagID, Name: v.Name, ComicCount: v.ComicCount}
	}
	vocab := make([]store.TagVariant, 0, len(vocabItems))
	for _, v := range vocabItems {
		vocab = append(vocab, vocabByID[v.TagID])
	}
	inVocab := make(map[int]bool, len(vocabItems))
	for _, v := range vocabItems {
		inVocab[v.TagID] = true
	}
	variants := make([]store.TagVariant, 0, len(tags))
	for _, t := range tags {
		if !inVocab[t.ID] {
			variants = append(variants, t)
		}
	}
	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].ComicCount != variants[j].ComicCount {
			return variants[i].ComicCount > variants[j].ComicCount
		}
		return variants[i].ID < variants[j].ID
	})
	if len(variants) > aiMappingMaxVariants {
		variants = variants[:aiMappingMaxVariants]
	}

	toCandidates := func(vs []store.TagVariant) []service.TagNormCandidate {
		out := make([]service.TagNormCandidate, len(vs))
		for i, v := range vs {
			out[i] = service.TagNormCandidate{Name: v.Name, Count: v.ComicCount}
		}
		return out
	}

	suggestions, err := service.SuggestTagMapping(cfg, toCandidates(vocab), toCandidates(variants))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	mappings := validateTagMapping(suggestions, variants, vocab)

	c.JSON(http.StatusOK, gin.H{
		"analyzed":  len(variants),
		"vocabSize": len(vocab),
		"mappings":  mappings,
	})
}
