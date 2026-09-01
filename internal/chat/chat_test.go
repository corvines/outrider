package chat

import (
	"bytes"
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
	sendText(tm, "second")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t)
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

func TestUnreachableEndpoint(t *testing.T) {
	err := Run("http://127.0.0.1:1")
	if err == nil {
		t.Fatalf("expected unreachable endpoint error")
	}
}
