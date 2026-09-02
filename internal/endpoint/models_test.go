package endpoint

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestVerifyModelContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"data": []any{map[string]any{
			"id": "qwen3-1.7b", "meta": map[string]any{"n_ctx": 32768, "n_ctx_train": 40960},
		}}})
	}))
	defer server.Close()
	profile, _ := manifest.Get("qwen3-1.7b")
	contract, err := VerifyModelContract(context.Background(), server.URL, profile)
	if err != nil {
		t.Fatal(err)
	}
	if contract.ModelID != profile.ID || contract.LoadedContext != 32768 || contract.TrainingContext != 40960 || contract.MTPEnabled {
		t.Fatalf("contract = %#v", contract)
	}
}

func TestVerifyModelContractRejectsWrongLoadedContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{"data": []any{map[string]any{
			"id": "qwen3-1.7b", "meta": map[string]any{"n_ctx": 4096},
		}}})
	}))
	defer server.Close()
	profile, _ := manifest.Get("qwen3-1.7b")
	_, err := VerifyModelContract(context.Background(), server.URL, profile)
	if err == nil || !strings.Contains(err.Error(), "loaded context is 4096") {
		t.Fatalf("error = %v", err)
	}
}
