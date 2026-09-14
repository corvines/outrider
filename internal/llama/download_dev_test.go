package llama

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDevelopmentDownloadRecovery(t *testing.T) {
	t.Setenv("OUTRIDER_DEV", "1")
	payload := bytes.Repeat([]byte("GGUF-download-test"), 4096)
	for _, scenario := range []string{"drop", "503-retry", "503-exhausted", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("OUTRIDER_DOWNLOAD_BPS", "0")
			t.Setenv("OUTRIDER_DOWNLOAD_DROP_AFTER", "0")
			t.Setenv("OUTRIDER_DOWNLOAD_503_ATTEMPTS", "0")
			var mu sync.Mutex
			var ranges []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				ranges = append(ranges, r.Header.Get("Range"))
				mu.Unlock()
				w.Header().Set("ETag", `"tiny-v1"`)
				http.ServeContent(w, r, "tiny.gguf", time.Time{}, bytes.NewReader(payload))
			}))
			defer server.Close()
			destination := filepath.Join(t.TempDir(), "download-test-stories260K.gguf.part")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "drop":
				t.Setenv("OUTRIDER_DOWNLOAD_DROP_AFTER", "4096")
			case "503-retry":
				t.Setenv("OUTRIDER_DOWNLOAD_503_ATTEMPTS", "2")
			case "503-exhausted":
				t.Setenv("OUTRIDER_DOWNLOAD_503_ATTEMPTS", "3")
			case "cancel":
				t.Setenv("OUTRIDER_DOWNLOAD_BPS", "20480")
			}
			done := false
			err := DownloadFileWithProgress(ctx, server.URL, destination, func(p DownloadProgress) {
				done = done || p.Done
				if scenario == "cancel" && p.Downloaded > 0 {
					cancel()
				}
			})
			switch scenario {
			case "503-exhausted":
				if err == nil || done {
					t.Fatalf("exhausted retries: err=%v done=%v", err, done)
				}
				if _, err := os.Stat(destination); !os.IsNotExist(err) {
					t.Fatalf("failed request created a file: %v", err)
				}
				t.Setenv("OUTRIDER_DOWNLOAD_503_ATTEMPTS", "0")
				err = DownloadFile(context.Background(), server.URL, destination)
			case "cancel":
				if !errors.Is(err, context.Canceled) || done {
					t.Fatalf("cancel: err=%v done=%v", err, done)
				}
				info, statErr := os.Stat(destination)
				if statErr != nil || info.Size() <= 0 || info.Size() >= int64(len(payload)) {
					t.Fatalf("partial: %v %v", info, statErr)
				}
				t.Setenv("OUTRIDER_DOWNLOAD_BPS", "0")
				err = DownloadFile(context.Background(), server.URL, destination)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("download differs: %v", err)
			}
			if _, err := os.Stat(resumeMetadataPath(destination)); !os.IsNotExist(err) {
				t.Fatalf("resume metadata remains: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if scenario == "drop" || scenario == "cancel" {
				if len(ranges) != 2 || ranges[0] != "" || ranges[1] == "" {
					t.Fatalf("resume requests: %v", ranges)
				}
			} else if len(ranges) != 1 {
				t.Fatalf("unexpected network requests: %v", ranges)
			}
		})
	}
}

func TestDevelopmentDownloadScope(t *testing.T) {
	t.Setenv("OUTRIDER_DOWNLOAD_BPS", "invalid")
	t.Setenv("OUTRIDER_DEV", "")
	if client, err := developmentDownloadClient("download-test-stories260K.gguf.part"); err != nil || client != http.DefaultClient {
		t.Fatalf("production affected: %v", err)
	}
	t.Setenv("OUTRIDER_DEV", "1")
	if client, err := developmentDownloadClient("real-model.gguf.part"); err != nil || client != http.DefaultClient {
		t.Fatalf("other download affected: %v", err)
	}
	if _, err := developmentDownloadClient("download-test-stories260K.gguf.part"); err == nil {
		t.Fatal("invalid rate accepted")
	}
}

func TestDevelopmentDownloadThrottle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &developmentBody{ReadCloser: io.NopCloser(bytes.NewReader(make([]byte, 100))), ctx: ctx, rate: 1000, started: time.Now()}
	start := time.Now()
	if n, err := body.Read(make([]byte, 100)); n != 100 || err != nil {
		t.Fatalf("read: %d %v", n, err)
	}
	if time.Since(start) < 90*time.Millisecond {
		t.Fatal("rate limit did not delay delivery")
	}
	body.ReadCloser = io.NopCloser(bytes.NewReader(make([]byte, 100)))
	body.rate = 1
	finished := make(chan error, 1)
	go func() { _, err := body.Read(make([]byte, 100)); finished <- err }()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("throttle blocked cancellation")
	}
}
