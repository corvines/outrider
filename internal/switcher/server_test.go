package switcher

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/corvines/outrider/internal/catalog"
	"github.com/corvines/outrider/internal/endpoint"
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
	server, err := New([]catalog.Entry{
		{ID: "helper", Context: 16384, TrainingContext: 131072, Quant: "Q4_K_M"},
		{ID: "primary", Context: 32768, TrainingContext: 131072, Quant: "Q4_K_M"},
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
	server, err := New([]catalog.Entry{{ID: "helper"}, {ID: "primary"}}, backend, nil)
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
	server, err := New([]catalog.Entry{{ID: "helper"}}, backend, nil)
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
	available := map[string]catalog.Weights{
		"ready":      {State: catalog.WeightsPresent, DeclaredBytes: 400, OnDiskBytes: 400},
		"partial":    {State: catalog.WeightsMismatched, DeclaredBytes: 400, OnDiskBytes: 120},
		"unexpected": {State: catalog.WeightsMismatched, DeclaredBytes: 400, OnDiskBytes: 900},
		"absent":     {State: catalog.WeightsMissing, DeclaredBytes: 400},
	}
	server, err := New(
		[]catalog.Entry{{ID: "ready"}, {ID: "partial"}, {ID: "unexpected"}, {ID: "absent"}},
		&fakeBackend{},
		func(id string) (catalog.Weights, error) { return available[id], nil },
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
		if entry.Weights != want.State || entry.SizeBytes != want.DeclaredBytes {
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
	server, err := New([]catalog.Entry{{ID: "absent"}}, &fakeBackend{},
		func(string) (catalog.Weights, error) {
			return catalog.Weights{State: catalog.WeightsMissing, DeclaredBytes: 400}, nil
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
	server, err := New([]catalog.Entry{{ID: "ready"}}, &fakeBackend{},
		func(string) (catalog.Weights, error) {
			return catalog.Weights{}, errors.New("state directory unreadable")
		})
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
	server, err := New([]catalog.Entry{{ID: "ready"}}, &fakeBackend{}, nil)
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

// The standard listing is the only route a client should need. A model that
// only outrider knows how to describe is described there, not on a second
// route the client has to learn.
func TestListModelsCarriesTheFactsAModelChoiceNeeds(t *testing.T) {
	entry := catalog.Entry{
		ID: "primary", Description: "35B primary local agent",
		Capabilities: []string{catalog.CapabilityCompletion, catalog.CapabilityVision},
		Repo:         "org/model-GGUF", File: "model-Q4_K_M.gguf", Quant: "Q4_K_M",
		Context: 32768, TrainingContext: 131072, MinMemoryMiB: 32768, Speculation: "mtp",
	}
	server, err := New([]catalog.Entry{entry}, &fakeBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var parsed struct {
		Data []modelEntry `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Data) != 1 {
		t.Fatalf("returned %d models", len(parsed.Data))
	}
	got := parsed.Data[0]
	if got.Object != "model" || got.OwnedBy != "outrider" {
		t.Errorf("required fields = %q %q", got.Object, got.OwnedBy)
	}
	if got.Description != entry.Description || got.Repo != entry.Repo || got.File != entry.File {
		t.Errorf("provenance = %#v", got)
	}
	if len(got.Capabilities) != 2 || got.Capabilities[1] != catalog.CapabilityVision {
		t.Errorf("capabilities = %v", got.Capabilities)
	}
	if got.Meta.Context != 32768 || got.Meta.TrainingContext != 131072 {
		t.Errorf("context = %#v", got.Meta)
	}
	if got.MinMemoryBytes != 32768<<20 {
		t.Errorf("minMemoryBytes = %d", got.MinMemoryBytes)
	}
	if got.Speculation != "mtp" {
		t.Errorf("speculation = %q", got.Speculation)
	}
}

// A client filtering on capabilities must never see the key absent and read
// that as "unknown", so an empty set is sent as an empty list.
func TestListModelsAlwaysSendsCapabilities(t *testing.T) {
	server, err := New([]catalog.Entry{{ID: "bare"}}, &fakeBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if !strings.Contains(recorder.Body.String(), `"capabilities":[]`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

// A gateway answers from the catalog compiled into it, so a client that
// upgraded the binary needs a way to see it is still talking to the old one.
func TestHealthReportsTheRunningBuild(t *testing.T) {
	server, err := New([]catalog.Entry{{ID: "tiny"}}, &fakeBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.Build = Build{Version: "1.2.3", Commit: "abc123"}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	var parsed struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Status != "ok" || parsed.Version != "1.2.3" || parsed.Commit != "abc123" {
		t.Fatalf("health = %#v", parsed)
	}
}

type loadedBackend struct {
	fakeBackend
	model string
	url   string
}

func (backend *loadedBackend) Loaded(context.Context) (string, string, bool) {
	if backend.model == "" {
		return "", "", false
	}
	return backend.model, backend.url, true
}

func detailFor(t *testing.T, server *Server, id string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models/"+id, nil))
	if recorder.Code != http.StatusOK {
		return recorder.Code, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	return recorder.Code, parsed
}

func detailEntry(profile string) catalog.Entry {
	return catalog.Entry{
		ID: profile, Context: 32768,
		Requested: catalog.Requested{
			Context: 32768, GPULayers: "all", FlashAttention: true,
			KVKeyType: "q8_0", KVValueType: "q8_0", Speculation: "mtp",
		},
	}
}

// A caller compares what was asked for against what the process reports, so
// the two halves are published side by side and never merged.
func TestShowModelSeparatesRequestedFromResolved(t *testing.T) {
	backend := &loadedBackend{model: "primary", url: "http://127.0.0.1:11481"}
	server, err := New([]catalog.Entry{detailEntry("primary")}, backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.resolve = func(context.Context, string, string) (endpoint.Resolved, error) {
		return endpoint.Resolved{
			Endpoint: "http://127.0.0.1:11481", Build: "b10516",
			Context: 16384, TrainingContext: 262144, Quantization: "Q4_K_M",
			SupportsTools: true, Modalities: endpoint.Modalities{Vision: true},
		}, nil
	}
	status, body := detailFor(t, server, "primary")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	requested, _ := body["requested"].(map[string]any)
	if requested["n_ctx"] != float64(32768) || requested["kv_key_type"] != "q8_0" ||
		requested["n_gpu_layers"] != "all" || requested["speculation"] != "mtp" {
		t.Fatalf("requested = %#v", requested)
	}
	resolved, _ := body["resolved"].(map[string]any)
	if resolved["loaded"] != true || resolved["n_ctx"] != float64(16384) ||
		resolved["n_ctx_train"] != float64(262144) || resolved["supports_tools"] != true {
		t.Fatalf("resolved = %#v", resolved)
	}
	// The disagreement is the point: 32768 was asked for and 16384 arrived.
	if requested["n_ctx"] == resolved["n_ctx"] {
		t.Fatal("the two halves collapsed into one value")
	}
	if modalities, _ := resolved["modalities"].(map[string]any); modalities["vision"] != true {
		t.Fatalf("modalities = %#v", resolved["modalities"])
	}
}

// A lookup must never start a model, so a model that is not the loaded one
// reports itself as not loaded rather than loading to find out.
func TestShowModelDoesNotLoadToAnswer(t *testing.T) {
	backend := &loadedBackend{model: "other", url: "http://127.0.0.1:11481"}
	server, err := New([]catalog.Entry{detailEntry("primary")}, backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.resolve = func(context.Context, string, string) (endpoint.Resolved, error) {
		t.Fatal("resolved a model that is not loaded")
		return endpoint.Resolved{}, nil
	}
	_, body := detailFor(t, server, "primary")
	resolved, _ := body["resolved"].(map[string]any)
	if resolved["loaded"] != false {
		t.Fatalf("resolved = %#v", resolved)
	}
	if len(backend.models) != 0 {
		t.Fatalf("backend was asked to load %v", backend.models)
	}
}

// A backend that will not describe itself leaves the resolved half empty. It
// does not turn a lookup of a known model into a failure.
func TestShowModelSurvivesAnUndescribableBackend(t *testing.T) {
	backend := &loadedBackend{model: "primary", url: "http://127.0.0.1:11481"}
	server, err := New([]catalog.Entry{detailEntry("primary")}, backend, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.resolve = func(context.Context, string, string) (endpoint.Resolved, error) {
		return endpoint.Resolved{}, errors.New("backend closed the connection")
	}
	status, body := detailFor(t, server, "primary")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if resolved, _ := body["resolved"].(map[string]any); resolved["loaded"] != false {
		t.Fatalf("resolved = %#v", resolved)
	}
}

// A backend with no way to report what is loaded still answers the route.
func TestShowModelWorksWithoutALoader(t *testing.T) {
	server, err := New([]catalog.Entry{detailEntry("primary")}, &fakeBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, body := detailFor(t, server, "primary")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if resolved, _ := body["resolved"].(map[string]any); resolved["loaded"] != false {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestShowModelRejectsAnUnknownModel(t *testing.T) {
	server, err := New([]catalog.Entry{detailEntry("primary")}, &fakeBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := detailFor(t, server, "absent"); status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}
