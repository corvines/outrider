package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatModeRequiredAndHelpReference(t *testing.T) {
	requests := make(chan map[string]json.RawMessage, 2)
	service := chatService(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- body
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})
	service.NewChat()
	if _, err := service.SendChat("Hello"); err == nil {
		t.Fatal("sent without choosing a purpose")
	}
	if _, err := service.SetChatMode("unknown"); err == nil {
		t.Fatal("accepted unknown mode")
	}
	service.conversation.guide = func() (string, error) { return "Offline reference: use Models to load a model.", nil }
	for _, mode := range []string{"help", "bare"} {
		state, err := service.SetChatMode(mode)
		if err != nil || state.Mode != mode {
			t.Fatalf("choose mode: %+v %v", state, err)
		}
		if _, err := service.SendChat("Hello"); err != nil {
			t.Fatal(err)
		}
		state = waitChat(t, service)
		if state.Error != "" || state.Mode != mode {
			t.Fatalf("reply: %+v", state)
		}
		body := <-requests
		var messages []ChatMessage
		if err := json.Unmarshal(body["messages"], &messages); err != nil {
			t.Fatal(err)
		}
		if mode == "help" {
			if len(messages) != 2 || messages[0].Role != "system" || !strings.Contains(messages[0].Content, "Offline reference:") || string(body["temperature"]) != "0" {
				t.Fatalf("help request: %s", body["messages"])
			}
		} else if len(messages) != 1 || messages[0].Role != "user" || body["temperature"] != nil {
			t.Fatalf("bare request has help settings: %v", body)
		}
		if _, err := service.SetChatMode("help"); err == nil {
			t.Fatal("changed purpose without clearing conversation")
		}
		if _, err := service.NewChat(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChatGuideMissingAndBundled(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "Outrider.app", "Contents", "MacOS", "outrider-dashboard")
	if _, err := readChatGuide(executable); err == nil {
		t.Fatal("missing bundle reference fell back to working directory")
	}
	resource := filepath.Join(root, "Outrider.app", "Contents", "Resources")
	if err := os.MkdirAll(resource, 0755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"", "bundled guide"} {
		if err := os.WriteFile(filepath.Join(resource, "llms.txt"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		guide, err := readChatGuide(executable)
		if content == "" && err == nil || content != "" && (err != nil || guide != content) {
			t.Fatalf("guide: %q %v", guide, err)
		}
	}
	service := NewDashboardService("")
	service.conversation.guide = func() (string, error) { return "", fmt.Errorf("missing reference") }
	if _, err := service.SetChatMode("help"); err == nil || service.ChatSnapshot().Mode != "" {
		t.Fatal("help accepted missing reference")
	}
	if _, err := service.SetChatMode("bare"); err != nil {
		t.Fatal("bare requires a help reference", err)
	}
}
