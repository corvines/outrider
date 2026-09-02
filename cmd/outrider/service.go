package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	_, backendPort, err := gatewayPorts(environment)
	if err != nil {
		return useOutput{}, err
	}
	backendEnvironment := cloneEnvironment(environment)
	backendEnvironment["OUTRIDER_PORT"] = fmt.Sprintf("%d", backendPort)
	backend := gatewayBackend{environment: backendEnvironment, options: options}
	if _, err := backend.Ensure(ctx, profileID); err != nil {
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
