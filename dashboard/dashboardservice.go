package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var revealInFinder = func(path string) error {
	output, err := exec.Command("open", "-R", path).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("show in Finder: %w", err)
		}
		return fmt.Errorf("show in Finder: %s", message)
	}
	_ = exec.Command("osascript", "-e", `tell application "Finder" to activate`).Run()
	return nil
}

type DashboardService struct {
	endpoint      string
	client        *http.Client
	controlClient *http.Client
	mu            sync.Mutex
	lastCatalog   []AdvertisedModel
}

type DashboardSnapshot struct {
	GatewayEndpoint string            `json:"gatewayEndpoint"`
	GatewayHealth   string            `json:"gatewayHealth"`
	Model           ModelSnapshot     `json:"model"`
	Loading         *LoadingSnapshot  `json:"loading,omitempty"`
	Models          []AdvertisedModel `json:"models"`
	LogFile         string            `json:"logFile,omitempty"`
	LogLines        []string          `json:"logLines,omitempty"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	Error           string            `json:"error,omitempty"`
}

type ModelSnapshot struct {
	Kind          string `json:"kind"`
	Preset        string `json:"preset"`
	Health        *bool  `json:"health,omitempty"`
	ResidentBytes int64  `json:"residentBytes,omitempty"`
	StartedAt     string `json:"startedAt,omitempty"`
}

type LoadingSnapshot struct {
	Model          string    `json:"model"`
	Phase          string    `json:"phase"`
	StartedAt      time.Time `json:"startedAt"`
	Downloaded     int64     `json:"downloaded,omitempty"`
	Total          int64     `json:"total,omitempty"`
	BytesPerSecond float64   `json:"bytesPerSecond,omitempty"`
	ETASeconds     int64     `json:"etaSeconds,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type AdvertisedModel struct {
	ID              string `json:"id"`
	Context         int    `json:"context"`
	TrainingContext int    `json:"trainingContext,omitempty"`
	Quantization    string `json:"quantization,omitempty"`
	SizeBytes       int64  `json:"sizeBytes,omitempty"`
	Cached          bool   `json:"cached"`
	CanDelete       bool   `json:"canDelete"`
	Protected       bool   `json:"protected,omitempty"`
	Custom          bool   `json:"custom,omitempty"`
	Path            string `json:"path,omitempty"`
}

