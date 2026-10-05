package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// AI 标签归一：把内容标签清单（名称+用量）交给 LLM，
// 找出「同一概念的不同写法」（译名、罗马音、繁简体、空格/连字符差异等）
// 并给出归并分组（规范目标 + 待并入变体）。只做建议；
// 是否落库由调用方严格校验后决定（复用归一工作台的合并逻辑）。
// ============================================================

// TagNormCandidate 参与归一分析的标签候选。
type TagNormCandidate struct {
	Name  string
	Count int
}

// TagMergeSuggestion 一组归并建议：Target 为规范标签，Sources 为待并入变体。
// 名称均应从候选清单逐字复制。
type TagMergeSuggestion struct {
	Target  string   `json:"target"`
	Sources []string `json:"sources"`
}

// SuggestTagMerges 单次 LLM 调用，从标签清单中找出同义变体分组。
// 调用方负责严格匹配：返回的名称必须与既有标签名完全相等才采用。
func SuggestTagMerges(cfg AIConfig, candidates []TagNormCandidate) ([]TagMergeSuggestion, error) {
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		return nil, fmt.Errorf("cloud AI not configured")
	}
	if len(candidates) == 0 {
		return []TagMergeSuggestion{}, nil
	}

	systemPrompt := `You are a manga/comic/novel library tag taxonomy expert. You will get content tags with usage counts. Find groups of tags that denote EXACTLY THE SAME concept: different spellings, translations (e.g. English / Japanese / Chinese), romanizations, abbreviations, Traditional/Simplified Chinese variants, extra/missing spaces, hyphens or punctuation.

Rules:
- Merge ONLY exact synonyms. Tags with related but DIFFERENT meanings (e.g. "少女" vs "萝莉", "swimsuit" vs "bikini") must NOT be merged
- Choose the canonical "target" per group: the variant with the highest usage count; when counts are close, prefer the clearest Chinese name if present
- Copy tag names VERBATIM from the provided list — never invent, translate or modify names
- Each tag may appear in at most one group; a tag must never be both target and source
- Return ONLY a JSON array, no extra text: [{"target":"<canonical>","sources":["<variant>",...]}]
- If nothing is a duplicate, return []`

	var lines strings.Builder
	for _, c := range candidates {
		fmt.Fprintf(&lines, "%s #%d\n", c.Name, c.Count)
	}
	userPrompt := fmt.Sprintf("Tags (name #comicCount):\n%s\nGroup exact-synonym tags and return the JSON array.", lines.String())

	content, err := CallCloudLLM(cfg, systemPrompt, userPrompt, &LLMCallOptions{
		Scenario:  "norm_tags",
		MaxTokens: 3000,
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
		return nil, fmt.Errorf("failed to parse AI tag merge suggestion: no JSON array in response")
	}
	content = content[start : end+1]

	var suggestions []TagMergeSuggestion
	if err := json.Unmarshal([]byte(content), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse AI tag merge suggestion: %w", err)
	}
	return suggestions, nil
}
