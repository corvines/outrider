package capabilities

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/corvines/outrider/internal/manifest"
)

func TestParseHelpFlags(t *testing.T) {
	flags := ParseHelpFlags("  -h, --help show help\n--spec-type TYPE\n--model FNAME")
	if !reflect.DeepEqual(flags, []string{"-h", "--help", "--spec-type", "--model"}) {
		t.Fatalf("flags = %v", flags)
	}
}

func TestProbeAndAssertCapabilities(t *testing.T) {
	runner := func(_ context.Context, argv []string, _ string) (CommandResult, error) {
		if strings.Join(argv, " ") != "/fake/llama-server --help" {
			t.Fatalf("argv = %v", argv)
		}
		return CommandResult{
			ExitCode: 0,
			Stdout:   "--model FNAME\n--spec-type TYPE\n--spec-draft-n-max N\ndraft-mtp",
		}, nil
	}
	got, err := Probe(context.Background(), "/fake/llama-server", runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := Assert(got, []string{"--model", "/tmp/model.gguf", "--cache-ram", "512"}); err == nil {
		t.Fatal("unsupported generated flag was accepted")
	} else {
		var capabilityErr *CapabilityError
		if !errors.As(err, &capabilityErr) {
			t.Fatalf("error type = %T", err)
		}
	}
	if err := Assert(got, []string{
		"--model", "/tmp/model.gguf", "--spec-type", "draft-mtp", "--spec-draft-n-max", "2",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProbeFailureIncludesStderr(t *testing.T) {
	runner := func(context.Context, []string, string) (CommandResult, error) {
		return CommandResult{ExitCode: 1, Stderr: "unknown option\nsecond line"}, nil
	}
	_, err := Probe(context.Background(), "/fake/llama-server", runner)
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("error = %v", err)
	}
}

func TestProbeExecutionFailureIsTyped(t *testing.T) {
	runner := func(context.Context, []string, string) (CommandResult, error) {
		return CommandResult{}, errors.New("permission denied")
	}
	_, err := Probe(context.Background(), "/fake/llama-server", runner)
	var target *CapabilityError
	if !errors.As(err, &target) || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %v", err)
	}
}

func TestSpeculationRequiresValueAndAdvertisedType(t *testing.T) {
	got := Capabilities{
		Executable: "/fake/llama-server",
		HelpText:   "--spec-type TYPE\ndraft-mtp",
		Flags:      map[string]struct{}{"--spec-type": {}},
	}

	err := Assert(got, []string{"--spec-type"})
	var manifestErr *manifest.ManifestError
	if !errors.As(err, &manifestErr) {
		t.Fatalf("missing value error = %v", err)
	}

	err = Assert(got, []string{"--spec-type", "ngram-mod"})
	var capabilityErr *CapabilityError
	if !errors.As(err, &capabilityErr) {
		t.Fatalf("unknown type error = %v", err)
	}
}

func TestPinnedServerSupportsProfiles(t *testing.T) {
	executable := os.Getenv("OUTRIDER_TEST_LLAMA_SERVER")
	if executable == "" {
		t.Skip("OUTRIDER_TEST_LLAMA_SERVER is not set")
	}
	got, err := Probe(context.Background(), executable, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"qwen35-0.8b", "qwen35b-mtp"} {
		profile, err := manifest.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := manifest.Resolve(profile, manifest.ResolveOptions{
			Root: t.TempDir(), Executable: executable,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := Assert(got, plan.Args); err != nil {
			t.Fatalf("profile %s: %v", id, err)
		}
	}
}
