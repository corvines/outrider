package chat

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	colText   = lipgloss.Color("#c6c6c6")
	colDim    = lipgloss.Color("#808080")
	colAccent = lipgloss.Color("#EC5B2B")
	colBlock  = lipgloss.Color("#222222")

	styleText   = lipgloss.NewStyle().Foreground(colText)
	styleDim    = lipgloss.NewStyle().Foreground(colDim)
	styleAccent = lipgloss.NewStyle().Foreground(colAccent)
	styleUser   = lipgloss.NewStyle().Foreground(colText).Background(colBlock)

	styleBandText  = lipgloss.NewStyle().Foreground(colText).Background(colBlock)
	styleBandCaret = lipgloss.NewStyle().Foreground(colDim).Background(colBlock)
	styleBullet    = lipgloss.NewStyle().Foreground(colDim).Bold(true)

	// The textarea adds the reverse itself, so this paints the block accent.
	styleInputCursor = lipgloss.NewStyle().Foreground(colAccent)
)

const (
	userCaret  = "› "
	gutter     = 2
	sideMargin = 2
)

// bandWidth is the full terminal width used by the user band.
func (m *model) bandWidth() int {
	return max(24, m.width)
}

// contentWidth is the text column left of the marker gutter and side margin.
func (m *model) contentWidth() int {
	return max(20, m.width-gutter-sideMargin)
}

// composerInnerWidth is contentWidth less the composer border and padding.
func (m *model) composerInnerWidth() int {
	return max(20, m.contentWidth()-4)
}

// modelLabel drops the directory and .gguf suffix a server reports as a model id.
func modelLabel(name string) string {
	return strings.TrimSuffix(filepath.Base(name), ".gguf")
}

func rule(width int) string {
	return styleDim.Render(strings.Repeat("─", max(4, width)))
}

func wrap(style lipgloss.Style, text string, width int) []string {
	return strings.Split(style.Width(width).Render(text), "\n")
}

// userBlock is a full-width band: one blank row, the caret and wrapped text,
// then one blank row, all carrying the band background.
func userBlock(text string, width int) []string {
	lead := 1 + lipgloss.Width(userCaret)
	body := max(4, width-lead)
	blank := styleBandText.Width(width).Render("")
	out := []string{blank}
	for i, line := range strings.Split(lipgloss.NewStyle().Width(body).Render(text), "\n") {
		prefix := strings.Repeat(" ", lipgloss.Width(userCaret))
		if i == 0 {
			prefix = userCaret
		}
		out = append(out, styleBandCaret.Render(" "+prefix)+styleBandText.Width(body).Render(line))
	}
	return append(out, blank)
}

// markerBlock puts the assistant bullet in the gutter and indents the rest.
func markerBlock(lines []string) []string {
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			out = append(out, styleBullet.Render("• ")+line)
			continue
		}
		out = append(out, strings.Repeat(" ", gutter)+line)
	}
	return out
}

