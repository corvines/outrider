package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/corvines/outrider/internal/llama"
	"github.com/corvines/outrider/internal/manifest"
	runnerprocess "github.com/corvines/outrider/internal/process"
)

func gatewayProcessPlan(environment map[string]string) (manifest.Plan, error) {
	frontPort, _, err := gatewayPorts(environment)
	if err != nil {
		return manifest.Plan{}, err
	}
	state, err := activeState(environment)
	if err != nil {
		return manifest.Plan{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return manifest.Plan{}, fmt.Errorf("resolve Outrider executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return manifest.Plan{}, err
	}
	runDirectory := filepath.Join(state.Root, "runs", "gateway")
	state.Run = runDirectory
	state.PID = filepath.Join(state.Root, "runs", "gateway.json")
	state.Lock = filepath.Join(state.Root, "runs", "gateway.lock")
	state.Log = filepath.Join(runDirectory, "gateway.log")
	state.Executable = executable
	endpoint := fmt.Sprintf("http://%s:%d", manifest.DefaultHost, frontPort)
	return manifest.Plan{
		Profile: manifest.Profile{ID: "gateway"}, Host: manifest.DefaultHost, Port: frontPort,
		Endpoint: endpoint, HealthEndpoint: endpoint + "/health", Executable: executable,
		Args: []string{"serve"}, State: state,
	}, nil
}

func startGateway(ctx context.Context, environment map[string]string) (runnerprocess.Status, error) {
	plan, err := gatewayProcessPlan(environment)
	if err != nil {
		return runnerprocess.Status{}, err
	}
	return runnerprocess.Start(ctx, plan, runnerprocess.StartOptions{})
}

func serveProfile(
	ctx context.Context,
	profileID string,
	environment map[string]string,
	options runOptions,
) (useOutput, error) {
	if _, err := runnableProfile(profileID); err != nil {
		return useOutput{}, err
	}
	if _, err := startGateway(ctx, environment); err != nil {
		return useOutput{}, err
	}
	return useGatewayModel(ctx, profileID, environment, options)
}

func getServiceStatus(ctx context.Context, environment map[string]string) (serviceStatusOutput, error) {
	gatewayPlan, err := gatewayProcessPlan(environment)
	if err != nil {
		return serviceStatusOutput{}, err
	}
	gateway, err := runnerprocess.GetStatus(ctx, gatewayPlan)
	if err != nil {
		return serviceStatusOutput{}, err
	}
	state, err := activeState(environment)
	if err != nil {
		return serviceStatusOutput{}, err
	}
	model, err := runnerprocess.GetActiveStatus(ctx, state)
	if err != nil {
		return serviceStatusOutput{}, err
	}
	return serviceStatusOutput{Gateway: gateway, Model: model}, nil
}

func useGatewayModel(
	ctx context.Context,
	profileID string,
	environment map[string]string,
	options runOptions,
) (useOutput, error) {
	if _, err := runnableProfile(profileID); err != nil {
		return useOutput{}, err
	}
	options.notice("Checking the Outrider gateway...")
	gatewayPlan, err := gatewayProcessPlan(environment)
	if err != nil {
		return useOutput{}, err
	}
	gatewayStatus, err := runnerprocess.GetStatus(ctx, gatewayPlan)
	if err != nil {
		return useOutput{}, err
	}
	if gatewayStatus.Kind != runnerprocess.StatusRunning || gatewayStatus.Health == nil || !*gatewayStatus.Health {
		return useOutput{}, runnerErrorf("Outrider gateway is not healthy; run `outrider start` first")
	}
	options.notice("Switching to %s...", profileID)
	if err := switchGatewayModel(ctx, gatewayPlan.Endpoint, profileID, options); err != nil {
		return useOutput{}, err
	}
	state, err := activeState(environment)
	if err != nil {
		return useOutput{}, err
	}
	model, err := runnerprocess.GetActiveStatus(ctx, state)
	if err != nil {
		return useOutput{}, err
	}
	return useOutput{Profile: profileID, Endpoint: gatewayPlan.Endpoint, Model: model}, nil
}

func switchGatewayModel(ctx context.Context, endpoint string, profileID string, options runOptions) error {
	done := make(chan struct{})
	go pollGatewayLoading(ctx, endpoint, options, done)
	err := postGatewayJSON(ctx, endpoint+"/admin/model", map[string]string{"model": profileID})
	close(done)
	return err
}

func pollGatewayLoading(ctx context.Context, endpoint string, options runOptions, done <-chan struct{}) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			var status gatewayDashboardStatus
			if err := getGatewayJSON(ctx, endpoint+"/admin/status", &status); err != nil || status.Loading == nil {
				continue
			}
			if options.Progress == nil {
				continue
			}
			name := status.Loading.Name
			if name == "" {
				name = status.Loading.Model
			}
			options.Progress(llama.DownloadProgress{
				Name:           name,
				Downloaded:     status.Loading.Downloaded,
				Total:          status.Loading.Total,
				BytesPerSecond: status.Loading.BytesPerSecond,
				ETA:            time.Duration(status.Loading.ETASeconds) * time.Second,
			})
		}
	}
}

func getGatewayJSON(ctx context.Context, url string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Outrider returned HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func postGatewayJSON(ctx context.Context, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 30 * time.Minute}).Do(request)
	if err != nil {
		if ctx.Err() != nil || gatewayGone(err) {
			return runnerErrorf("Outrider stopped")
		}
		return fmt.Errorf("Outrider gateway closed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(message, &payload) == nil && payload.Error != "" {
			return fmt.Errorf("%s", payload.Error)
		}
		if len(message) > 0 {
			return fmt.Errorf("Outrider returned HTTP %d: %s", response.StatusCode, string(message))
		}
		return fmt.Errorf("Outrider returned HTTP %d", response.StatusCode)
	}
	return nil
}

func gatewayGone(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	message := err.Error()
	for _, fragment := range []string{
		"EOF",
		"connection refused",
		"connection reset",
		"broken pipe",
		"server closed",
		"use of closed network connection",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func stopServices(ctx context.Context, environment map[string]string) (serviceStatusOutput, error) {
	gatewayPlan, err := gatewayProcessPlan(environment)
	if err != nil {
		return serviceStatusOutput{}, err
	}
	gateway, err := runnerprocess.Stop(ctx, gatewayPlan, runnerprocess.StopOptions{})
	if err != nil {
		return serviceStatusOutput{}, err
	}
	state, err := activeState(environment)
	if err != nil {
		return serviceStatusOutput{}, err
	}
	model, err := runnerprocess.StopActive(ctx, state, runnerprocess.StopOptions{})
	if err != nil {
		return serviceStatusOutput{}, err
	}
	return serviceStatusOutput{Gateway: gateway, Model: model}, nil
}
