package process

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

func TestStartRestoresCheckpointSavedByStop(t *testing.T) {
	plan := fakePersistentServerPlan(t)
	started, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Session == nil || started.Session.Detail != "no compatible snapshot" {
		t.Fatalf("first start session = %#v", started.Session)
	}
	stopped, err := Stop(context.Background(), plan, StopOptions{
		Wait: 2 * time.Second, PollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Session == nil || stopped.Session.Detail != "saved" || stopped.Session.Tokens != 42 {
		t.Fatalf("stop session = %#v", stopped.Session)
	}
	restarted, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = Stop(context.Background(), plan, StopOptions{DiscardSession: true})
	})
	if restarted.Session == nil || restarted.Session.Detail != "restored" || restarted.Session.Tokens != 42 {
		t.Fatalf("restart session = %#v", restarted.Session)
	}
}

func TestStartCanSkipSessionRestore(t *testing.T) {
	plan := fakePersistentServerPlan(t)
	started, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
		SkipSessionRestore: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = Stop(context.Background(), plan, StopOptions{DiscardSession: true})
	})
	if started.Session == nil || started.Session.Detail != "skipped" {
		t.Fatalf("start session = %#v", started.Session)
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

func TestStartReportsEarlyModelLoadFailure(t *testing.T) {
	plan := fakeServerPlanWithArgs(t, "--fake-load-failure")
	startedAt := time.Now()
	_, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 5 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "Likely cause:") ||
		!strings.Contains(err.Error(), "Qwen3.5 metadata layout") {
		t.Fatalf("start error = %v", err)
	}
	if time.Since(startedAt) >= 2*time.Second {
		t.Fatalf("early failure took %s", time.Since(startedAt))
	}
	if _, statErr := os.Stat(plan.State.PID); !os.IsNotExist(statErr) {
		t.Fatalf("active record remains: %v", statErr)
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

func TestStatusRepairsChildCrashAfterHealth(t *testing.T) {
	plan := fakeServerPlanWithArgs(t, "--fake-exit-after-health")
	started, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Kind != StatusRunning {
		t.Fatalf("started = %#v", started)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := GetActiveStatus(context.Background(), plan.State)
		if err != nil {
			t.Fatal(err)
		}
		if status.Kind == StatusStopped && strings.Contains(status.Detail, "repaired stale") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %#v", status)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestSuspendedServerRetainsIdentityAndRecoversHealth(t *testing.T) {
	plan := fakeServerPlan(t, false)
	started, err := Start(context.Background(), plan, StartOptions{
		HealthTimeout: 3 * time.Second, HealthPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Kill(started.PID, syscall.SIGCONT)
		_, _ = StopActive(context.Background(), plan.State, StopOptions{})
	}()
	if err := syscall.Kill(started.PID, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	checkContext, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	status, err := GetActiveStatus(checkContext, plan.State)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != StatusRunning || status.Health == nil || *status.Health {
		t.Fatalf("suspended status = %#v", status)
	}
	if err := syscall.Kill(started.PID, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, err = GetActiveStatus(context.Background(), plan.State)
		if err != nil {
			t.Fatal(err)
		}
		if status.Health != nil && *status.Health {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("resumed status = %#v", status)
		}
		time.Sleep(25 * time.Millisecond)
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

func TestServerLogRotationIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	log, err := openRotatingLog(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte(strings.Repeat("a", 70))); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte(strings.Repeat("b", 70))); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + ".1"} {
		info, err := os.Stat(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 100 {
			t.Fatalf("%s size = %d", candidate, info.Size())
		}
	}
}

func TestStatusRepairsDeadActiveRecord(t *testing.T) {
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
	argv := []string{os.Args[0], "not-running"}
	record := ProcessRecord{
		SchemaVersion: ProcessRecordSchemaVersion, PID: 999999,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano), ProcessStartedAt: "not-running",
		Executable: os.Args[0], Command: "not-running", Argv: argv, ArgvSHA256: ArgvSHA256(argv),
		Preset: "tiny", Port: plan.Port, LogFile: plan.State.Log,
	}
	if err := writeProcessRecord(plan.State.PID, record); err != nil {
		t.Fatal(err)
	}
	status, err := GetActiveStatus(context.Background(), plan.State)
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != StatusStopped || !strings.Contains(status.Detail, "repaired stale") {
		t.Fatalf("status = %#v", status)
	}
	if _, err := os.Stat(plan.State.PID); !os.IsNotExist(err) {
		t.Fatalf("active record remains: %v", err)
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
	slotSavePath := ""
	noHealth := false
	exitAfterHealth := false
	loadFailure := false
	for i := separator; i < len(args); i++ {
		switch args[i] {
		case "--fake-no-health":
			noHealth = true
		case "--fake-exit-after-health":
			exitAfterHealth = true
		case "--fake-load-failure":
			loadFailure = true
		case "--port":
			i++
			if i < len(args) {
				port = args[i]
			}
		case "--slot-save-path":
			i++
			if i < len(args) {
				slotSavePath = args[i]
			}
		}
	}
	if port == "" {
		os.Exit(2)
	}
	if loadFailure {
		_, _ = fmt.Fprintln(
			os.Stderr,
			"error loading model: key qwen35.rope.dimension_sections has wrong array length; expected 4, got 3",
		)
		os.Exit(1)
	}
	handler := http.NewServeMux()
	var exitOnce sync.Once
	handler.HandleFunc("/health", func(response http.ResponseWriter, _ *http.Request) {
		if noHealth {
			http.Error(response, "loading", http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte("ready"))
		if exitAfterHealth {
			exitOnce.Do(func() {
				go func() {
					time.Sleep(100 * time.Millisecond)
					os.Exit(0)
				}()
			})
		}
	})
	handler.HandleFunc("/slots/0", func(response http.ResponseWriter, request *http.Request) {
		if slotSavePath == "" {
			http.Error(response, "persistence disabled", http.StatusNotFound)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		filename := body["filename"]
		path := filepath.Join(slotSavePath, filename)
		if request.URL.Query().Get("action") == "save" {
			if err := os.WriteFile(path, []byte("persistent slot"), 0o600); err != nil {
				http.Error(response, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"id_slot": 0, "filename": filename, "n_saved": 42, "n_written": 15,
			})
			return
		}
		if _, err := os.Stat(path); err != nil {
			http.Error(response, err.Error(), http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"id_slot": 0, "filename": filename, "n_restored": 42, "n_read": 15,
		})
	})
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: handler}
	if err := server.ListenAndServe(); err != nil {
		os.Exit(3)
	}
}

func fakeServerPlan(t *testing.T, noHealth bool) manifest.Plan {
	if noHealth {
		return fakeServerPlanWithArgs(t, "--fake-no-health")
	}
	return fakeServerPlanWithArgs(t)
}

func fakeServerPlanWithArgs(t *testing.T, helperArgs ...string) manifest.Plan {
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
	profile.Persistence.Enabled = false
	plan, err := manifest.ResolveCached(profile, manifest.ResolveOptions{
		Root: t.TempDir(), Executable: os.Args[0], Port: &port,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestProcessHelper$", "--"}
	args = append(args, helperArgs...)
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

func fakePersistentServerPlan(t *testing.T) manifest.Plan {
	t.Helper()
	plan := fakeServerPlan(t, false)
	profile, err := manifest.Get("tiny")
	if err != nil {
		t.Fatal(err)
	}
	profile.Persistence.Enabled = true
	plan, err = manifest.ResolveCached(profile, manifest.ResolveOptions{
		Root: plan.State.Root, Executable: os.Args[0], Port: &plan.Port,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan.Executable, err = filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	plan.Args = append([]string{"-test.run=^TestProcessHelper$", "--"}, plan.Args...)
	plan.Endpoint = fmt.Sprintf("http://127.0.0.1:%d", plan.Port)
	plan.HealthEndpoint = plan.Endpoint + "/health"
	return plan
}
