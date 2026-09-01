package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultSystemPrompt = "You are a concise, helpful local assistant."
	defaultUserPrompt   = "Reply with one short sentence confirming that the local runner is working."
)

var generationRatePattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:tokens?\s*(?:per\s*second|/s)|t/s)\b`)

type ChatOptions struct {
	Model              string
	SystemPrompt       string
	UserPrompt         string
	Temperature        *float64
	TopP               *float64
	MaxTokens          int
	ChatTemplateKwargs map[string]any
	RequestTimeout     time.Duration
}

type GenerationTiming struct {
	TokensPerSecond float64 `json:"tokensPerSecond"`
	Source          string  `json:"source"`
}

type ChatResult struct {
	AssistantResponse string            `json:"assistantResponse"`
	Body              map[string]any    `json:"body"`
	GenerationTiming  *GenerationTiming `json:"generationTiming,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model              string         `json:"model"`
	Messages           []message      `json:"messages"`
	Temperature        float64        `json:"temperature"`
	TopP               float64        `json:"top_p"`
	MaxTokens          int            `json:"max_tokens"`
	Stream             bool           `json:"stream"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs"`
}

func RequestChatCompletion(ctx context.Context, endpoint string, options ChatOptions) (ChatResult, error) {
	requestTimeout := options.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = 2 * time.Minute
	}
	requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	temperature := 0.2
	if options.Temperature != nil {
		temperature = *options.Temperature
	}
	topP := 0.9
	if options.TopP != nil {
		topP = *options.TopP
	}
	maxTokens := options.MaxTokens
	if maxTokens == 0 {
		maxTokens = 64
	}
	model := options.Model
	if model == "" {
		model = "outrider"
	}
	systemPrompt := options.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	userPrompt := options.UserPrompt
	if userPrompt == "" {
		userPrompt = defaultUserPrompt
	}
	kwargs := options.ChatTemplateKwargs
	if kwargs == nil {
		kwargs = map[string]any{"enable_thinking": false}
	}
	body, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    []message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
		Temperature: temperature, TopP: topP, MaxTokens: maxTokens,
		Stream: false, ChatTemplateKwargs: kwargs,
	})
	if err != nil {
		return ChatResult{}, fmt.Errorf("could not encode chat-completions request: %w", err)
	}
	completionEndpoint := strings.TrimRight(endpoint, "/") + "/v1/chat/completions"
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, completionEndpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, fmt.Errorf("chat-completions request failed at %s: %w", completionEndpoint, err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ChatResult{}, fmt.Errorf("chat-completions request failed at %s: %w", completionEndpoint, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return ChatResult{}, fmt.Errorf("chat-completions request failed at %s: %w", completionEndpoint, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := firstUsefulLine(string(responseBody))
		if detail == "" {
			detail = "empty response"
		}
		return ChatResult{}, fmt.Errorf(
			"chat-completions request failed with HTTP %d: %s", response.StatusCode, detail,
		)
	}
	var parsed map[string]any
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return ChatResult{}, fmt.Errorf("chat-completions response was not valid JSON: %w", err)
	}
	if parsed == nil {
		return ChatResult{}, fmt.Errorf("chat-completions response was not a JSON object")
	}
	assistantResponse, err := extractAssistantResponse(parsed)
	if err != nil {
		return ChatResult{}, err
	}
	result := ChatResult{AssistantResponse: assistantResponse, Body: parsed}
	if rate, ok := extractResponseTokensPerSecond(parsed); ok {
		result.GenerationTiming = &GenerationTiming{TokensPerSecond: rate, Source: "response"}
	}
	return result, nil
}

func ReadGenerationTimingFromLog(logFile string) (*GenerationTiming, error) {
	content, err := os.ReadFile(logFile)
	if err != nil {
		return nil, nil
	}
	lines := strings.Split(string(content), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := lines[index]
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "eval time") || strings.Contains(lower, "prompt eval time") {
			continue
		}
		match := generationRatePattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		var rate float64
		if _, err := fmt.Sscanf(match[1], "%f", &rate); err == nil && rate > 0 {
			return &GenerationTiming{TokensPerSecond: rate, Source: "log"}, nil
		}
	}
	return nil, nil
}

func extractAssistantResponse(body map[string]any) (string, error) {
	choices, ok := body["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("chat-completions response did not contain a choice")
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("chat-completions response did not contain a choice")
	}
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("chat-completions response did not contain a message")
	}
	var text string
	switch content := message["content"].(type) {
	case string:
		text = content
	case []any:
		var combined strings.Builder
		for _, rawPart := range content {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := part["text"].(string); ok {
				combined.WriteString(value)
			}
		}
		text = combined.String()
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("chat-completions response contained an empty assistant message")
	}
	return text, nil
}

func extractResponseTokensPerSecond(body map[string]any) (float64, bool) {
	timings, ok := body["timings"].(map[string]any)
	if !ok {
		return 0, false
	}
	if rate, ok := positiveNumber(timings["predicted_per_second"]); ok {
		return rate, true
	}
	tokens, tokensOK := positiveNumber(timings["predicted_n"])
	milliseconds, millisecondsOK := positiveNumber(timings["predicted_ms"])
	if tokensOK && millisecondsOK {
		return tokens / (milliseconds / 1000), true
	}
	return 0, false
}

func positiveNumber(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok && number > 0
}
