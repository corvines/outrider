package chat

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var frameSizes = [][2]int{{120, 40}, {100, 32}, {84, 30}, {80, 24}, {56, 18}, {40, 12}}

func frameModel() *model {
	return frameModelAt(84, 30)
}

func frameModelAt(width, height int) *model {
	m := New(RunOptions{Endpoint: "http://127.0.0.1:11435"})
	m.scanPorts = nil
	m.width, m.height = width, height
	m.textarea.SetWidth(m.composerInnerWidth())
	m.currentModel, m.quantization, m.contextWindow = "qwen35b-mtp", "Q4_K_M", 32768
	m.rows = []modelRow{
		{id: "llama-3.2-3b", label: "llama-3.2-3b", endpoint: m.endpoint,
			source: ":11435 llama.cpp", quant: "Q4_0", ctx: 4096},
		{id: "qwen35b-mtp", label: "qwen35b-mtp", endpoint: m.endpoint,
			source: ":11435 llama.cpp", quant: "Q4_K_M", ctx: 32768},
		{id: "tiny", label: "tiny", endpoint: "http://127.0.0.1:11434",
			source: ":11434 ollama", quant: "", ctx: 0},
	}
	m.messages = []message{
		{role: roleUser, content: "write a haiku on oranges"},
		{role: "assistant", reasoning: "five seven five", reasoningMs: 1800,
			content: "Sun-warm, peel open\nthe bright crescent slips inside\nsweet dusk in your mouth."},
		{role: roleUser, content: "and one on lemons"},
	}
	m.turns = 2
	m.totalOutputTokens = 1204
	m.lastPromptTokens = 3892
	m.lastTurnOutputTokens = 204
	m.lastWallMs = 3918
	prefill, decode := 812.4, 47.3
	m.promptPerSecond, m.predictedPerSecond = &prefill, &decode
	m.rebuildStatus()
	return m
}

func checkFrame(t *testing.T, m *model, frame string) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) > m.height {
		t.Errorf("frame is %d lines, terminal is %d", len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d cells wide, terminal is %d: %q", i, w, m.width, line)
		}
	}
	if os.Getenv("DUMP") != "" {
		os.Stdout.WriteString("\n" + frame + "\n")
	}
}

func TestFrameFitsEveryTerminalSize(t *testing.T) {
	for _, size := range frameSizes {
		m := frameModelAt(size[0], size[1])
		checkFrame(t, m, m.View())
		m.activityPrefix = "Generating"
		m.streamActive = true
		m.rebuildStatus()
		checkFrame(t, m, m.View())
		m.openPicker()
		checkFrame(t, m, m.View())
	}
}

func TestFrameFitsTerminal(t *testing.T) {
	m := frameModel()
	frame := m.View()
	checkFrame(t, m, frame)
	for _, want := range []string{m.identityLine, m.statsLine, "ctrl+p models"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame is missing %q", want)
		}
	}
}

func TestPickerFrameFitsTerminal(t *testing.T) {
	m := frameModel()
	m.openPicker()
	checkFrame(t, m, m.View())
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestPickerRowsReadWithoutColor(t *testing.T) {
	m := frameModel()
	m.openPicker()
	plain := ansi.ReplaceAllString(m.View(), "")
	for _, want := range []string{"›   llama-3.2-3b", "  ● qwen35b-mtp"} {
		if !strings.Contains(plain, want) {
			t.Errorf("stripped of color the picker has no %q row:\n%s", want, plain)
		}
	}
}

func TestComposerGrowsAndCaps(t *testing.T) {
	m := frameModel()
	base := m.composerHeight()
	m.textarea.SetValue("one\ntwo\nthree")
	if grown := m.composerHeight(); grown != base+2 {
		t.Errorf("composer height is %d for three lines, want %d", grown, base+2)
	}
	m.textarea.SetValue(strings.Repeat("x\n", 40))
	if capped := m.composerHeight(); capped != composerMaxRows+4 {
		t.Errorf("composer height is %d, want the cap %d", capped, composerMaxRows+4)
	}
	m.textarea.SetValue(strings.Repeat("x\n", 40))
	checkFrame(t, m, m.View())
}

func TestPickerWindowsLongListAtEverySize(t *testing.T) {
	for _, size := range frameSizes {
		m := frameModelAt(size[0], size[1])
		m.rows = nil
		for i := 0; i < 30; i++ {
			name := fmt.Sprintf("model-%02d-with-a-fairly-long-name", i)
			m.rows = append(m.rows, modelRow{id: name, label: name,
				endpoint: m.endpoint, source: ":11435 llama.cpp",
				quant: "Q4_K_M", ctx: 32768})
		}
		m.openPicker()
		for step := 0; step < 30; step++ {
			checkFrame(t, m, m.View())
			m.applyPickerKey(tea.KeyMsg{Type: tea.KeyDown})
		}
	}
}

func TestPickerRowsNameTheirSource(t *testing.T) {
	m := frameModel()
	m.openPicker()
	plain := ansi.ReplaceAllString(m.View(), "")
	for _, want := range []string{
		"llama-3.2-3b", "Q4_0", "4k", ":11435 llama.cpp",
		"tiny", ":11434 ollama",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("the picker table is missing %q:\n%s", want, plain)
		}
	}
}

func TestPickerKeepsSourceWhenNarrow(t *testing.T) {
	m := frameModelAt(56, 18)
	m.openPicker()
	plain := ansi.ReplaceAllString(m.View(), "")
	if !strings.Contains(plain, "ollama") {
		t.Errorf("a narrow picker dropped the source column:\n%s", plain)
	}
}

func TestShortQuant(t *testing.T) {
	cases := map[string]string{
		"Q4_K - Medium": "Q4_K_M",
		"Q5_K - Small":  "Q5_K_S",
		"Q4_0":          "Q4_0",
		"":              "",
	}
	for in, want := range cases {
		if got := shortQuant(in); got != want {
			t.Errorf("shortQuant(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWrapHardBreaksAPathWithNoSpaces(t *testing.T) {
	text := "*1  :11434 ollama  ~/.ollama/models/blobs/sha256-16a9369d0805f80b7377d25d87f937a90c05dc04ad79173a52001e42c9aab311"
	lines := wrapHard(text, 44, 4)
	if len(lines) < 3 {
		t.Fatalf("a 115 character entry did not wrap: %q", lines)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 44 {
			t.Errorf("line is %d wide, panel is 44: %q", lipgloss.Width(line), line)
		}
	}
	if !strings.HasPrefix(lines[0], "*1  :11434 ollama") {
		t.Errorf("the marker and server were not kept together: %q", lines[0])
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "    ") {
			t.Errorf("a continuation line was not indented: %q", line)
		}
	}
	joined := strings.ReplaceAll(strings.Join(lines, ""), " ", "")
	if !strings.HasSuffix(joined, "c9aab311") {
		t.Errorf("the end of the path was dropped: %q", joined)
	}
}

func TestPickerFootnotesEverySource(t *testing.T) {
	m := frameModelAt(120, 40)
	m.openPicker()
	frame := ansi.ReplaceAllString(m.View(), "")
	for _, want := range []string{"*1", "*2", ":11434 ollama", ":11435 llama.cpp"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the picker never showed %q:\n%s", want, frame)
		}
	}
	if strings.Count(frame, ":11434 ollama") != 1 {
		t.Errorf("the server name repeated instead of being footnoted:\n%s", frame)
	}
}
