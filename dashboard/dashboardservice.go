package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type DashboardService struct {
	endpoint string
	client   *http.Client
}

type DashboardSnapshot struct {
	GatewayEndpoint string            `json:"gatewayEndpoint"`
	GatewayHealth   string            `json:"gatewayHealth"`
	Model           ModelSnapshot     `json:"model"`
	Models          []AdvertisedModel `json:"models"`
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

type AdvertisedModel struct {
	ID              string `json:"id"`
	Context         int    `json:"context"`
	TrainingContext int    `json:"trainingContext,omitempty"`
	Quantization    string `json:"quantization,omitempty"`
}

type statusResponse struct {
	GatewayEndpoint string        `json:"gatewayEndpoint"`
	GatewayHealth   string        `json:"gatewayHealth"`
	Model           ModelSnapshot `json:"model"`
	UpdatedAt       time.Time     `json:"updatedAt"`
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
	return &DashboardService{endpoint: endpoint, client: &http.Client{Timeout: 3 * time.Second}}
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
			return snapshot
		}
		snapshot.GatewayHealth = "legacy"
		snapshot.UpdatedAt = time.Now().UTC()
		return snapshot
	}
	snapshot.GatewayEndpoint = status.GatewayEndpoint
	snapshot.GatewayHealth = status.GatewayHealth
	snapshot.Model = status.Model
	snapshot.UpdatedAt = status.UpdatedAt
	_ = service.populateCatalog(&snapshot)
	return snapshot
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
	if err := service.postJSON("/admin/model", map[string]string{"model": modelID}); err != nil {
		snapshot := service.offlineSnapshot()
		snapshot.Error = err.Error()
		return snapshot
	}
	return service.Snapshot()
}

func (service *DashboardService) StopModel() DashboardSnapshot {
	if err := service.postJSON("/admin/stop", nil); err != nil {
		snapshot := service.offlineSnapshot()
		snapshot.Error = err.Error()
		return snapshot
	}
	return service.Snapshot()
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
	response, err := service.client.Do(request)
	if err != nil {
		return fmt.Errorf("Outrider request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if len(message) > 0 {
			return fmt.Errorf("Outrider returned HTTP %d: %s", response.StatusCode, string(message))
		}
		return fmt.Errorf("Outrider returned HTTP %d", response.StatusCode)
	}
	return nil
}
