package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/corvines/outrider/internal/admission"
	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/llama"
	"github.com/corvines/outrider/internal/manifest"
	runnerprocess "github.com/corvines/outrider/internal/process"
)

const usage = `outrider: loopback llama.cpp runner

  outrider plan <profile>
  outrider check <profile>
  outrider verify <profile>
  outrider serve <profile>
  outrider smoke
  outrider demo <profile>
  outrider ps
  outrider stop

Compatibility aliases: up = serve, status = ps, down = stop.

Environment overrides: LLAMA_SERVER_BIN, OUTRIDER_HOME,
OUTRIDER_PORT.
`

type runPreparation struct {
	Profile              manifest.Profile
	Plan                 manifest.Plan
	Baseline             runnerprocess.Status
	Admission            admission.Report
	StartedAt            time.Time
	RuntimePreparationMS float64
	ModelPreparationMS   float64
	RuntimeDownloadMS    float64
	ModelDownloadMS      float64
	TotalReadyMS         float64
}

type runSession struct {
	Preparation runPreparation
	Status      runnerprocess.Status
	ColdStartMS *float64
	OwnsProcess bool
}

type runOptions struct {
	Progress llama.ProgressFunc
}

type downloadTracker struct {
	forward llama.ProgressFunc
	starts  map[string]time.Time
	totalMS float64
}

func newDownloadTracker(forward llama.ProgressFunc) *downloadTracker {
	return &downloadTracker{forward: forward, starts: make(map[string]time.Time)}
}

func (tracker *downloadTracker) report(progress llama.DownloadProgress) {
	if _, exists := tracker.starts[progress.Name]; !exists {
		tracker.starts[progress.Name] = time.Now()
	}
	if tracker.forward != nil {
		tracker.forward(progress)
	}
	if progress.Done {
		tracker.totalMS += elapsedMilliseconds(tracker.starts[progress.Name])
		delete(tracker.starts, progress.Name)
	}
}

func run(ctx context.Context, argv []string, environment map[string]string) (string, error) {
	return runWithOptions(ctx, argv, environment, runOptions{})
}

func runWithOptions(
	ctx context.Context,
	argv []string,
	environment map[string]string,
	options runOptions,
) (string, error) {
	var command string
	if len(argv) > 0 {
		command = argv[0]
	}
	command = canonicalCommand(command)
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
	case "check":
		if len(argv) != 2 {
			return "", usageError("check expects exactly one profile id")
		}
		profile, err := manifest.Get(argv[1])
		if err != nil {
			return "", err
		}
		plan, err := resolvePlan(argv[1], environment, true, "")
		if err != nil {
			return "", err
		}
		status, err := runnerprocess.GetStatus(ctx, plan)
		if err != nil {
			return "", err
		}
		portOwned := status.Kind == runnerprocess.StatusRunning
		report := admission.Inspect(ctx, profile, plan, portOwned)
		return encodeOutput(admission.WithRuntimeCapabilities(ctx, report, plan, false))
	case "verify":
		if len(argv) != 2 {
			return "", usageError("verify expects exactly one runnable profile id")
		}
		profile, err := runnableProfile(argv[1])
		if err != nil {
			return "", err
		}
		plan, err := resolvePlan(argv[1], environment, true, "")
		if err != nil {
			return "", err
		}
		status, err := runnerprocess.GetStatus(ctx, plan)
		if err != nil {
			return "", err
		}
		if status.Kind != runnerprocess.StatusRunning || status.Health == nil || !*status.Health {
			return "", runnerErrorf("profile %s is not the healthy active server", profile.ID)
		}
		contract, err := endpoint.VerifyModelContract(ctx, plan.Endpoint, profile)
		if err != nil {
			return "", err
		}
		return encodeOutput(contract)
	case "serve":
		if len(argv) != 2 {
			return "", usageError("serve expects exactly one runnable profile id")
		}
		session, err := startSession(ctx, argv[1], environment, options)
		if err != nil {
			return "", err
		}
		return encodeOutput(newUpOutput(session))
	case "smoke":
		if len(argv) != 1 {
			return "", usageError("smoke does not accept a preset id")
		}
		return runDemo(ctx, "tiny", environment, options)
	case "demo":
		if len(argv) != 2 {
			return "", usageError("demo expects exactly one runnable profile id")
		}
		return runDemo(ctx, argv[1], environment, options)
	case "ps", "stop":
		if len(argv) != 1 {
			return "", usageError(fmt.Sprintf("%s does not accept a profile id", command))
		}
		state, err := activeState(environment)
		if err != nil {
			return "", err
		}
		var status runnerprocess.Status
		if command == "ps" {
			status, err = runnerprocess.GetActiveStatus(ctx, state)
		} else {
			status, err = runnerprocess.StopActive(ctx, state, runnerprocess.StopOptions{})
		}
		if err != nil {
			return "", err
		}
		return encodeOutput(status)
	default:
		return "", usageError(fmt.Sprintf("unknown command %q; see usage", command))
	}
}

func canonicalCommand(command string) string {
	switch command {
	case "up":
		return "serve"
	case "status":
		return "ps"
	case "down":
		return "stop"
	default:
		return command
	}
}

