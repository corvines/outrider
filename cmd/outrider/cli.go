package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/corvines/outrider/internal/admission"
	"github.com/corvines/outrider/internal/chat"
	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/installer"
	"github.com/corvines/outrider/internal/llama"
	"github.com/corvines/outrider/internal/manifest"
	"github.com/corvines/outrider/internal/ollamacache"
	runnerprocess "github.com/corvines/outrider/internal/process"
)

const usage = `outrider: loopback llama.cpp runner

  outrider plan <profile>
  outrider check <profile>
  outrider verify <profile>
  outrider models
  outrider show <profile>
  outrider pull <profile>
  outrider cache clean [--apply]
  outrider install [--link] [--replace-unmanaged]
  outrider uninstall [--purge | --keep-state]
  outrider version
  outrider start
  outrider use <profile>
  outrider status
  outrider serve [profile]
  outrider run <profile|cached-model>
  outrider chat [--endpoint URL]
  outrider smoke
  outrider demo <profile>
  outrider ps
  outrider logs [--lines N]
  outrider stop [--skip-checkpoint]

Compatibility aliases: ls = models, up = serve, down = stop.

Add --json anywhere for machine-readable output.

Environment overrides: LLAMA_SERVER_BIN, OUTRIDER_HOME, OUTRIDER_PORT.

Cached-model runs read GGUF files already on the machine. They start no
other program.
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
	Progress          llama.ProgressFunc
	Notice            func(string)
	Chat              func(string) error
	CurrentExecutable func() (string, error)
	Confirm           func(string) (bool, error)
	Human             bool
	Ephemeral         bool
}

func (options runOptions) notice(format string, arguments ...any) {
	if options.Notice != nil {
		options.Notice(fmt.Sprintf(format, arguments...))
	}
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
	case "ls":
		if len(argv) != 1 {
			return "", usageError("ls does not accept arguments")
		}
		profiles, err := manifest.Offered()
		if err != nil {
			return "", err
		}
		output := profileListOutput{Profiles: make([]profileSummaryOutput, 0, len(profiles))}
		for _, profile := range profiles {
			state, err := manifest.Paths(environment["OUTRIDER_HOME"], profile, "")
			if err != nil {
				return "", err
			}
			summary, err := newProfileSummary(profile, state)
			if err != nil {
				return "", err
			}
			output.Profiles = append(output.Profiles, summary)
		}
		if manifest.DevEnabled() {
			cacheRoot, rootErr := ollamacache.DefaultRoot(environment["HOME"], environment["OLLAMA_MODELS"])
			if rootErr != nil {
				return "", rootErr
			}
			discovered, discoverErr := ollamacache.Discover(cacheRoot)
			if discoverErr != nil {
				return "", discoverErr
			}
			output.DevelopmentModels = discovered
		}
		return formatOutput(output, options.Human)
	case "show":
		if len(argv) != 2 {
			return "", usageError("show expects exactly one profile id")
		}
		profile, err := manifest.Get(argv[1])
		if err != nil {
			return "", err
		}
		state, err := manifest.Paths(environment["OUTRIDER_HOME"], profile, "")
		if err != nil {
			return "", err
		}
		cache, err := inspectProfileCache(profile, state.Model)
		if err != nil {
			return "", err
		}
		return formatOutput(profileDetailOutput{Profile: profile, Cache: cache}, options.Human)
	case "pull":
		if len(argv) != 2 {
			return "", usageError("pull expects exactly one runnable profile id")
		}
		output, err := pullProfile(ctx, argv[1], environment, options)
		if err != nil {
			return "", err
		}
		return formatOutput(output, options.Human)
	case "cache":
		if len(argv) < 2 || argv[1] != "clean" {
			return "", usageError("cache expects the clean subcommand")
		}
		apply, err := parseCacheCleanArguments(argv[2:])
		if err != nil {
			return "", err
		}
		output, err := cleanCache(environment, apply)
		if err != nil {
			return "", err
		}
		return formatOutput(output, options.Human)
	case "install":
		installOptions, err := parseInstallArguments(argv[1:])
		if err != nil {
			return "", err
		}
		executable := options.CurrentExecutable
		if executable == nil {
			executable = currentExecutable
		}
		source, err := executable()
		if err != nil {
			return "", err
		}
		marker, err := installer.InstallUserWithOptions(source, environment["HOME"], installOptions)
		if err != nil {
			return "", err
		}
		layout, err := installer.ResolveUserLayout(environment["HOME"])
		if err != nil {
			return "", err
		}
		return formatOutput(installOutput{
			Status: "installed", Target: layout.Target, Marker: layout.Marker,
			SHA256: marker.SHA256, Link: marker.Link,
		}, options.Human)
	case "uninstall":
		choice, err := parseUninstallArguments(argv[1:])
		if err != nil {
			return "", err
		}
		return uninstall(ctx, environment, choice, options)
	case "version":
		if len(argv) != 1 {
			return "", usageError("version does not accept arguments")
		}
		return formatOutput(currentVersion(), options.Human)
	case "start":
		if len(argv) != 1 {
			return "", usageError("start does not accept arguments")
		}
		status, err := startGateway(ctx, environment)
		if err != nil {
			return "", err
		}
		return formatOutput(status, options.Human)
	case "use":
		if len(argv) != 2 {
			return "", usageError("use expects exactly one runnable profile id")
		}
		output, err := useGatewayModel(ctx, argv[1], environment, options)
		if err != nil {
			return "", err
		}
		return formatOutput(output, options.Human)
	case "status":
		if len(argv) != 1 {
			return "", usageError("status does not accept arguments")
		}
		status, err := getServiceStatus(ctx, environment)
		if err != nil {
			return "", err
		}
		return formatOutput(status, options.Human)
	case "chat":
		endpoint, err := parseChatArguments(argv[1:])
		if err != nil {
			return "", err
		}
		runChat := options.Chat
		if runChat == nil {
			runChat = chat.Run
		}
		if err := runChat(endpoint); err != nil {
			return "", err
		}
		return "", nil
	case "plan":
		if len(argv) != 2 {
			return "", usageError("plan expects exactly one preset id")
		}
		plan, err := resolvePlan(argv[1], environment, false, "")
		if err != nil {
			return "", err
		}
		return formatOutput(newPlanOutput(plan), options.Human)
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
		portOwned, err := outriderOwnsPort(ctx, plan, environment)
		if err != nil {
			return "", err
		}
		report := admission.Inspect(ctx, profile, plan, portOwned)
		return formatOutput(admission.WithRuntimeCapabilities(ctx, report, plan, false), options.Human)
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
		return formatOutput(contract, options.Human)
	case "serve":
		if len(argv) == 1 {
			if err := runGateway(ctx, environment, options); err != nil {
				return "", err
			}
			return "", nil
		}
		if len(argv) != 2 {
			return "", usageError("serve accepts at most one runnable profile id")
		}
		output, err := serveProfile(ctx, argv[1], environment, options)
		if err != nil {
			return "", err
		}
		return formatOutput(output, options.Human)
	case "run":
		if len(argv) != 2 {
			return "", usageError("run expects exactly one profile or development model name")
		}
		return runInteractive(ctx, argv[1], environment, options)
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
	case "ps":
		if len(argv) != 1 {
			return "", usageError(fmt.Sprintf("%s does not accept a profile id", command))
		}
		state, err := activeState(environment)
		if err != nil {
			return "", err
		}
		status, err := runnerprocess.GetActiveStatus(ctx, state)
		if err != nil {
			return "", err
		}
		return formatOutput(status, options.Human)
	case "stop":
		discardSession, err := parseStopArguments(argv[1:])
		if err != nil {
			return "", err
		}
		status, err := stopServices(ctx, environment, discardSession)
		if err != nil {
			return "", err
		}
		return formatOutput(status, options.Human)
	case "logs":
		lines, err := parseLogArguments(argv[1:])
		if err != nil {
			return "", err
		}
		output, err := readActiveLog(ctx, environment, lines)
		if err != nil {
			return "", err
		}
		return formatOutput(output, options.Human)
	default:
		return "", usageError(fmt.Sprintf("unknown command %q; see usage", command))
	}
}

type stateChoice int

const (
	stateAsk stateChoice = iota
	statePurge
	stateKeep
)

func parseUninstallArguments(arguments []string) (stateChoice, error) {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	purge := flags.Bool("purge", false, "remove the state root without asking")
	keep := flags.Bool("keep-state", false, "keep the state root without asking")
	if err := flags.Parse(arguments); err != nil {
		return stateAsk, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return stateAsk, usageError("uninstall accepts only --purge or --keep-state")
	}
	switch {
	case *purge && *keep:
		return stateAsk, usageError("--purge and --keep-state contradict each other")
	case *purge:
		return statePurge, nil
	case *keep:
		return stateKeep, nil
	}
	return stateAsk, nil
}

func uninstall(
	ctx context.Context,
	environment map[string]string,
	choice stateChoice,
	options runOptions,
) (string, error) {
	if _, err := stopServices(ctx, environment, false); err != nil {
		return "", err
	}
	layout, err := installer.ResolveUserLayout(environment["HOME"])
	if err != nil {
		return "", err
	}
	root, err := manifest.StateRoot(environment["OUTRIDER_HOME"])
	if err != nil {
		return "", err
	}
	state, err := installer.InspectStateRoot(root)
	if err != nil {
		return "", err
	}
	remove, prompted, err := decideStateRemoval(state, choice, options)
	if err != nil {
		return "", err
	}
	if remove {
		if err := installer.RemoveStateRoot(state.Root); err != nil {
			return "", err
		}
	}
	if err := installer.UninstallUser(environment["HOME"]); err != nil {
		return "", err
	}
	output := installOutput{
		Status: "uninstalled", Target: layout.Target,
		StateRoot: state.Root, StateBytes: state.Bytes,
		StateRemoved: remove, StatePrompted: prompted,
	}
	return formatOutput(output, options.Human)
}

// decideStateRemoval resolves the flags, the prompt, and the non-interactive
// fallback into one answer. A run that cannot ask keeps the state root.
func decideStateRemoval(
	state installer.StateRootReport,
	choice stateChoice,
	options runOptions,
) (bool, bool, error) {
	switch {
	case choice == statePurge:
		return true, false, nil
	case choice == stateKeep, !state.Exists, options.Confirm == nil:
		return false, false, nil
	}
	prompt := fmt.Sprintf(
		"Remove the Outrider state root %s (%s)?", state.Root, formatByteCount(state.Bytes),
	)
	answer, err := options.Confirm(prompt)
	if err != nil {
		return false, false, err
	}
	return answer, true, nil
}

func parseInstallArguments(arguments []string) (installer.UserInstallOptions, error) {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	replaceUnmanaged := flags.Bool("replace-unmanaged", false, "replace an existing unmarked Outrider binary")
	link := flags.Bool("link", false, "symlink the install target at this binary instead of copying it")
	if err := flags.Parse(arguments); err != nil {
		return installer.UserInstallOptions{}, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return installer.UserInstallOptions{}, usageError("install accepts only --link and --replace-unmanaged")
	}
	return installer.UserInstallOptions{ReplaceUnmanaged: *replaceUnmanaged, Link: *link}, nil
}

func runInteractive(
	ctx context.Context,
	profileID string,
	environment map[string]string,
	options runOptions,
) (string, error) {
	_, nativeErr := manifest.Get(profileID)
	var session runSession
	var operationErr error
	if nativeErr == nil {
		session, operationErr = startSession(ctx, profileID, environment, options)
	} else {
		session, operationErr = startDevelopmentSession(ctx, profileID, environment, options)
	}
	if operationErr == nil {
		runChat := options.Chat
		if runChat == nil {
			runChat = chat.Run
		}
		operationErr = runChat(session.Preparation.Plan.Endpoint)
	}
	cleanupErr := cleanupSession(session, false)
	if operationErr != nil && cleanupErr != nil {
		return "", runnerErrorf("%v; cleanup also failed: %v", operationErr, cleanupErr)
	}
	if operationErr != nil {
		return "", operationErr
	}
	return "", cleanupErr
}

func startDevelopmentSession(
	ctx context.Context,
	name string,
	environment map[string]string,
	options runOptions,
) (runSession, error) {
	ollamaRoot, err := ollamacache.DefaultRoot(environment["HOME"], environment["OLLAMA_MODELS"])
	if err != nil {
		return runSession{}, err
	}
	model, found, err := ollamacache.Find(ollamaRoot, name)
	if err != nil {
		return runSession{}, err
	}
	if !found {
		return runSession{}, usageError(fmt.Sprintf("unknown profile or development model %q", name))
	}
	profile, err := developmentProfile(model)
	if err != nil {
		return runSession{}, err
	}
	baseState, err := activeState(environment)
	if err != nil {
		return runSession{}, err
	}
	executable, err := llama.EnsureServer(ctx, llama.EnsureServerOptions{
		StateRoot: baseState.Root, ExecutableOverride: environment["LLAMA_SERVER_BIN"], Progress: options.Progress,
	})
	if err != nil {
		return runSession{}, err
	}
	port, err := availableDevelopmentPort()
	if err != nil {
		return runSession{}, err
	}
	developmentRoot := filepath.Join(baseState.Root, "development")
	plan, err := manifest.Resolve(profile, manifest.ResolveOptions{
		Root: developmentRoot, Executable: executable, Port: &port,
	})
	if err != nil {
		return runSession{}, err
	}
	startedAt := time.Now()
	report := admission.Inspect(ctx, profile, plan, false)
	if report.Blocking() {
		return runSession{}, &admission.Error{Report: report}
	}
	report = admission.WithRuntimeCapabilities(ctx, report, plan, true)
	if report.Blocking() {
		return runSession{}, &admission.Error{Report: report}
	}
	if err := ollamacache.Verify(ctx, model, developmentVerifyProgress(options.Progress)); err != nil {
		return runSession{}, err
	}
	status, err := runnerprocess.Start(ctx, plan, runnerprocess.StartOptions{})
	if err != nil {
		return runSession{}, err
	}
	session := runSession{
		Preparation: runPreparation{
			Profile: profile, Plan: plan, Baseline: runnerprocess.Status{Kind: runnerprocess.StatusStopped},
			Admission: report, StartedAt: startedAt, TotalReadyMS: elapsedMilliseconds(startedAt),
		},
		Status: status, OwnsProcess: status.Detail == "started",
	}
	if status.Timings != nil {
		coldStart := status.Timings.TimeToHealthMS
		session.ColdStartMS = &coldStart
	}
	return session, nil
}

func developmentProfile(model ollamacache.Model) (manifest.Profile, error) {
	profile, err := manifest.Get("tiny")
	if err != nil {
		return manifest.Profile{}, err
	}
	profile.ID = model.Name
	profile.Description = "Development model from the local GGUF cache"
	profile.Persistence.Enabled = false
	profile.Model = manifest.Artifact{
		LocalPath: model.Path, SHA256: strings.TrimPrefix(model.Digest, "sha256:"), SizeBytes: model.SizeBytes,
	}
	if parameters := model.Parameters; parameters != nil {
		if parameters.Temperature != nil {
			profile.Sampling.Temperature = *parameters.Temperature
		}
		if parameters.TopP != nil {
			profile.Sampling.TopP = *parameters.TopP
		}
		if parameters.TopK != nil {
			profile.Sampling.TopK = *parameters.TopK
		}
		if parameters.MinP != nil {
			profile.Sampling.MinP = *parameters.MinP
		}
		if parameters.RepeatPenalty != nil {
			profile.Sampling.RepeatPenalty = *parameters.RepeatPenalty
		}
	}
	profile.ExtraArgs = append(profile.ExtraArgs, "--no-webui")
	if err := manifest.Validate(profile); err != nil {
		return manifest.Profile{}, err
	}
	return profile, nil
}

func availableDevelopmentPort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(manifest.DefaultHost, "0"))
	if err != nil {
		return 0, runnerErrorf("could not reserve a development port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, runnerErrorf("could not release the development port probe: %v", err)
	}
	return port, nil
}

func developmentVerifyProgress(forward llama.ProgressFunc) ollamacache.VerifyProgressFunc {
	if forward == nil {
		return nil
	}
	return func(progress ollamacache.VerifyProgress) {
		forward(llama.DownloadProgress{
			Name: "verify " + progress.Name, Downloaded: progress.Verified, Total: progress.Total,
			BytesPerSecond: progress.BytesPerSecond, ETA: progress.ETA, Done: progress.Done,
		})
	}
}

func parseChatArguments(arguments []string) (string, error) {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("endpoint", "", "model endpoint")
	if err := flags.Parse(arguments); err != nil {
		return "", usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return "", usageError("chat accepts only --endpoint URL")
	}
	return *endpoint, nil
}

func parseStopArguments(arguments []string) (bool, error) {
	flags := flag.NewFlagSet("stop", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	discard := flags.Bool("skip-checkpoint", false, "stop without checkpointing persistent KV")
	if err := flags.Parse(arguments); err != nil {
		return false, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return false, usageError("stop accepts only --skip-checkpoint")
	}
	return *discard, nil
}

func canonicalCommand(command string) string {
	switch command {
	case "models":
		return "ls"
	case "up":
		return "serve"
	case "down":
		return "stop"
	default:
		return command
	}
}

func pullProfile(
	ctx context.Context,
	profileID string,
	environment map[string]string,
	options runOptions,
) (pullOutput, error) {
	profile, err := runnableProfile(profileID)
	if err != nil {
		return pullOutput{}, err
	}
	initialPlan, err := resolvePlan(profileID, environment, true, "")
	if err != nil {
		return pullOutput{}, err
	}
	// Pulling does not bind the serving port, so an existing listener must not
	// block cache preparation.
	report := admission.Inspect(ctx, profile, initialPlan, true)
	if report.Blocking() {
		return pullOutput{}, &admission.Error{Report: report}
	}
	executable, err := llama.EnsureServer(ctx, llama.EnsureServerOptions{
		StateRoot: initialPlan.State.Root, ExecutableOverride: environment["LLAMA_SERVER_BIN"],
		Progress: options.Progress,
	})
	if err != nil {
		return pullOutput{}, err
	}
	plan, err := resolvePlan(profileID, environment, true, executable)
	if err != nil {
		return pullOutput{}, err
	}
	report = admission.WithRuntimeCapabilities(ctx, report, plan, true)
	if report.Blocking() {
		return pullOutput{}, &admission.Error{Report: report}
	}
	model, err := llama.EnsureModelCached(ctx, profile, plan, llama.EnsureModelOptions{Progress: options.Progress})
	if err != nil {
		return pullOutput{}, err
	}
	return pullOutput{
		Profile: profile.ID, Runtime: executable, Model: model,
		SizeBytes: profile.Model.SizeBytes, Admission: report,
	}, nil
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
		status, err := runnerprocess.StartWithLock(ctx, initialPlan, runnerprocess.StartOptions{
			SkipSessionRestore: options.Ephemeral,
		}, lock)
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
	options.notice("Checking the llama.cpp runtime...")
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
	options.notice("Verifying the %s model cache (%s)...", profile.ID, formatByteCount(profile.Model.SizeBytes))
	if _, err := llama.EnsureModelCached(ctx, profile, plan, llama.EnsureModelOptions{Progress: tracker.report}); err != nil {
		return runSession{}, err
	}
	modelPreparationMS := elapsedMilliseconds(modelStartedAt)
	modelDownloadMS := tracker.totalMS - runtimeDownloadMS
	options.notice("Loading %s and waiting for it to become ready...", profile.ID)
	startName := fmt.Sprintf("starting on %s:%d", plan.Host, plan.Port)
	if options.Progress != nil {
		options.Progress(llama.DownloadProgress{Name: startName})
	}
	status, err := runnerprocess.StartWithLock(ctx, plan, runnerprocess.StartOptions{
		SkipSessionRestore: options.Ephemeral,
	}, lock)
	if options.Progress != nil {
		options.Progress(llama.DownloadProgress{Name: startName, Done: err == nil})
	}
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
	options.Ephemeral = true
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
			output, operationErr = formatOutput(newDemoOutput(
				session, completion.AssistantResponse, elapsedMilliseconds(requestStartedAt), generationTiming,
			), options.Human)
		}
	}

	cleanupErr := cleanupSession(session, true)
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

func cleanupSession(session runSession, discardSession bool) error {
	if !shouldCleanup(session) {
		return nil
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	status, err := runnerprocess.Stop(cleanupContext, session.Preparation.Plan, runnerprocess.StopOptions{
		DiscardSession: discardSession,
	})
	if err != nil {
		return err
	}
	if status.Kind != runnerprocess.StatusStopped {
		return runnerErrorf("cleanup did not stop the runner: %s", status.Detail)
	}
	return nil
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
	if profile.Dev && !manifest.DevEnabled() {
		return manifest.Profile{}, usageError(
			fmt.Sprintf("profile %q is a development profile; set OUTRIDER_DEV=1 to use it", id),
		)
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

func formatOutput(value any, human bool) (string, error) {
	if human {
		return humanOutput(value)
	}
	return encodeOutput(value)
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

// outriderOwnsPort reports whether the port the plan wants is already held by
// an Outrider process. The gateway listens on the front port and the model
// backend on the next one, so both records are consulted.
func outriderOwnsPort(ctx context.Context, plan manifest.Plan, environment map[string]string) (bool, error) {
	model, err := runnerprocess.GetStatus(ctx, plan)
	if err != nil {
		return false, err
	}
	if model.Kind == runnerprocess.StatusRunning {
		return true, nil
	}
	gatewayPlan, err := gatewayProcessPlan(environment)
	if err != nil {
		return false, err
	}
	if gatewayPlan.Port != plan.Port {
		return false, nil
	}
	gateway, err := runnerprocess.GetStatus(ctx, gatewayPlan)
	if err != nil {
		return false, err
	}
	return ownsPort(plan.Port, model, gatewayPlan.Port, gateway), nil
}

// ownsPort decides whether either Outrider record holds the wanted port. The
// gateway record covers the front port, which is the one a profile plan names.
func ownsPort(wanted int, model runnerprocess.Status, gatewayPort int, gateway runnerprocess.Status) bool {
	if model.Kind == runnerprocess.StatusRunning {
		return true
	}
	return gatewayPort == wanted && gateway.Kind == runnerprocess.StatusRunning
}
