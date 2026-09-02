package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func newStreamingServer(chunks []string, includeTimings bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen35b-mtp","quantization":"Q4_K_M","context_window":32768}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, chunk := range chunks {
				payload := map[string]any{
					"choices": []any{
						map[string]any{
							"delta": map[string]any{"content": chunk},
						},
					},
				}
				if includeTimings {
					payload["timings"] = map[string]any{
						"prompt_n":             4096,
						"predicted_n":          4,
						"prompt_per_second":    812.4,
						"predicted_per_second": 47.3,
					}
				}
				raw, _ := json.Marshal(payload)
				_, _ = w.Write([]byte("data: " + string(raw) + "\n\n"))
				time.Sleep(20 * time.Millisecond)
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func runApp(t *testing.T, srv *httptest.Server) *teatest.TestModel {
	m := New(RunOptions{Endpoint: srv.URL})
	m.scanPorts = nil
	return teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 40))
}

func sendText(tm *teatest.TestModel, value string) {
	for _, r := range value {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func readOutput(t *testing.T, tm *teatest.TestModel) string {
	b, err := io.ReadAll(tm.FinalOutput(t))
	if err != nil {
		t.Fatalf("read final output: %v", err)
	}
	return string(b)
}

func TestCtrlCThreeStages(t *testing.T) {
	srv := newStreamingServer([]string{"hello"}, true)
	defer srv.Close()

	tm := runApp(t, srv)
	sendText(tm, "draft")
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	sendText(tm, "live")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)

	tm = runApp(t, srv)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)
}

func TestEscapeDoesNotQuit(t *testing.T) {
	srv := newStreamingServer([]string{"stream"}, true)
	defer srv.Close()
	tm := runApp(t, srv)
	sendText(tm, "ask")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(40 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	time.Sleep(40 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)
}

func TestHistoryNavigation(t *testing.T) {
	srv := newStreamingServer([]string{"a", "b"}, true)
	defer srv.Close()
	tm := runApp(t, srv)
	sendText(tm, "first")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(120 * time.Millisecond)
	sendText(tm, "second")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(120 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)
}

func TestEnterDoesNotStartConcurrentTurn(t *testing.T) {
	m := New(RunOptions{})
	m.streamActive = true
	m.textarea.SetValue("second prompt")

	_, command := m.applyKey(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil {
		t.Fatal("enter returned a command while a turn was active")
	}
	if len(m.messages) != 0 || m.turns != 0 {
		t.Fatalf("active session changed: messages=%d turns=%d", len(m.messages), m.turns)
	}
	if m.textarea.Value() != "second prompt" {
		t.Fatalf("draft changed to %q", m.textarea.Value())
	}
}

func TestStreamedTurnAndStats(t *testing.T) {
	srv := newStreamingServer([]string{"he", "llo"}, true)
	defer srv.Close()
	tm := runApp(t, srv)
	sendText(tm, "ask")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(120 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)
	out := readOutput(t, tm)
	if !bytes.Contains([]byte(out), []byte("hello")) {
		t.Fatalf("expected streamed content")
	}
	if !bytes.Contains([]byte(out), []byte("ctx 4,100/32,768")) {
		t.Fatalf("expected context usage")
	}
	if !bytes.Contains([]byte(out), []byte("47.3 tok/s")) {
		t.Fatalf("expected decode rate")
	}
	if !bytes.Contains([]byte(out), []byte("812.4 prompt tok/s")) {
		t.Fatalf("expected prefill rate")
	}
	if !bytes.Contains([]byte(out), []byte("1 turn")) {
		t.Fatalf("expected singular turn noun")
	}
	if !bytes.Contains([]byte(out), []byte("4 output tokens")) {
		t.Fatalf("expected final timing to be counted once")
	}
}

func TestStatsFallbackWhenTimingsMissing(t *testing.T) {
	srv := newStreamingServer([]string{"ok"}, false)
	defer srv.Close()
	tm := runApp(t, srv)
	sendText(tm, "ask")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(120 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)
	out := readOutput(t, tm)
	if !strings.Contains(out, "? tok/s") || !strings.Contains(out, "? prompt tok/s") {
		t.Fatalf("expected rate fallbacks")
	}
}

func TestPromptAppearsOnceInCompletionRequest(t *testing.T) {
	requests := make(chan completionRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"tiny","context_window":4096}]}`))
			return
		case "/v1/chat/completions":
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- request
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	tm := runApp(t, srv)
	sendText(tm, "hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case request := <-requests:
		if len(request.Messages) != 1 || request.Messages[0].Content != "hello" {
			t.Fatalf("expected one user message, got %#v", request.Messages)
		}
	case <-time.After(time.Second):
		t.Fatal("completion request was not received")
	}

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)
}

func TestCompletionHTTPErrorIncludesServerDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"context is too large"}}`))
	}))
	defer srv.Close()

	m := New(RunOptions{Endpoint: srv.URL})
	m.currentModel = "tiny"
	m.messages = []message{
		{role: "user", content: "hello"},
		{role: "assistant"},
	}
	go m.streamResponse(context.Background(), m.endpoint, m.completionPayload())

	select {
	case response := <-m.streamCh:
		if response.err == nil || !strings.Contains(response.err.Error(), "HTTP 400") ||
			!strings.Contains(response.err.Error(), "context is too large") {
			t.Fatalf("completion error = %v", response.err)
		}
	case <-time.After(time.Second):
		t.Fatal("completion error was not received")
	}
}

func TestUnreachableEndpoint(t *testing.T) {
	err := Run("http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("expected unreachable endpoint error")
	}
	if !strings.Contains(err.Error(), "outrider serve tiny") {
		t.Fatalf("unreachable endpoint error = %q", err)
	}
}
