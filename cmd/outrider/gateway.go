package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/manifest"
	runnerprocess "github.com/corvines/outrider/internal/process"
	"github.com/corvines/outrider/internal/switcher"
)

type gatewayBackend struct {
	environment map[string]string
	options     runOptions
}

func (backend *gatewayBackend) Ensure(ctx context.Context, modelID string) (string, error) {
	state, err := activeState(backend.environment)
	if err != nil {
		return "", err
	}
	status, err := runnerprocess.GetActiveStatus(ctx, state)
	if err != nil {
		return "", err
	}
	if status.Kind == runnerprocess.StatusMismatched {
		return "", fmt.Errorf("active model cannot be safely inspected: %s", status.Detail)
	}
	if status.Kind == runnerprocess.StatusRunning && status.Preset == modelID &&
		status.Health != nil && *status.Health {
		if err := verifyGatewayModel(ctx, status.Endpoint, modelID); err != nil {
			return "", err
		}
		return status.Endpoint, nil
	}
	if status.Kind != runnerprocess.StatusStopped {
		if _, err := runnerprocess.StopActive(ctx, state, runnerprocess.StopOptions{}); err != nil {
			return "", fmt.Errorf("stop active model: %w", err)
		}
	}
	session, err := startSession(ctx, modelID, backend.environment, backend.options)
	if err != nil {
		return "", err
	}
	if err := verifyGatewayModel(ctx, session.Preparation.Plan.Endpoint, modelID); err != nil {
		return "", err
	}
	return session.Preparation.Plan.Endpoint, nil
}

func verifyGatewayModel(ctx context.Context, endpointURL string, modelID string) error {
	profile, err := manifest.Get(modelID)
	if err != nil {
		return err
	}
	if _, err := endpoint.VerifyModelContract(ctx, endpointURL, profile); err != nil {
		return fmt.Errorf("loaded model identity does not match %q: %w", modelID, err)
	}
	return nil
}

func runGateway(ctx context.Context, environment map[string]string, options runOptions) error {
	frontPort, backendPort, err := gatewayPorts(environment)
	if err != nil {
		return err
	}
	models, err := gatewayModels()
	if err != nil {
		return err
	}
	backendEnvironment := cloneEnvironment(environment)
	backendEnvironment["OUTRIDER_PORT"] = strconv.Itoa(backendPort)
	gateway, err := switcher.New(models, &gatewayBackend{environment: backendEnvironment, options: options})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(manifest.DefaultHost, strconv.Itoa(frontPort)))
	if err != nil {
		return fmt.Errorf("model switcher cannot listen on 127.0.0.1:%d: %w", frontPort, err)
	}
	server := &http.Server{Handler: gateway.Handler(), ReadHeaderTimeout: 5 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	serveErr := server.Serve(listener)
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	<-shutdownDone
	return nil
}

func gatewayModels() ([]switcher.Model, error) {
	profiles, err := manifest.All()
	if err != nil {
		return nil, err
	}
	models := make([]switcher.Model, 0, len(profiles))
	for _, profile := range profiles {
		if !profile.Runnable {
			continue
		}
		models = append(models, switcher.Model{
			ID: profile.ID, ContextWindow: profile.Context.Size, TrainingContext: profile.Context.Original,
			Quantization: profile.Model.Quant,
		})
	}
	return models, nil
}

func gatewayPorts(environment map[string]string) (int, int, error) {
	front := manifest.DefaultPort
	if value, exists := environment["OUTRIDER_PORT"]; exists {
		parsed, err := parsePort(value, true)
		if err != nil {
			return 0, 0, err
		}
		front = *parsed
	}
	if front == 65535 {
		return 0, 0, runnerErrorf("OUTRIDER_PORT must leave the next port available for the model backend")
	}
	return front, front + 1, nil
}

func cloneEnvironment(environment map[string]string) map[string]string {
	cloned := make(map[string]string, len(environment))
	for key, value := range environment {
		cloned[key] = value
	}
	return cloned
}
