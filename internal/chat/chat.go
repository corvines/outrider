package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

const defaultEndpoint = "http://127.0.0.1:11435"

// Run executes the chat TUI.
func Run(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = defaultEndpoint
	}
	if err := checkEndpoint(endpoint); err != nil {
		return err
	}
	_, err := tea.NewProgram(New(RunOptions{Endpoint: endpoint})).Run()
	if err != nil {
		return err
	}
	return nil
}

// RunOptions configures a chat session.
type RunOptions struct {
	Endpoint string
}

type message struct {
	role      string
	content   string
	reasoning string
}

type modelDef struct {
	ID            string `json:"id"`
	Quantization  string `json:"quantization"`
	ContextWindow int    `json:"context_window"`
}

type modelsResp struct {
	Data []modelDef `json:"data"`
}

type requestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type timingsResp struct {
	PromptN            *int     `json:"prompt_n"`
	PredictedN         *int     `json:"predicted_n"`
	PromptPerSecond    *float64 `json:"prompt_per_second"`
	PredictedPerSecond *float64 `json:"predicted_per_second"`
}

type streamChoiceDelta struct {
	Content       string `json:"content"`
	Reasoning     string `json:"reasoning"`
	ReasoningText string `json:"reasoning_content"`
}

type streamChoice struct {
	Delta streamChoiceDelta `json:"delta"`
}

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
	Timings *timingsResp   `json:"timings"`
	Usage   struct {
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type endpointUnreachable struct {
	url string
	err error
}

func (e endpointUnreachable) Error() string {
	return fmt.Sprintf("cannot reach endpoint %s\nrun `outrider up` first", e.url)
}

func checkEndpoint(endpoint string) error {
	req, err := http.NewRequest(http.MethodGet, endpoint+"/v1/models", nil)
	if err != nil {
		return endpointUnreachable{url: endpoint, err: err}
	}
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return endpointUnreachable{url: endpoint, err: err}
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return endpointUnreachable{url: endpoint, err: fmt.Errorf("status %d", res.StatusCode)}
	}
	return nil
}

type endpointMsg struct {
	models map[string]modelDef
	err    error
}

type streamMsg struct {
	chunk  string
	reason string
	timing *timingsResp
	err    error
	done   bool
	ms     int
}

type model struct {
	width, height int
	endpoint      string
	textarea      textarea.Model
	models        map[string]modelDef
	currentModel  string
	quantization  string
	contextWindow int

	messages []message
	turns    int

	promptHistory []string
	historyIndex  int

	streamActive   bool
	streamAborting bool
	promptCancel   context.CancelFunc

	streamCh chan streamMsg

	showReasoning bool
	viewOffset    int

	totalOutputTokens    int
	lastTurnOutputTokens int
	lastPromptTokens     int
	promptPerSecond      *float64
	predictedPerSecond   *float64
	lastWallMs           int

	activityPrefix string

	status1 string
	status2 string

	renderedLines []string
	runningError  error
}

func New(opts RunOptions) *model {
	if strings.TrimSpace(opts.Endpoint) == "" {
		opts.Endpoint = defaultEndpoint
	}
	ta := textarea.New()
	ta.SetHeight(4)
	ta.ShowLineNumbers = false
	ta.Focus()
	ta.SetWidth(72)
	return &model{
		width:         80,
		height:        24,
		endpoint:      opts.Endpoint,
		textarea:      ta,
		models:        map[string]modelDef{},
		quantization:  "?",
		contextWindow: 32768,
		historyIndex:  -1,
		streamCh:      make(chan streamMsg, 16),
		showReasoning: true,
	}
}

func (m *model) Init() tea.Cmd {
	return fetchModels(m.endpoint)
}

func fetchModels(endpoint string) tea.Cmd {
	return func() tea.Msg {
		res, err := http.Get(endpoint + "/v1/models")
		if err != nil {
			return endpointMsg{err: endpointUnreachable{url: endpoint, err: err}}
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			return endpointMsg{err: endpointUnreachable{url: endpoint, err: fmt.Errorf("status %d", res.StatusCode)}}
		}
		b, err := io.ReadAll(res.Body)
		if err != nil {
			return endpointMsg{err: endpointUnreachable{url: endpoint, err: err}}
		}
		var data modelsResp
		if err := json.Unmarshal(b, &data); err != nil {
			return endpointMsg{err: endpointUnreachable{url: endpoint, err: err}}
		}
		m := map[string]modelDef{}
		for _, item := range data.Data {
			m[item.ID] = item
		}
		return endpointMsg{models: m}
	}
}

