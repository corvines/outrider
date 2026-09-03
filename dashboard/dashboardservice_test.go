package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
