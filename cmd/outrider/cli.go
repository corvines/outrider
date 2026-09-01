package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/corvines/outrider/internal/capabilities"
	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/llama"
	"github.com/corvines/outrider/internal/manifest"
	runnerprocess "github.com/corvines/outrider/internal/process"
)

const usage = `outrider: loopback llama.cpp runner

  outrider plan tiny
  outrider plan qwen35b-mtp
  outrider up tiny
  outrider smoke
  outrider demo tiny
  outrider status
  outrider down

Environment overrides: LLAMA_SERVER_BIN, OUTRIDER_HOME,
OUTRIDER_PORT.
`

type tinyPreparation struct {
	Profile   manifest.Profile
	Plan      manifest.Plan
	Baseline  runnerprocess.Status
	StartedAt time.Time
}

type tinySession struct {
	Preparation tinyPreparation
	Status      runnerprocess.Status
	ColdStartMS *float64
	OwnsProcess bool
}

func run(ctx context.Context, argv []string, environment map[string]string) (string, error) {
	var command string
	if len(argv) > 0 {
		command = argv[0]
	}
	switch command {
	case "plan":
		if len(argv) != 2 {
			return "", usageError("plan expects exactly one preset id")
		}
		plan, err := resolvePlan(argv[1], environment, false, "")
		if err != nil {
			return "", err
		}
		return encodeOutput(newPlanOutput(plan))
	case "up":
		if len(argv) != 2 || argv[1] != "tiny" {
			return "", usageError("up expects the runnable `tiny` preset")
		}
		session, err := startTinySession(ctx, environment)
		if err != nil {
			return "", err
		}
		return encodeOutput(newUpOutput(session))
	case "smoke":
		if len(argv) != 1 {
			return "", usageError("smoke does not accept a preset id")
		}
		return runTinyDemo(ctx, environment)
	case "demo":
		if len(argv) != 2 || argv[1] != "tiny" {
			return "", usageError("demo expects exactly the runnable `tiny` preset")
		}
		return runTinyDemo(ctx, environment)
	case "status", "down":
		if len(argv) != 1 {
			return "", usageError(fmt.Sprintf("%s does not accept arguments", command))
		}
		plan, err := resolvePlan("tiny", environment, true, "")
		if err != nil {
			return "", err
		}
		var status runnerprocess.Status
		if command == "status" {
			status, err = runnerprocess.GetStatus(ctx, plan)
		} else {
			status, err = runnerprocess.Stop(ctx, plan, runnerprocess.StopOptions{})
		}
		if err != nil {
			return "", err
		}
		return encodeOutput(status)
	default:
		return "", usageError(fmt.Sprintf("unknown command %q; see usage", command))
	}
}

func startTinySession(ctx context.Context, environment map[string]string) (tinySession, error) {
	profile, err := manifest.Get("tiny")
	if err != nil {
		return tinySession{}, err
	}
	initialPlan, err := resolvePlan("tiny", environment, false, "")
	if err != nil {
		return tinySession{}, err
	}
	lock, err := runnerprocess.AcquireUpLock(ctx, initialPlan)
	if err != nil {
		return tinySession{}, err
	}
	defer lock.Release()
	startedAt := time.Now()
	executable, err := llama.EnsureServer(ctx, llama.EnsureServerOptions{
		StateRoot: initialPlan.State.Root, ExecutableOverride: environment["LLAMA_SERVER_BIN"],
	})
	if err != nil {
		return tinySession{}, err
	}
	plan, err := resolvePlan("tiny", environment, true, executable)
	if err != nil {
		return tinySession{}, err
	}
	baseline, err := runnerprocess.GetStatus(ctx, plan)
	if err != nil {
		return tinySession{}, err
	}
	if baseline.Kind == runnerprocess.StatusMismatched {
		return tinySession{}, runnerErrorf("%s", baseline.Detail)
	}
	probed, err := capabilities.Probe(ctx, plan.Executable, nil)
	if err != nil {
		return tinySession{}, err
	}
	if err := capabilities.Assert(probed, plan.Args); err != nil {
		return tinySession{}, err
	}
	if _, err := llama.EnsureModelCached(ctx, profile, plan, llama.EnsureModelOptions{}); err != nil {
		return tinySession{}, err
	}
	status, err := runnerprocess.StartWithLock(ctx, plan, runnerprocess.StartOptions{}, lock)
	if err != nil {
		return tinySession{}, err
	}
	session := tinySession{
		Preparation: tinyPreparation{Profile: profile, Plan: plan, Baseline: baseline, StartedAt: startedAt},
		Status:      status, OwnsProcess: status.Detail == "started",
	}
	if session.OwnsProcess {
		coldStart := elapsedMilliseconds(startedAt)
		session.ColdStartMS = &coldStart
	}
	return session, nil
}

