package process

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/manifest"
)

type StatusKind string

const (
	StatusStopped    StatusKind = "stopped"
	StatusRunning    StatusKind = "running"
	StatusStale      StatusKind = "stale"
	StatusMismatched StatusKind = "mismatched"
)

type Status struct {
	Kind          StatusKind `json:"kind"`
	PID           int        `json:"pid,omitempty"`
	Preset        string     `json:"preset,omitempty"`
	Endpoint      string     `json:"endpoint"`
	Health        *bool      `json:"health,omitempty"`
	Detail        string     `json:"detail,omitempty"`
	LogFile       string     `json:"logFile"`
	StartedAt     string     `json:"startedAt,omitempty"`
	ResidentBytes int64      `json:"residentBytes,omitempty"`
	Timings       *Timings   `json:"timings,omitempty"`
}

type Timings struct {
	TimeToHealthMS float64 `json:"timeToHealthMs"`
}

type StartOptions struct {
	CWD                  string
	HealthTimeout        time.Duration
	HealthPollInterval   time.Duration
	HealthRequestTimeout time.Duration
}

type StopOptions struct {
	Wait         time.Duration
	PollInterval time.Duration
}

func Start(ctx context.Context, plan manifest.Plan, options StartOptions) (Status, error) {
	lock, err := AcquireLifecycleLock(ctx, plan.State.Lock)
	if err != nil {
		return Status{}, err
	}
	defer lock.Release()
	return StartWithLock(ctx, plan, options, lock)
}

func StartWithLock(
	ctx context.Context,
	plan manifest.Plan,
	options StartOptions,
	lock *LifecycleLock,
) (Status, error) {
	if err := lock.assertPath(plan.State.Lock); err != nil {
		return Status{}, err
	}
	return start(ctx, plan, options)
}

func start(ctx context.Context, plan manifest.Plan, options StartOptions) (Status, error) {
	record, err := ReadProcessRecord(plan.State.PID)
	if err != nil {
		return Status{}, err
	}
	if record != nil {
		observed := inspectProcess(record.PID)
		switch {
		case observed == nil:
			if processExists(record.PID) {
				return Status{}, cannotInspectError(record.PID)
			}
			if err := os.Remove(plan.State.PID); err != nil && !os.IsNotExist(err) {
				return Status{}, runnerError("could not remove stale process record", err)
			}
		case !IdentityMatches(*record, *observed):
			return Status{}, identityMismatchError(*record, *observed)
		default:
			if !recordMatchesPlan(*record, plan) {
				return Status{}, planMismatchError(*record, plan)
			}
			if _, err := endpoint.WaitForHealth(ctx, plan.HealthEndpoint, healthOptions(options)); err != nil {
				return Status{}, err
			}
			if err := assertRecordStillOwnsProcess(*record); err != nil {
				return Status{}, err
			}
			health := true
			return withProcessMetrics(Status{
				Kind: StatusRunning, PID: record.PID, Preset: record.Preset, Endpoint: plan.Endpoint,
				Health: &health, LogFile: plan.State.Log, Detail: "already running",
			}, *record), nil
		}
	}
	if err := ctx.Err(); err != nil {
		return Status{}, runnerError("runner start aborted", err)
	}
	if err := assertPortAvailable(plan); err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(plan.State.Run, 0o700); err != nil {
		return Status{}, runnerError("could not create run directory", err)
	}
	if err := os.MkdirAll(plan.State.Slots, 0o700); err != nil {
		return Status{}, runnerError("could not create slot cache directory", err)
	}
	command := exec.Command(plan.Executable, plan.Args...)
	command.Dir = options.CWD
	command.Stdin = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logFile, err := inheritLogFile(command, plan.State.Log)
	if err != nil {
		return Status{}, runnerError("could not open server log", err)
	}
	launchStartedAt := time.Now()
	if err := command.Start(); err != nil {
		logFile.Close()
		return Status{}, runnerError("could not launch llama-server", err)
	}
	pid := command.Process.Pid
	commandDone := make(chan error, 1)
	go func() {
		waitErr := command.Wait()
		_ = logFile.Close()
		commandDone <- waitErr
	}()
	observed := waitForProcessObservation(ctx, pid)
	if observed == nil {
		select {
		case waitErr := <-commandDone:
			return Status{}, startupExitError(waitErr, plan)
		default:
		}
		_ = command.Process.Kill()
		return Status{}, runnerErrorf(
			"launched llama-server PID %d but could not verify its process identity; see %s",
			pid, plan.State.Log,
		)
	}
	argv := append([]string{plan.Executable}, plan.Args...)
	expectedCommand := strings.Join(argv, " ")
	if observed.Command != expectedCommand {
		_ = command.Process.Kill()
		return Status{}, runnerErrorf(
			"launched llama-server PID %d with an unexpected command identity; see %s",
			pid, plan.State.Log,
		)
	}
	record = &ProcessRecord{
		SchemaVersion: ProcessRecordSchemaVersion,
		PID:           pid, StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ProcessStartedAt: observed.ProcessStartedAt,
		Executable:       plan.Executable, Command: observed.Command,
		Argv: argv, ArgvSHA256: ArgvSHA256(argv), Preset: plan.Profile.ID,
		Port: plan.Port, LogFile: plan.State.Log,
	}
	if err := writeProcessRecord(plan.State.PID, *record); err != nil {
		_ = command.Process.Kill()
		return Status{}, err
	}

	healthContext, cancelHealth := context.WithCancel(ctx)
	healthDone := make(chan error, 1)
	go func() {
		_, err := endpoint.WaitForHealth(healthContext, plan.HealthEndpoint, healthOptions(options))
		healthDone <- err
	}()
	select {
	case waitErr := <-commandDone:
		cancelHealth()
		if err := os.Remove(plan.State.PID); err != nil && !os.IsNotExist(err) {
			return Status{}, runnerError("llama-server exited and its active record could not be removed", err)
		}
		return Status{}, startupExitError(waitErr, plan)
	case err := <-healthDone:
		cancelHealth()
		if err == nil {
			break
		}
		failure := healthFailure(err, plan)
		if _, cleanupErr := stop(plan, StopOptions{}); cleanupErr != nil {
			return Status{}, runnerErrorf("%s; cleanup also failed: %v", failure, cleanupErr)
		}
		return Status{}, runnerErrorf("%s", failure)
	}
	if err := assertRecordStillOwnsProcess(*record); err != nil {
		if _, cleanupErr := stop(plan, StopOptions{}); cleanupErr != nil {
			return Status{}, runnerErrorf("%v; cleanup also failed: %v", err, cleanupErr)
		}
		return Status{}, err
	}
	if err := ctx.Err(); err != nil {
		if _, cleanupErr := stop(plan, StopOptions{}); cleanupErr != nil {
			return Status{}, runnerErrorf("runner start aborted: %v; cleanup also failed: %v", err, cleanupErr)
		}
		return Status{}, runnerError("runner start aborted", err)
	}
	health := true
	return withProcessMetrics(Status{
		Kind: StatusRunning, PID: record.PID, Preset: record.Preset, Endpoint: plan.Endpoint,
		Health: &health, LogFile: plan.State.Log, Detail: "started",
		Timings: &Timings{TimeToHealthMS: elapsedMilliseconds(launchStartedAt)},
	}, *record), nil
}

