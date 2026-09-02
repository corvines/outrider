package main

import (
	"fmt"
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
	case upOutput:
		return humanUp(output), nil
	case runnerprocess.Status:
		return humanStatus(output), nil
	case logOutput:
		if len(output.Lines) == 0 {
			return fmt.Sprintf("No log output yet.\nLog: %s\n", output.LogFile), nil
		}
		return strings.Join(output.Lines, "\n") + "\n", nil
	default:
		return encodeOutput(value)
	}
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

func compactTokenCount(value int) string {
	if value >= 1000 && value%1000 == 0 {
		return fmt.Sprintf("%dk", value/1000)
	}
	return fmt.Sprintf("%d", value)
}
