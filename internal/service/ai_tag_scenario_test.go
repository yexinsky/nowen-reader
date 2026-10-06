package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// scenarioFakeLLM 假 OpenAI 兼容服务：按批返回「每个标签一条」的情景建议，
// 并记录每批收到的标签数，供分批/截断行为断言。
type scenarioFakeLLM struct {
	mu         sync.Mutex
	batchSizes []int
	failBatch  int // 第几批（1 起）返回 500，0 表示不失败
	truncate   bool
}

func (f *scenarioFakeLLM) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)

		var tags []string
		if len(req.Messages) > 1 {
			prompt := req.Messages[1].Content
			marker := "Tags to classify:\n"
			if i := strings.Index(prompt, marker); i >= 0 {
				rest := prompt[i+len(marker):]
				if j := strings.Index(rest, "\n\nAssign each tag"); j >= 0 {
					rest = rest[:j]
				}
				tags = strings.Split(rest, "\n")
			}
		}

		f.mu.Lock()
		f.batchSizes = append(f.batchSizes, len(tags))
		batchNo := len(f.batchSizes)
		fail, truncate := f.failBatch == batchNo, f.truncate
		f.mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}

		entries := make([]string, 0, len(tags))
		for _, t := range tags {
			entries = append(entries, fmt.Sprintf(`{"tag":%q,"scenario":"剧情"}`, t))
		}
		content := "[" + strings.Join(entries, ",") + "]"
		finish := "stop"
		if truncate {
			content = strings.TrimSuffix(content, "]") // 半截 JSON
			finish = "length"
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"choices":[{"message":{"content":%q},"finish_reason":%q}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
			content, finish)))
	}
}

func scenarioTestConfig(serverURL string) AIConfig {
	cfg := AIConfig{
		EnableCloudAI:    true,
		CloudProvider:    "deepseek",
		CloudAPIKey:      "test-key",
		CloudAPIURL:      serverURL + "/v1",
		CloudModel:       "test-model",
		MaxRetries:       0,
		EnableLocalAI:    false,
		TagNormBatchSize: defaultTagNormBatchSize,
	}
	return cfg
}

func makeTagNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("标签%03d", i)
	}
	return names
}

// 超过单批上限的标签应分批送入，情景清单每批完整携带，结果合并返回。
func TestSuggestTagScenariosBatches(t *testing.T) {
	fake := &scenarioFakeLLM{}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	tags := makeTagNames(scenarioBatchSize + 1)
	got, err := SuggestTagScenarios(scenarioTestConfig(server.URL), tags, []string{"剧情", "工具"})
	if err != nil {
		t.Fatalf("SuggestTagScenarios failed: %v", err)
	}
	if len(got) != len(tags) {
		t.Fatalf("suggestions = %d, want %d (merged across batches)", len(got), len(tags))
	}
	if fake.batchSizes == nil || len(fake.batchSizes) != 2 {
		t.Fatalf("batch sizes = %v, want 2 requests", fake.batchSizes)
	}
	if fake.batchSizes[0] != scenarioBatchSize || fake.batchSizes[1] != 1 {
		t.Fatalf("batch sizes = %v, want [%d 1]", fake.batchSizes, scenarioBatchSize)
	}
}

// 单批即可容纳时只发一次请求。
func TestSuggestTagScenariosSingleBatch(t *testing.T) {
	fake := &scenarioFakeLLM{}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	got, err := SuggestTagScenarios(scenarioTestConfig(server.URL), makeTagNames(3), []string{"剧情"})
	if err != nil {
		t.Fatalf("SuggestTagScenarios failed: %v", err)
	}
	if len(got) != 3 || len(fake.batchSizes) != 1 || fake.batchSizes[0] != 3 {
		t.Fatalf("got %d suggestions, batches %v; want 3 in one batch", len(got), fake.batchSizes)
	}
}

// 输出被 max_tokens 截断（finish_reason=length）时应给出明确错误，
// 而不是把半截 JSON 交给解析器产生难懂的 unmarshal 报错。
func TestSuggestTagScenariosTruncationError(t *testing.T) {
	fake := &scenarioFakeLLM{truncate: true}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	_, err := SuggestTagScenarios(scenarioTestConfig(server.URL), makeTagNames(5), []string{"剧情"})
	if err == nil {
		t.Fatalf("expected truncation error, got nil")
	}
	if !strings.Contains(err.Error(), "截断") {
		t.Fatalf("error = %v, want truncation hint", err)
	}
}

// 某批失败时报错需带批次序号，便于定位。
func TestSuggestTagScenariosBatchFailure(t *testing.T) {
	fake := &scenarioFakeLLM{failBatch: 2}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	_, err := SuggestTagScenarios(scenarioTestConfig(server.URL), makeTagNames(scenarioBatchSize+1), []string{"剧情"})
	if err == nil {
		t.Fatalf("expected batch failure, got nil")
	}
	if !strings.Contains(err.Error(), "第 2/2 批") {
		t.Fatalf("error = %v, want batch index", err)
	}
}
