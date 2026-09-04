package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corvines/outrider/internal/llama"
	"github.com/corvines/outrider/internal/manifest"
	runnerprocess "github.com/corvines/outrider/internal/process"
	"github.com/corvines/outrider/internal/switcher"
)

func TestGatewayModelsAdvertiseRunnableProfiles(t *testing.T) {
	models, err := gatewayModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("gateway has no models")
	}
	found := false
	for _, model := range models {
		if model.ID == "gemma4-26b" {
			found = true
		}
	}
	if !found {
		t.Fatal("gateway does not advertise gemma4-26b")
	}
}

func TestGatewayPortsReserveAdjacentBackend(t *testing.T) {
	front, backend, err := gatewayPorts(map[string]string{"OUTRIDER_PORT": "12000"})
	if err != nil {
		t.Fatal(err)
	}
	if front != 12000 || backend != 12001 {
		t.Fatalf("ports = %d, %d", front, backend)
	}
}

func TestGatewayHTTPHandlerReportsStoppedModel(t *testing.T) {
	gateway, err := switcher.New([]switcher.Model{{ID: "tiny"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := gatewayHTTPHandler(gateway, nil, map[string]string{"OUTRIDER_HOME": t.TempDir()}, 12000)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var status gatewayDashboardStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.GatewayEndpoint != "http://127.0.0.1:12000" {
		t.Fatalf("gateway endpoint = %q", status.GatewayEndpoint)
	}
	if status.GatewayHealth != "ok" {
		t.Fatalf("gateway health = %q", status.GatewayHealth)
	}
	if status.Model.Kind != runnerprocess.StatusStopped {
		t.Fatalf("model kind = %q", status.Model.Kind)
	}
}

func TestGatewayHTTPHandlerReturnsCurrentRunLogs(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "runs", "gateway", "gateway.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("first line\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gateway, err := switcher.New([]switcher.Model{{ID: "tiny"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := gatewayHTTPHandler(gateway, nil, map[string]string{"OUTRIDER_HOME": root}, 12000)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/logs", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var logs gatewayLogsResponse
	if err := json.NewDecoder(response.Body).Decode(&logs); err != nil {
		t.Fatal(err)
	}
	if logs.LogFile != logPath {
		t.Fatalf("log file = %q, want %q", logs.LogFile, logPath)
	}
	if len(logs.Lines) != 2 || logs.Lines[0] != "first line" || logs.Lines[1] != "second line" {
		t.Fatalf("log lines = %#v", logs.Lines)
	}
}

func TestGatewayHTTPHandlerReportsModelLoading(t *testing.T) {
	backend := &gatewayBackend{}
	backend.beginLoading("gemma4-26b")
	backend.reportLoadingProgress(llama.DownloadProgress{
		Name: "gemma4-26B_q4_0-it.gguf", Downloaded: 64, Total: 100,
		BytesPerSecond: 20, ETA: 2 * time.Second,
	})
	gateway, err := switcher.New([]switcher.Model{{ID: "gemma4-26b"}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := gatewayHTTPHandler(gateway, backend, map[string]string{"OUTRIDER_HOME": t.TempDir()}, 12000)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var status gatewayDashboardStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Loading == nil || status.Loading.Model != "gemma4-26b" {
		t.Fatalf("loading = %+v", status.Loading)
	}
	if status.Loading.Phase != "downloading" || status.Loading.Downloaded != 64 || status.Loading.Total != 100 {
		t.Fatalf("loading progress = %+v", status.Loading)
	}
	if status.Loading.Name != "gemma4-26B_q4_0-it.gguf" {
		t.Fatalf("loading name = %q", status.Loading.Name)
	}
}

func TestSwitchGatewayModelEmitsLoadingProgress(t *testing.T) {
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/admin/status":
			_ = json.NewEncoder(writer).Encode(gatewayDashboardStatus{
				Loading: &gatewayLoadingStatus{
					Model:      "granite4-h-tiny",
					Name:       "file.gguf",
					Phase:      "downloading",
					Downloaded: 10,
					Total:      100,
				},
			})
		case "/admin/model":
			<-released
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	progresses := make(chan llama.DownloadProgress, 8)
	errCh := make(chan error, 1)
	go func() {
		errCh <- switchGatewayModel(context.Background(), server.URL, "granite4-h-tiny", runOptions{
			Progress: func(progress llama.DownloadProgress) {
				select {
				case progresses <- progress:
				default:
				}
			},
		})
	}()

	select {
	case progress := <-progresses:
		if progress.Name != "file.gguf" || progress.Downloaded != 10 || progress.Total != 100 {
			t.Fatalf("progress = %+v", progress)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for progress")
	}
	close(released)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestPostGatewayJSONReportsStoppedWhenServerCloses(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	go func() {
		<-started
		_ = server.Config.Close()
	}()
	err := postGatewayJSON(context.Background(), server.URL+"/admin/model", map[string]string{"model": "tiny"})
	if err == nil || !strings.Contains(err.Error(), "Outrider stopped") {
		t.Fatalf("err = %v", err)
	}
}

type recordingGatewayBackend struct {
	model string
}

func (backend *recordingGatewayBackend) Ensure(_ context.Context, modelID string) (string, error) {
	backend.model = modelID
	return "http://127.0.0.1:12001", nil
}

func TestGatewayHTTPHandlerLoadsRequestedModel(t *testing.T) {
	backend := &recordingGatewayBackend{}
	gateway, err := switcher.New([]switcher.Model{{ID: "tiny"}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := gatewayHTTPHandler(gateway, backend, map[string]string{"OUTRIDER_HOME": t.TempDir()}, 12000)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/admin/model", strings.NewReader(`{"model":"tiny"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if backend.model != "tiny" {
		t.Fatalf("model = %q", backend.model)
	}
}

func TestGatewayCatalogReportsProtectedAndCustomModels(t *testing.T) {
	root := t.TempDir()
	state, err := activeState(map[string]string{"OUTRIDER_HOME": root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state.Models, 0o700); err != nil {
		t.Fatal(err)
	}
	customPath := filepath.Join(state.Models, "custom-example.gguf")
	if err := os.WriteFile(customPath, []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	models, err := gatewayCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]gatewayModelStatus{}
	for _, model := range models {
		found[model.ID] = model
	}
	tiny := found["tiny"]
	lite := found["qwen3-1.7b"]
	helper := found["granite4.2-3b"]
	primary := found["qwen35b-mtp"]
	custom := found["custom-example"]
	if tiny.ID != "tiny" || tiny.Protected || tiny.CanDelete || tiny.Cached {
		t.Fatalf("tiny = %#v", tiny)
	}
	for _, model := range []gatewayModelStatus{lite, helper, primary} {
		if !model.Protected || model.CanDelete || model.Cached {
			t.Fatalf("protected model = %#v", model)
		}
	}
	if !custom.Custom || !custom.Cached || !custom.CanDelete || custom.Protected || custom.SizeBytes != 5 || custom.Path != customPath {
		t.Fatalf("custom model = %#v", custom)
	}
	if err := deleteCustomModel(root, "custom-example"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(customPath); !os.IsNotExist(err) {
		t.Fatalf("custom model remains: %v", err)
	}
}

func TestGatewayDeleteRemovesProtectedDownloadAndKeepsCatalogRow(t *testing.T) {
	root := t.TempDir()
	profile := mustGatewayProfile("granite4.2-3b")
	state, err := manifest.Paths(root, profile, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(state.Model), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.Model, []byte("gguf"), 0o600); err != nil {
		t.Fatal(err)
	}
	gateway, err := switcher.New([]switcher.Model{{ID: profile.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := gatewayHTTPHandler(gateway, nil, map[string]string{"OUTRIDER_HOME": root}, 12000)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/delete", strings.NewReader(`{"model":"granite4.2-3b"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(state.Model); !os.IsNotExist(err) {
		t.Fatalf("download remains: %v", err)
	}
	models, err := gatewayCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	var helper gatewayModelStatus
	for _, model := range models {
		if model.ID == "granite4.2-3b" {
			helper = model
		}
	}
	if helper.ID != "granite4.2-3b" || !helper.Protected || helper.Cached || helper.CanDelete || helper.Path != "" {
		t.Fatalf("catalog row after delete = %#v", helper)
	}
}

func TestGatewayCatalogReportsOnDiskPath(t *testing.T) {
	root := t.TempDir()
	profile := mustGatewayProfile("granite4.2-3b")
	state, err := manifest.Paths(root, profile, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(state.Model), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.Model, []byte("gguf"), 0o600); err != nil {
		t.Fatal(err)
	}
	models, err := gatewayCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	var helper gatewayModelStatus
	for _, model := range models {
		if model.ID == "granite4.2-3b" {
			helper = model
		}
	}
	if helper.Path != state.Model || helper.Cached || !helper.CanDelete {
		t.Fatalf("incomplete download = %#v", helper)
	}
}

func TestGatewayRevealOpensCachedModel(t *testing.T) {
	root := t.TempDir()
	profile := mustGatewayProfile("granite4.2-3b")
	state, err := manifest.Paths(root, profile, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(state.Model), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.Model, []byte("gguf"), 0o600); err != nil {
		t.Fatal(err)
	}
	var revealed string
	original := revealInFinder
	revealInFinder = func(path string) error {
		revealed = path
		return nil
	}
	t.Cleanup(func() { revealInFinder = original })
	gateway, err := switcher.New([]switcher.Model{{ID: profile.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := gatewayHTTPHandler(gateway, nil, map[string]string{"OUTRIDER_HOME": root}, 12000)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/reveal", strings.NewReader(`{"model":"granite4.2-3b"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	want, err := filepath.Abs(state.Model)
	if err != nil {
		t.Fatal(err)
	}
	if revealed != want {
		t.Fatalf("revealed = %q, want %q", revealed, want)
	}
	missing := httptest.NewRequest(http.MethodPost, "/admin/reveal", strings.NewReader(`{"model":"qwen3-1.7b"}`))
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing reveal status = %d, body = %s", missingResponse.Code, missingResponse.Body.String())
	}
}

func TestMachineProgressJSONMatchesVeraProgressLines(t *testing.T) {
	download, err := encodeMachineProgress(llama.DownloadProgress{
		Name: "qwen35b-mtp", Downloaded: 8_400_000_000, Total: 21_000_000_000,
		BytesPerSecond: 15_000_000, ETA: 840 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(download) != `{"name":"qwen35b-mtp","downloaded":8400000000,"total":21000000000,"bytes_per_second":15000000,"eta_seconds":840,"done":false}` {
		t.Fatalf("download progress = %s", download)
	}
	step, err := encodeMachineProgress(llama.DownloadProgress{Name: "starting on 127.0.0.1:11435", Done: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(step) != `{"name":"starting on 127.0.0.1:11435","done":true}` {
		t.Fatalf("step progress = %s", step)
	}
}

func TestNormalizeDownloadPath(t *testing.T) {
	urlValue, filename, err := normalizeDownloadPath("owner/repo/path/to/model file.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if urlValue != "https://huggingface.co/owner/repo/resolve/main/path/to/model%20file.gguf?download=true" || filename != "model-file.gguf" {
		t.Fatalf("normalized path = %q, %q", urlValue, filename)
	}
	urlValue, filename, err = normalizeDownloadPath("https://example.com/model.gguf")
	if err != nil || urlValue != "https://example.com/model.gguf" || filename != "model.gguf" {
		t.Fatalf("normalized URL = %q, %q, %v", urlValue, filename, err)
	}
	if _, _, err := normalizeDownloadPath("http://example.com/model.gguf"); err == nil {
		t.Fatal("accepted non-HTTPS model URL")
	}
}
