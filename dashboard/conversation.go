package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatState struct {
	Mode      string        `json:"mode"`
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	Streaming bool          `json:"streaming"`
	Error     string        `json:"error"`
	Stopped   bool          `json:"stopped"`
}

type conversation struct {
	mu       sync.Mutex
	state    ChatState
	cancel   context.CancelFunc
	endpoint string
	client   *http.Client
	guide    func() (string, error)
	system   string
}

func newConversation(endpoint string) *conversation {
	return &conversation{endpoint: endpoint, client: &http.Client{Timeout: 5 * time.Minute}, guide: loadChatGuide}
}

func (service *DashboardService) ChatSnapshot() ChatState {
	chat := service.conversation
	chat.mu.Lock()
	defer chat.mu.Unlock()
	state := chat.state
	state.Messages = append([]ChatMessage{}, state.Messages...)
	return state
}

func (service *DashboardService) CancelChat() {
	chat := service.conversation
	if chat == nil {
		return
	}
	chat.mu.Lock()
	defer chat.mu.Unlock()
	if chat.cancel != nil {
		chat.state.Stopped = true
		chat.cancel()
	}
}

func (service *DashboardService) NewChat() (ChatState, error) {
	chat := service.conversation
	chat.mu.Lock()
	if chat.state.Streaming {
		chat.mu.Unlock()
		return ChatState{}, fmt.Errorf("Stop the reply before starting a new chat")
	}
	chat.state = ChatState{}
	chat.system = ""
	chat.mu.Unlock()
	return service.ChatSnapshot(), nil
}

func loadingActive(loading *LoadingSnapshot) bool {
	return loading != nil && loading.Phase != "error" && loading.Phase != "paused"
}

func (service *DashboardService) SendChat(text string) (ChatState, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ChatState{}, fmt.Errorf("Enter a message first")
	}
	if len(text) > 32*1024 {
		return ChatState{}, fmt.Errorf("This message is too long; shorten it to less than 32 KiB")
	}
	var status statusResponse
	if err := service.getJSON("/admin/status", &status); err != nil {
		return ChatState{}, fmt.Errorf("Cannot reach the local server. Open Overview and choose Start server or Retry")
	}
	if status.Model.Kind != "running" || status.Model.Health == nil || !*status.Model.Health || loadingActive(status.Loading) {
		return ChatState{}, fmt.Errorf("Your model is not ready yet. Use the model setup card in Chat to get it ready")
	}
	chat := service.conversation
	chat.mu.Lock()
	if chat.state.Mode == "" {
		chat.mu.Unlock()
		return ChatState{}, fmt.Errorf("Choose Help or Just chat before sending a message")
	}
	if chat.state.Streaming {
		chat.mu.Unlock()
		return ChatState{}, fmt.Errorf("Wait for the reply or press Stop")
	}
	if chat.state.Model != "" && chat.state.Model != status.Model.Preset {
		chat.mu.Unlock()
		return ChatState{}, fmt.Errorf("The model changed. Copy this conversation if needed, then choose New chat")
	}
	history := append([]ChatMessage{}, chat.state.Messages...)
	total := len(text)
	for _, message := range history {
		total += len(message.Content)
	}
	if total > 256*1024 || len(history) >= 120 {
		chat.mu.Unlock()
		return ChatState{}, fmt.Errorf("This conversation is full. Copy it if needed, then choose New chat")
	}
	// A canceled or failed empty reply is not useful context for the next turn.
	if len(history) > 0 && history[len(history)-1].Role == "assistant" && history[len(history)-1].Content == "" {
		history = history[:len(history)-1]
	}
	history = append(history, ChatMessage{Role: "user", Content: text})
	chat.state = ChatState{Mode: chat.state.Mode, Model: status.Model.Preset, Messages: append(append([]ChatMessage{}, history...), ChatMessage{Role: "assistant"}), Streaming: true}
	ctx, cancel := context.WithCancel(context.Background())
	chat.cancel = cancel
	model := chat.state.Model
	mode, system := chat.state.Mode, chat.system
	chat.mu.Unlock()
	go chat.generate(ctx, model, mode, system, history)
	return service.ChatSnapshot(), nil
}

func (chat *conversation) generate(ctx context.Context, model, mode, system string, history []ChatMessage) {
	err := chat.stream(ctx, model, mode, system, history)
	chat.mu.Lock()
	defer chat.mu.Unlock()
	chat.cancel()
	chat.cancel = nil
	chat.state.Streaming = false
	if err != nil && !chat.state.Stopped {
		chat.state.Error = err.Error()
	}
}

func (chat *conversation) stream(ctx context.Context, model, mode, system string, history []ChatMessage) error {
	messages := history
	if system != "" {
		messages = append([]ChatMessage{{Role: "system", Content: system}}, history...)
	}
	body := map[string]any{
		"model": model, "messages": messages, "stream": true, "max_tokens": 2048,
		"chat_template_kwargs": map[string]bool{"enable_thinking": false},
	}
	if mode == "help" {
		body["temperature"] = 0
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, chat.endpoint+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := chat.client.Do(request)
	if err != nil {
		return fmt.Errorf("Reply interrupted: %w. Check the server and retry", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Model returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	completed, visible, limited := false, false, false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			completed = true
			break
		}
		var chunk struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("The server sent an unreadable reply; retry the message")
		}
		if chunk.Error != nil {
			return fmt.Errorf("Model error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != nil {
			completed = true
			limited = *choice.FinishReason == "length"
		}
		if choice.Delta.Content == "" {
			continue
		}
		chat.mu.Lock()
		last := &chat.state.Messages[len(chat.state.Messages)-1]
		if len(last.Content)+len(choice.Delta.Content) > 256*1024 {
			chat.mu.Unlock()
			return fmt.Errorf("Reply exceeded the display limit")
		}
		last.Content += choice.Delta.Content
		chat.mu.Unlock()
		visible = true
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("Reply interrupted: %w", err)
	}
	if !completed {
		return fmt.Errorf("Connection ended before the reply finished. Partial text is kept")
	}
	if !visible {
		return fmt.Errorf("The model returned no visible answer. Try again")
	}
	if limited {
		return fmt.Errorf("Reply reached its length limit. Ask the model to continue")
	}
	return nil
}
