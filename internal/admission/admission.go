package admission

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/corvines/outrider/internal/capabilities"
	"github.com/corvines/outrider/internal/manifest"
)

type Class string

const (
	ClassReady      Class = "ready"
	ClassDegraded   Class = "degraded"
	ClassBlocked    Class = "blocked"
	ClassImpossible Class = "impossible"
)

type Result string

const (
	ResultPass Result = "pass"
	ResultWarn Result = "warn"
	ResultFail Result = "fail"
)

type Check struct {
	ID         string `json:"id"`
	Result     Result `json:"result"`
	Measured   string `json:"measured"`
	Required   string `json:"required"`
	NextAction string `json:"nextAction,omitempty"`
	class      Class
}

type Report struct {
	Profile string  `json:"profile"`
	Class   Class   `json:"class"`
	Checks  []Check `json:"checks"`
}

type Snapshot struct {
	OS                  string
	Arch                string
	PhysicalMemoryBytes int64
	MemoryFreePercent   int
	AvailableDiskBytes  int64
	StateWritable       bool
	StateOwned          bool
	PortAvailable       bool
	ModelCached         bool
	RuntimeCached       bool
}

type Error struct {
	Report Report
}

func (e *Error) Error() string {
	for _, check := range e.Report.Checks {
		if check.Result == ResultFail {
			return fmt.Sprintf(
				"admission %s: %s measured %s; requires %s; %s",
				check.ID, check.Result, check.Measured, check.Required, check.NextAction,
			)
		}
	}
	return fmt.Sprintf("admission failed for %s", e.Report.Profile)
}

func (r Report) Blocking() bool {
	return r.Class == ClassBlocked || r.Class == ClassImpossible
}

func Inspect(ctx context.Context, profile manifest.Profile, plan manifest.Plan, portOwned bool) Report {
	snapshot := measure(ctx, plan)
	if portOwned {
		snapshot.PortAvailable = true
	}
	return Evaluate(profile, plan, snapshot)
}

func WithRuntimeCapabilities(ctx context.Context, report Report, plan manifest.Plan, required bool) Report {
	if !regularFile(plan.Executable) {
		result := ResultWarn
		class := ClassDegraded
		action := "install the pinned runtime before treating this report as complete"
		if required {
			result = ResultFail
			class = ClassBlocked
			action = "restore the verified llama-server executable"
		}
		report.add(Check{
			ID: "runtime_capabilities", Result: result, Measured: "runtime executable unavailable",
			Required: "every profile flag advertised by llama-server", NextAction: action, class: class,
		})
		return report
	}
	probed, err := capabilities.Probe(ctx, plan.Executable, nil)
	if err == nil {
		err = capabilities.Assert(probed, plan.Args)
	}
	if err != nil {
		report.add(Check{
			ID: "runtime_capabilities", Result: ResultFail, Measured: err.Error(),
			Required:   "every profile flag advertised by llama-server",
			NextAction: "use the pinned compatible runtime or repair the profile", class: ClassBlocked,
		})
		return report
	}
	report.add(Check{
		ID: "runtime_capabilities", Result: ResultPass, Measured: "all profile flags advertised",
		Required: "every profile flag advertised by llama-server",
	})
	return report
}

