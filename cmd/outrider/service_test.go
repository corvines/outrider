package main

import (
	"path/filepath"
	"testing"
)

func TestGatewayProcessPlanUsesDedicatedState(t *testing.T) {
	root := t.TempDir()
	plan, err := gatewayProcessPlan(map[string]string{"OUTRIDER_HOME": root, "OUTRIDER_PORT": "12000"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Profile.ID != "gateway" || plan.Port != 12000 || plan.Endpoint != "http://127.0.0.1:12000" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.State.PID != filepath.Join(root, "runs", "gateway.json") ||
		plan.State.Log != filepath.Join(root, "runs", "gateway", "gateway.log") {
		t.Fatalf("state = %#v", plan.State)
	}
	if len(plan.Args) != 1 || plan.Args[0] != "serve" {
		t.Fatalf("args = %v", plan.Args)
	}
}