func formatSeconds(ms int) string {
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// reasoningElapsed counts up while the reasoning is still streaming and holds
// the frozen duration once the first content token arrives.
func (m *model) reasoningElapsed(msg message) int {
	if msg.reasoningMs > 0 || m.reasoningStart.IsZero() {
		return msg.reasoningMs
	}
	return int(time.Since(m.reasoningStart).Milliseconds())
}

// assistantLines renders one assistant turn. Reasoning stays collapsed behind a
// duration unless ctrl+o is on, except while it streams with no answer yet.
func (m *model) assistantLines(msg message, width int, last bool) []string {
	var out []string
	if msg.reasoning != "" {
		streaming := last && m.streamActive && strings.TrimSpace(msg.content) == ""
		head := "Thought: " + formatSeconds(m.reasoningElapsed(msg))
		if !m.showReasoning && !streaming && last {
			head += " · ctrl+o"
		}
		out = append(out, styleAccent.Render(head))
		if m.showReasoning || streaming {
			out = append(out, "")
			out = append(out, wrap(styleDim, strings.TrimSpace(msg.reasoning), width)...)
		}
		out = append(out, "", rule(width), "")
	}
	if strings.TrimSpace(msg.content) != "" {
		out = append(out, wrap(styleText, msg.content, width)...)
		return out
	}
	// A turn can end inside the reasoning with nothing left for an answer.
	if msg.reasoning != "" && !(last && m.streamActive) {
		out = append(out, wrap(styleDim, "no answer: the turn ran out inside the thinking. Ask again, or press ctrl+o to read it.", width)...)
	}
	return out
}

func (m *model) rebuildTranscripts() {
	band := m.bandWidth()
	width := m.contentWidth()
	var out []string
	for i, msg := range m.messages {
		if i > 0 && msg.role == roleUser {
			out = append(out, "", indentLines(rule(width), gutter), "")
		}
		if msg.role == roleUser {
			out = append(out, userBlock(msg.content, band)...)
			out = append(out, "")
			continue
		}
		out = append(out, markerBlock(m.assistantLines(msg, width, i == len(m.messages)-1))...)
	}
	if m.runningError != nil {
		out = append(out, "")
		out = append(out, markerBlock([]string{styleAccent.Render("× ") + styleText.Render(m.runningError.Error())})...)
	}
	if len(out) == 0 {
		out = []string{" "}
	}
	m.renderedLines = out
}

const (
	composerMinRows = 1
	composerMaxRows = 6
)

func composerRows(value string) int {
	rows := strings.Count(value, "\n") + 1
	return max(composerMinRows, min(composerMaxRows, rows))
}

func (m *model) composerHeight() int {
	return composerRows(m.textarea.Value()) + 4
}

func (m *model) syncComposerHeight() {
	if rows := composerRows(m.textarea.Value()); rows != m.textarea.Height() {
		m.textarea.SetHeight(rows)
	}
}

func (m *model) composerView() string {
	width := m.contentWidth()
	inner := strings.Join([]string{
		m.textarea.View(),
		rule(m.composerInnerWidth()),
		styleDim.Render(truncate(m.identityLine, m.composerInnerWidth())),
	}, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colDim).
		Width(width-2).
		Padding(0, 1).
		Render(inner)
}

func dimText(text string) string { return styleDim.Render(text) }

// truncate shortens plain text to max cells, marking the cut with an ellipsis.
func truncate(text string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= max {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > max {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// spreadPlain pushes right to the far edge, trimming left when the two collide
// and dropping right entirely when even a trimmed left cannot fit beside it.
func spreadPlain(left, right string, width int, leftStyle func(string) string) string {
	rightWidth := lipgloss.Width(right)
	if rightWidth+2 > width {
		return leftStyle(truncate(left, width))
	}
	left = truncate(left, width-rightWidth-1)
	gap := width - lipgloss.Width(left) - rightWidth
	if gap < 1 {
		gap = 1
	}
	return leftStyle(left) + strings.Repeat(" ", gap) + styleDim.Render(right)
}

func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) footerView() string {
	width := m.contentWidth()
	head := "● " + m.activityPrefix
	activityStyle := func(text string) string {
		if m.activityPrefix != "" && strings.HasPrefix(text, head) {
			return styleAccent.Render(head) + styleDim.Render(strings.TrimPrefix(text, head))
		}
		return styleDim.Render(text)
	}
	stop := "ctrl+c quit"
	if m.streamActive {
		stop = "ctrl+c stop"
	}
	return spreadPlain(m.activityLine, stop, width, activityStyle) + "\n" +
		spreadPlain(m.statsLine, "ctrl+p models", width, dimText)
}

func (m *model) View() string {
	m.syncComposerHeight()
	m.rebuildTranscripts()
	lines := m.renderedLines
	height := m.viewHeight()
	if len(lines) > height {
		if m.viewOffset < 0 {
			m.viewOffset = 0
		}
		if limit := len(lines) - height; m.viewOffset > limit {
			m.viewOffset = limit
		}
		lines = lines[m.viewOffset : m.viewOffset+height]
	} else if pad := height - len(lines); pad > 0 {
		lines = append(make([]string, pad), lines...)
	}
	if m.picker != nil {
		return m.pickerView()
	}
	return strings.Join(lines, "\n") + "\n\n" +
		indentLines(m.composerView(), sideMargin) + "\n" +
		indentLines(m.footerView(), sideMargin)
}

// wrapHard breaks text to width, preferring a space but splitting mid-word when
// a run has none, which is the normal case for a path. Continuation lines are
// indented so a wrapped entry stays visually one item.
func wrapHard(text string, width, indent int) []string {
	if width <= 0 {
		return nil
	}
	var out []string
	for {
		limit := width
		if len(out) > 0 {
			limit = max(1, width-indent)
		}
		runes := []rune(text)
		if len(runes) <= limit {
			out = append(out, prefixLine(text, len(out) > 0, indent))
			return out
		}
		cut := limit
		if space := strings.LastIndex(string(runes[:limit+1]), " "); space > limit/2 {
			cut = len([]rune(string(runes[:limit+1])[:space]))
		}
		out = append(out, prefixLine(strings.TrimRight(string(runes[:cut]), " "), len(out) > 0, indent))
		text = strings.TrimLeft(string(runes[cut:]), " ")
		if text == "" {
			return out
		}
	}
}

func prefixLine(text string, continuation bool, indent int) string {
	if !continuation {
		return text
	}
	return strings.Repeat(" ", indent) + text
}