func (m *model) View() string {
	m.rebuildTranscripts()
	lines := strings.Split(strings.TrimRight(strings.Join(m.renderedLines, "\n"), "\n"), "\n")
	vHeight := m.viewHeight()
	if len(lines) > vHeight {
		if m.viewOffset < 0 {
			m.viewOffset = 0
		}
		max := len(lines) - vHeight
		if m.viewOffset > max {
			m.viewOffset = max
		}
		lines = lines[m.viewOffset : m.viewOffset+vHeight]
	}
	transcript := strings.Join(lines, "\n")
	b := strings.Builder{}
	b.WriteString(transcript)
	b.WriteString("\n")
	b.WriteString(m.status1)
	b.WriteString("\n")
	b.WriteString(m.status2)
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString(indentLines(m.textarea.View(), 2))
	if m.runningError != nil {
		b.WriteString("\n")
		b.WriteString(m.runningError.Error())
	}
	return b.String()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = x.Width
		m.height = x.Height
		m.textarea.SetWidth(max(20, m.width-4))
		m.rebuildStatus()
		return m, nil
	case endpointMsg:
		if x.err != nil {
			m.runningError = x.err
			m.rebuildStatus()
			return m, tea.Quit
		}
		m.models = x.models
		if m.currentModel == "" {
			for name, modelData := range m.models {
				m.currentModel = name
				if modelData.Quantization != "" {
					m.quantization = modelData.Quantization
				}
				if modelData.ContextWindow != 0 {
					m.contextWindow = modelData.ContextWindow
				}
				break
			}
		}
		m.rebuildStatus()
		return m, nil
	case streamMsg:
		return m.applyStreamMsg(x)
	case tea.KeyMsg:
		return m.applyKey(x)
	}
	return m, nil
}

func (m *model) applyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		if strings.TrimSpace(m.textarea.Value()) != "" {
			m.textarea.SetValue("")
			return m, nil
		}
		if m.streamActive {
			if !m.streamAborting {
				m.streamAborting = true
				m.abortStream()
				return m, nil
			}
			return m, tea.Quit
		}
		return m, tea.Quit
	}

	if msg.Type == tea.KeyEsc {
		if m.streamActive && !m.streamAborting {
			m.streamAborting = true
			m.abortStream()
		}
		return m, nil
	}

	if msg.Type == tea.KeyEnter && msg.Alt {
		m.textarea.InsertString("\n")
		return m, nil
	}

	if msg.Type == tea.KeyEnter {
		return m, m.submitPrompt(m.textarea.Value())
	}

	if msg.Type == tea.KeyUp {
		if strings.TrimSpace(m.textarea.Value()) == "" || m.historyIndex >= 0 {
			if len(m.promptHistory) > 0 {
				if m.historyIndex < len(m.promptHistory)-1 {
					m.historyIndex++
				}
				m.textarea.SetValue(m.promptHistory[len(m.promptHistory)-1-m.historyIndex])
			}
		}
		return m, nil
	}

	if msg.Type == tea.KeyDown {
		if strings.TrimSpace(m.textarea.Value()) == "" || m.historyIndex >= 0 {
			if m.historyIndex <= 0 {
				m.historyIndex = -1
				m.textarea.SetValue("")
			} else {
				m.historyIndex--
				m.textarea.SetValue(m.promptHistory[len(m.promptHistory)-1-m.historyIndex])
			}
		}
		return m, nil
	}

	if msg.Type == tea.KeyCtrlO {
		m.showReasoning = !m.showReasoning
		m.rebuildTranscripts()
		return m, nil
	}

	if msg.Type == tea.KeyCtrlUp {
		m.scrollLines(1)
		return m, nil
	}
	if msg.Type == tea.KeyCtrlDown {
		m.scrollLines(-1)
		return m, nil
	}
	if msg.Type == tea.KeyCtrlU {
		m.scrollLines(max(1, m.viewHeight()/2))
		return m, nil
	}
	if msg.Type == tea.KeyCtrlD {
		m.scrollLines(-max(1, m.viewHeight()/2))
		return m, nil
	}
	if msg.Type == tea.KeyCtrlEnd {
		m.followBottom()
		return m, nil
	}

	m.textarea, _ = m.textarea.Update(msg)
	return m, nil
}

