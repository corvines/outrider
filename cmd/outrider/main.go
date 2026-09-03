package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/corvines/outrider/internal/llama"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	arguments, jsonOutput := outputArguments(os.Args[1:])
	options := runOptions{Human: !jsonOutput}
	if terminal, err := os.Stderr.Stat(); err == nil && terminal.Mode()&os.ModeCharDevice != 0 {
		options.Progress = renderDownloadProgress
		if !jsonOutput {
			options.Notice = renderNotice
		}
	}
	if isatty.IsTerminal(os.Stdin.Fd()) {
		options.Confirm = confirm
	}
	output, err := runWithOptions(ctx, arguments, environmentMap(os.Environ()), options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "outrider: %v\n", err)
		os.Exit(1)
	}
	_, _ = os.Stdout.WriteString(output)
}

func confirm(prompt string) (bool, error) {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

func renderNotice(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
}

func outputArguments(arguments []string) ([]string, bool) {
	filtered := make([]string, 0, len(arguments))
	jsonOutput := false
	for _, argument := range arguments {
		if argument == "--json" {
			jsonOutput = true
			continue
		}
		filtered = append(filtered, argument)
	}
	return filtered, jsonOutput
}

func renderDownloadProgress(progress llama.DownloadProgress) {
	const width = 24
	filled := 0
	percent := 0.0
	if progress.Total > 0 {
		percent = min(1, float64(progress.Downloaded)/float64(progress.Total))
		filled = int(percent * width)
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	eta := ""
	if progress.ETA > 0 {
		eta = " eta " + progress.ETA.Round(time.Second).String()
	}
	fmt.Fprintf(
		os.Stderr, "\r%s [%s] %s / %s  %s/s%s\x1b[K",
		progress.Name, bar, formatByteCount(progress.Downloaded), formatByteCount(progress.Total),
		formatByteCount(int64(progress.BytesPerSecond)), eta,
	)
	if progress.Done {
		_, _ = fmt.Fprintln(os.Stderr)
	}
}

func formatByteCount(value int64) string {
	if value < 0 {
		return "unknown"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	amount := float64(value)
	unit := 0
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", amount, units[unit])
}

func environmentMap(values []string) map[string]string {
	environment := make(map[string]string, len(values))
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) == 2 {
			environment[parts[0]] = parts[1]
		}
	}
	return environment
}
