package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// AI 标签情景分配：把情景名清单 + 标签名清单交给 LLM，
// 为每个标签从既有情景中选一个（不适合的标签直接省略）。只允许已有情景，不新增。
//
// 分批处理：输出量随标签数线性增长（实测约 23 token/条），
// 上千标签一次性送入必然撞上输出上限、得到半截 JSON。
// 情景清单每批完整带入，批间相互独立。
// ============================================================

// scenarioBatchSize 情景分配每批标签数。
// 与归一工作台的可调批大小无关：归一每批输出是一组归并关系（远小于输入），
// 而这里每个标签都要回一条记录，批大小直接决定单批输出量。
const scenarioBatchSize = 250

// TagScenarioSuggestion 单个标签的情景建议；Scenario 为空串表示 AI 建议跳过。
type TagScenarioSuggestion struct {
	Tag      string `json:"tag"`
	Scenario string `json:"scenario"`
}

// scenarioBatchMaxTokens 按批大小推算单批输出上限：
// 每标签约一条 {"tag","scenario"} 记录，实测 23 token/条左右，取 28 留余量；
// 夹取到 [4096, 8192]。
func scenarioBatchMaxTokens(batchSize int) int {
	tokens := batchSize * 28
	if tokens < 4096 {
		tokens = 4096
	}
	if tokens > 8192 {
		tokens = 8192
	}
	return tokens
}

// SuggestTagScenarios 分批调用 LLM，为标签清单推荐情景。
// 调用方负责严格匹配：仅当返回的 tag/scenario 与既有名字完全相等时才采用。
func SuggestTagScenarios(cfg AIConfig, tagNames, scenarioNames []string) ([]TagScenarioSuggestion, error) {
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		return nil, fmt.Errorf("cloud AI not configured")
	}
	if len(tagNames) == 0 || len(scenarioNames) == 0 {
		return []TagScenarioSuggestion{}, nil
	}

	maxTokens := scenarioBatchMaxTokens(scenarioBatchSize)
	total := (len(tagNames) + scenarioBatchSize - 1) / scenarioBatchSize

	var all []TagScenarioSuggestion
	for i, start := 0, 0; start < len(tagNames); i, start = i+1, start+scenarioBatchSize {
		end := start + scenarioBatchSize
		if end > len(tagNames) {
			end = len(tagNames)
		}
		batch, err := suggestTagScenariosBatch(cfg, tagNames[start:end], scenarioNames, maxTokens)
		if err != nil {
			return nil, fmt.Errorf("第 %d/%d 批分析失败: %w", i+1, total, err)
		}
		all = append(all, batch...)
	}
	return all, nil
}

// suggestTagScenariosBatch 单次 LLM 调用处理一批标签（附带完整情景清单）。
func suggestTagScenariosBatch(cfg AIConfig, tagNames, scenarioNames []string, maxTokens int) ([]TagScenarioSuggestion, error) {
	systemPrompt := `You are a manga/comic/novel library tag taxonomy expert working on an ADULT (18+) comic collection. Explicit sexual tags are normal taxonomy terms here: judge them factually, never censor, euphemize, sanitize or refuse. Tags on the library are grouped into fixed "scenarios" (tag categories, e.g. plot, body, clothing, situation, art style).

Rules:
- For each tag, choose exactly ONE scenario from the provided scenario list -- the one whose meaning fits best
- Only use scenario names from the provided list verbatim -- NEVER invent new scenarios
- Copy tag names verbatim from the provided list
- Skip a tag by OMITTING it entirely -- never output an entry with an empty scenario
- Return ONLY a JSON array of objects, no extra text. Format: [{"tag":"<tag copied verbatim>","scenario":"<scenario name copied verbatim>"}]
- If no tag fits any scenario, return []`

	userPrompt := fmt.Sprintf("Available scenarios (use name verbatim):\n%s\n\nTags to classify:\n%s\n\nAssign each tag to one scenario (omit tags that fit none) and return the JSON array.",
		strings.Join(scenarioNames, "\n"),
		strings.Join(tagNames, "\n"),
	)

	content, err := CallCloudLLM(cfg, systemPrompt, userPrompt, &LLMCallOptions{
		Scenario:         "assign_tag_scenarios",
		MaxTokens:        maxTokens,
		StrictTruncation: true,
	})
	if err != nil {
		return nil, err
	}

	// 清理并截取 JSON 数组
	content = strings.ReplaceAll(content, "```json", "")
	content = strings.ReplaceAll(content, "```", "")
	content = strings.TrimSpace(content)
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("failed to parse AI tag scenario suggestion: no JSON array in response")
	}
	content = content[start : end+1]

	var suggestions []TagScenarioSuggestion
	if err := json.Unmarshal([]byte(content), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse AI tag scenario suggestion: %w", err)
	}
	return suggestions, nil
}