func (m *model) submitPrompt(raw string) tea.Cmd {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	m.textarea.SetValue("")

	if text == "/exit" {
		return tea.Quit
	}

	if text == "/clear" {
		m.messages = nil
		m.turns = 0
		m.totalOutputTokens = 0
		m.lastPromptTokens = 0
		m.promptPerSecond = nil
		m.predictedPerSecond = nil
		m.lastWallMs = 0
		m.rebuildStatus()
		m.rebuildTranscripts()
		m.promptHistory = append(m.promptHistory, text)
		if len(m.promptHistory) > 100 {
			m.promptHistory = m.promptHistory[len(m.promptHistory)-100:]
		}
		m.historyIndex = -1
		return nil
	}

	if strings.HasPrefix(text, "/model") {
		parts := strings.Fields(text)
		if len(parts) == 1 {
			var names []string
			for name := range m.models {
				names = append(names, name)
			}
			m.messages = append(m.messages, message{role: "assistant", content: "models " + strings.Join(names, " ")})
			m.rebuildTranscripts()
			m.historyIndex = -1
			m.promptHistory = append(m.promptHistory, text)
			return fetchModels(m.endpoint)
		}
		target := parts[1]
		if info, ok := m.models[target]; ok {
			m.currentModel = target
			if info.Quantization != "" {
				m.quantization = info.Quantization
			}
			if info.ContextWindow != 0 {
				m.contextWindow = info.ContextWindow
			}
			m.activityPrefix = "Loading " + target
			m.rebuildStatus()
			m.messages = nil
			m.turns = 0
			m.totalOutputTokens = 0
			m.lastPromptTokens = 0
			m.promptPerSecond = nil
			m.predictedPerSecond = nil
			m.lastWallMs = 0
			m.activityPrefix = ""
			m.rebuildStatus()
			m.rebuildTranscripts()
			m.rebuildStatus()
		} else {
			m.messages = append(m.messages, message{role: "assistant", content: "model not found: " + target})
			m.rebuildTranscripts()
		}
		m.historyIndex = -1
		m.promptHistory = append(m.promptHistory, text)
		if len(m.promptHistory) > 100 {
			m.promptHistory = m.promptHistory[len(m.promptHistory)-100:]
		}
		return nil
	}

	m.promptHistory = append(m.promptHistory, text)
	if len(m.promptHistory) > 100 {
		m.promptHistory = m.promptHistory[len(m.promptHistory)-100:]
	}
	m.historyIndex = -1

	m.messages = append(m.messages, message{role: "user", content: text})
	m.messages = append(m.messages, message{role: "assistant", content: ""})
	m.turns++
	m.streamActive = true
	m.streamAborting = false
	m.activityPrefix = "Generating"
	m.runningError = nil
	m.rebuildStatus()
	m.rebuildTranscripts()
	m.followBottom()

	go m.streamResponse(text)
	return func() tea.Msg {
		m2 := m
		return <-m2.streamCh
	}
}

func (m *model) streamResponse(prompt string) {
	if m.currentModel == "" {
		for name := range m.models {
			m.currentModel = name
			break
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.promptCancel = cancel
	payload := struct {
		Model    string           `json:"model"`
		Messages []requestMessage `json:"messages"`
		Stream   bool             `json:"stream"`
	}{}
	payload.Model = m.currentModel
	payload.Stream = true
	for _, msg := range m.messages {
		if msg.role == "user" || msg.role == "assistant" {
			payload.Messages = append(payload.Messages, requestMessage{Role: msg.role, Content: msg.content})
		}
	}
	payload.Messages = append(payload.Messages, requestMessage{Role: "user", Content: prompt})

	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint+"/v1/chat/completions", bytes.NewBuffer(buf))
	if err != nil {
		m.streamCh <- streamMsg{err: err}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		m.streamCh <- streamMsg{err: err}
		return
	}
	defer res.Body.Close()
	r := bufio.NewReader(res.Body)
	seenTiming := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			m.streamCh <- streamMsg{err: err}
			return
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" || ch.Delta.Reasoning != "" || ch.Delta.ReasoningText != "" {
				m.streamCh <- streamMsg{
					chunk:  ch.Delta.Content,
					reason: strings.TrimSpace(ch.Delta.Reasoning + ch.Delta.ReasoningText),
				}
			}
		}
		if chunk.Timings != nil {
			seenTiming = true
			m.streamCh <- streamMsg{timing: chunk.Timings}
		}
		_ = chunk.Usage.CompletionTokens
	}
	ms := int(time.Since(start).Milliseconds())
	if !seenTiming {
		m.streamCh <- streamMsg{timing: &timingsResp{}}
	}
	m.streamCh <- streamMsg{done: true, ms: ms}
}

