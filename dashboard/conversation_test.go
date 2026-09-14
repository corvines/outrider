package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func chatService(t *testing.T, handler http.HandlerFunc) *DashboardService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/status" {
			fmt.Fprint(w, `{"model":{"kind":"running","preset":"ling3-tiny","health":true}}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	service := NewDashboardService(server.URL)
	if _, err := service.SetChatMode("bare"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.CancelChat)
	return service
}

func waitChat(t *testing.T, service *DashboardService) ChatState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if state := service.ChatSnapshot(); !state.Streaming {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not finish")
	return ChatState{}
}

func TestChatStreamsAndKeepsConversation(t *testing.T) {
	requests := make(chan []ChatMessage, 2)
	service := chatService(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string
			Messages []ChatMessage
			Stream   bool
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || body.Model != "ling3-tiny" || !body.Stream {
			t.Errorf("invalid request: %+v", body)
		}
		requests <- body.Messages
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	for _, question := range []string{"Hi", "What did you say?"} {
		if _, err := service.SendChat(question); err != nil {
			t.Fatal(err)
		}
		state := waitChat(t, service)
		if state.Error != "" || state.Messages[len(state.Messages)-1].Content != "Hello world" {
			t.Fatalf("state: %+v", state)
		}
	}
	first, second := <-requests, <-requests
	if len(first) != 1 || first[0].Role != "user" || len(second) != 3 || second[1].Content != "Hello world" {
		t.Fatalf("history: %+v / %+v", first, second)
	}
	copy := service.ChatSnapshot()
	copy.Messages[0].Content = "changed"
	if service.ChatSnapshot().Messages[0].Content != "Hi" {
		t.Fatal("snapshot mutated conversation")
	}
	if state, err := service.NewChat(); err != nil || len(state.Messages) != 0 || state.Model != "" || state.Mode != "" {
		t.Fatalf("new chat: %+v %v", state, err)
	}
}

func TestChatFailurePreservesPartialText(t *testing.T) {
	for _, name := range []string{"http", "broken", "malformed", "empty", "error", "limited"} {
		t.Run(name, func(t *testing.T) {
			service := chatService(t, func(w http.ResponseWriter, r *http.Request) {
				switch name {
				case "http":
					http.Error(w, "model stopped", http.StatusServiceUnavailable)
				case "broken":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
				case "malformed":
					fmt.Fprint(w, "data: not-json\n\n")
				case "empty":
					fmt.Fprint(w, "data: [DONE]\n\n")
				case "error":
					fmt.Fprint(w, "data: {\"error\":{\"message\":\"context full\"}}\n\n")
				case "limited":
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n")
				}
			})
			if _, err := service.SendChat("Hello"); err != nil {
				t.Fatal(err)
			}
			state := waitChat(t, service)
			if state.Error == "" || len(state.Messages) != 2 {
				t.Fatalf("state: %+v", state)
			}
			if name == "broken" && state.Messages[1].Content != "partial" {
				t.Fatal("lost partial reply")
			}
		})
	}
}

func TestChatCancellationAndBusyGuards(t *testing.T) {
	started := make(chan struct{})
	service := chatService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	if _, err := service.SendChat("Hello"); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := service.SendChat("duplicate"); err == nil {
		t.Fatal("allowed concurrent send")
	}
	if _, err := service.NewChat(); err == nil {
		t.Fatal("cleared an active reply")
	}
	if _, err := service.SetChatMode("help"); err == nil {
		t.Fatal("changed mode during an active reply")
	}
	service.CancelChat()
	state := waitChat(t, service)
	if !state.Stopped || state.Error != "" {
		t.Fatalf("state: %+v", state)
	}
}

func TestChatValidation(t *testing.T) {
	service := chatService(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid message reached model") })
	for _, input := range []string{" ", strings.Repeat("a", 32769)} {
		if _, err := service.SendChat(input); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
	service.conversation.state.Model = "other-model"
	if _, err := service.SendChat("Hello"); err == nil {
		t.Fatal("silently switched model")
	}
	service.conversation.state = ChatState{Mode: "bare", Messages: []ChatMessage{{Role: "user", Content: strings.Repeat("a", 256*1024)}}}
	if _, err := service.SendChat("Hello"); err == nil {
		t.Fatal("accepted full history")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"model":{"kind":"stopped"}}`)
	}))
	defer server.Close()
	service = NewDashboardService(server.URL)
	if _, err := service.SendChat("Hello"); err == nil || !strings.Contains(err.Error(), "model setup card in Chat") {
		t.Fatalf("missing model guidance: %v", err)
	}
}