func runTinyDemo(ctx context.Context, environment map[string]string) (string, error) {
	session, operationErr := startTinySession(ctx, environment)
	var output string
	if operationErr == nil {
		requestStartedAt := time.Now()
		temperature := session.Preparation.Profile.Sampling.Temperature
		topP := session.Preparation.Profile.Sampling.TopP
		completion, err := endpoint.RequestChatCompletion(ctx, session.Preparation.Plan.Endpoint, endpoint.ChatOptions{
			Model:        session.Preparation.Profile.Model.File,
			SystemPrompt: session.Preparation.Profile.SystemPrompt,
			Temperature:  &temperature, TopP: &topP,
		})
		if err != nil {
			if ctx.Err() != nil {
				operationErr = runnerErrorf("demo interrupted: %v", ctx.Err())
			} else {
				operationErr = err
			}
		} else {
			generationTiming := completion.GenerationTiming
			if generationTiming == nil {
				generationTiming, _ = endpoint.ReadGenerationTimingFromLog(session.Preparation.Plan.State.Log)
			}
			output, operationErr = encodeOutput(newDemoOutput(
				session, completion.AssistantResponse, elapsedMilliseconds(requestStartedAt), generationTiming,
			))
		}
	}

	var cleanupErr error
	if shouldCleanup(session) {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		status, err := runnerprocess.Stop(cleanupContext, session.Preparation.Plan, runnerprocess.StopOptions{})
		if err != nil {
			cleanupErr = err
		} else if status.Kind != runnerprocess.StatusStopped {
			cleanupErr = runnerErrorf("demo cleanup did not stop the runner: %s", status.Detail)
		}
	}
	if operationErr != nil && cleanupErr != nil {
		return "", runnerErrorf("%v; cleanup also failed: %v", operationErr, cleanupErr)
	}
	if operationErr != nil {
		return "", operationErr
	}
	if cleanupErr != nil {
		return "", cleanupErr
	}
	return output, nil
}

func shouldCleanup(session tinySession) bool {
	if session.Preparation.Plan.Profile.ID == "" {
		return false
	}
	if session.OwnsProcess {
		return true
	}
	return session.Preparation.Baseline.Kind == runnerprocess.StatusStopped ||
		session.Preparation.Baseline.Kind == runnerprocess.StatusStale
}

func resolvePlan(id string, environment map[string]string, cached bool, executable string) (manifest.Plan, error) {
	profile, err := manifest.Get(id)
	if err != nil {
		return manifest.Plan{}, err
	}
	portValue, portSet := environment["OUTRIDER_PORT"]
	port, err := parsePort(portValue, portSet)
	if err != nil {
		return manifest.Plan{}, err
	}
	if executable == "" {
		executable = environment["LLAMA_SERVER_BIN"]
	}
	options := manifest.ResolveOptions{
		Root: environment["OUTRIDER_HOME"], Executable: executable, Port: port,
	}
	if cached {
		return manifest.ResolveCached(profile, options)
	}
	return manifest.Resolve(profile, options)
}

func parsePort(value string, set bool) (*int, error) {
	if !set {
		return nil, nil
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return nil, runnerErrorf("OUTRIDER_PORT must be an integer from 1 to 65535, got %q", value)
		}
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return nil, runnerErrorf("OUTRIDER_PORT must be an integer from 1 to 65535, got %s", value)
	}
	return &port, nil
}

func encodeOutput(value any) (string, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

func usageError(message string) error {
	return &manifest.ManifestError{Message: message + "\n\n" + usage}
}

func runnerErrorf(format string, args ...any) error {
	return &llama.RunnerError{Message: fmt.Sprintf(format, args...)}
}

func elapsedMilliseconds(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}