type statusResponse struct {
	GatewayEndpoint string            `json:"gatewayEndpoint"`
	GatewayHealth   string            `json:"gatewayHealth"`
	Model           ModelSnapshot     `json:"model"`
	Models          []AdvertisedModel `json:"models"`
	Loading         *LoadingSnapshot  `json:"loading,omitempty"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type logsResponse struct {
	LogFile string   `json:"logFile"`
	Lines   []string `json:"lines"`
}

type modelsResponse struct {
	Data []struct {
		ID   string `json:"id"`
		Meta struct {
			Context         int `json:"n_ctx"`
			TrainingContext int `json:"n_ctx_train"`
		} `json:"meta"`
		Quantization string `json:"quantization"`
	} `json:"data"`
}

func NewDashboardService(endpoint string) *DashboardService {
	return &DashboardService{
		endpoint:      endpoint,
		client:        &http.Client{Timeout: 3 * time.Second},
		controlClient: &http.Client{Timeout: 30 * time.Minute},
	}
}

func (service *DashboardService) Snapshot() DashboardSnapshot {
	snapshot := service.offlineSnapshot()
	var status statusResponse
	if err := service.getJSON("/admin/status", &status); err != nil {
		// Older gateways predate the dashboard API but still expose the
		// OpenAI-compatible model catalog. Keep the dashboard useful while
		// making it explicit that controls require a current gateway.
		if catalogErr := service.populateCatalog(&snapshot); catalogErr != nil {
			snapshot.Error = err.Error()
			return service.withCatalog(snapshot)
		}
		snapshot.GatewayHealth = "legacy"
		snapshot.UpdatedAt = time.Now().UTC()
		return service.withCatalog(snapshot)
	}
	snapshot.GatewayEndpoint = status.GatewayEndpoint
	snapshot.GatewayHealth = status.GatewayHealth
	snapshot.Model = status.Model
	snapshot.Loading = status.Loading
	snapshot.UpdatedAt = status.UpdatedAt
	var logs logsResponse
	if err := service.getJSON("/admin/logs?lines=200", &logs); err == nil {
		snapshot.LogFile = logs.LogFile
		snapshot.LogLines = logs.Lines
	}
	if len(status.Models) > 0 {
		snapshot.Models = status.Models
	} else {
		_ = service.populateCatalog(&snapshot)
	}
	return service.withCatalog(snapshot)
}

func (service *DashboardService) populateCatalog(snapshot *DashboardSnapshot) error {
	var models modelsResponse
	if err := service.getJSON("/v1/models", &models); err != nil {
		return err
	}
	snapshot.Models = make([]AdvertisedModel, 0, len(models.Data))
	for _, model := range models.Data {
		snapshot.Models = append(snapshot.Models, AdvertisedModel{
			ID: model.ID, Context: model.Meta.Context, TrainingContext: model.Meta.TrainingContext,
			Quantization: model.Quantization,
		})
	}
	return nil
}

func (service *DashboardService) LoadModel(modelID string) DashboardSnapshot {
	return service.finishControl(service.postJSON("/admin/model", map[string]string{"model": modelID}))
}

func (service *DashboardService) DownloadModel(modelID string) DashboardSnapshot {
	return service.finishControl(service.postJSON("/admin/download", map[string]string{"model": modelID}))
}

func (service *DashboardService) DownloadPath(path string) DashboardSnapshot {
	return service.finishControl(service.postJSON("/admin/download-path", map[string]string{"path": path}))
}

func (service *DashboardService) DeleteModel(modelID string) DashboardSnapshot {
	return service.finishControl(service.postJSON("/admin/delete", map[string]string{"model": modelID}))
}

func (service *DashboardService) RevealModel(modelID string) DashboardSnapshot {
	snapshot := service.Snapshot()
	path := ""
	for _, model := range snapshot.Models {
		if model.ID == modelID {
			path = model.Path
			break
		}
	}
	if path == "" {
		snapshot.Error = fmt.Sprintf("model %s is not downloaded", modelID)
		return snapshot
	}
	if err := revealCachedPath(path); err != nil {
		snapshot.Error = err.Error()
	}
	return snapshot
}

func revealCachedPath(path string) error {
	modelPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(modelPath); os.IsNotExist(err) {
		return fmt.Errorf("model file is not on disk")
	} else if err != nil {
		return err
	}
	return revealInFinder(modelPath)
}

func (service *DashboardService) StopModel() DashboardSnapshot {
	return service.finishControl(service.postJSON("/admin/stop", nil))
}

func (service *DashboardService) PauseModel() DashboardSnapshot {
	return service.finishControl(service.postJSON("/admin/pause", nil))
}

func (service *DashboardService) finishControl(err error) DashboardSnapshot {
	snapshot := service.Snapshot()
	if err != nil {
		snapshot.Error = err.Error()
	}
	return snapshot
}

func (service *DashboardService) withCatalog(snapshot DashboardSnapshot) DashboardSnapshot {
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(snapshot.Models) > 0 {
		service.lastCatalog = append([]AdvertisedModel(nil), snapshot.Models...)
		return snapshot
	}
	if len(service.lastCatalog) > 0 {
		snapshot.Models = append([]AdvertisedModel(nil), service.lastCatalog...)
	}
	return snapshot
}

func (service *DashboardService) offlineSnapshot() DashboardSnapshot {
	return DashboardSnapshot{
		GatewayEndpoint: service.endpoint,
		GatewayHealth:   "offline",
		UpdatedAt:       time.Now().UTC(),
	}
}

func (service *DashboardService) getJSON(path string, target any) error {
	response, err := service.client.Get(service.endpoint + path)
	if err != nil {
		return fmt.Errorf("Outrider is offline: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Outrider returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("invalid Outrider response: %w", err)
	}
	return nil
}

func (service *DashboardService) postJSON(path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Outrider request: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, service.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Outrider request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := service.controlClient.Do(request)
	if err != nil {
		return fmt.Errorf("Outrider request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if len(message) > 0 {
			var payload struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(message, &payload) == nil && payload.Error != "" {
				return fmt.Errorf("%s", payload.Error)
			}
			return fmt.Errorf("Outrider returned HTTP %d: %s", response.StatusCode, string(message))
		}
		return fmt.Errorf("Outrider returned HTTP %d", response.StatusCode)
	}
	return nil
}
