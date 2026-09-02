package chat

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type picker struct {
	query  string
	cursor int
	caret  cursor.Model
	rows   []modelRow
}

func (m *model) openPicker() tea.Cmd {
	caret := cursor.New()
	caret.Style = styleInputCursor
	caret.SetChar(" ")
	m.picker = &picker{rows: m.rows, caret: caret}
	m.picker.refine()
	return m.picker.caret.Focus()
}

func (p *picker) matches() []modelRow {
	if strings.TrimSpace(p.query) == "" {
		return p.rows
	}
	needle := strings.ToLower(p.query)
	var out []modelRow
	for _, row := range p.rows {
		if strings.Contains(strings.ToLower(row.label), needle) ||
			strings.Contains(strings.ToLower(row.source), needle) {
			out = append(out, row)
		}
	}
	return out
}

func (p *picker) refine() {
	if limit := len(p.matches()) - 1; p.cursor > limit {
		p.cursor = max(0, limit)
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *picker) move(delta int) {
	p.cursor += delta
	p.refine()
}

func (m *model) applyPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlP:
		m.picker = nil
	case tea.KeyUp:
		p.move(-1)
	case tea.KeyDown:
		p.move(1)
	case tea.KeyCtrlU:
		p.move(-5)
	case tea.KeyCtrlD:
		p.move(5)
	case tea.KeyBackspace:
		if p.query != "" {
			p.query = p.query[:len(p.query)-1]
			p.refine()
		}
	case tea.KeyEnter:
		hits := p.matches()
		if len(hits) > 0 {
			m.picker = nil
			return m, m.selectModel(hits[p.cursor])
		}
		m.picker = nil
	case tea.KeyRunes:
		p.query += string(msg.Runes)
		p.refine()
	case tea.KeySpace:
		p.query += " "
		p.refine()
	}
	return m, nil
}

// searchLine renders the typed query with a block cursor after it. The
// placeholder stands in only while nothing is typed.
func (p *picker) searchLine(width int) string {
	if p.query == "" {
		text := "Search models\u2026"
		p.caret.SetChar(text[:1])
		return p.caret.View() + styleDim.Render(truncate(text[1:], width-1))
	}
	p.caret.SetChar(" ")
	return styleText.Render(truncate(p.query, width-1)) + p.caret.View()
}

func (m *model) pickerView() string {
	p := m.picker
	width := max(24, min(72, m.width-6))
	inner := width - 4
	hits := p.matches()
	first, last := p.window(len(hits), m.height)

	rows := []string{
		spread(styleText.Bold(true).Render("Select model"), styleDim.Render("esc"), inner),
		"",
		p.searchLine(inner),
		"",
	}
	if len(hits) == 0 {
		rows = append(rows, styleDim.Render("no model matches that"))
	}
	cols := p.columns(hits[first:last], inner)
	for i := first; i < last; i++ {
		row := hits[i]
		current := row.id == m.currentModel && row.endpoint == m.endpoint
		cursor, dot := "  ", "  "
		if i == p.cursor {
			cursor = "› "
		}
		if current {
			dot = "● "
		}
		label := truncate(cursor+dot+cols.render(row), inner)
		if i == p.cursor {
			rows = append(rows, lipgloss.NewStyle().
				Foreground(colBlock).Background(colAccent).
				Width(inner).Render(label))
			continue
		}
		if current {
			rows = append(rows, styleAccent.Render(label))
			continue
		}
		rows = append(rows, styleText.Render(label))
	}
	if showPeek(m.height) && len(hits) > 0 && p.cursor < len(hits) {
		rows = append(rows, "", p.peekLine(hits[p.cursor], inner))
	}
	rows = append(rows, "", styleDim.Render(truncate("↑↓ move · ⏎ select · esc close", inner)))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colDim).
		Padding(1, 2).
		Width(width).
		Render(strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

// window returns the slice of matches that fits the panel with the cursor shown.
// showPeek reports whether the panel can spare the two rows the weights path
// needs without pushing the model list below one entry.
func showPeek(height int) bool {
	return height >= 16
}

func (p *picker) window(count, height int) (int, int) {
	chrome := 10
	if showPeek(height) {
		chrome = 12
	}
	rows := max(1, height-chrome)
	if count <= rows {
		return 0, count
	}
	first := p.cursor - rows/2
	if first < 0 {
		first = 0
	}
	if first > count-rows {
		first = count - rows
	}
	return first, first + rows
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// pickerColumns is the table layout for one screenful of rows. Columns drop
// from the right when the panel is too narrow to hold them.
type pickerColumns struct {
	name   int
	quant  int
	ctx    int
	source int
}

const pickerGap = 2

func (p *picker) columns(rows []modelRow, inner int) pickerColumns {
	body := inner - 4
	var quant, ctxw, source int
	for _, row := range rows {
		quant = max(quant, lipgloss.Width(ifEmpty(row.quant, "?")))
		ctxw = max(ctxw, lipgloss.Width(formatContext(row.ctx)))
		source = max(source, lipgloss.Width(row.source))
	}
	cols := pickerColumns{quant: quant, ctx: ctxw, source: source}
	for {
		fixed := 0
		if cols.quant > 0 {
			fixed += pickerGap + cols.quant
		}
		if cols.ctx > 0 {
			fixed += pickerGap + cols.ctx
		}
		if cols.source > 0 {
			fixed += pickerGap + cols.source
		}
		cols.name = body - fixed
		if cols.name >= 12 || (cols.quant == 0 && cols.ctx == 0 && cols.source == 0) {
			break
		}
		// The source is the point of the table, so quant and context go first.
		switch {
		case cols.quant > 0:
			cols.quant = 0
		case cols.ctx > 0:
			cols.ctx = 0
		default:
			cols.source = 0
		}
	}
	cols.name = max(4, cols.name)
	return cols
}

func (c pickerColumns) render(row modelRow) string {
	out := pad(truncate(row.label, c.name), c.name)
	if c.quant > 0 {
		out += strings.Repeat(" ", pickerGap) + pad(truncate(ifEmpty(row.quant, "?"), c.quant), c.quant)
	}
	if c.ctx > 0 {
		out += strings.Repeat(" ", pickerGap) + pad(formatContext(row.ctx), c.ctx)
	}
	if c.source > 0 {
		out += strings.Repeat(" ", pickerGap) + truncate(row.source, c.source)
	}
	return strings.TrimRight(out, " ")
}

func pad(text string, width int) string {
	gap := width - lipgloss.Width(text)
	if gap <= 0 {
		return text
	}
	return text + strings.Repeat(" ", gap)
}

// peekLine names the weights behind the highlighted row, for anyone who wants
// to go look at the file.
func (p *picker) peekLine(row modelRow, width int) string {
	if row.path == "" {
		return styleDim.Render(truncate(row.endpoint, width))
	}
	return styleDim.Render(truncateMiddle(shortPath(row.path), width))
}
