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
	// started records that Ensure launched the gateway. A gateway that was
	// already serving belongs to whoever started it, so Stop leaves it alone.
	started bool
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
	owner.started = true
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
	if !owner.started {
		return nil
	}
	binary, err := owner.lookPath()
	if err != nil {
		return err
	}
	return owner.run(ctx, binary, "stop")
}

// resolveOutriderBinary finds the server binary the app should drive. It never
// searches PATH: a Finder-launched bundle inherits launchd's PATH, which does
// not contain ~/.local/bin, so a PATH hit would be luck rather than a contract.
// The bundle ships the binary beside the dashboard executable.
func resolveOutriderBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("OUTRIDER_BIN")); override != "" {
		return override, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate the dashboard executable: %w", err)
	}
	return resolveSiblingBinary(executable)
}

func resolveSiblingBinary(executable string) (string, error) {
	directory := filepath.Dir(executable)
	sibling := filepath.Join(directory, "outrider")
	info, err := os.Stat(sibling)
	if err == nil && !info.IsDir() {
		return sibling, nil
	}
	return "", fmt.Errorf(
		"cannot find the Outrider server binary at %s; set OUTRIDER_BIN to run outside an app bundle",
		sibling,
	)
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

// InstallCommandLineTool points ~/.local/bin/outrider at the binary the app
// runs, so a terminal and the app drive the same build. It returns the path it
// installed. A symlink rather than a copy keeps the two in step when the app is
// replaced.
func (owner *gatewayOwner) InstallCommandLineTool(ctx context.Context) (string, error) {
	binary, err := owner.lookPath()
	if err != nil {
		return "", err
	}
	target, err := commandLineToolPath()
	if err != nil {
		return "", err
	}
	if err := owner.run(ctx, binary, "install", "--link"); err != nil {
		return "", err
	}
	return target, nil
}

func commandLineToolPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "bin", "outrider"), nil
}
