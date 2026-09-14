package main

import (
	"context"
	"fmt"
	"time"
)

func (service *DashboardService) StartServer() DashboardSnapshot {
	return service.controlServer("starting", service.owner.Ensure)
}

func (service *DashboardService) StopServer() DashboardSnapshot {
	service.CancelChat()
	return service.controlServer("stopping", service.owner.Stop)
}

func (service *DashboardService) QuitAndStopServer() DashboardSnapshot {
	snapshot := service.StopServer()
	if snapshot.ServerError == "" && snapshot.ServerAction == "stopped" && service.quit != nil {
		service.quit()
	}
	return snapshot
}

func (service *DashboardService) controlServer(action string, control func(context.Context) error) DashboardSnapshot {
	if !service.serverMu.TryLock() {
		snapshot := service.Snapshot()
		snapshot.ServerError = "A server action is already in progress. Please wait and try again."
		return snapshot
	}
	defer service.serverMu.Unlock()
	service.mu.Lock()
	service.serverAction, service.serverError = action, ""
	service.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := control(ctx)
	service.mu.Lock()
	service.serverAction = ""
	if err != nil {
		service.serverError = err.Error()
	} else if action == "stopping" {
		service.serverAction = "stopped"
	}
	service.mu.Unlock()
	return service.Snapshot()
}

func loadingActive(loading *LoadingSnapshot) bool {
	return loading != nil && loading.Phase != "error" && loading.Phase != "paused"
}

func chatCandidate(snapshot DashboardSnapshot) (string, bool) {
	if snapshot.Model.Kind == "running" && snapshot.Model.Health != nil && *snapshot.Model.Health {
		return snapshot.Model.Preset, false
	}
	for _, id := range []string{snapshot.Model.Preset, "ling3-tiny"} {
		for _, model := range snapshot.Models {
			if model.ID == id && model.Cached && !model.Custom {
				return model.ID, false
			}
		}
	}
	for _, model := range snapshot.Models {
		if model.Cached && !model.Custom {
			return model.ID, false
		}
	}
	return "ling3-tiny", true
}

// StartChat downloads only after explicit consent when no runnable cache exists.
func (service *DashboardService) StartChat(downloadStarter bool) DashboardSnapshot {
	if !service.chatMu.TryLock() {
		return service.finishControl(fmt.Errorf("Chat is already being prepared"))
	}
	defer service.chatMu.Unlock()
	snapshot := service.Snapshot()
	if snapshot.GatewayHealth != "ok" {
		return service.finishControl(fmt.Errorf("Start the server before opening chat"))
	}
	if loadingActive(snapshot.Loading) {
		return service.finishControl(fmt.Errorf("Wait for the current download or model load, or pause it first"))
	}
	modelID, download := chatCandidate(snapshot)
	if download && !downloadStarter {
		return service.finishControl(fmt.Errorf("Download the starter model to begin, or choose a model in Models"))
	}
	if err := service.postJSON("/admin/model", map[string]string{"model": modelID}); err != nil {
		snapshot := service.Snapshot()
		if snapshot.Loading != nil && snapshot.Loading.Phase == "paused" {
			return snapshot
		}
		return service.finishControl(err)
	}
	snapshot = service.Snapshot()
	if snapshot.Model.Preset != modelID || snapshot.Model.Kind != "running" || snapshot.Model.Health == nil || !*snapshot.Model.Health {
		return service.finishControl(fmt.Errorf("The model is not ready yet. Check its status and retry Chat"))
	}
	return snapshot
}
