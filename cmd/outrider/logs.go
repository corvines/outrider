package main

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"

	runnerprocess "github.com/corvines/outrider/internal/process"
)

func parseLogArguments(arguments []string) (int, error) {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	lines := flags.Int("lines", 40, "number of trailing lines")
	if err := flags.Parse(arguments); err != nil {
		return 0, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return 0, usageError("logs accepts only --lines N")
	}
	if *lines < 1 || *lines > 10000 {
		return 0, usageError("logs --lines must be between 1 and 10000")
	}
	return *lines, nil
}

func readActiveLog(ctx context.Context, environment map[string]string, lineCount int) (logOutput, error) {
	state, err := activeState(environment)
	if err != nil {
		return logOutput{}, err
	}
	status, err := runnerprocess.GetActiveStatus(ctx, state)
	if err != nil {
		return logOutput{}, err
	}
	if status.LogFile == "" {
		return logOutput{}, runnerErrorf("no active Outrider log; start a model first")
	}
	data, err := os.ReadFile(status.LogFile)
	if err != nil {
		return logOutput{}, runnerErrorf("cannot read log %s: %v", status.LogFile, err)
	}
	trimmed := strings.TrimRight(string(data), "\r\n")
	if trimmed == "" {
		return logOutput{LogFile: status.LogFile}, nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > lineCount {
		lines = lines[len(lines)-lineCount:]
	}
	return logOutput{LogFile: status.LogFile, Lines: lines}, nil
}
