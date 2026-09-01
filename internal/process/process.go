package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Kind     StatusKind `json:"kind"`
	PID      int        `json:"pid,omitempty"`
	Endpoint string     `json:"endpoint"`
	Health   *bool      `json:"health,omitempty"`
	Detail   string     `json:"detail,omitempty"`
	LogFile  string     `json:"logFile"`
	Timings  *Timings   `json:"timings,omitempty"`
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
	lockPath := lifecycleLockPath(plan.State.Run)
	lock, err := AcquireLifecycleLock(ctx, lockPath)
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
	if err := lock.assertPath(lifecycleLockPath(plan.State.Run)); err != nil {
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
			return Status{
				Kind: StatusRunning, PID: record.PID, Endpoint: plan.Endpoint,
				Health: &health, LogFile: plan.State.Log, Detail: "already running",
			}, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return Status{}, runnerError("runner start aborted", err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.State.PID), 0o700); err != nil {
		return Status{}, runnerError("could not create run directory", err)
	}
	logFile, err := os.OpenFile(plan.State.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Status{}, runnerError("could not open server log", err)
	}
	command := exec.Command(plan.Executable, plan.Args...)
	command.Dir = options.CWD
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	launchStartedAt := time.Now()
	if err := command.Start(); err != nil {
		logFile.Close()
		return Status{}, runnerError("could not launch llama-server", err)
	}
	if err := logFile.Close(); err != nil {
		_ = command.Process.Kill()
		return Status{}, runnerError("could not close server log", err)
	}
	pid := command.Process.Pid
	go func() { _ = command.Wait() }()
	observed := waitForProcessObservation(ctx, pid)
	if observed == nil {
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

	if _, err := endpoint.WaitForHealth(ctx, plan.HealthEndpoint, healthOptions(options)); err != nil {
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
	return Status{
		Kind: StatusRunning, PID: record.PID, Endpoint: plan.Endpoint,
		Health: &health, LogFile: plan.State.Log, Detail: "started",
		Timings: &Timings{TimeToHealthMS: elapsedMilliseconds(launchStartedAt)},
	}, nil
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
		return Status{
			Kind: StatusStale, PID: record.PID, Endpoint: plan.Endpoint, LogFile: plan.State.Log,
			Detail: fmt.Sprintf("PID file remains but PID %d is no longer running", record.PID),
		}, nil
	}
	if !IdentityMatches(*record, *observed) {
		return Status{
			Kind: StatusMismatched, PID: record.PID, Endpoint: plan.Endpoint, LogFile: plan.State.Log,
			Detail: identityMismatchError(*record, *observed).Error(),
		}, nil
	}
	if !recordMatchesPlan(*record, plan) {
		return Status{
			Kind: StatusMismatched, PID: record.PID, Endpoint: plan.Endpoint, LogFile: plan.State.Log,
			Detail: planMismatchError(*record, plan).Error(),
		}, nil
	}
	check := endpoint.CheckHealth(ctx, plan.HealthEndpoint, 2*time.Second)
	detail := "process running but endpoint is not healthy"
	if check.OK {
		detail = "healthy"
	}
	return Status{
		Kind: StatusRunning, PID: record.PID, Endpoint: plan.Endpoint, Health: &check.OK,
		Detail:  detail,
		LogFile: plan.State.Log,
	}, nil
}

func Stop(ctx context.Context, plan manifest.Plan, options StopOptions) (Status, error) {
	lock, err := AcquireLifecycleLock(ctx, lifecycleLockPath(plan.State.Run))
	if err != nil {
		return Status{}, err
	}
	defer lock.Release()
	return stop(plan, options)
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
	observed := inspectProcess(record.PID)
	if observed == nil {
		if err := os.Remove(plan.State.PID); err != nil && !os.IsNotExist(err) {
			return Status{}, runnerError("could not remove stale process record", err)
		}
		return Status{
			Kind: StatusStopped, Endpoint: plan.Endpoint, LogFile: plan.State.Log, Detail: "removed stale PID record",
		}, nil
	}
	if !IdentityMatches(*record, *observed) {
		return Status{}, identityMismatchError(*record, *observed)
	}
	if err := sendSignal(*record, syscall.SIGTERM); err != nil {
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
			return stoppedAfterRemovingRecord(plan, "stopped")
		}
		if !IdentityMatches(*record, *observed) {
			return Status{}, identityMismatchError(*record, *observed)
		}
		if time.Now().After(deadline) {
			break
		}
	}
	observed = inspectProcess(record.PID)
	if observed == nil {
		return stoppedAfterRemovingRecord(plan, "stopped")
	}
	if !IdentityMatches(*record, *observed) {
		return Status{}, identityMismatchError(*record, *observed)
	}
	if err := sendSignal(*record, syscall.SIGKILL); err != nil {
		return Status{}, err
	}
	time.Sleep(min(poll, 250*time.Millisecond))
	if inspectProcess(record.PID) != nil {
		return Status{}, runnerErrorf("PID %d did not stop; leaving %s for inspection", record.PID, plan.State.PID)
	}
	return stoppedAfterRemovingRecord(plan, "stopped with SIGKILL after SIGTERM timeout")
}

func sendSignal(record ProcessRecord, signal syscall.Signal) error {
	observed := inspectProcess(record.PID)
	if observed == nil {
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
		return runnerErrorf("llama-server PID %d exited before the healthy response; see %s", record.PID, record.LogFile)
	}
	if !IdentityMatches(record, *observed) {
		return identityMismatchError(record, *observed)
	}
	return nil
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

func stoppedAfterRemovingRecord(plan manifest.Plan, detail string) (Status, error) {
	if err := os.Remove(plan.State.PID); err != nil && !os.IsNotExist(err) {
		return Status{}, runnerError("could not remove process record", err)
	}
	return Status{Kind: StatusStopped, Endpoint: plan.Endpoint, LogFile: plan.State.Log, Detail: detail}, nil
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