func assertPortAvailable(plan manifest.Plan) error {
	address := net.JoinHostPort(plan.Host, strconv.Itoa(plan.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return runnerErrorf(
			"cannot start %s: %s is already in use and no matching active record owns it",
			plan.Profile.ID, address,
		)
	}
	if err := listener.Close(); err != nil {
		return runnerError("could not release the endpoint admission probe", err)
	}
	return nil
}

func GetStatus(ctx context.Context, plan manifest.Plan) (Status, error) {
	record, err := ReadProcessRecord(plan.State.PID)
	if err != nil {
		return Status{}, err
	}
	if record == nil {
		return Status{Kind: StatusStopped, Endpoint: plan.Endpoint, LogFile: plan.State.Log}, nil
	}
	observed := inspectProcess(record.PID)
	if observed == nil {
		if processExists(record.PID) {
			return Status{
				Kind: StatusMismatched, PID: record.PID, Preset: record.Preset,
				Endpoint: plan.Endpoint, LogFile: record.LogFile, Detail: cannotInspectError(record.PID).Error(),
			}, nil
		}
		return Status{
			Kind: StatusStale, PID: record.PID, Preset: record.Preset, Endpoint: plan.Endpoint, LogFile: record.LogFile,
			Detail: fmt.Sprintf("PID file remains but PID %d is no longer running", record.PID),
		}, nil
	}
	if !IdentityMatches(*record, *observed) {
		return Status{
			Kind: StatusMismatched, PID: record.PID, Preset: record.Preset, Endpoint: plan.Endpoint, LogFile: record.LogFile,
			Detail: identityMismatchError(*record, *observed).Error(),
		}, nil
	}
	if !recordMatchesPlan(*record, plan) {
		return Status{
			Kind: StatusMismatched, PID: record.PID, Preset: record.Preset, Endpoint: plan.Endpoint, LogFile: record.LogFile,
			Detail: planMismatchError(*record, plan).Error(),
		}, nil
	}
	check := endpoint.CheckHealth(ctx, plan.HealthEndpoint, 2*time.Second)
	detail := "process running but endpoint is not healthy"
	if check.OK {
		detail = "healthy"
	}
	return withProcessMetrics(Status{
		Kind: StatusRunning, PID: record.PID, Preset: record.Preset, Endpoint: plan.Endpoint, Health: &check.OK,
		Detail:  detail,
		LogFile: plan.State.Log,
	}, *record), nil
}

func GetActiveStatus(ctx context.Context, state manifest.StatePaths) (Status, error) {
	record, err := ReadProcessRecord(state.PID)
	if err != nil {
		return Status{}, err
	}
	if record == nil {
		return Status{Kind: StatusStopped, Endpoint: "", LogFile: ""}, nil
	}
	endpointURL := processEndpoint(*record)
	observed := inspectProcess(record.PID)
	if observed == nil {
		if processExists(record.PID) {
			return Status{
				Kind: StatusMismatched, PID: record.PID, Preset: record.Preset,
				Endpoint: endpointURL, LogFile: record.LogFile, Detail: cannotInspectError(record.PID).Error(),
			}, nil
		}
		if err := os.Remove(state.PID); err != nil && !os.IsNotExist(err) {
			return Status{}, runnerError("could not repair stale active record", err)
		}
		return Status{
			Kind: StatusStopped, PID: record.PID, Preset: record.Preset,
			Endpoint: endpointURL, LogFile: record.LogFile,
			Detail: fmt.Sprintf("repaired stale active record for PID %d", record.PID),
		}, nil
	}
	if !IdentityMatches(*record, *observed) {
		return Status{
			Kind: StatusMismatched, PID: record.PID, Preset: record.Preset,
			Endpoint: endpointURL, LogFile: record.LogFile,
			Detail: identityMismatchError(*record, *observed).Error(),
		}, nil
	}
	check := endpoint.CheckHealth(ctx, endpointURL+"/health", 2*time.Second)
	detail := "process running but endpoint is not healthy"
	if check.OK {
		detail = "healthy"
	}
	return withProcessMetrics(Status{
		Kind: StatusRunning, PID: record.PID, Preset: record.Preset,
		Endpoint: endpointURL, Health: &check.OK, Detail: detail, LogFile: record.LogFile,
	}, *record), nil
}

func withProcessMetrics(status Status, record ProcessRecord) Status {
	status.StartedAt = record.StartedAt
	residentKiB, err := strconv.ParseInt(strings.TrimSpace(ps(record.PID, "rss=")), 10, 64)
	if err == nil && residentKiB > 0 {
		status.ResidentBytes = residentKiB * 1024
	}
	return status
}

func Stop(ctx context.Context, plan manifest.Plan, options StopOptions) (Status, error) {
	lock, err := AcquireLifecycleLock(ctx, plan.State.Lock)
	if err != nil {
		return Status{}, err
	}
	defer lock.Release()
	return stop(plan, options)
}

func StopActive(ctx context.Context, state manifest.StatePaths, options StopOptions) (Status, error) {
	lock, err := AcquireLifecycleLock(ctx, state.Lock)
	if err != nil {
		return Status{}, err
	}
	defer lock.Release()
	record, err := ReadProcessRecord(state.PID)
	if err != nil {
		return Status{}, err
	}
	if record == nil {
		return Status{Kind: StatusStopped, Detail: "already stopped"}, nil
	}
	return stopRecord(state.PID, *record, options)
}

func stop(plan manifest.Plan, options StopOptions) (Status, error) {
	record, err := ReadProcessRecord(plan.State.PID)
	if err != nil {
		return Status{}, err
	}
	if record == nil {
		return Status{
			Kind: StatusStopped, Endpoint: plan.Endpoint, LogFile: plan.State.Log, Detail: "already stopped",
		}, nil
	}
	return stopRecord(plan.State.PID, *record, options)
}

func stopRecord(recordPath string, record ProcessRecord, options StopOptions) (Status, error) {
	endpointURL := processEndpoint(record)
	observed := inspectProcess(record.PID)
	if observed == nil {
		if processExists(record.PID) {
			return Status{}, cannotInspectError(record.PID)
		}
		if err := os.Remove(recordPath); err != nil && !os.IsNotExist(err) {
			return Status{}, runnerError("could not remove stale process record", err)
		}
		return Status{
			Kind: StatusStopped, Preset: record.Preset, Endpoint: endpointURL,
			LogFile: record.LogFile, Detail: "removed stale process record",
		}, nil
	}
	if !IdentityMatches(record, *observed) {
		return Status{}, identityMismatchError(record, *observed)
	}
	if err := sendSignal(record, syscall.SIGTERM); err != nil {
		return Status{}, err
	}
	wait := options.Wait
	if wait == 0 {
		wait = 5 * time.Second
	}
	poll := options.PollInterval
	if poll == 0 {
		poll = 100 * time.Millisecond
	}
	deadline := time.Now().Add(wait)
	for {
		time.Sleep(min(poll, max(time.Millisecond, time.Until(deadline))))
		observed = inspectProcess(record.PID)
		if observed == nil {
			if processExists(record.PID) {
				return Status{}, cannotInspectError(record.PID)
			}
			return stoppedAfterRemovingRecord(recordPath, record, "stopped")
		}
		if !IdentityMatches(record, *observed) {
			return Status{}, identityMismatchError(record, *observed)
		}
		if time.Now().After(deadline) {
			break
		}
	}
	observed = inspectProcess(record.PID)
	if observed == nil {
		if processExists(record.PID) {
			return Status{}, cannotInspectError(record.PID)
		}
		return stoppedAfterRemovingRecord(recordPath, record, "stopped")
	}
	if !IdentityMatches(record, *observed) {
		return Status{}, identityMismatchError(record, *observed)
	}
	if err := sendSignal(record, syscall.SIGKILL); err != nil {
		return Status{}, err
	}
	time.Sleep(min(poll, 250*time.Millisecond))
	if inspectProcess(record.PID) != nil {
		return Status{}, runnerErrorf("PID %d did not stop; leaving %s for inspection", record.PID, recordPath)
	}
	if processExists(record.PID) {
		return Status{}, runnerErrorf("PID %d still exists; leaving %s for inspection", record.PID, recordPath)
	}
	return stoppedAfterRemovingRecord(recordPath, record, "stopped with SIGKILL after SIGTERM timeout")
}

func sendSignal(record ProcessRecord, signal syscall.Signal) error {
	observed := inspectProcess(record.PID)
	if observed == nil {
		if processExists(record.PID) {
			return cannotInspectError(record.PID)
		}
		return nil
	}
	if !IdentityMatches(record, *observed) {
		return identityMismatchError(record, *observed)
	}
	if err := syscall.Kill(record.PID, signal); err != nil && inspectProcess(record.PID) != nil {
		return runnerErrorf("could not send %s to verified PID %d: %v", signal, record.PID, err)
	}
	return nil
}

func assertRecordStillOwnsProcess(record ProcessRecord) error {
	observed := inspectProcess(record.PID)
	if observed == nil {
		if processExists(record.PID) {
			return cannotInspectError(record.PID)
		}
		return runnerErrorf("llama-server PID %d exited before the healthy response; see %s", record.PID, record.LogFile)
	}
	if !IdentityMatches(record, *observed) {
		return identityMismatchError(record, *observed)
	}
	return nil
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func cannotInspectError(pid int) error {
	return runnerErrorf("PID %d exists but its process identity cannot be inspected; refusing to modify state", pid)
}

func waitForProcessObservation(ctx context.Context, pid int) *ObservedProcess {
	for range 20 {
		if observed := inspectProcess(pid); observed != nil {
			return observed
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}

func stoppedAfterRemovingRecord(recordPath string, record ProcessRecord, detail string) (Status, error) {
	if err := os.Remove(recordPath); err != nil && !os.IsNotExist(err) {
		return Status{}, runnerError("could not remove process record", err)
	}
	return Status{
		Kind: StatusStopped, Preset: record.Preset, Endpoint: processEndpoint(record),
		LogFile: record.LogFile, Detail: detail,
	}, nil
}

func processEndpoint(record ProcessRecord) string {
	return fmt.Sprintf("http://%s:%d", manifest.DefaultHost, record.Port)
}

func healthOptions(options StartOptions) endpoint.WaitOptions {
	return endpoint.WaitOptions{
		Timeout: options.HealthTimeout, PollInterval: options.HealthPollInterval,
		RequestTimeout: options.HealthRequestTimeout,
	}
}

func healthFailure(err error, plan manifest.Plan) string {
	return fmt.Sprintf(
		"llama-server did not become healthy at %s: %v; log: %s",
		plan.HealthEndpoint, err, plan.State.Log,
	)
}

func elapsedMilliseconds(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

func recordMatchesPlan(record ProcessRecord, plan manifest.Plan) bool {
	argv := append([]string{plan.Executable}, plan.Args...)
	return record.Executable == plan.Executable &&
		record.ArgvSHA256 == ArgvSHA256(argv) &&
		record.Preset == plan.Profile.ID &&
		record.Port == plan.Port &&
		record.LogFile == plan.State.Log
}

func planMismatchError(record ProcessRecord, plan manifest.Plan) error {
	return runnerErrorf(
		"running PID %d does not match the requested %s plan; run outrider down before starting the new plan",
		record.PID, plan.Profile.ID,
	)
}

func inheritLogFile(command *exec.Cmd, path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	command.Stdout = file
	command.Stderr = file
	return file, nil
}
