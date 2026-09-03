package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	runnerprocess "github.com/corvines/outrider/internal/process"
)

func humanOutput(value any) (string, error) {
	switch output := value.(type) {
	case profileListOutput:
		return humanProfileList(output), nil
	case profileDetailOutput:
		return humanProfileDetail(output), nil
	case pullOutput:
		return fmt.Sprintf(
			"Ready: %s\nModel: %s (%s)\nRuntime: %s\n",
			output.Profile, output.Model, formatByteCount(output.SizeBytes), output.Runtime,
		), nil
	case cacheCleanupOutput:
		return humanCacheCleanup(output), nil
	case upOutput:
		return humanUp(output), nil
	case runnerprocess.Status:
		return humanStatus(output), nil
	case serviceStatusOutput:
		return humanServiceStatus(output), nil
	case useOutput:
		return fmt.Sprintf(
			"Active model: %s\nVera endpoint: %s/v1\nVera model: %s\nMemory: %s\nLog: %s\n",
			output.Profile, output.Endpoint, output.Profile,
			formatByteCount(output.Model.ResidentBytes), output.Model.LogFile,
		), nil
	case installOutput:
		if output.Status == "uninstalled" {
			return fmt.Sprintf("Removed Outrider from %s\n", output.Target), nil
		}
		return fmt.Sprintf(
			"Installed Outrider at %s\nAdd %s to PATH if needed.\n",
			output.Target, filepath.Dir(output.Target),
		), nil
	case versionOutput:
		version := output.Version
		if output.Commit != "" {
			commit := output.Commit
			if len(commit) > 12 {
				commit = commit[:12]
			}
			version += " (" + commit + ")"
		}
		if output.Dirty {
			version += " dirty"
		}
		return "outrider " + version + "\n", nil
	case logOutput:
		if len(output.Lines) == 0 {
			return fmt.Sprintf("No log output yet.\nLog: %s\n", output.LogFile), nil
		}
		return strings.Join(output.Lines, "\n") + "\n", nil
	default:
		return encodeOutput(value)
	}
}

func humanCacheCleanup(output cacheCleanupOutput) string {
	var result strings.Builder
	if output.DryRun {
		_, _ = fmt.Fprintf(&result, "Cache cleanup (dry run): %s\n", output.Root)
	} else {
		_, _ = fmt.Fprintf(&result, "Cache cleanup: removed %d file(s), reclaimed %s\n", len(output.Removed), formatByteCount(output.ReclaimedBytes))
	}
	for _, entry := range output.Protected {
		_, _ = fmt.Fprintf(&result, "Preserved: %s (%s)\n", entry.Path, entry.Reason)
	}
	if output.DryRun {
		if len(output.Candidates) == 0 {
			_, _ = fmt.Fprintln(&result, "No cleanup candidates found.")
		} else {
			_, _ = fmt.Fprintf(&result, "Would remove %d file(s), reclaiming %s:\n", len(output.Candidates), formatByteCount(cacheCleanupBytes(output.Candidates)))
			for _, entry := range output.Candidates {
				_, _ = fmt.Fprintf(&result, "  %s (%s)\n", entry.Path, entry.Reason)
			}
			_, _ = fmt.Fprintln(&result, "Re-run with `outrider cache clean --apply` to remove them.")
		}
	}
	return result.String()
}

func cacheCleanupBytes(entries []cacheCleanupEntry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.SizeBytes
	}
	return total
}

func humanProfileList(output profileListOutput) string {
	var result strings.Builder
	writer := tabwriter.NewWriter(&result, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "MODEL\tSIZE\tCONTEXT\tCACHE\tDESCRIPTION")
	for _, profile := range output.Profiles {
		availability := profile.Cache.State
		if !profile.Runnable {
			availability = "plan only"
		}
		_, _ = fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\t%s\n",
			profile.ID, formatByteCount(profile.SizeBytes), compactTokenCount(profile.Context),
			availability, profile.Description,
		)
	}
	if len(output.DevelopmentModels) > 0 {
		_, _ = fmt.Fprintln(writer)
		_, _ = fmt.Fprintln(writer, "LOCAL OLLAMA MODEL\tSIZE\tSOURCE")
		for _, model := range output.DevelopmentModels {
			_, _ = fmt.Fprintf(writer, "%s\t%s\tOllama cache\n", model.Name, formatByteCount(model.SizeBytes))
		}
	}
	_ = writer.Flush()
	return result.String()
}

