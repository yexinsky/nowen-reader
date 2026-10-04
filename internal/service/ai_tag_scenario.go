package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================
// AI 标签情景分配：把情景名清单 + 标签名清单交给 LLM，
// 为每个标签从既有情景中选一个（或跳过）。只允许已有情景，不新增。
// ============================================================

// TagScenarioSuggestion 单个标签的情景建议；Scenario 为空串表示 AI 建议跳过。
type TagScenarioSuggestion struct {
	Tag      string `json:"tag"`
	Scenario string `json:"scenario"`
}

// SuggestTagScenarios 单次 LLM 调用，为标签清单推荐情景。
// 调用方负责严格匹配：仅当返回的 tag/scenario 与既有名字完全相等时才采用。
func SuggestTagScenarios(cfg AIConfig, tagNames, scenarioNames []string) ([]TagScenarioSuggestion, error) {
	if !cfg.EnableCloudAI || cfg.CloudAPIKey == "" {
		return nil, fmt.Errorf("cloud AI not configured")
	}
	if len(tagNames) == 0 || len(scenarioNames) == 0 {
		return []TagScenarioSuggestion{}, nil
	}

	systemPrompt := `You are a manga/comic tag taxonomy expert. Tags on a library are grouped into fixed "scenarios" (categories of tags, e.g. plot, body, clothing, art style, tool).

Rules:
- For each tag, choose exactly ONE scenario from the provided scenario list, or skip it if none fits
- Only use scenario names from the provided list verbatim — NEVER invent new scenarios
- Copy tag names verbatim from the provided list
- Return ONLY a JSON array of objects, no extra text
- Use {"tag": "<tag>", "scenario": ""} to skip a tag

Example response: [{"tag":"汉化","scenario":"工具"},{"tag":"巨乳","scenario":"身体"},{"tag":"冷门","scenario":""}]`

	userPrompt := fmt.Sprintf("Available scenarios (use name verbatim):\n%s\n\nTags to classify:\n%s\n\nClassify each tag into one scenario or skip. Return a JSON array of {\"tag\",\"scenario\"} objects.",
		strings.Join(scenarioNames, "\n"),
		strings.Join(tagNames, "\n"),
	)

	content, err := CallCloudLLM(cfg, systemPrompt, userPrompt, &LLMCallOptions{
		Scenario:  "assign_tag_scenarios",
		MaxTokens: 2000,
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
