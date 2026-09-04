package switcher

import (
	"context"
	"encoding/json"
	"errors"
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
	}, &fakeBackend{}, nil)
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
	server, err := New([]Model{{ID: "helper"}, {ID: "primary"}}, backend, nil)
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
	server, err := New([]Model{{ID: "helper"}}, backend, nil)
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

// The catalog reports weights so a caller can tell a model that serves now
// from one that downloads first.
func TestListModelsReportsWeightAvailability(t *testing.T) {
	available := map[string]Availability{
		"ready":      {Weights: WeightsPresent, SizeBytes: 400, OnDiskBytes: 400},
		"partial":    {Weights: WeightsMismatched, SizeBytes: 400, OnDiskBytes: 120},
		"unexpected": {Weights: WeightsMismatched, SizeBytes: 400, OnDiskBytes: 900},
		"absent":     {Weights: WeightsMissing, SizeBytes: 400},
	}
	server, err := New(
		[]Model{{ID: "ready"}, {ID: "partial"}, {ID: "unexpected"}, {ID: "absent"}},
		&fakeBackend{},
		func(id string) (Availability, error) { return available[id], nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			Weights     string `json:"weights"`
			SizeBytes   int64  `json:"size_bytes"`
			OnDiskBytes int64  `json:"on_disk_bytes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Data) != 4 {
		t.Fatalf("returned %d models", len(parsed.Data))
	}
	for _, entry := range parsed.Data {
		want := available[entry.ID]
		if entry.Weights != want.Weights || entry.SizeBytes != want.SizeBytes {
			t.Errorf("%s: weights=%q size=%d", entry.ID, entry.Weights, entry.SizeBytes)
		}
		if entry.OnDiskBytes != want.OnDiskBytes {
			t.Errorf("%s: onDisk = %d, want %d", entry.ID, entry.OnDiskBytes, want.OnDiskBytes)
		}
	}
	// A file bigger than declared is not an interrupted download, so a caller
	// comparing the two sizes can refuse to present it as resumable.
	var unexpected int64
	for _, entry := range parsed.Data {
		if entry.ID == "unexpected" {
			unexpected = entry.OnDiskBytes
		}
	}
	if unexpected <= 400 {
		t.Fatalf("unexpected on-disk size = %d, want greater than the declared 400", unexpected)
	}
}

// A missing file has no size to report, so the field is left out rather than
// sent as a zero a caller could read as an empty download.
func TestListModelsOmitsOnDiskBytesWhenWeightsAreMissing(t *testing.T) {
	server, err := New([]Model{{ID: "absent"}}, &fakeBackend{},
		func(string) (Availability, error) {
			return Availability{Weights: WeightsMissing, SizeBytes: 400}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if strings.Contains(recorder.Body.String(), "on_disk_bytes") {
		t.Fatalf("body carries on_disk_bytes: %s", recorder.Body.String())
	}
}

// Reporting "missing" for a model that is on disk would provoke a needless
// multi-gigabyte download, so a failed lookup fails the request.
func TestListModelsFailsWhenAvailabilityCannotBeRead(t *testing.T) {
	server, err := New([]Model{{ID: "ready"}}, &fakeBackend{},
		func(string) (Availability, error) { return Availability{}, errors.New("state directory unreadable") })
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

// Without an availability source the catalog still lists models, just without
// the weight fields, so an embedder that has no state root is not forced to
// invent one.
func TestListModelsOmitsWeightFieldsWithoutAvailability(t *testing.T) {
	server, err := New([]Model{{ID: "ready"}}, &fakeBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	body := recorder.Body.String()
	if strings.Contains(body, "weights") || strings.Contains(body, "size_bytes") {
		t.Fatalf("body carries weight fields: %s", body)
	}
}
