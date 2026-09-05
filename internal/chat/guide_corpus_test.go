package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The guide is only worth what a small model can get out of it, and that is a
// property of the two together. This asks a running model the questions a new
// user asks and grades the answers by substring, so an edit to the guide can
// be shown to help or hurt instead of argued about.
//
// It needs weights and a server, so it is skipped unless an endpoint is named:
//
//	OUTRIDER_CORPUS_ENDPOINT=http://127.0.0.1:11455 \
//	OUTRIDER_CORPUS_MODEL=qwen35-0.8b \
//	go test ./internal/chat/ -run TestGuideCorpus -v
type guideQuestion struct {
	name string
	ask  string
	// want is satisfied by any one of its entries, so a fact with two
	// reasonable phrasings does not count as a miss.
	want []string
	// deny is a miss on any entry. These are the confabulations seen in
	// practice, not every wrong answer imaginable.
	deny []string
}

var guideCorpus = []guideQuestion{
	{
		name: "opens the chat window",
		ask:  "How do I start the chat window?",
		want: []string{"outrider chat"},
		deny: []string{"--chat", "outrider tui"},
	},
	{
		name: "switches model from inside chat",
		ask:  "I am in the chat window. How do I switch to a different model without leaving it?",
		want: []string{"/model"},
		deny: []string{"/switch", "/load", "restart"},
	},
	{
		name: "names the port",
		ask:  "What port does outrider listen on?",
		want: []string{"11435"},
		deny: []string{"11434", "8080"},
	},
	{
		name: "picks a model for a small machine",
		ask:  "My Mac has 16 GB of memory. Which of the models should I run?",
		want: []string{"qwen35-0.8b", "qwen35-2b"},
		deny: []string{"run qwen35b-mtp", "run the qwen35b-mtp", "use qwen35b-mtp"},
	},
	{
		name: "checks before starting",
		ask:  "How can I find out whether a model will fit before I start it?",
		want: []string{"outrider check"},
		deny: []string{"outrider serve", "outrider pull"},
	},
	{
		name: "names the weights directory",
		ask:  "Where does outrider put the model files it downloads?",
		want: []string{"Library/Caches/Outrider/models"},
		deny: []string{".cache/outrider", "~/.outrider"},
	},
	{
		name: "refuses a config file that does not exist",
		ask:  "Which config file do I edit to add my own model to the list?",
		want: []string{"rebuild", "source", "compiled"},
		deny: []string{"profiles.json", "config.yaml", "config.json", "~/.config"},
	},
	{
		name: "rules out intel",
		ask:  "Will outrider run on my Intel MacBook Pro?",
		want: []string{"Apple silicon"},
		deny: []string{"yes, it", "Yes, it", "Rosetta"},
	},
	{
		name: "one model at a time",
		ask:  "Can I have two models loaded at the same time?",
		want: []string{"one model", "only one"},
		deny: []string{"yes, you can", "Yes, you can"},
	},
	{
		name: "shuts down",
		ask:  "How do I shut the model down and get my memory back?",
		want: []string{"outrider stop"},
		deny: []string{"kill", "Activity Monitor", "pkill"},
	},
	{
		name: "greets back",
		ask:  "hi how are you",
		want: []string{"outrider"},
		deny: []string{"i don't know", "i do not know", "cannot answer", "sorry"},
	},
	{
		// A greeting that recites the rules back is a template being copied,
		// and it reads as a machine explaining its own paperwork.
		name: "greets without reading out its rules",
		ask:  "hi",
		want: []string{"outrider"},
		deny: []string{"from the guide", "i will answer", "headings",
			"quote commands", "cannot see"},
	},
	{
		name: "takes thanks without reciting rules",
		ask:  "thanks!",
		want: []string{"outrider", "welcome", "glad"},
		deny: []string{"from the guide", "i will answer", "quote commands"},
	},
	{
		name: "says what it covers",
		ask:  "what do you know",
		want: []string{"outrider"},
		deny: []string{"i don't know", "i do not know", "cannot answer", "sorry"},
	},
	{
		name: "answers a question worded casually",
		ask:  "so how do i get a model running",
		want: []string{"outrider serve"},
		deny: []string{"i don't know", "i do not know", "cannot answer"},
	},
	{
		name: "still refuses a question about the world",
		ask:  "What is the capital of France?",
		want: []string{"don't know", "do not know", "not in", "only answer", "about outrider"},
		deny: []string{"paris"},
	},
	{
		name: "refuses a question about the machine",
		ask:  "What is my OPENAI_API_KEY set to right now?",
		want: []string{"do not know", "don't know", "cannot see", "can't see", "no access"},
		deny: []string{"sk-", "export OPENAI"},
	},
	{
		name: "refuses training",
		ask:  "How do I fine-tune a model with outrider?",
		want: []string{"not", "cannot", "does not"},
		deny: []string{"outrider train", "outrider finetune", "--train", "--lora"},
	},
}

type corpusChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

type corpusResponse struct {
	Choices []corpusChoice `json:"choices"`
}

func TestGuideCorpus(t *testing.T) {
	endpoint := os.Getenv("OUTRIDER_CORPUS_ENDPOINT")
	if endpoint == "" {
		t.Skip("set OUTRIDER_CORPUS_ENDPOINT to grade the guide against a running model")
	}
	target := os.Getenv("OUTRIDER_CORPUS_MODEL")
	if target == "" {
		t.Fatal("OUTRIDER_CORPUS_MODEL must name the model to grade")
	}

	text := repositoryGuide(t)
	passed := 0
	for _, question := range guideCorpus {
		answer, err := askGuide(endpoint, target, question.ask, text)
		if err != nil {
			t.Fatalf("%s: %v", question.name, err)
		}
		if reason := gradeAnswer(question, answer); reason != "" {
			t.Errorf("%s: %s\nQ: %s\nA: %s", question.name, reason, question.ask, answer)
			continue
		}
		passed++
		t.Logf("pass %-40s %s", question.name, firstLine(answer))
	}
	t.Logf("score %d/%d", passed, len(guideCorpus))
}

func gradeAnswer(question guideQuestion, answer string) string {
	folded := strings.ToLower(answer)
	for _, denied := range question.deny {
		if strings.Contains(folded, strings.ToLower(denied)) {
			return fmt.Sprintf("says %q", denied)
		}
	}
	for _, wanted := range question.want {
		if strings.Contains(folded, strings.ToLower(wanted)) {
			return ""
		}
	}
	return fmt.Sprintf("says none of %v", question.want)
}

// askGuide sends exactly what help mode sends, so a score here is a score for
// the shipped path rather than for a prompt that only the test uses.
func askGuide(endpoint string, target string, ask string, text string) (string, error) {
	zero := 0.0
	body, err := json.Marshal(completionRequest{
		Model: target,
		Messages: []requestMessage{
			{Role: "system", Content: guidePrompt(text)},
			{Role: roleUser, Content: ask},
		},
		Temperature:    &zero,
		TemplateKwargs: map[string]any{"enable_thinking": false},
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Post(
		strings.TrimSuffix(endpoint, "/")+"/v1/chat/completions",
		"application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", res.StatusCode)
	}

	var decoded corpusResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("no choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if index := strings.IndexByte(trimmed, '\n'); index >= 0 {
		trimmed = trimmed[:index]
	}
	if len(trimmed) > 90 {
		trimmed = trimmed[:90] + "..."
	}
	return trimmed
}
