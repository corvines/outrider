package chat

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type picker struct {
	query   string
	cursor  int
	caret   cursor.Model
	rows    []modelRow
	sources []sourceRef
}

// sourceRef is one server, footnoted so its long name and weights path are
// written once below the table instead of on every row it serves.
type sourceRef struct {
	endpoint string
	label    string
	path     string
}

func collectSources(rows []modelRow) []sourceRef {
	var out []sourceRef
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.endpoint] {
			continue
		}
		seen[row.endpoint] = true
		out = append(out, sourceRef{endpoint: row.endpoint, label: row.source, path: row.path})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].endpoint < out[j].endpoint })
	return out
}

func (p *picker) marker(endpoint string) string {
	for i, src := range p.sources {
		if src.endpoint == endpoint {
			return "*" + strconv.Itoa(i+1)
		}
	}
	return ""
}

// legend spells out every footnote the table uses, wrapped to the panel. The
// weights path is the first thing dropped when the panel is short.
func (p *picker) legend(width int, withPath bool, used map[string]bool) []string {
	var out []string
	for i, src := range p.sources {
		if !used[src.endpoint] {
			continue
		}
		text := "*" + strconv.Itoa(i+1) + "  " + src.label
		if withPath && src.path != "" {
			text += "  " + shortPath(src.path)
		}
		for _, line := range wrapHard(text, width, 4) {
			out = append(out, styleDim.Render(line))
		}
	}
	return out
}

// fitLegend picks the longest legend that still leaves the list some rows. Only
// the servers the filtered list actually cites are spelled out; the numbers
// themselves stay fixed to the whole list so they do not shift while typing.
func (p *picker) fitLegend(width, height int, hits []modelRow) []string {
	used := map[string]bool{}
	for _, row := range hits {
		used[row.endpoint] = true
	}
	room := height - 10
	for _, want := range []struct {
		path  bool
		spare int
	}{{true, 3}, {false, 3}, {false, 1}} {
		lines := p.legend(width, want.path, used)
		if len(lines) > 0 && room-len(lines)-1 >= want.spare {
			return lines
		}
	}
	return nil
}

func (m *model) openPicker() tea.Cmd {
	caret := cursor.New()
	caret.Style = styleInputCursor
	caret.SetChar(" ")
	rows := append([]modelRow(nil), m.rows...)
	sortModelRows(rows)
	m.picker = &picker{rows: rows, sources: collectSources(rows), caret: caret}
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
	legend := p.fitLegend(inner, m.height, hits)
	first, last := p.window(len(hits), m.height, legend, groupCount(hits))

	rows := []string{
		spread(styleText.Bold(true).Render("Select model"), styleDim.Render("esc"), inner),
		"",
		p.searchLine(inner),
		"",
	}
	if len(hits) == 0 {
		rows = append(rows, styleDim.Render("no model matches that"))
	}
	cols := p.columns(hits[first:last], inner, len(legend) > 0)
	previousGroup := ""
	for i := first; i < last; i++ {
		row := hits[i]
		if row.group != previousGroup {
			rows = append(rows, styleAccent.Bold(true).Render(row.group))
			previousGroup = row.group
		}
		current := row.id == m.currentModel && row.endpoint == m.endpoint
		cursor, dot := "  ", "  "
		if i == p.cursor {
			cursor = "› "
		}
		if current {
			dot = "● "
		}
		label := truncate(cursor+dot+cols.render(row, p.marker(row.endpoint)), inner)
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
	if len(legend) > 0 {
		rows = append(rows, "")
		rows = append(rows, legend...)
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
func (p *picker) window(count, height int, legend []string, groups int) (int, int) {
	chrome := 10
	if len(legend) > 0 {
		chrome += len(legend) + 1
	}
	rows := max(1, height-chrome-groups)
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

func groupCount(rows []modelRow) int {
	seen := make(map[string]bool)
	for _, row := range rows {
		seen[row.group] = true
	}
	return len(seen)
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

// columns lays out one screenful. The source column is a footnote marker, so
// it is only worth a column when the legend that explains it is on screen.
func (p *picker) columns(rows []modelRow, inner int, footnoted bool) pickerColumns {
	body := inner - 4
	var quant, ctxw, source int
	for _, row := range rows {
		quant = max(quant, lipgloss.Width(ifEmpty(row.quant, "?")))
		ctxw = max(ctxw, lipgloss.Width(formatContext(row.ctx)))
		if footnoted {
			source = max(source, lipgloss.Width(p.marker(row.endpoint)))
		}
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

func (c pickerColumns) render(row modelRow, source string) string {
	out := pad(truncate(row.label, c.name), c.name)
	if c.quant > 0 {
		out += strings.Repeat(" ", pickerGap) + pad(truncate(ifEmpty(row.quant, "?"), c.quant), c.quant)
	}
	if c.ctx > 0 {
		out += strings.Repeat(" ", pickerGap) + pad(formatContext(row.ctx), c.ctx)
	}
	if c.source > 0 {
		out += strings.Repeat(" ", pickerGap) + truncate(source, c.source)
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
