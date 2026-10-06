package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// AI 标签归一（两段式）：
//   规范池 = 高用量标签（按用量降序的头部，主流表达所在）；
//   变体批 = 低用量标签，分批送入，让 AI 把每个变体归入规范池
//   中的同义标签；池中没有合适目标时允许变体批内互配，
//   或新建更规范的标签作为目标（结果标 "new": true）。
//
// 为什么两段式：合并方向必须「低用量变体 → 高用量主流名」，
// 若同批混着高低用量标签，AI 缺少参照系会选错方向，且同义对
// 容易被切进不同批而漏配。规范池每批完整带入，天然解决两者。
//
// 只做建议；是否落库由调用方严格校验后决定（复用归一工作台的
// 合并逻辑，写别名 + 操作日志，可撤销）。
// ============================================================

// aiNormPoolSize 规范池大小（按用量降序的头部标签数）；
// 候选不足时取一半，保证留有变体可归。
const aiNormPoolSize = 150

// defaultTagNormBatchSize 每批变体数的缺省值。
// 可通过 AI 设置的 tagNormBatchSize 调整（50-800）。
const defaultTagNormBatchSize = 250

// TagNormCandidate 参与归一分析的标签候选。
type TagNormCandidate struct {
	Name  string
	Count int
}

// TagMergeSuggestion 一组归并建议：Target 为规范标签，Sources 为待并入变体。
// New=true 表示 Target 不在既有清单中、需要新建。
// 名称（除新建目标外）均应从清单逐字复制。
type TagMergeSuggestion struct {
	Target  string   `json:"target"`
	Sources []string `json:"sources"`
	New     bool     `json:"new"`
}

// tagNormBatchMaxTokens 按批大小推算单批输出上限：
// 每标签约 16 token 的建议输出预算，夹取到 [4096, 8192]
//（归一单批的现实输出量级，与具体模型的输出上限无关）。
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

// SuggestTagMerges 两段式归一建议：
//  1. 规范池自归并——池内两个高用量标签互为同义的对（两段式切分后
//     变体批不再覆盖它们，需单独一轮批内互配，否则成为盲区）；
//  2. 变体分批——每批携带完整规范池，把低用量变体归入池中同义标签。
//
// pool 应为按用量降序的头部标签；variants 为其余候选。
// 池的建议排在前，校验时优先占用标签，变体组不得与池组重叠。
// 调用方负责严格匹配：返回的既有标签名必须与清单完全相等才采用，
// 新建目标需经 ResolveOrCreateCanonicalTag 落库。
func SuggestTagMerges(cfg AIConfig, pool, variants []TagNormCandidate) ([]TagMergeSuggestion, error) {
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		return nil, fmt.Errorf("cloud AI not configured")
	}
	if len(pool) == 0 && len(variants) == 0 {
		return []TagMergeSuggestion{}, nil
	}

	batchSize := cfg.TagNormBatchSize
	if batchSize <= 0 {
		batchSize = defaultTagNormBatchSize
	}

	var all []TagMergeSuggestion

	// 第一轮：规范池内互配（高用量同义对）
	if len(pool) >= 2 {
		poolSuggestions, err := suggestTagMergesPoolBatch(cfg, pool, tagNormBatchMaxTokens(len(pool)))
		if err != nil {
			return nil, fmt.Errorf("规范池分析失败: %w", err)
		}
		all = append(all, poolSuggestions...)
	}

	// 第二轮：变体分批（每批携带完整规范池）
	if len(variants) > 0 {
		total := (len(variants) + batchSize - 1) / batchSize
		for i, start := 0, 0; start < len(variants); i, start = i+1, start+batchSize {
			end := start + batchSize
			if end > len(variants) {
				end = len(variants)
			}
			batchSuggestions, err := suggestTagMergesBatch(cfg, pool, variants[start:end], tagNormBatchMaxTokens(batchSize))
			if err != nil {
				return nil, fmt.Errorf("第 %d/%d 批分析失败: %w", i+1, total, err)
			}
			all = append(all, batchSuggestions...)
		}
	}
	return all, nil
}