func (m *model) applyStreamMsg(x streamMsg) (tea.Model, tea.Cmd) {
	if x.err != nil {
		m.streamActive = false
		m.streamAborting = false
		m.activityPrefix = ""
		m.runningError = x.err
		m.rebuildStatus()
		m.streamCh = make(chan streamMsg, 16)
		return m, nil
	}
	if x.chunk != "" {
		idx := len(m.messages) - 1
		m.messages[idx].content += x.chunk
		m.rebuildTranscripts()
		m.followBottom()
		return m, func() tea.Msg { return <-m.streamCh }
	}
	if x.reason != "" {
		idx := len(m.messages) - 1
		m.messages[idx].reasoning += x.reason + " "
		m.rebuildTranscripts()
		m.followBottom()
		return m, func() tea.Msg { return <-m.streamCh }
	}
	if x.timing != nil {
		if x.timing.PromptN == nil {
			m.promptPerSecond = nil
			m.predictedPerSecond = nil
			m.lastPromptTokens = 0
			m.lastTurnOutputTokens = 0
		} else {
			if x.timing.PromptN != nil {
				m.lastPromptTokens = *x.timing.PromptN
			}
			if x.timing.PredictedN != nil {
				m.lastTurnOutputTokens = *x.timing.PredictedN
			}
			if x.timing.PromptPerSecond != nil {
				m.promptPerSecond = x.timing.PromptPerSecond
			}
			if x.timing.PredictedPerSecond != nil {
				m.predictedPerSecond = x.timing.PredictedPerSecond
			}
			m.totalOutputTokens += m.lastTurnOutputTokens
		}
		m.rebuildStatus()
		return m, func() tea.Msg { return <-m.streamCh }
	}
	if x.done {
		m.streamActive = false
		m.streamAborting = false
		m.activityPrefix = ""
		m.lastWallMs = x.ms
		m.rebuildStatus()
		m.followBottom()
		m.streamCh = make(chan streamMsg, 16)
		return m, nil
	}
	return m, func() tea.Msg { return <-m.streamCh }
}

func (m *model) abortStream() {
	if m.promptCancel != nil {
		m.promptCancel()
	}
}

func (m *model) rebuildTranscripts() {
	var b strings.Builder
	for i, msg := range m.messages {
		if i > 0 {
			b.WriteString("\n\n│\n\n")
		}
		b.WriteString(msg.role)
		b.WriteString(":\n")
		if msg.content != "" {
			b.WriteString("│ ")
			b.WriteString(strings.ReplaceAll(msg.content, "\n", "\n│ "))
			b.WriteString("\n")
		}
		if msg.role == "assistant" && msg.reasoning != "" {
			if m.showReasoning {
				b.WriteString("│ reasoning: ")
				b.WriteString(msg.reasoning)
				b.WriteString("\n")
			} else {
				b.WriteString("│ ···\n")
			}
		}
	}
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		m.renderedLines = []string{" "}
	} else {
		m.renderedLines = lines
	}
}

func (m *model) rebuildStatus() {
	model := ifEmpty(m.currentModel, "?")
	quant := ifEmpty(m.quantization, "?")
	ctxUsed := "?"
	ctxWindow := "?"
	if m.contextWindow > 0 {
		ctxWindow = formatInt(m.contextWindow)
		if m.lastPromptTokens > 0 || m.lastTurnOutputTokens > 0 {
			ctxUsed = formatInt(m.lastPromptTokens + m.lastTurnOutputTokens)
		}
	}
	decode := "?"
	prefill := "?"
	if m.predictedPerSecond != nil {
		decode = fmt.Sprintf("%.1f", *m.predictedPerSecond)
	}
	if m.promptPerSecond != nil {
		prefill = fmt.Sprintf("%.1f", *m.promptPerSecond)
	}
	prefix := ""
	if m.activityPrefix != "" {
		prefix = "● " + m.activityPrefix + " · "
	}
	m.status1 = fmt.Sprintf("%s%s · %s · ctx %s/%s · %s tok/s · %s prompt tok/s", prefix, model, quant, ctxUsed, ctxWindow, decode, prefill)
	m.status2 = fmt.Sprintf("%s · %s output tokens · %s ms · %s", formatTurns(m.turns), formatInt(m.totalOutputTokens), formatInt(m.lastWallMs), endpointHost(m.endpoint))
}

func (m *model) scrollLines(n int) {
	total := len(strings.Split(strings.Join(m.renderedLines, "\n"), "\n"))
	maxOffset := max(0, total-m.viewHeight())
	m.viewOffset -= n
	if m.viewOffset < 0 {
		m.viewOffset = 0
	}
	if m.viewOffset > maxOffset {
		m.viewOffset = maxOffset
	}
}

func (m *model) followBottom() {
	m.viewOffset = max(0, len(strings.Split(strings.Join(m.renderedLines, "\n"), "\n"))-m.viewHeight())
}

func (m *model) viewHeight() int {
	return max(3, m.height-9)
}

func (m *model) scrollPageHalf() {
}

func formatTurns(count int) string {
	if count == 1 {
		return "1 turn"
	}
	return formatInt(count) + " turns"
}

func formatInt(v int) string {
	if v == 0 {
		return "0"
	}
	s := strconv.Itoa(v)
	if len(s) < 4 {
		return s
	}
	a := make([]byte, 0, len(s)+len(s)/3)
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			a = append(a, ',')
		}
		a = append(a, s[i])
	}
	return string(a)
}

func endpointHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}

func ifEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func indentLines(text string, width int) string {
	pad := strings.Repeat(" ", width)
	return pad + strings.ReplaceAll(text, "\n", "\n"+pad)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