func humanProfileDetail(output profileDetailOutput) string {
	profile := output.Profile
	status := output.Cache.State
	if !profile.Runnable {
		status = "plan only"
	}
	return fmt.Sprintf(
		"%s\n%s\nSize: %s\nContext: %s\nQuantization: %s\nCache: %s\nPath: %s\n",
		profile.ID, profile.Description, formatByteCount(profile.Model.SizeBytes),
		compactTokenCount(profile.Context.Size), profile.Model.Quant, status, output.Cache.Path,
	)
}

func humanUp(output upOutput) string {
	health := "unknown"
	if output.Health != nil {
		health = map[bool]string{true: "healthy", false: "unhealthy"}[*output.Health]
	}
	return fmt.Sprintf(
		"Outrider %s\nModel: %s\nHealth: %s\nEndpoint: %s\nPID: %d\nLog: %s\n",
		output.Kind, output.Profile, health, output.Endpoint, output.PID, output.LogFile,
	)
}

func humanStatus(status runnerprocess.Status) string {
	if status.Kind == runnerprocess.StatusStopped {
		if status.Detail == "" {
			return "Outrider is stopped.\n"
		}
		return fmt.Sprintf("Outrider is stopped (%s).\n", status.Detail)
	}
	health := "unknown"
	if status.Health != nil {
		health = map[bool]string{true: "healthy", false: "unhealthy"}[*status.Health]
	}
	var result strings.Builder
	if status.Preset == "gateway" {
		_, _ = fmt.Fprintf(&result, "Outrider gateway is %s.\n", status.Kind)
		_, _ = fmt.Fprintf(&result, "Health: %s\nEndpoint: %s/v1\nPID: %d\nLog: %s\n", health, status.Endpoint, status.PID, status.LogFile)
		return result.String()
	}
	_, _ = fmt.Fprintf(&result, "Outrider is %s.\n", status.Kind)
	_, _ = fmt.Fprintf(&result, "Model: %s\nHealth: %s\n", status.Preset, health)
	if status.ResidentBytes > 0 {
		_, _ = fmt.Fprintf(&result, "Memory: %s\n", formatByteCount(status.ResidentBytes))
	}
	if status.StartedAt != "" {
		_, _ = fmt.Fprintf(&result, "Started: %s\n", status.StartedAt)
	}
	_, _ = fmt.Fprintf(
		&result, "Endpoint: %s\nPID: %d\nLog: %s\n", status.Endpoint, status.PID, status.LogFile,
	)
	if status.Detail != "" && status.Detail != "healthy" {
		_, _ = fmt.Fprintf(&result, "Detail: %s\n", status.Detail)
	}
	return result.String()
}

func humanServiceStatus(status serviceStatusOutput) string {
	var result strings.Builder
	if status.Gateway.Kind == runnerprocess.StatusRunning {
		health := "unknown"
		if status.Gateway.Health != nil {
			health = map[bool]string{true: "healthy", false: "unhealthy"}[*status.Gateway.Health]
		}
		_, _ = fmt.Fprintf(&result, "Gateway: %s (%s)\nEndpoint: %s/v1\n", status.Gateway.Kind, health, status.Gateway.Endpoint)
	} else {
		_, _ = fmt.Fprintf(&result, "Gateway: %s\n", status.Gateway.Kind)
	}
	if status.Model.Kind == runnerprocess.StatusRunning {
		health := "unknown"
		if status.Model.Health != nil {
			health = map[bool]string{true: "healthy", false: "unhealthy"}[*status.Model.Health]
		}
		_, _ = fmt.Fprintf(&result, "Model: %s (%s)\n", status.Model.Preset, health)
		if status.Model.ResidentBytes > 0 {
			_, _ = fmt.Fprintf(&result, "Memory: %s\n", formatByteCount(status.Model.ResidentBytes))
		}
		_, _ = fmt.Fprintf(&result, "Model log: %s\n", status.Model.LogFile)
	} else {
		_, _ = fmt.Fprintf(&result, "Model: %s\n", status.Model.Kind)
	}
	return result.String()
}

func compactTokenCount(value int) string {
	if value >= 1000 && value%1000 == 0 {
		return fmt.Sprintf("%dk", value/1000)
	}
	return fmt.Sprintf("%d", value)
}
