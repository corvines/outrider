package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/corvines/outrider/internal/admission"
)

type offlineOutput struct {
	OfflineReady bool             `json:"offlineReady"`
	Admission    admission.Report `json:"admission"`
	Scope        string           `json:"scope"`
}

func parseCheckArguments(arguments []string) (string, bool, error) {
	var profile string
	offline := false
	for _, argument := range arguments {
		switch {
		case argument == "--offline" && !offline:
			offline = true
		case strings.HasPrefix(argument, "-") || profile != "":
			return "", false, usageError("check expects [--offline] and exactly one profile id")
		default:
			profile = argument
		}
	}
	if profile == "" {
		return "", false, usageError("check expects [--offline] and exactly one profile id")
	}
	return profile, offline, nil
}

func checkOffline(ctx context.Context, id string, environment map[string]string, options runOptions) (string, error) {
	profile, err := runnableProfile(id)
	if err != nil {
		return "", err
	}
	plan, err := resolvePlan(id, environment, true, "")
	if err != nil {
		return "", err
	}
	portOwned, err := outriderOwnsPort(ctx, plan, environment)
	if err != nil {
		return "", err
	}
	report := admission.Inspect(ctx, profile, plan, portOwned)
	probeContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	report = admission.WithRuntimeCapabilities(probeContext, report, plan, true)
	cancel()
	options.notice("Verifying every file for %s. Large models can take a few seconds.", id)
	report = admission.WithCachedArtifacts(ctx, report, plan, options.Progress)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	output, err := formatOutput(offlineOutput{
		OfflineReady: !report.Blocking(), Admission: report,
		Scope: "Checks local model files, runtime launch/flags and current resources. No downloads or model loading. Before travel, also stop, start and chat with each model you need.",
	}, options.Human)
	if err == nil && report.Blocking() {
		err = &admission.Error{Report: report}
	}
	return output, err
}

func humanOffline(output offlineOutput) string {
	var result strings.Builder
	status := "passed"
	if !output.OfflineReady {
		status = "blocked"
	}
	fmt.Fprintf(&result, "Offline preflight %s: %s (%s)\n", status, output.Admission.Profile, output.Admission.Class)
	for _, check := range output.Admission.Checks {
		fmt.Fprintf(&result, "  %s %s: %s\n", check.Result, check.ID, check.Measured)
		if check.NextAction != "" {
			fmt.Fprintf(&result, "    Next: %s\n", check.NextAction)
		}
	}
	fmt.Fprintln(&result, output.Scope)
	return result.String()
}
