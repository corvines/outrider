package llama

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/corvines/outrider/internal/manifest"
)

// Controls apply only to the tiny development profile, including its retries.
func developmentDownloadClient(destination string) (*http.Client, error) {
	if !manifest.DevEnabled() || filepath.Base(destination) != "download-test-stories260K.gguf.part" {
		return http.DefaultClient, nil
	}
	values := make([]int64, 3)
	for i, name := range []string{"OUTRIDER_DOWNLOAD_BPS", "OUTRIDER_DOWNLOAD_DROP_AFTER", "OUTRIDER_DOWNLOAD_503_ATTEMPTS"} {
		value := os.Getenv(name)
		if value == "" {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%s must be a nonnegative integer", name)
		}
		values[i] = n
	}
	client := *http.DefaultClient
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = &developmentTransport{base: transport, rate: values[0], drop: values[1], failures: values[2]}
	return &client, nil
}

type developmentTransport struct {
	base                 http.RoundTripper
	rate, drop, failures int64
}

func (t *developmentTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.failures > 0 {
		t.failures--
		return &http.Response{StatusCode: 503, Status: "503 Service Unavailable (development)", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("development download failure")), Request: request}, nil
	}
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusPartialContent {
		response.Body = &developmentBody{ReadCloser: response.Body, ctx: request.Context(), rate: t.rate, drop: t.drop, started: time.Now()}
		// One dropped connection per download invocation allows automatic resume.
		t.drop = 0
	}
	return response, nil
}

type developmentBody struct {
	io.ReadCloser
	ctx              context.Context
	rate, drop, read int64
	started          time.Time
}

func (b *developmentBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.drop > 0 {
		remaining := b.drop - b.read
		if remaining <= 0 {
			return 0, io.ErrUnexpectedEOF
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}
	if b.rate > 0 {
		chunk := b.rate / 10
		if chunk < 1 {
			chunk = 1
		}
		if int64(len(p)) > chunk {
			p = p[:chunk]
		}
	}
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if b.rate > 0 && n > 0 {
		delay := time.Duration(float64(b.read)/float64(b.rate)*float64(time.Second)) - time.Since(b.started)
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-b.ctx.Done():
				return n, b.ctx.Err()
			case <-timer.C:
			}
		}
	}
	return n, err
}
