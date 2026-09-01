package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	output, err := run(ctx, os.Args[1:], environmentMap(os.Environ()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "outrider: %v\n", err)
		os.Exit(1)
	}
	_, _ = os.Stdout.WriteString(output)
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
