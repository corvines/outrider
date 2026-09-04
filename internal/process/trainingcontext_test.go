package process

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestTrainingContextNoteSaysNothingWhenTheyAgree(t *testing.T) {
	if note := trainingContextNote("tiny", 4096, 4096); note != "" {
		t.Fatalf("note = %q, want empty", note)
	}
}

// A backend that reports no training context has not answered, which is not
// the same as agreeing with the profile.
func TestTrainingContextNoteSaysNothingWithoutAnAnswer(t *testing.T) {
	if note := trainingContextNote("tiny", 4096, 0); note != "" {
		t.Fatalf("note = %q, want empty", note)
	}
}

func TestTrainingContextNoteNamesBothNumbers(t *testing.T) {
	note := trainingContextNote("tiny", 4096, 262144)
	if !strings.Contains(note, "tiny") ||
		!strings.Contains(note, "4096") || !strings.Contains(note, "262144") {
		t.Fatalf("note = %q", note)
	}
	if !strings.Contains(note, "caps its own context") {
		t.Fatalf("under-declaring should say what it costs: %q", note)
	}
}

func TestTrainingContextNoteReadsTheOtherDirection(t *testing.T) {
	note := trainingContextNote("tiny", 262144, 4096)
	if strings.Contains(note, "caps its own context") {
		t.Fatalf("over-declaring does not cap anything: %q", note)
	}
	if !strings.Contains(note, "larger") {
		t.Fatalf("note = %q", note)
	}
}

func TestNoteTrainingContextWritesToTheServerLog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/props":
				_, _ = response.Write([]byte(`{"default_generation_settings":{"n_ctx":4096}}`))
			case "/v1/models":
				_, _ = response.Write([]byte(
					`{"data":[{"id":"tiny","meta":{"n_ctx_train":262144}}]}`))
			default:
				http.NotFound(response, request)
			}
		}))
	defer server.Close()

	logPath := filepath.Join(t.TempDir(), "server.log")
	plan := manifest.Plan{Endpoint: server.URL}
	plan.Profile.ID = "tiny"
	plan.Profile.Context.Original = 4096
	plan.State.Log = logPath

	noteTrainingContext(context.Background(), plan)

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "262144") {
		t.Fatalf("log = %q", content)
	}
}

// An unreachable backend cannot answer, and a start that already succeeded
// must not be disturbed by that.
func TestNoteTrainingContextIgnoresAnUnreachableBackend(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "server.log")
	plan := manifest.Plan{Endpoint: "http://127.0.0.1:1"}
	plan.Profile.ID = "tiny"
	plan.Profile.Context.Original = 4096
	plan.State.Log = logPath

	noteTrainingContext(context.Background(), plan)

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("stat err = %v, want the log left untouched", err)
	}
}
