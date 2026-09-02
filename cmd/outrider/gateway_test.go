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
