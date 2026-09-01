package endpoint

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HealthCheck struct {
	OK     bool
	Status int
	Body   string
	Err    error
}

type WaitOptions struct {
	Timeout        time.Duration
	PollInterval   time.Duration
	RequestTimeout time.Duration
}

func CheckHealth(ctx context.Context, endpoint string, requestTimeout time.Duration) HealthCheck {
	if requestTimeout <= 0 {
		requestTimeout = 2 * time.Second
	}
	requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return HealthCheck{Err: err}
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return HealthCheck{Err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return HealthCheck{Status: response.StatusCode, Err: err}
	}
	return HealthCheck{
		OK: response.StatusCode == http.StatusOK, Status: response.StatusCode, Body: string(body),
	}
}

func WaitForHealth(ctx context.Context, endpoint string, options WaitOptions) (HealthCheck, error) {
	if options.Timeout == 0 {
		options.Timeout = 2 * time.Minute
	}
	if options.PollInterval == 0 {
		options.PollInterval = 250 * time.Millisecond
	}
	if options.RequestTimeout == 0 {
		options.RequestTimeout = 2 * time.Second
	}
	if options.Timeout < 0 {
		return HealthCheck{}, fmt.Errorf("health timeout must be positive, got %s", options.Timeout)
	}
	if options.PollInterval < 0 {
		return HealthCheck{}, fmt.Errorf("health poll interval must be positive, got %s", options.PollInterval)
	}

	deadline := time.Now().Add(options.Timeout)
	last := HealthCheck{Err: fmt.Errorf("not attempted")}
	for {
		if err := ctx.Err(); err != nil {
			return last, fmt.Errorf("health check aborted: %w", err)
		}
		last = CheckHealth(ctx, endpoint, options.RequestTimeout)
		if last.OK {
			return last, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := min(options.PollInterval, remaining)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, fmt.Errorf("health check aborted: %w", ctx.Err())
		case <-timer.C:
		}
	}
	return last, fmt.Errorf("health check timed out after %s: %s", options.Timeout, healthDetail(last))
}

func healthDetail(check HealthCheck) string {
	if check.Status == 0 {
		if check.Err != nil {
			return check.Err.Error()
		}
		return "no response"
	}
	detail := firstUsefulLine(check.Body)
	if detail == "" {
		detail = "empty response"
	}
	return fmt.Sprintf("HTTP %d: %s", check.Status, detail)
}

func firstUsefulLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(strings.TrimSuffix(line, "\r")); line != "" {
			return line
		}
	}
	return ""
}
