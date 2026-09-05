package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/corvines/outrider/internal/guide"
)

func TestLaunchScreenFitsEveryTerminalSize(t *testing.T) {
	for _, size := range frameSizes {
		m := New(RunOptions{Endpoint: "http://127.0.0.1:11435"})
		m.width, m.height = size[0], size[1]
		checkFrame(t, m, m.View())
	}
}

// Nothing is typed into a session whose purpose is not settled.
func TestLaunchScreenSwallowsTyping(t *testing.T) {
	m := New(RunOptions{Endpoint: "http://127.0.0.1:11435"})
	m.applyKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if m.textarea.Value() != "" {
		t.Fatalf("composer = %q", m.textarea.Value())
	}
	if m.mode != modeUnset {
		t.Fatalf("mode = %v", m.mode)
	}
}

func TestNumberKeysPickAMode(t *testing.T) {
	for key, want := range map[rune]chatMode{'1': modeHelp, '2': modeChat} {
		m := New(RunOptions{Endpoint: "http://127.0.0.1:11435"})
		m.applyKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if m.mode != want {
			t.Fatalf("%c gave mode %v", key, m.mode)
		}
	}
}

// Help answers are read as fact, so the guide goes in the prompt and the
// backend is asked for its least random output.
func TestHelpModeSendsTheGuide(t *testing.T) {
	t.Setenv(guide.EnvOverride, filepath.Join("..", "..", "docs", guide.Filename))
	m := New(RunOptions{Endpoint: "http://127.0.0.1:11435"})
	m.chooseMode(choiceFor(modeHelp))
	m.currentModel = "qwen35-2b"
	m.messages = []message{{role: roleUser, content: "what is outrider"}}

	payload := m.completionPayload()
	if len(payload.Messages) == 0 || payload.Messages[0].Role != "system" {
		t.Fatalf("messages = %+v", payload.Messages)
	}
	if !strings.Contains(payload.Messages[0].Content, "outrider chat") {
		t.Fatal("the guide did not reach the prompt")
	}
	if payload.Temperature == nil || *payload.Temperature != 0 {
		t.Fatalf("temperature = %v", payload.Temperature)
	}
	if payload.TemplateKwargs["enable_thinking"] != false {
		t.Fatalf("template kwargs = %v", payload.TemplateKwargs)
	}
}

func TestChatModeSendsNoGuide(t *testing.T) {
	m := New(RunOptions{Endpoint: "http://127.0.0.1:11435"})
	m.chooseMode(choiceFor(modeChat))
	m.currentModel = "qwen35-2b"
	m.messages = []message{{role: roleUser, content: "hello"}}

	payload := m.completionPayload()
	for _, msg := range payload.Messages {
		if msg.Role == "system" {
			t.Fatal("a plain conversation was given the guide")
		}
	}
	if payload.Temperature != nil {
		t.Fatal("a plain conversation had its sampling overridden")
	}
}

// The guide names the commands the chat window actually has, because a small
// model asked about a tool it has not seen will otherwise invent them.
func TestGuideNamesEveryChatCommand(t *testing.T) {
	prompt := guidePrompt(repositoryGuide(t))
	for _, command := range []string{"/model", "/clear", "/exit"} {
		if !strings.Contains(prompt, command) {
			t.Fatalf("guide does not mention %s", command)
		}
	}
}

// repositoryGuide reads the guide that a release ships beside the binary. The
// chat window reads its copy from disk, so the tests read the same file rather
// than a fixture that can drift from it.
func repositoryGuide(t *testing.T) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "docs", guide.Filename))
	if err != nil {
		t.Fatalf("read shipped guide: %v", err)
	}
	return string(text)
}

// Help mode answers from the guide, so with no guide on disk it says why
// instead of asking a model to answer from nothing.
func TestHelpModeWithoutAGuideSendsNothing(t *testing.T) {
	if _, err := os.Stat(guide.SystemPath); err == nil {
		t.Skipf("%s exists on this machine", guide.SystemPath)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(guide.EnvOverride, filepath.Join(t.TempDir(), guide.Filename))

	m := New(RunOptions{Endpoint: "http://127.0.0.1:11435"})
	m.chooseMode(choiceFor(modeHelp))
	m.currentModel = "qwen35-2b"

	if cmd := m.submitPrompt("what is outrider"); cmd != nil {
		t.Fatal("a prompt was sent without a guide")
	}
	if len(m.messages) != 0 {
		t.Fatalf("messages = %+v", m.messages)
	}
	var missing *guide.NotFoundError
	if !errors.As(m.runningError, &missing) {
		t.Fatalf("running error = %v", m.runningError)
	}
}
