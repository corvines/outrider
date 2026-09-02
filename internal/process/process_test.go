package process

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/corvines/outrider/internal/manifest"
)

func TestStartIsIdempotentAndStopsServer(t *testing.T) {
	plan := fakeServerPlan(t, false)
	ctx := context.Background()
	started, err := Start(ctx, plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = Stop(context.Background(), plan, StopOptions{}) })
	if started.Kind != StatusRunning || started.Health == nil || !*started.Health {
		t.Fatalf("started = %#v", started)
	}
	status, err := GetStatus(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != StatusRunning || status.Health == nil || !*status.Health {
		t.Fatalf("status = %#v", status)
	}
	again, err := Start(ctx, plan, StartOptions{
		HealthTimeout: time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Detail != "already running" || again.PID != started.PID {
		t.Fatalf("second start = %#v, first = %#v", again, started)
	}
	stopped, err := Stop(ctx, plan, StopOptions{Wait: 2 * time.Second, PollInterval: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Kind != StatusStopped {
		t.Fatalf("stopped = %#v", stopped)
	}
	status, err = GetStatus(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != StatusStopped {
		t.Fatalf("final status = %#v", status)
	}
}

func TestConcurrentStartsLaunchOneProcess(t *testing.T) {
	plan := fakeServerPlan(t, false)
	t.Cleanup(func() { _, _ = Stop(context.Background(), plan, StopOptions{}) })
	start := make(chan struct{})
	results := make(chan Status, 2)
	errors := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			status, err := Start(context.Background(), plan, StartOptions{
				HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
			})
			results <- status
			errors <- err
		}()
	}
	ready.Wait()
	close(start)
	first := <-results
	second := <-results
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if first.PID == 0 || first.PID != second.PID {
		t.Fatalf("start results = %#v and %#v", first, second)
	}
	details := map[string]bool{first.Detail: true, second.Detail: true}
	if !details["started"] || !details["already running"] {
		t.Fatalf("start details = %q and %q", first.Detail, second.Detail)
	}
}

func TestStopRefusesMismatchedProcessIdentity(t *testing.T) {
	profile, _ := manifest.Get("tiny")
	plan, err := manifest.ResolveCached(profile, manifest.ResolveOptions{
		Root: t.TempDir(), Executable: os.Args[0],
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.State.PID), 0o700); err != nil {
		t.Fatal(err)
	}
	argv := []string{os.Args[0], "not-the-current-process"}
	record := ProcessRecord{
		SchemaVersion: ProcessRecordSchemaVersion, PID: os.Getpid(),
		StartedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		ProcessStartedAt: "not-the-current-process", Executable: os.Args[0],
		Command: "not-the-current-process", Argv: argv, ArgvSHA256: ArgvSHA256(argv),
		Preset: "tiny", Port: plan.Port, LogFile: plan.State.Log,
	}
	if err := writeProcessRecord(plan.State.PID, record); err != nil {
		t.Fatal(err)
	}
	if _, err := Stop(context.Background(), plan, StopOptions{}); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("stop error = %v", err)
	}
	status, err := GetStatus(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != StatusMismatched {
		t.Fatalf("status = %#v", status)
	}
}

func TestStartCleansUpWhenHealthFails(t *testing.T) {
	plan := fakeServerPlan(t, true)
	_, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 250 * time.Millisecond, HealthPollInterval: 25 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "did not become healthy") {
		t.Fatalf("start error = %v", err)
	}
	status, statusErr := GetStatus(context.Background(), plan)
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status.Kind != StatusStopped {
		t.Fatalf("status = %#v", status)
	}
}

func TestStartRefusesOwnedProcessWithDifferentPlan(t *testing.T) {
	plan := fakeServerPlan(t, false)
	t.Cleanup(func() { _, _ = Stop(context.Background(), plan, StopOptions{}) })
	started, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := plan
	changed.Args = append(append([]string(nil), plan.Args...), "--changed-policy")
	if _, err := Start(context.Background(), changed, StartOptions{}); err == nil || !strings.Contains(err.Error(), "does not match the requested") {
		t.Fatalf("start error = %v", err)
	}
	status, err := GetStatus(context.Background(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != StatusMismatched || status.PID != started.PID {
		t.Fatalf("status = %#v", status)
	}
}

func TestStartRefusesPortOwnedWithoutActiveRecord(t *testing.T) {
	plan := fakeServerPlan(t, false)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", plan.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := Start(context.Background(), plan, StartOptions{}); err == nil ||
		!strings.Contains(err.Error(), "already in use and no matching active record owns it") {
		t.Fatalf("start error = %v", err)
	}
	if record, err := ReadProcessRecord(plan.State.PID); err != nil || record != nil {
		t.Fatalf("active record = %#v, error = %v", record, err)
	}
}

func TestProfilesShareOneActiveRecordAndLifecycleLock(t *testing.T) {
	plan := fakeServerPlan(t, false)
	other := plan
	other.Profile.ID = "other"
	other.State.Run = filepath.Join(other.State.Root, "runs", other.Profile.ID)
	other.State.Log = filepath.Join(other.State.Run, "server.log")
	if other.State.PID != plan.State.PID || other.State.Lock != plan.State.Lock {
		t.Fatalf("global lifecycle paths differ: %#v and %#v", plan.State, other.State)
	}
	t.Cleanup(func() { _, _ = StopActive(context.Background(), plan.State, StopOptions{}) })
	started, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), other, StartOptions{}); err == nil ||
		!strings.Contains(err.Error(), "does not match the requested other plan") {
		t.Fatalf("other profile start error = %v", err)
	}
	active, err := GetActiveStatus(context.Background(), other.State)
	if err != nil {
		t.Fatal(err)
	}
	if active.Kind != StatusRunning || active.PID != started.PID || active.Preset != plan.Profile.ID {
		t.Fatalf("active status = %#v", active)
	}
}

func TestLifecycleLockWaitHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "up.lock")
	first, err := AcquireLifecycleLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := AcquireLifecycleLock(ctx, path); err == nil || !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("lock error = %v", err)
	}
}

