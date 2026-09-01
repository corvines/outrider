package endpoint

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestChatCompletion(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Origin") != "" {
			t.Fatalf("native request sent browser origin %q", request.Header.Get("Origin"))
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": []any{
					map[string]any{"type": "text", "text": "It is working."},
				}},
			}},
			"timings": map[string]any{"predicted_per_second": 37.25},
		})
	}))
	defer server.Close()
	result, err := RequestChatCompletion(context.Background(), server.URL, ChatOptions{
		Model: "tiny.gguf", SystemPrompt: "system", UserPrompt: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AssistantResponse != "It is working." {
		t.Fatalf("assistant response = %q", result.AssistantResponse)
	}
	if result.GenerationTiming == nil || result.GenerationTiming.TokensPerSecond != 37.25 || result.GenerationTiming.Source != "response" {
		t.Fatalf("timing = %#v", result.GenerationTiming)
	}
	if requestBody["model"] != "tiny.gguf" || requestBody["stream"] != false || requestBody["temperature"] != 0.2 || requestBody["top_p"] != 0.9 {
		t.Fatalf("request body = %#v", requestBody)
	}
	kwargs, ok := requestBody["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != false {
		t.Fatalf("template kwargs = %#v", requestBody["chat_template_kwargs"])
	}
}

func TestRequestChatCompletionRejectsEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "  "}}},
		})
	}))
	defer server.Close()
	_, err := RequestChatCompletion(context.Background(), server.URL, ChatOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty assistant message") {
		t.Fatalf("error = %v", err)
	}
}

func TestRequestChatCompletionReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "model unavailable\nmore", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := RequestChatCompletion(context.Background(), server.URL, ChatOptions{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 503: model unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestRequestChatCompletionCalculatesTimingFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "working"}}},
			"timings": map[string]any{"predicted_n": 8, "predicted_ms": 20},
		})
	}))
	defer server.Close()
	result, err := RequestChatCompletion(context.Background(), server.URL, ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.GenerationTiming == nil || result.GenerationTiming.TokensPerSecond != 400 {
		t.Fatalf("timing = %#v", result.GenerationTiming)
	}
}

func TestReadGenerationTimingFromLog(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(logFile, []byte(strings.Join([]string{
		"prompt eval time = 10 ms / 4 runs (2.5 ms per token, 400 tokens per second)",
		"eval time = 20 ms / 8 runs (2.5 ms per token, 320 tokens per second)",
	}, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	timing, err := ReadGenerationTimingFromLog(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if timing == nil || timing.TokensPerSecond != 320 || timing.Source != "log" {
		t.Fatalf("timing = %#v", timing)
	}
}
