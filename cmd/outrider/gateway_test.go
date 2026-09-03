package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	handler, err := gatewayHTTPHandler(gateway, map[string]string{"OUTRIDER_HOME": t.TempDir()}, 12000)
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
