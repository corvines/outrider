package switcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeBackend struct {
	endpoint string
	mu       sync.Mutex
	models   []string
}

func (backend *fakeBackend) Ensure(_ context.Context, model string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.models = append(backend.models, model)
	return backend.endpoint, nil
}

func TestListsEverySwitchableModel(t *testing.T) {
	server, err := New([]Model{
		{ID: "helper", ContextWindow: 16384, TrainingContext: 131072, Quantization: "Q4_K_M"},
		{ID: "primary", ContextWindow: 32768, TrainingContext: 131072, Quantization: "Q4_K_M"},
	}, &fakeBackend{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body struct {
		Data []struct {
			ID   string `json:"id"`
			Meta struct {
				Context int `json:"n_ctx"`
			} `json:"meta"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Data[0].ID != "helper" || body.Data[0].Meta.Context != 16384 {
		t.Fatalf("models = %#v", body.Data)
	}
}

func TestRoutesUsingRequestedModelAndPreservesIdentity(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("data: backend\n\n"))
	}))
	t.Cleanup(upstream.Close)
	backend := &fakeBackend{endpoint: upstream.URL}
	server, err := New([]Model{{ID: "helper"}, {ID: "primary"}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"primary","stream":true}`),
	)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Outrider-Model") != "primary" {
		t.Fatalf("status = %d, model = %q", response.Code, response.Header().Get("X-Outrider-Model"))
	}
	if !response.Flushed {
		t.Fatal("stream was not flushed")
	}
	if len(backend.models) != 1 || backend.models[0] != "primary" {
		t.Fatalf("ensured models = %v", backend.models)
	}
}

func TestRejectsUnknownModelWithoutCallingBackend(t *testing.T) {
	backend := &fakeBackend{}
	server, err := New([]Model{{ID: "helper"}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"stale-model"}`),
	)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || len(backend.models) != 0 {
		t.Fatalf("status = %d, ensured models = %v", response.Code, backend.models)
	}
}
