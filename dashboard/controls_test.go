package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerFailureRemainsVisibleAndRetryClearsIt(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	service := NewDashboardService(server.URL)
	failure := true
	service.owner.lookPath = func() (string, error) { return "/bin/outrider", nil }
	service.owner.healthy = func(context.Context, string) bool { return !failure }
	service.owner.run = func(context.Context, string, ...string) error { return errors.New("port is occupied") }
	if got := service.StartServer(); got.ServerError != "port is occupied" {
		t.Fatalf("snapshot = %+v", got)
	}
	if got := service.Snapshot(); got.ServerError != "port is occupied" {
		t.Fatalf("lost failure: %+v", got)
	}
	failure = false
	if got := service.StartServer(); got.ServerError != "" {
		t.Fatalf("retry = %+v", got)
	}
}

func TestQuitAndStopDoesNotQuitAfterStopFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	service := NewDashboardService(server.URL)
	service.owner.lookPath = func() (string, error) { return "/bin/outrider", nil }
	service.owner.run = func(context.Context, string, ...string) error { return errors.New("cannot stop") }
	quit := false
	service.quit = func() { quit = true }
	if got := service.QuitAndStopServer(); got.ServerError != "cannot stop" || quit {
		t.Fatalf("quit=%v, snapshot=%+v", quit, got)
	}
	service.owner.run = func(context.Context, string, ...string) error { return nil }
	if got := service.QuitAndStopServer(); got.ServerError != "" || got.Error != "" || !quit {
		t.Fatalf("quit=%v, snapshot=%+v", quit, got)
	}
}

func TestChatConsentCacheSelectionAndFailures(t *testing.T) {
	for _, name := range []string{"no-consent", "consent", "cached", "custom-only", "loaded", "load-failed", "not-ready", "paused", "pause-request", "busy"} {
		t.Run(name, func(t *testing.T) {
			health := true
			snapshot := DashboardSnapshot{GatewayHealth: "ok", Model: ModelSnapshot{Kind: "stopped"}, Models: []AdvertisedModel{{ID: "ling3-tiny"}}}
			if name == "cached" {
				snapshot.Models = append(snapshot.Models, AdvertisedModel{ID: "other", Cached: true})
			}
			if name == "custom-only" {
				snapshot.Models = append(snapshot.Models, AdvertisedModel{ID: "file", Cached: true, Custom: true})
			}
			if name == "loaded" {
				snapshot.Model = ModelSnapshot{Kind: "running", Preset: "active", Health: &health}
			}
			if name == "paused" {
				snapshot.Loading = &LoadingSnapshot{Phase: "paused"}
			}
			if name == "busy" {
				snapshot.Loading = &LoadingSnapshot{Phase: "downloading"}
			}
			var loaded string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/admin/status":
					_ = json.NewEncoder(w).Encode(snapshot)
				case "/admin/logs":
					_, _ = w.Write([]byte(`{"lines":[]}`))
				case "/admin/model":
					var input struct {
						Model string `json:"model"`
					}
					_ = json.NewDecoder(r.Body).Decode(&input)
					loaded = input.Model
					if name == "pause-request" {
						snapshot.Loading = &LoadingSnapshot{Phase: "paused"}
						http.Error(w, "context canceled", 500)
						return
					}
					if name == "load-failed" {
						http.Error(w, "download failed", 500)
						return
					}
					if name != "not-ready" {
						snapshot.Model = ModelSnapshot{Kind: "running", Preset: loaded, Health: &health}
						snapshot.Loading = nil
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			service := NewDashboardService(server.URL)
			service.owner.lookPath = func() (string, error) { return "/bundle/outrider", nil }
			consent := name != "no-consent" && name != "custom-only" && name != "cached" && name != "loaded"
			got := service.StartChat(consent)
			success := name == "consent" || name == "cached" || name == "loaded" || name == "paused"
			if (got.Error == "") != (success || name == "pause-request") {
				t.Fatalf("loaded=%s error=%s", loaded, got.Error)
			}
			if success && (got.Model.Kind != "running" || got.Model.Health == nil || !*got.Model.Health) {
				t.Fatalf("chat model not ready: %+v", got.Model)
			}
			if (name == "no-consent" || name == "custom-only" || name == "busy") && loaded != "" {
				t.Fatal("loaded without consent or during active load")
			}
			if name == "cached" && loaded != "other" {
				t.Fatalf("ignored cached model: %s", loaded)
			}
			if name == "loaded" && loaded != "active" {
				t.Fatalf("replaced active model: %s", loaded)
			}
		})
	}
}