// suggestTagMergesPoolBatch 规范池自归并：在高用量标签内部找同义组。
func suggestTagMergesPoolBatch(cfg AIConfig, pool []TagNormCandidate, maxTokens int) ([]TagMergeSuggestion, error) {
	systemPrompt := `You are a manga/comic/novel library tag taxonomy expert. You get the library's HIGH-USAGE tags. Find groups of tags that denote EXACTLY THE SAME concept: different spellings, translations (e.g. English / Japanese / Chinese), romanizations, abbreviations, Traditional/Simplified Chinese variants, extra/missing spaces, hyphens or punctuation.

Rules:
- Merge ONLY exact synonyms. Tags with related but DIFFERENT meanings (e.g. "少女" vs "萝莉", "swimsuit" vs "bikini") must NOT be merged
- Target priority: the intuitive mainstream name ALWAYS wins — a clear Chinese word like "巨乳" or "少女" beats a transliteration, foreign word, acronym or odd casing like "besar", "SHOUJO", "BBD"; usage count only breaks ties between equally intuitive names; NEVER pick a target just because it has more comics
- Copy tag names VERBATIM from the provided list — never invent, translate or modify names
- Each tag may appear in at most one group; a tag must never be both target and source
- Return ONLY a JSON array, no extra text: [{"target":"<canonical>","sources":["<variant>",...]}]
- If nothing is a duplicate, return []`

	var poolLines strings.Builder
	for _, c := range pool {
		fmt.Fprintf(&poolLines, "%s #%d\n", c.Name, c.Count)
	}
	userPrompt := fmt.Sprintf("High-usage tags (name #comicCount):\n%s\nGroup exact-synonym tags and return the JSON array.", poolLines.String())

	return parseTagMergeResponse(cfg, systemPrompt, userPrompt, maxTokens)
}

// suggestTagMergesBatch 单次 LLM 调用处理一批变体（附带完整规范池）。
func suggestTagMergesBatch(cfg AIConfig, pool, variants []TagNormCandidate, maxTokens int) ([]TagMergeSuggestion, error) {
	systemPrompt := `You are a manga/comic/novel library tag taxonomy expert. You get TWO lists:
1. CANONICAL POOL — high-usage tags users browse most (preferred merge targets)
2. VARIANTS — low-usage tags, likely redundant spellings/translations of something

Task: for each variant, find a CANONICAL POOL tag denoting EXACTLY THE SAME concept and group them under it. If no pool tag matches, variants in this batch may group among themselves — then pick the most intuitive mainstream name as target. Only when neither the pool nor the batch contains a good intuitive canonical name, you may CREATE a new tag as target and mark it "new": true (e.g. merge "besar" into a new "巨乳").

Rules:
- Merge ONLY exact synonyms. Tags with related but DIFFERENT meanings (e.g. "少女" vs "萝莉", "swimsuit" vs "bikini") must NOT be merged
- Target priority: the intuitive mainstream name ALWAYS wins — a clear Chinese word like "巨乳" or "少女" beats a transliteration, foreign word, acronym or odd casing like "besar", "SHOUJO", "BBD"; usage count only breaks ties between equally intuitive names; NEVER pick a target just because it has more comics
- New targets: concise intuitive names (Chinese ≤6 characters); use sparingly — prefer existing pool tags
- Copy existing tag names VERBATIM from the lists — never invent, translate or modify them; only "new": true targets may be absent from the lists
- Each tag may appear in at most one group; a tag must never be both target and source
- Return ONLY a JSON array, no extra text: [{"target":"<canonical>","sources":["<variant>",...],"new":false}]
- If nothing merges, return []

Example: pool ["巨乳 #340","少女 #120"], variants ["besar #12","SHOUJO #3","seijin #2"] → [{"target":"巨乳","sources":["besar"]},{"target":"少女","sources":["SHOUJO"]},{"target":"成人","sources":["seijin"],"new":true}]`

	var poolLines, variantLines strings.Builder
	for _, c := range pool {
		fmt.Fprintf(&poolLines, "%s #%d\n", c.Name, c.Count)
	}
	for _, c := range variants {
		fmt.Fprintf(&variantLines, "%s #%d\n", c.Name, c.Count)
	}
	userPrompt := fmt.Sprintf("Canonical pool (high-usage):\n%s\nVariant batch (low-usage):\n%s\nGroup each variant into the pool (or within the batch) and return the JSON array.",
		poolLines.String(), variantLines.String())

	return parseTagMergeResponse(cfg, systemPrompt, userPrompt, maxTokens)
}

// parseTagMergeResponse 发起 LLM 调用并从响应中解析归并建议（严格截断检测）。
func parseTagMergeResponse(cfg AIConfig, systemPrompt, userPrompt string, maxTokens int) ([]TagMergeSuggestion, error) {
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
