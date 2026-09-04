package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDashboardServiceLoadModelPostsToGateway(t *testing.T) {
	var loaded string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/admin/model":
			var payload struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			loaded = payload.Model
		case "/admin/status":
			_, _ = writer.Write([]byte(`{"gatewayEndpoint":"http://127.0.0.1:11435","gatewayHealth":"ok","model":{"kind":"stopped"}}`))
		case "/admin/logs":
			_, _ = writer.Write([]byte(`{"logFile":"/tmp/gateway.log","lines":["ready","request failed"]}`))
		case "/v1/models":
			_, _ = writer.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	snapshot := NewDashboardService(server.URL).LoadModel("gemma4-26b")
	if loaded != "gemma4-26b" {
		t.Fatalf("loaded model = %q", loaded)
	}
	if snapshot.Error != "" {
		t.Fatalf("snapshot error = %q", snapshot.Error)
	}
	if len(snapshot.LogLines) != 2 || snapshot.LogLines[1] != "request failed" {
		t.Fatalf("log lines = %#v", snapshot.LogLines)
	}
}

func TestDashboardServiceLoadModelKeepsCatalogOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/admin/model":
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(`{"error":"admission port: fail measured 127.0.0.1:11436 available=false"}`))
		case "/admin/status":
			_, _ = writer.Write([]byte(`{"gatewayEndpoint":"http://127.0.0.1:11435","gatewayHealth":"ok","model":{"kind":"stopped"},"models":[{"id":"qwen3-1.7b","cached":true,"canDelete":true,"protected":true,"path":"/tmp/qwen.gguf"}]}`))
		case "/admin/logs":
			_, _ = writer.Write([]byte(`{"logFile":"","lines":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	snapshot := NewDashboardService(server.URL).LoadModel("qwen3-1.7b")
	if snapshot.Error != "admission port: fail measured 127.0.0.1:11436 available=false" {
		t.Fatalf("snapshot error = %q", snapshot.Error)
	}
	if snapshot.GatewayHealth != "ok" {
		t.Fatalf("gateway health = %q", snapshot.GatewayHealth)
	}
	if len(snapshot.Models) != 1 || snapshot.Models[0].ID != "qwen3-1.7b" || snapshot.Models[0].Path == "" {
		t.Fatalf("models = %#v", snapshot.Models)
	}
}

func TestDashboardServiceRevealModelOpensLocalPath(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "qwen-*.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var revealed string
	original := revealInFinder
	revealInFinder = func(path string) error {
		revealed = path
		return nil
	}
	t.Cleanup(func() { revealInFinder = original })
	payload, err := json.Marshal(map[string]any{
		"gatewayEndpoint": "http://127.0.0.1:11435",
		"gatewayHealth":   "ok",
		"model":           map[string]string{"kind": "stopped"},
		"models":          []map[string]any{{"id": "qwen3-1.7b", "cached": true, "canDelete": true, "path": file.Name()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/admin/status":
			_, _ = writer.Write(payload)
		case "/admin/logs":
			_, _ = writer.Write([]byte(`{"logFile":"","lines":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	snapshot := NewDashboardService(server.URL).RevealModel("qwen3-1.7b")
	if snapshot.Error != "" {
		t.Fatalf("snapshot error = %q", snapshot.Error)
	}
	want, err := filepath.Abs(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if revealed != want {
		t.Fatalf("revealed = %q, want %q", revealed, want)
	}
}

func TestDashboardServiceStopModelReportsGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "model is not running", http.StatusInternalServerError)
	}))
	defer server.Close()

	snapshot := NewDashboardService(server.URL).StopModel()
	if snapshot.Error == "" {
		t.Fatal("expected stop error")
	}
}

func TestDashboardServiceSnapshotFallsBackToLegacyGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/admin/status":
			writer.WriteHeader(http.StatusNotFound)
		case "/v1/models":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"data":[{"id":"qwen35b-mtp","meta":{"n_ctx":32768,"n_ctx_train":131072},"quantization":"UD-Q4_K_M"}]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	snapshot := NewDashboardService(server.URL).Snapshot()
	if snapshot.Error != "" {
		t.Fatalf("Snapshot() error = %q", snapshot.Error)
	}
	if snapshot.GatewayHealth != "legacy" {
		t.Fatalf("GatewayHealth = %q, want legacy", snapshot.GatewayHealth)
	}
	if len(snapshot.Models) != 1 || snapshot.Models[0].ID != "qwen35b-mtp" {
		t.Fatalf("Models = %+v, want qwen35b-mtp", snapshot.Models)
	}
}

func TestDashboardServiceSnapshotKeepsCatalogWhenGatewayDrops(t *testing.T) {
	healthy := true
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !healthy {
			http.Error(writer, "gone", http.StatusBadGateway)
			return
		}
		switch request.URL.Path {
		case "/admin/status":
			_, _ = writer.Write([]byte(`{"gatewayEndpoint":"http://127.0.0.1:11435","gatewayHealth":"ok","model":{"kind":"stopped"},"models":[{"id":"qwen3-1.7b","cached":true,"canDelete":true,"path":"/tmp/qwen.gguf"}]}`))
		case "/admin/logs":
			_, _ = writer.Write([]byte(`{"logFile":"","lines":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := NewDashboardService(server.URL)
	first := service.Snapshot()
	if first.Error != "" || len(first.Models) != 1 || first.Models[0].ID != "qwen3-1.7b" {
		t.Fatalf("first snapshot = %#v", first)
	}
	healthy = false
	second := service.Snapshot()
	if second.GatewayHealth != "offline" {
		t.Fatalf("second health = %q", second.GatewayHealth)
	}
	if len(second.Models) != 1 || second.Models[0].ID != "qwen3-1.7b" || second.Models[0].Path == "" {
		t.Fatalf("second models = %#v", second.Models)
	}
}