func Evaluate(profile manifest.Profile, plan manifest.Plan, snapshot Snapshot) Report {
	report := Report{Profile: profile.ID, Class: ClassReady}
	if snapshot.OS != "darwin" || snapshot.Arch != "arm64" {
		report.add(Check{
			ID: "platform", Result: ResultFail, Measured: snapshot.OS + "/" + snapshot.Arch,
			Required: "darwin/arm64", NextAction: "use an Apple silicon Mac", class: ClassImpossible,
		})
	} else {
		report.add(Check{ID: "platform", Result: ResultPass, Measured: "darwin/arm64", Required: "darwin/arm64"})
	}

	digest := "missing"
	size := "missing"
	artifactResult := ResultFail
	if profile.Model.SHA256 != "" {
		digest = "declared"
	}
	if profile.Model.SizeBytes > 0 {
		size = formatBytes(profile.Model.SizeBytes)
	}
	if profile.Model.SHA256 != "" && profile.Model.SizeBytes > 0 {
		artifactResult = ResultPass
	}
	report.add(Check{
		ID: "artifact", Result: artifactResult, Measured: "digest " + digest + ", size " + size,
		Required: "declared SHA-256 and byte size", NextAction: action(artifactResult, "fix the profile manifest"),
		class: ClassBlocked,
	})

	stateResult := ResultPass
	if !snapshot.StateWritable || !snapshot.StateOwned {
		stateResult = ResultFail
	}
	report.add(Check{
		ID: "state_directory", Result: stateResult,
		Measured:   fmt.Sprintf("writable=%t owned=%t", snapshot.StateWritable, snapshot.StateOwned),
		Required:   "writable and owned by the current user",
		NextAction: action(stateResult, "repair ownership and permissions on "+plan.State.Root), class: ClassBlocked,
	})

	requiredDisk := int64(0)
	if !snapshot.ModelCached {
		requiredDisk += profile.Model.SizeBytes
	}
	if !snapshot.RuntimeCached {
		requiredDisk += manifest.LlamaRelease.SizeBytes
	}
	diskResult := ResultPass
	if snapshot.AvailableDiskBytes < requiredDisk {
		diskResult = ResultFail
	}
	report.add(Check{
		ID: "disk_space", Result: diskResult, Measured: formatBytes(snapshot.AvailableDiskBytes),
		Required: formatBytes(requiredDisk), NextAction: action(diskResult, "free cache-volume space before retrying"),
		class: ClassBlocked,
	})

	validatedBytes := int64(profile.Admission.ValidatedPhysicalMemoryMiB) * 1024 * 1024
	memoryResult := ResultPass
	memoryClass := ClassReady
	memoryAction := ""
	if snapshot.PhysicalMemoryBytes <= 0 {
		memoryResult = ResultFail
		memoryClass = ClassBlocked
		memoryAction = "restore access to the macOS hw.memsize probe"
	} else if snapshot.PhysicalMemoryBytes < validatedBytes {
		memoryResult = ResultFail
		memoryClass = ClassBlocked
		memoryAction = "choose a profile validated for this machine's memory"
	}
	report.add(Check{
		ID: "physical_memory", Result: memoryResult, Measured: formatBytes(snapshot.PhysicalMemoryBytes),
		Required: "validated at " + formatBytes(validatedBytes), NextAction: memoryAction, class: memoryClass,
	})

	pressureResult := ResultPass
	pressureAction := ""
	if snapshot.MemoryFreePercent < 0 {
		pressureResult = ResultWarn
		pressureAction = "retry when macOS memory pressure can be measured"
	} else if snapshot.MemoryFreePercent < 10 {
		pressureResult = ResultWarn
		pressureAction = "close memory-heavy applications before loading the model"
	}
	report.add(Check{
		ID: "memory_pressure", Result: pressureResult,
		Measured: fmt.Sprintf("%d%% free", snapshot.MemoryFreePercent), Required: "at least 10% free",
		NextAction: pressureAction, class: ClassDegraded,
	})

	portResult := ResultPass
	if !snapshot.PortAvailable {
		portResult = ResultFail
	}
	report.add(Check{
		ID: "port", Result: portResult,
		Measured:   fmt.Sprintf("%s:%d available=%t", plan.Host, plan.Port, snapshot.PortAvailable),
		Required:   "available or owned by the active Outrider record",
		NextAction: action(portResult, "stop the existing listener or choose OUTRIDER_PORT"), class: ClassBlocked,
	})
	return report
}

func (r *Report) add(check Check) {
	r.Checks = append(r.Checks, check)
	if check.Result != ResultPass && classRank(check.class) > classRank(r.Class) {
		r.Class = check.class
	}
}

func classRank(class Class) int {
	switch class {
	case ClassImpossible:
		return 3
	case ClassBlocked:
		return 2
	case ClassDegraded:
		return 1
	default:
		return 0
	}
}

func measure(ctx context.Context, plan manifest.Plan) Snapshot {
	snapshot := Snapshot{OS: runtime.GOOS, Arch: runtime.GOARCH, MemoryFreePercent: -1}
	if err := os.MkdirAll(plan.State.Root, 0o700); err == nil {
		snapshot.StateWritable = writable(plan.State.Root)
		if info, statErr := os.Stat(plan.State.Root); statErr == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				snapshot.StateOwned = int(stat.Uid) == os.Geteuid()
			}
		}
		var filesystem syscall.Statfs_t
		if err := syscall.Statfs(plan.State.Root, &filesystem); err == nil {
			snapshot.AvailableDiskBytes = int64(filesystem.Bavail) * int64(filesystem.Bsize)
		}
	}
	snapshot.ModelCached = regularFile(plan.State.Model)
	snapshot.RuntimeCached = regularFile(plan.Executable)
	snapshot.PortAvailable = portAvailable(plan.Host, plan.Port)
	snapshot.PhysicalMemoryBytes = commandInt64(ctx, "sysctl", "-n", "hw.memsize")
	snapshot.MemoryFreePercent = memoryFreePercent(ctx)
	return snapshot
}

func writable(directory string) bool {
	file, err := os.CreateTemp(directory, ".admission-")
	if err != nil {
		return false
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return closeErr == nil && removeErr == nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func portAvailable(host string, port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	return listener.Close() == nil
}

func commandInt64(ctx context.Context, name string, args ...string) int64 {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

var freePercentPattern = regexp.MustCompile(`System-wide memory free percentage:\s*(\d+)%`)

func memoryFreePercent(ctx context.Context) int {
	output, err := exec.CommandContext(ctx, "memory_pressure", "-Q").CombinedOutput()
	if err != nil {
		return -1
	}
	match := freePercentPattern.FindStringSubmatch(string(output))
	if len(match) != 2 {
		return -1
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		return -1
	}
	return value
}

func formatBytes(value int64) string {
	if value < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d bytes", value)
}

func action(result Result, value string) string {
	if result == ResultPass {
		return ""
	}
	return value
}
