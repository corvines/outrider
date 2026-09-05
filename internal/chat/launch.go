package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// chatMode is what the session was opened for. The two modes send different
// prompts, so the choice is made before the first message rather than after.
type chatMode int

const (
	modeUnset chatMode = iota
	modeChat
	modeHelp
)

type launchChoice struct {
	mode    chatMode
	title   string
	detail  string
	waiting string
}

var launchChoices = []launchChoice{
	{
		mode:    modeHelp,
		title:   "[ QA ] Ask about outrider",
		detail:  "Answers from outrider's guide and nothing else.",
		waiting: "Answering",
	},
	{
		mode:    modeChat,
		title:   "[ Debug: bare ] Talk to the model",
		detail:  "No system prompt. Whichever model is loaded, as it comes.",
		waiting: "Generating",
	},
}

// choiceFor finds a mode's entry, so the order above is presentation and
// nothing reads a mode by its position in the list.
func choiceFor(mode chatMode) launchChoice {
	for _, choice := range launchChoices {
		if choice.mode == mode {
			return choice
		}
	}
	return launchChoices[0]
}

// guidePrompt is the instruction that turns whatever model is loaded into an
// answer surface for the guide. It is deliberately closed: a small model asked
// about a tool it has never seen will invent commands unless told not to.
func guidePrompt(guide string) string {
	return strings.Join([]string{
		"You answer questions about Outrider, a program that runs a language",
		"model locally on a Mac. The guide below is everything you know about",
		"it. Answer only from the guide. Quote its commands exactly. If the",
		"guide does not answer the question, say so and suggest nothing.",
		"Never invent a command, a flag, or a file path. Keep answers",
		"short.",
		"Answer a greeting with a greeting, then offer to answer questions",
		"about outrider.",
		"You cannot see the person's files, accounts, or machine. Say so if",
		"they ask about those, and do not mention it otherwise.",
		"List the guide's headings if you are asked what you know.",
		"",
		"--- OUTRIDER GUIDE ---",
		guide,
		"--- END GUIDE ---",
	}, "\n")
}

func (m *model) chooseMode(choice launchChoice) {
	m.mode = choice.mode
	m.waitingWord = choice.waiting
	m.rebuildStatus()
	m.rebuildTranscripts()
}

// applyLaunchKey runs the startup choice. Every other key is swallowed, so a
// stray keystroke cannot land in a session whose mode is not settled yet.
func (m *model) applyLaunchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "up", "k":
		m.launchCursor = max(0, m.launchCursor-1)
	case "down", "j":
		m.launchCursor = min(len(launchChoices)-1, m.launchCursor+1)
	case "enter", " ":
		m.chooseMode(launchChoices[m.launchCursor])
	case "1":
		m.chooseMode(launchChoices[0])
	case "2":
		m.chooseMode(launchChoices[1])
	}
	return m, nil
}

func (m *model) launchView() string {
	width := max(24, min(64, m.width-6))
	inner := width - 4
	// Border and padding cost four lines before any content.
	roomy := m.launchRows(inner, true)
	rows := roomy
	if len(roomy)+4 > m.height {
		rows = m.launchRows(inner, false)
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colDim).
		Padding(1, 2).
		Width(width).
		Render(strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

// launchRows builds the panel body. What each mode is for is the first thing
// dropped when the terminal is short, because the two titles already say it.
func (m *model) launchRows(inner int, detailed bool) []string {
	rows := []string{styleText.Bold(true).Render("outrider")}
	if detailed {
		rows = append(rows, "", styleDim.Render(truncate("What do you want this session for?", inner)))
	}
	rows = append(rows, "")
	for index, choice := range launchChoices {
		label := truncate(" "+choice.title+" ", inner)
		if index == m.launchCursor {
			rows = append(rows, lipgloss.NewStyle().
				Foreground(colBlock).Background(colAccent).
				Width(inner).Render("›"+label))
		} else {
			rows = append(rows, styleText.Render(" "+label))
		}
		if detailed {
			rows = append(rows, wrap(styleDim, "   "+choice.detail, inner)...)
			rows = append(rows, "")
		}
	}
	if detailed {
		return append(rows, styleDim.Render(truncate("↑↓ move · ⏎ choose · esc quit", inner)))
	}
	return append(rows, "", styleDim.Render(truncate("⏎ choose · esc quit", inner)))
}
