package main

import (
	"testing"
)

func TestGatewayModelsAdvertiseRunnableProfiles(t *testing.T) {
	models, err := gatewayModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("gateway has no models")
	}
	if models[len(models)-1].ID != "qwen35b-mtp" {
		t.Fatalf("last model = %q", models[len(models)-1].ID)
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
