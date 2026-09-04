package endpoint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const propsPayload = `{
  "default_generation_settings": {
    "n_ctx": 16384,
    "params": {
      "temperature": 0.6,
      "top_p": 0.95,
      "top_k": 20,
      "min_p": 0.0,
      "repeat_penalty": 1.05,
      "samplers": ["penalties", "dry", "top_n_sigma", "top_k", "typ_p", "top_p", "min_p", "xtc", "temperature"],
      "speculative.types": "none"
    }
  },
  "total_slots": 1,
  "model_ftype": "Q4_K - Medium",
  "model_path": "/models/qwen.gguf",
  "build_info": "b10516-b95502ba9",
  "modalities": {"vision": true, "audio": false, "video": false},
  "chat_template_caps": {"supports_tools": true}
}`

func propsServer(t *testing.T, models string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/props", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(propsPayload))
	})
	mux.HandleFunc("/v1/models", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(models))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

// The training context is reported only by the model listing, so a resolved
// reading is assembled from two routes rather than one.
func TestFetchResolvedReadsBothRoutes(t *testing.T) {
	url := propsServer(t, `{"data":[{"id":"qwen","meta":{"n_ctx":16384,"n_ctx_train":262144}}]}`)
	resolved, err := FetchResolved(context.Background(), url, "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Context != 16384 || resolved.TrainingContext != 262144 {
		t.Fatalf("context = %d/%d", resolved.Context, resolved.TrainingContext)
	}
	if resolved.Build != "b10516-b95502ba9" || resolved.Quantization != "Q4_K - Medium" {
		t.Fatalf("build = %q, quantization = %q", resolved.Build, resolved.Quantization)
	}
	if !resolved.Modalities.Vision || resolved.Modalities.Audio {
		t.Fatalf("modalities = %#v", resolved.Modalities)
	}
	if !resolved.SupportsTools {
		t.Fatal("supports_tools was dropped")
	}
	if len(resolved.Samplers) != 9 || resolved.Samplers[0] != "penalties" {
		t.Fatalf("samplers = %v", resolved.Samplers)
	}
	if resolved.Slots != 1 || resolved.Sampling.TopK != 20 {
		t.Fatalf("slots = %d, top_k = %d", resolved.Slots, resolved.Sampling.TopK)
	}
}

// The backend says "none" where a profile says nothing. Both mean no draft
// path, and a caller comparing the two halves should see them agree.
func TestFetchResolvedEmptiesTheBackendsWordForNoSpeculation(t *testing.T) {
	url := propsServer(t, `{"data":[{"id":"qwen","meta":{"n_ctx_train":262144}}]}`)
	resolved, err := FetchResolved(context.Background(), url, "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Speculation != "" {
		t.Fatalf("speculation = %q", resolved.Speculation)
	}
}

// A listing that does not carry the model leaves the fields it owns unset
// rather than borrowing another model's.
func TestFetchResolvedIgnoresOtherModelsInTheListing(t *testing.T) {
	url := propsServer(t, `{"data":[{"id":"other","meta":{"n_ctx_train":8192}}]}`)
	resolved, err := FetchResolved(context.Background(), url, "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TrainingContext != 0 {
		t.Fatalf("training context = %d", resolved.TrainingContext)
	}
}

func TestFetchResolvedFailsOnAnUnreachableBackend(t *testing.T) {
	if _, err := FetchResolved(context.Background(), "http://127.0.0.1:1", "qwen"); err == nil {
		t.Fatal("expected an error")
	}
}
