package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// AI 标签归一：把内容标签清单（名称+用量）分批交给 LLM，
// 找出「同一概念的不同写法」（译名、罗马音、繁简体、空格/连字符差异等）
// 并给出归并分组（规范目标 + 待并入变体）。只做建议；
// 是否落库由调用方严格校验后决定（复用归一工作台的合并逻辑）。
//
// 分批的原因：一次性送入数百标签时，重复组多的大库会输出上百组建议，
// 极易撞上模型输出上限（如 deepseek-chat 8k）被截断成半截 JSON。
// 分批后单批输出天然有界；批大小可在 AI 设置调整（tagNormBatchSize）。
// 代价：跨批的同义对（如「少女」在批1、「SHOUJO」在批2）本次不会被发现，
// 机械归一预览（normKey 相同的对）不受影响，多跑几轮可逐步收敛。
// ============================================================

// defaultTagNormBatchSize 每批送入 LLM 的标签数缺省值。
// 可通过 AI 设置的 tagNormBatchSize 调整（50-800）。
const defaultTagNormBatchSize = 250

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

// tagNormBatchMaxTokens 按批大小推算单批输出上限：
// 每标签约 16 token 的建议输出预算，下限 4096、上限 8192（deepseek-chat 等的输出硬顶）。
func tagNormBatchMaxTokens(batchSize int) int {
	tokens := batchSize * 16
	if tokens < 4096 {
		tokens = 4096
	}
	if tokens > 8192 {
		tokens = 8192
	}
	return tokens
}

// SuggestTagMerges 分批调用 LLM，从标签清单中找出同义变体分组并合并结果。
// 调用方负责严格匹配：返回的名称必须与既有标签名完全相等才采用。
func SuggestTagMerges(cfg AIConfig, candidates []TagNormCandidate) ([]TagMergeSuggestion, error) {
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		return nil, fmt.Errorf("cloud AI not configured")
	}
	if len(candidates) == 0 {
		return []TagMergeSuggestion{}, nil
	}

	batchSize := cfg.TagNormBatchSize
	if batchSize <= 0 {
		batchSize = defaultTagNormBatchSize
	}
	batchMaxTokens := tagNormBatchMaxTokens(batchSize)

	total := (len(candidates) + batchSize - 1) / batchSize
	var all []TagMergeSuggestion
	for i, start := 0, 0; start < len(candidates); i, start = i+1, start+batchSize {
		end := start + batchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batchSuggestions, err := suggestTagMergesBatch(cfg, candidates[start:end], batchMaxTokens)
		if err != nil {
			return nil, fmt.Errorf("第 %d/%d 批分析失败: %w", i+1, total, err)
		}
		all = append(all, batchSuggestions...)
	}
	return all, nil
}

// suggestTagMergesBatch 单次 LLM 调用处理一批标签。
func suggestTagMergesBatch(cfg AIConfig, candidates []TagNormCandidate, maxTokens int) ([]TagMergeSuggestion, error) {
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
		Scenario:         "norm_tags",
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
		return nil, fmt.Errorf("failed to parse AI tag merge suggestion: no JSON array in response")
	}
	content = content[start : end+1]

	var suggestions []TagMergeSuggestion
	if err := json.Unmarshal([]byte(content), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse AI tag merge suggestion: %w", err)
	}
	return suggestions, nil
}
