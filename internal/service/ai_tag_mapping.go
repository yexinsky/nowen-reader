package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// AI 词表映射（词表驱动式标签重构的核心步骤）：
//   词表 = 人工策展的规范目标集合（几十个常用标签）；
//   长尾 = 词表外的全部内容标签，分批送入。
//   AI 对每个长尾标签给出处置建议：
//     map    — 语义被词表某项覆盖（同义/近义/变体/子集），归入之
//     delete — 垃圾（水印、站名、无意义串）
//     keep   — 有独立价值，保留（隐式：不出现在响应里）
//
// 只做建议；应用由前端逐组走既有归并/删除端点（可撤销、有快照）。
// 简繁处理：匹配层按 TagNormKey（繁→简折叠）对齐，提示词要求
// target 逐字复制词表名，词表展示写法不受影响。
// ============================================================

// TagMappingSuggestion 单个长尾标签的处置建议（keep 隐式，不出现在响应中）。
type TagMappingSuggestion struct {
	Tag    string `json:"tag"`
	Action string `json:"action"` // map | delete
	Target string `json:"target"` // action=map 时：词表项逐字名
	Reason string `json:"reason"`
}

// SuggestTagMapping 分批为长尾标签生成映射建议。vocab 每批完整带入。
func SuggestTagMapping(cfg AIConfig, vocab, variants []TagNormCandidate) ([]TagMappingSuggestion, error) {
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		return nil, fmt.Errorf("cloud AI not configured")
	}
	if len(vocab) == 0 {
		return nil, fmt.Errorf("vocabulary is empty")
	}
	if len(variants) == 0 {
		return []TagMappingSuggestion{}, nil
	}

	batchSize := cfg.TagNormBatchSize
	if batchSize <= 0 {
		batchSize = defaultTagNormBatchSize
	}
	maxTokens := tagNormBatchMaxTokens(batchSize)

	total := (len(variants) + batchSize - 1) / batchSize
	var all []TagMappingSuggestion
	for i, start := 0, 0; start < len(variants); i, start = i+1, start+batchSize {
		end := start + batchSize
		if end > len(variants) {
			end = len(variants)
		}
		batch, err := suggestTagMappingBatch(cfg, vocab, variants[start:end], maxTokens)
		if err != nil {
			return nil, fmt.Errorf("第 %d/%d 批分析失败: %w", i+1, total, err)
		}
		all = append(all, batch...)
	}
	return all, nil
}

// suggestTagMappingBatch 单次 LLM 调用处理一批长尾标签。
func suggestTagMappingBatch(cfg AIConfig, vocab, variants []TagNormCandidate, maxTokens int) ([]TagMappingSuggestion, error) {
	systemPrompt := `You are a manga/comic/novel library tag taxonomy expert. The library is being rebuilt around a CURATED VOCABULARY of canonical tags. You get the vocabulary plus a batch of leftover tags. For each leftover tag decide one of:

- "map": its meaning is covered by a vocabulary tag (exact synonym, near-synonym, translation/romanization variant, or a narrower variant of it) → set "target" to that vocabulary tag VERBATIM
- "delete": junk or noise (watermarks, site names, spam, meaningless strings) → no target
- (tags not mentioned in your response are kept as-is — do NOT output "keep" entries)

Rules:
- Copy leftover tag names VERBATIM; "target" must be copied VERBATIM from the vocabulary list — never modify, translate or invent names
- Tags may be Traditional or Simplified Chinese — treat the two scripts as the same writing when comparing meanings
- map is for meaning overlap; a distinctive niche concept (e.g. "NTR", "阿黑顏") must be kept, NOT mapped to a broader tag
- When unsure between map and keep, prefer keep
- delete only obvious junk; never delete meaningful tags
- "reason": short phrase (≤15 chars) in the tag's language
- Return ONLY a JSON array, no extra text: [{"tag":"<leftover>","action":"map","target":"<vocab tag>","reason":"..."}]
- If nothing should be mapped or deleted, return []`

	var vocabLines, variantLines strings.Builder
	for _, c := range vocab {
		fmt.Fprintf(&vocabLines, "%s #%d\n", c.Name, c.Count)
	}
	for _, c := range variants {
		fmt.Fprintf(&variantLines, "%s #%d\n", c.Name, c.Count)
	}
	userPrompt := fmt.Sprintf("Curated vocabulary (canonical targets):\n%s\nLeftover tags to decide (name #comicCount):\n%s\nReturn the JSON array of map/delete decisions.",
		vocabLines.String(), variantLines.String())

	content, err := CallCloudLLM(cfg, systemPrompt, userPrompt, &LLMCallOptions{
		Scenario:         "tag_mapping",
		MaxTokens:        maxTokens,
		StrictTruncation: true,
	})
	if err != nil {
		return nil, err
	}

	content = strings.ReplaceAll(content, "```json", "")
	content = strings.ReplaceAll(content, "```", "")
	content = strings.TrimSpace(content)
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("failed to parse AI tag mapping suggestion: no JSON array in response")
	}
	content = content[start : end+1]

	var suggestions []TagMappingSuggestion
	if err := json.Unmarshal([]byte(content), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse AI tag mapping suggestion: %w", err)
	}
	return suggestions, nil
}