func activeState(environment map[string]string) (manifest.StatePaths, error) {
	profile, err := manifest.Get("tiny")
	if err != nil {
		return manifest.StatePaths{}, err
	}
	return manifest.Paths(environment["OUTRIDER_HOME"], profile, "")
}

func startSession(
	ctx context.Context,
	profileID string,
	environment map[string]string,
	options runOptions,
) (runSession, error) {
	profile, err := runnableProfile(profileID)
	if err != nil {
		return runSession{}, err
	}
	initialPlan, err := resolvePlan(profileID, environment, true, "")
	if err != nil {
		return runSession{}, err
	}
	lock, err := runnerprocess.AcquireUpLock(ctx, initialPlan)
	if err != nil {
		return runSession{}, err
	}
	defer lock.Release()
	startedAt := time.Now()
	tracker := newDownloadTracker(options.Progress)
	baseline, err := runnerprocess.GetStatus(ctx, initialPlan)
	if err != nil {
		return runSession{}, err
	}
	if baseline.Kind == runnerprocess.StatusMismatched {
		return runSession{}, runnerErrorf("%s", baseline.Detail)
	}
	report := admission.Inspect(ctx, profile, initialPlan, baseline.Kind == runnerprocess.StatusRunning)
	if report.Blocking() {
		return runSession{}, &admission.Error{Report: report}
	}
	if baseline.Kind == runnerprocess.StatusRunning {
		report = admission.WithRuntimeCapabilities(ctx, report, initialPlan, true)
		if report.Blocking() {
			return runSession{}, &admission.Error{Report: report}
		}
		status, err := runnerprocess.StartWithLock(ctx, initialPlan, runnerprocess.StartOptions{}, lock)
		if err != nil {
			return runSession{}, err
		}
		return runSession{
			Preparation: runPreparation{
				Profile: profile, Plan: initialPlan, Baseline: baseline, Admission: report, StartedAt: startedAt,
				TotalReadyMS: elapsedMilliseconds(startedAt),
			},
			Status: status,
		}, nil
	}
	runtimeStartedAt := time.Now()
	executable, err := llama.EnsureServer(ctx, llama.EnsureServerOptions{
		StateRoot: initialPlan.State.Root, ExecutableOverride: environment["LLAMA_SERVER_BIN"], Progress: tracker.report,
	})
	if err != nil {
		return runSession{}, err
	}
	runtimePreparationMS := elapsedMilliseconds(runtimeStartedAt)
	runtimeDownloadMS := tracker.totalMS
	plan, err := resolvePlan(profileID, environment, true, executable)
	if err != nil {
		return runSession{}, err
	}
	report = admission.WithRuntimeCapabilities(ctx, report, plan, true)
	if report.Blocking() {
		return runSession{}, &admission.Error{Report: report}
	}
	modelStartedAt := time.Now()
	if _, err := llama.EnsureModelCached(ctx, profile, plan, llama.EnsureModelOptions{Progress: tracker.report}); err != nil {
		return runSession{}, err
	}
	modelPreparationMS := elapsedMilliseconds(modelStartedAt)
	modelDownloadMS := tracker.totalMS - runtimeDownloadMS
	status, err := runnerprocess.StartWithLock(ctx, plan, runnerprocess.StartOptions{}, lock)
	if err != nil {
		return runSession{}, err
	}
	session := runSession{
		Preparation: runPreparation{
			Profile: profile, Plan: plan, Baseline: baseline, Admission: report, StartedAt: startedAt,
			RuntimePreparationMS: runtimePreparationMS, ModelPreparationMS: modelPreparationMS,
			RuntimeDownloadMS: runtimeDownloadMS, ModelDownloadMS: modelDownloadMS,
			TotalReadyMS: elapsedMilliseconds(startedAt),
		},
		Status: status, OwnsProcess: status.Detail == "started",
	}
	if session.OwnsProcess {
		if status.Timings != nil {
			coldStart := status.Timings.TimeToHealthMS
			session.ColdStartMS = &coldStart
		}
	}
	return session, nil
}

func runDemo(
	ctx context.Context,
	profileID string,
	environment map[string]string,
	options runOptions,
) (string, error) {
	session, operationErr := startSession(ctx, profileID, environment, options)
	var output string
	if operationErr == nil {
		requestStartedAt := time.Now()
		temperature := session.Preparation.Profile.Sampling.Temperature
		topP := session.Preparation.Profile.Sampling.TopP
		completion, err := endpoint.RequestChatCompletion(ctx, session.Preparation.Plan.Endpoint, endpoint.ChatOptions{
			Model:        session.Preparation.Profile.ID,
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

func shouldCleanup(session runSession) bool {
	if session.Preparation.Plan.Profile.ID == "" {
		return false
	}
	if session.OwnsProcess {
		return true
	}
	return session.Preparation.Baseline.Kind == runnerprocess.StatusStopped ||
		session.Preparation.Baseline.Kind == runnerprocess.StatusStale
}

func runnableProfile(id string) (manifest.Profile, error) {
	profile, err := manifest.Get(id)
	if err != nil {
		return manifest.Profile{}, err
	}
	if !profile.Runnable {
		return manifest.Profile{}, usageError(fmt.Sprintf("profile %q is plan-only", id))
	}
	return profile, nil
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
