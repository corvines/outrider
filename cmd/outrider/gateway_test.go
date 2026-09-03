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
	var tiny, primary, custom gatewayModelStatus
	for _, model := range models {
		switch model.ID {
		case "tiny":
			tiny = model
		case "qwen35b-mtp":
			primary = model
		case "custom-example":
			custom = model
		}
	}
	if !tiny.Protected || tiny.CanDelete || !primary.Protected || primary.CanDelete {
		t.Fatalf("protected models = %#v %#v", tiny, primary)
	}
	if !custom.Custom || !custom.Cached || !custom.CanDelete || custom.SizeBytes != 5 {
		t.Fatalf("custom model = %#v", custom)
	}
	if err := deleteCustomModel(root, "custom-example"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(customPath); !os.IsNotExist(err) {
		t.Fatalf("custom model remains: %v", err)
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