func TestArgvHashMatchesJSONEncoding(t *testing.T) {
	const expected = "0473ef2dc0d324ab659d3580c1134e9d812035905c4781fdd6d529b0c6860e13"
	if got := ArgvSHA256([]string{"a", "b"}); got != expected {
		t.Fatalf("hash = %s", got)
	}
}

func TestProcessHelper(t *testing.T) {
	if os.Getenv("OUTRIDER_PROCESS_HELPER") != "1" {
		return
	}
	args := os.Args
	separator := 0
	for i, arg := range args {
		if arg == "--" {
			separator = i + 1
			break
		}
	}
	port := ""
	noHealth := false
	for i := separator; i < len(args); i++ {
		switch args[i] {
		case "--fake-no-health":
			noHealth = true
		case "--port":
			i++
			if i < len(args) {
				port = args[i]
			}
		}
	}
	if port == "" {
		os.Exit(2)
	}
	handler := http.NewServeMux()
	handler.HandleFunc("/health", func(response http.ResponseWriter, _ *http.Request) {
		if noHealth {
			http.Error(response, "loading", http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte("ready"))
	})
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: handler}
	if err := server.ListenAndServe(); err != nil {
		os.Exit(3)
	}
}

func fakeServerPlan(t *testing.T, noHealth bool) manifest.Plan {
	t.Helper()
	t.Setenv("OUTRIDER_PROCESS_HELPER", "1")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	profile, err := manifest.Get("tiny")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := manifest.ResolveCached(profile, manifest.ResolveOptions{
		Root: t.TempDir(), Executable: os.Args[0], Port: &port,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestProcessHelper$", "--"}
	if noHealth {
		args = append(args, "--fake-no-health")
	}
	args = append(args, "--port", strconv.Itoa(port))
	plan.Args = args
	plan.Executable, err = filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	plan.Endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
	plan.HealthEndpoint = plan.Endpoint + "/health"
	return plan
}
