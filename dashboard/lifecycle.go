package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type gatewayOwner struct {
	endpoint string
	lookPath func() (string, error)
	run      func(ctx context.Context, binary string, args ...string) error
	healthy  func(ctx context.Context, endpoint string) bool
}

func newGatewayOwner(endpoint string) *gatewayOwner {
	return &gatewayOwner{
		endpoint: endpoint,
		lookPath: resolveOutriderBinary,
		run:      runOutrider,
		healthy:  gatewayHealthy,
	}
}

func (owner *gatewayOwner) Ensure(ctx context.Context) error {
	if owner.healthy(ctx, owner.endpoint) {
		return nil
	}
	binary, err := owner.lookPath()
	if err != nil {
		return err
	}
	if err := owner.run(ctx, binary, "start"); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(20 * time.Second)
	}
	for time.Now().Before(deadline) {
		if owner.healthy(ctx, owner.endpoint) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for the Outrider server")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for the Outrider server")
}

func (owner *gatewayOwner) Stop(ctx context.Context) error {
	binary, err := owner.lookPath()
	if err != nil {
		return err
	}
	return owner.run(ctx, binary, "stop")
}

func resolveOutriderBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("OUTRIDER_BIN")); override != "" {
		return override, nil
	}
	if executable, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(executable), "outrider")
		if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
			return sibling, nil
		}
	}
	binary, err := exec.LookPath("outrider")
	if err != nil {
		return "", fmt.Errorf("cannot find the Outrider server binary; set OUTRIDER_BIN or install outrider on PATH")
	}
	return binary, nil
}

func runOutrider(ctx context.Context, binary string, args ...string) error {
	command := exec.CommandContext(ctx, binary, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return fmt.Errorf("%s %s: %w", binary, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s: %s", binary, strings.Join(args, " "), detail)
	}
	return nil
}

func gatewayHealthy(ctx context.Context, endpoint string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/admin/status", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 300
}
