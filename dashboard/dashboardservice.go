package main

import (
	"encoding/json"
	"fmt"
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
	snapshot := DashboardSnapshot{
		GatewayEndpoint: service.endpoint,
		GatewayHealth:   "offline",
		UpdatedAt:       time.Now().UTC(),
	}
	var status statusResponse
	if err := service.getJSON("/admin/status", &status); err != nil {
		snapshot.Error = err.Error()
		return snapshot
	}
	snapshot.GatewayEndpoint = status.GatewayEndpoint
	snapshot.GatewayHealth = status.GatewayHealth
	snapshot.Model = status.Model
	snapshot.UpdatedAt = status.UpdatedAt

	var models modelsResponse
	if err := service.getJSON("/v1/models", &models); err == nil {
		snapshot.Models = make([]AdvertisedModel, 0, len(models.Data))
		for _, model := range models.Data {
			snapshot.Models = append(snapshot.Models, AdvertisedModel{
				ID: model.ID, Context: model.Meta.Context, TrainingContext: model.Meta.TrainingContext,
				Quantization: model.Quantization,
			})
		}
	}
	return snapshot
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
