package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/corvines/outrider/internal/catalog"
	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/llama"
	"github.com/corvines/outrider/internal/manifest"
	runnerprocess "github.com/corvines/outrider/internal/process"
	"github.com/corvines/outrider/internal/switcher"
)

type gatewayDashboardStatus struct {
	GatewayEndpoint string                `json:"gatewayEndpoint"`
	GatewayHealth   string                `json:"gatewayHealth"`
	Model           runnerprocess.Status  `json:"model"`
	Models          []gatewayModelStatus  `json:"models"`
	Loading         *gatewayLoadingStatus `json:"loading,omitempty"`
	UpdatedAt       time.Time             `json:"updatedAt"`
}

type gatewayLogsResponse struct {
	LogFile string   `json:"logFile"`
	Lines   []string `json:"lines"`
}

type gatewayModelStatus struct {
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

var protectedGatewayModels = map[string]struct{}{
	"qwen3-1.7b":    {},
	"granite4.2-3b": {},
	"qwen35b-mtp":   {},
}

type gatewayLoadingStatus struct {
	Model          string    `json:"model"`
	Phase          string    `json:"phase"`
	StartedAt      time.Time `json:"startedAt"`
	Downloaded     int64     `json:"downloaded,omitempty"`
	Total          int64     `json:"total,omitempty"`
	BytesPerSecond float64   `json:"bytesPerSecond,omitempty"`
	ETASeconds     int64     `json:"etaSeconds,omitempty"`
	Name           string    `json:"name,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type gatewayBackend struct {
	environment map[string]string
	options     runOptions
	loadingMu   sync.RWMutex
	loading     *gatewayLoadingStatus
	loadCancel  context.CancelFunc
	paused      bool
}

func (backend *gatewayBackend) Download(ctx context.Context, modelID string) (operationErr error) {
	backend.beginLoading(modelID)
	loadContext, cancel := context.WithCancel(ctx)
	backend.setLoadCancel(cancel)
	defer func() {
		cancel()
		backend.finishLoading(operationErr)
	}()
	backend.setLoadingPhase("preparing")
	_, operationErr = pullProfile(loadContext, modelID, backend.environment, backend.options)
	return operationErr
}

func (backend *gatewayBackend) DownloadPath(ctx context.Context, source string) (operationErr error) {
	downloadURL, filename, err := normalizeDownloadPath(source)
	if err != nil {
		return err
	}
	state, err := activeState(backend.environment)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(state.Models, 0o700); err != nil {
		return fmt.Errorf("create model cache: %w", err)
	}
	destination := filepath.Join(state.Models, "custom-"+filename)
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("model %s is already downloaded", filename)
	} else if !os.IsNotExist(err) {
		return err
	}
	backend.beginLoading(filename)
	loadContext, cancel := context.WithCancel(ctx)
	backend.setLoadCancel(cancel)
	defer func() {
		cancel()
		backend.finishLoading(operationErr)
	}()
	backend.setLoadingPhase("downloading")
	partial := destination + ".part"
	if operationErr = llama.DownloadFileWithProgress(loadContext, downloadURL, partial, backend.options.Progress); operationErr != nil {
		return operationErr
	}
	if operationErr = os.Rename(partial, destination); operationErr != nil {
		return fmt.Errorf("install downloaded model: %w", operationErr)
	}
	return nil
}

func (backend *gatewayBackend) Ensure(ctx context.Context, modelID string) (endpointURL string, operationErr error) {
	backend.beginLoading(modelID)
	loadContext, cancel := context.WithCancel(ctx)
	backend.setLoadCancel(cancel)
	defer func() {
		cancel()
		backend.finishLoading(operationErr)
	}()

	state, err := activeState(backend.environment)
	if err != nil {
		return "", err
	}
	status, err := runnerprocess.GetActiveStatus(loadContext, state)
	if err != nil {
		return "", err
	}
	if status.Kind == runnerprocess.StatusMismatched {
		return "", fmt.Errorf("active model cannot be safely inspected: %s", status.Detail)
	}
	if status.Kind == runnerprocess.StatusRunning && status.Preset == modelID &&
		status.Health != nil && *status.Health {
		backend.setLoadingPhase("verifying")
		backend.options.notice("%s is already loaded; verifying the served model...", modelID)
		if err := verifyGatewayModel(loadContext, status.Endpoint, modelID); err != nil {
			return "", err
		}
		return status.Endpoint, nil
	}
	if status.Kind != runnerprocess.StatusStopped {
		backend.setLoadingPhase("stopping")
		backend.options.notice("Stopping the current model...")
		if _, err := runnerprocess.StopActive(loadContext, state, runnerprocess.StopOptions{}); err != nil {
			return "", fmt.Errorf("stop active model: %w", err)
		}
	}
	backend.setLoadingPhase("preparing")
	session, err := startSession(loadContext, modelID, backend.environment, backend.options)
	if err != nil {
		return "", err
	}
	backend.setLoadingPhase("verifying")
	backend.options.notice("Verifying the served model identity and context...")
	if err := verifyGatewayModel(loadContext, session.Preparation.Plan.Endpoint, modelID); err != nil {
		return "", err
	}
	return session.Preparation.Plan.Endpoint, nil
}

func (backend *gatewayBackend) beginLoading(modelID string) {
	backend.loadingMu.Lock()
	defer backend.loadingMu.Unlock()
	backend.loading = &gatewayLoadingStatus{Model: modelID, Phase: "checking", StartedAt: time.Now().UTC()}
	backend.loadCancel = nil
	backend.paused = false
}

func (backend *gatewayBackend) setLoadCancel(cancel context.CancelFunc) {
	backend.loadingMu.Lock()
	defer backend.loadingMu.Unlock()
	backend.loadCancel = cancel
}

func (backend *gatewayBackend) setLoadingPhase(phase string) {
	backend.loadingMu.Lock()
	defer backend.loadingMu.Unlock()
	if backend.loading != nil {
		backend.loading.Phase = phase
	}
}

func (backend *gatewayBackend) reportLoadingProgress(progress llama.DownloadProgress) {
	backend.loadingMu.Lock()
	defer backend.loadingMu.Unlock()
	if backend.loading == nil {
		return
	}
	if backend.paused {
		return
	}
	backend.loading.Name = progress.Name
	if strings.HasPrefix(progress.Name, "verify ") {
		backend.loading.Phase = "verifying"
	} else {
		backend.loading.Phase = "downloading"
	}
	backend.loading.Downloaded = progress.Downloaded
	backend.loading.Total = progress.Total
	backend.loading.BytesPerSecond = progress.BytesPerSecond
	backend.loading.ETASeconds = int64(progress.ETA / time.Second)
}

func (backend *gatewayBackend) finishLoading(operationErr error) {
	backend.loadingMu.Lock()
	defer backend.loadingMu.Unlock()
	if operationErr == nil {
		backend.loading = nil
		backend.loadCancel = nil
		backend.paused = false
		return
	}
	if backend.loading == nil {
		return
	}
	backend.loadCancel = nil
	if backend.paused {
		backend.loading.Phase = "paused"
		backend.loading.Error = ""
		return
	}
	backend.loading.Phase = "error"
	backend.loading.Error = operationErr.Error()
}

func (backend *gatewayBackend) pauseLoading() error {
	backend.loadingMu.Lock()
	if backend.loading == nil || backend.loadCancel == nil {
		backend.loadingMu.Unlock()
		return errors.New("no model load is in progress")
	}
	backend.paused = true
	backend.loading.Phase = "paused"
	backend.loading.Error = ""
	cancel := backend.loadCancel
	backend.loadingMu.Unlock()
	cancel()
	return nil
}

func (backend *gatewayBackend) loadingSnapshot() *gatewayLoadingStatus {
	backend.loadingMu.RLock()
	defer backend.loadingMu.RUnlock()
	if backend.loading == nil {
		return nil
	}
	snapshot := *backend.loading
	return &snapshot
}

func verifyGatewayModel(ctx context.Context, endpointURL string, modelID string) error {
	profile, err := manifest.Get(modelID)
	if err != nil {
		return err
	}
	if _, err := endpoint.VerifyModelContract(ctx, endpointURL, profile); err != nil {
		return fmt.Errorf("loaded model identity does not match %q: %w", modelID, err)
	}
	return nil
}

func runGateway(ctx context.Context, environment map[string]string, options runOptions) error {
	frontPort, backendPort, err := gatewayPorts(environment)
	if err != nil {
		return err
	}
	state, err := activeState(environment)
	if err != nil {
		return err
	}
	backendEnvironment := cloneEnvironment(environment)
	backendEnvironment["OUTRIDER_PORT"] = strconv.Itoa(backendPort)
	backend := &gatewayBackend{environment: backendEnvironment, options: options}
	forwardProgress := options.Progress
	backend.options.Progress = func(progress llama.DownloadProgress) {
		backend.reportLoadingProgress(progress)
		if forwardProgress != nil {
			forwardProgress(progress)
		}
	}
	models, err := gatewayEntries(state.Root)
	if err != nil {
		return err
	}
	gateway, err := switcher.New(models, backend, gatewayAvailability(state.Root))
	if err != nil {
		return err
	}
	build := currentVersion()
	gateway.Build = switcher.Build{Version: build.Version, Commit: build.Commit}
	listener, err := net.Listen("tcp", net.JoinHostPort(manifest.DefaultHost, strconv.Itoa(frontPort)))
	if err != nil {
		return fmt.Errorf("model switcher cannot listen on 127.0.0.1:%d: %w", frontPort, err)
	}
	handler, err := gatewayHTTPHandler(gateway, backend, environment, frontPort)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	serveErr := server.Serve(listener)
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	<-shutdownDone
	return nil
}

func gatewayHTTPHandler(gateway *switcher.Server, backend switcher.Backend, environment map[string]string, frontPort int) (http.Handler, error) {
	state, err := activeState(environment)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", gateway.Handler())
	mux.HandleFunc("GET /admin/status", func(writer http.ResponseWriter, request *http.Request) {
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	mux.HandleFunc("GET /admin/logs", func(writer http.ResponseWriter, request *http.Request) {
		status, err := runnerprocess.GetActiveStatus(request.Context(), state)
		if err != nil {
			writeGatewayJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		logFile := status.LogFile
		if logFile == "" {
			logFile = filepath.Join(state.Root, "runs", "gateway", "gateway.log")
		}
		lineCount := 200
		if raw := request.URL.Query().Get("lines"); raw != "" {
			parsed, parseErr := strconv.Atoi(raw)
			if parseErr != nil || parsed < 1 || parsed > 1000 {
				writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": "lines must be between 1 and 1000"})
				return
			}
			lineCount = parsed
		}
		lines, err := readLogTail(logFile, lineCount)
		if err != nil && !os.IsNotExist(err) {
			writeGatewayJSON(writer, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("cannot read log %s: %v", logFile, err)})
			return
		}
		writeGatewayJSON(writer, http.StatusOK, gatewayLogsResponse{LogFile: logFile, Lines: lines})
	})
	mux.HandleFunc("POST /admin/model", func(writer http.ResponseWriter, request *http.Request) {
		if backend == nil {
			writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "model controls are unavailable"})
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := decodeGatewayJSON(request, &payload); err != nil || strings.TrimSpace(payload.Model) == "" {
			writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": "request must name a model"})
			return
		}
		if _, err := backend.Ensure(request.Context(), strings.TrimSpace(payload.Model)); err != nil {
			writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	mux.HandleFunc("POST /admin/download", func(writer http.ResponseWriter, request *http.Request) {
		typedBackend, ok := backend.(*gatewayBackend)
		if !ok {
			writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "model controls are unavailable"})
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		if err := decodeGatewayJSON(request, &payload); err != nil || strings.TrimSpace(payload.Model) == "" {
			writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": "request must name a model"})
			return
		}
		if err := typedBackend.Download(request.Context(), strings.TrimSpace(payload.Model)); err != nil {
			writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	mux.HandleFunc("POST /admin/download-path", func(writer http.ResponseWriter, request *http.Request) {
		typedBackend, ok := backend.(*gatewayBackend)
		if !ok {
			writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "model controls are unavailable"})
			return
		}
		var payload struct {
			Path string `json:"path"`
		}
		if err := decodeGatewayJSON(request, &payload); err != nil || strings.TrimSpace(payload.Path) == "" {
			writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": "request must include a model URL or Hugging Face path"})
			return
		}
		if err := typedBackend.DownloadPath(request.Context(), strings.TrimSpace(payload.Path)); err != nil {
			writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	mux.HandleFunc("POST /admin/delete", func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := decodeGatewayJSON(request, &payload); err != nil || strings.TrimSpace(payload.Model) == "" {
			writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": "request must name a model"})
			return
		}
		modelID := strings.TrimSpace(payload.Model)
		active, err := runnerprocess.GetActiveStatus(request.Context(), state)
		if err != nil {
			writeGatewayJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if active.Kind == runnerprocess.StatusRunning && active.Preset == modelID {
			writeGatewayJSON(writer, http.StatusConflict, map[string]string{"error": "unload the model before deleting it"})
			return
		}
		var deleteErr error
		if strings.HasPrefix(modelID, "custom-") {
			deleteErr = deleteCustomModel(state.Root, modelID)
		} else {
			deleteErr = deleteCachedModel(state.Root, modelID)
		}
		if deleteErr != nil {
			writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": deleteErr.Error()})
			return
		}
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	mux.HandleFunc("POST /admin/reveal", func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := decodeGatewayJSON(request, &payload); err != nil || strings.TrimSpace(payload.Model) == "" {
			writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": "request must name a model"})
			return
		}
		if err := revealCachedModel(state.Root, strings.TrimSpace(payload.Model)); err != nil {
			writeGatewayJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	mux.HandleFunc("POST /admin/stop", func(writer http.ResponseWriter, request *http.Request) {
		if _, err := runnerprocess.StopActive(request.Context(), state, runnerprocess.StopOptions{}); err != nil {
			writeGatewayJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	mux.HandleFunc("POST /admin/pause", func(writer http.ResponseWriter, request *http.Request) {
		typedBackend, ok := backend.(*gatewayBackend)
		if !ok {
			writeGatewayJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "model controls are unavailable"})
			return
		}
		if err := typedBackend.pauseLoading(); err != nil {
			writeGatewayJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeGatewayStatus(writer, request, state, frontPort, backend)
	})
	return mux, nil
}

func writeGatewayStatus(writer http.ResponseWriter, request *http.Request, state manifest.StatePaths, frontPort int, backend switcher.Backend) {
	model, err := runnerprocess.GetActiveStatus(request.Context(), state)
	if err != nil {
		writeGatewayJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var loading *gatewayLoadingStatus
	if typedBackend, ok := backend.(*gatewayBackend); ok {
		loading = typedBackend.loadingSnapshot()
	}
	models, err := gatewayCatalog(state.Root)
	if err != nil {
		writeGatewayJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeGatewayJSON(writer, http.StatusOK, gatewayDashboardStatus{
		GatewayEndpoint: fmt.Sprintf("http://%s:%d", manifest.DefaultHost, frontPort),
		GatewayHealth:   "ok", Model: model, Models: models, Loading: loading, UpdatedAt: time.Now().UTC(),
	})
}

func decodeGatewayJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeGatewayJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func readLogTail(path string, lineCount int) ([]string, error) {
	if lineCount < 1 {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(string(data), "\r\n")
	if trimmed == "" {
		return []string{}, nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > lineCount {
		lines = lines[len(lines)-lineCount:]
	}
	return lines, nil
}

// gatewayEntries is the catalog every gateway surface renders: the OpenAI
// listing, the dashboard, and the status response.
func gatewayEntries(root string) ([]catalog.Entry, error) {
	entries, err := catalog.Offered(func(profile manifest.Profile) (string, error) {
		state, err := manifest.Paths(root, profile, "")
		if err != nil {
			return "", err
		}
		return state.Model, nil
	})
	if err != nil {
		return nil, err
	}
	runnable := make([]catalog.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Runnable {
			runnable = append(runnable, entry)
		}
	}
	return runnable, nil
}

// gatewayAvailability answers /v1/models from the same inspection the
// dashboard reads, so the two views of what is on disk cannot disagree.
func gatewayAvailability(root string) switcher.AvailabilityFunc {
	return func(modelID string) (catalog.Weights, error) {
		profile, err := manifest.Get(modelID)
		if err != nil {
			return catalog.Weights{}, err
		}
		state, err := manifest.Paths(root, profile, "")
		if err != nil {
			return catalog.Weights{}, err
		}
		return catalog.InspectWeights(profile, state.Model)
	}
}

func gatewayCatalog(root string) ([]gatewayModelStatus, error) {
	entries, err := gatewayEntries(root)
	if err != nil {
		return nil, err
	}
	models := make([]gatewayModelStatus, 0, len(entries))
	for _, entry := range entries {
		_, protected := protectedGatewayModels[entry.ID]
		onDisk := entry.Weights.State != catalog.WeightsMissing
		path := ""
		if onDisk {
			path = entry.Weights.Path
		}
		models = append(models, gatewayModelStatus{
			ID: entry.ID, Context: entry.Context, TrainingContext: entry.TrainingContext,
			Quantization: entry.Quant, SizeBytes: entry.Weights.DeclaredBytes,
			Cached: entry.Weights.State == catalog.WeightsPresent, CanDelete: onDisk,
			Protected: protected, Path: path,
		})
	}
	state, err := manifest.Paths(root, mustGatewayProfile("tiny"), "")
	if err != nil {
		return nil, err
	}
	customEntries, err := os.ReadDir(state.Models)
	if os.IsNotExist(err) {
		return models, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range customEntries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), "custom-") || !strings.HasSuffix(entry.Name(), ".gguf") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		models = append(models, gatewayModelStatus{
			ID: strings.TrimSuffix(entry.Name(), ".gguf"), SizeBytes: info.Size(), Cached: true, CanDelete: true, Custom: true,
			Path: filepath.Join(state.Models, entry.Name()),
		})
	}
	return models, nil
}

func mustGatewayProfile(id string) manifest.Profile {
	profile, err := manifest.Get(id)
	if err != nil {
		panic(err)
	}
	return profile
}

var revealInFinder = func(path string) error {
	output, err := exec.Command("open", "-R", path).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("show in Finder: %w", err)
		}
		return fmt.Errorf("show in Finder: %s", message)
	}
	return nil
}

func catalogModelFile(root string, modelID string) (string, error) {
	if strings.HasPrefix(modelID, "custom-") {
		if strings.ContainsAny(modelID, `/\\`) || strings.Contains(modelID, "..") {
			return "", fmt.Errorf("invalid custom model id")
		}
		state, err := manifest.Paths(root, mustGatewayProfile("tiny"), "")
		if err != nil {
			return "", err
		}
		return filepath.Join(state.Models, modelID+".gguf"), nil
	}
	profile, err := runnableProfile(modelID)
	if err != nil {
		return "", err
	}
	state, err := manifest.Paths(root, profile, "")
	if err != nil {
		return "", err
	}
	return state.Model, nil
}

func revealCachedModel(root string, modelID string) error {
	path, err := catalogModelFile(root, modelID)
	if err != nil {
		return err
	}
	modelPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	state, err := manifest.Paths(root, mustGatewayProfile("tiny"), "")
	if err != nil {
		return err
	}
	modelsRoot, err := filepath.Abs(state.Models)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(modelsRoot, modelPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to reveal a model outside the Outrider cache")
	}
	if _, err := os.Lstat(modelPath); os.IsNotExist(err) {
		return fmt.Errorf("model %s is not downloaded", modelID)
	} else if err != nil {
		return err
	}
	return revealInFinder(modelPath)
}

func deleteCachedModel(root string, modelID string) error {
	profile, err := runnableProfile(modelID)
	if err != nil {
		return err
	}
	state, err := manifest.Paths(root, profile, "")
	if err != nil {
		return err
	}
	modelPath, err := filepath.Abs(state.Model)
	if err != nil {
		return err
	}
	modelsRoot, err := filepath.Abs(state.Models)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(modelsRoot, modelPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete model outside the Outrider cache")
	}
	removed := false
	for _, suffix := range []string{"", ".corrupt"} {
		path := modelPath + suffix
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("delete %s: %w", modelID, err)
		}
		removed = true
	}
	if !removed {
		return fmt.Errorf("model %s is not downloaded", modelID)
	}
	return nil
}

func deleteCustomModel(root string, modelID string) error {
	if !strings.HasPrefix(modelID, "custom-") || strings.ContainsAny(modelID, `/\\`) || strings.Contains(modelID, "..") {
		return fmt.Errorf("invalid custom model id")
	}
	state, err := manifest.Paths(root, mustGatewayProfile("tiny"), "")
	if err != nil {
		return err
	}
	path := filepath.Join(state.Models, modelID+".gguf")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return fmt.Errorf("model %s is not downloaded", modelID)
	} else if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete %s: %w", modelID, err)
	}
	return nil
}

func normalizeDownloadPath(value string) (string, string, error) {
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "https" || parsed.Host == "" {
			return "", "", fmt.Errorf("model URL must use https")
		}
		filename := filepath.Base(parsed.Path)
		if filename == "." || filename == string(filepath.Separator) || filename == "" {
			return "", "", fmt.Errorf("model URL must include a filename")
		}
		return value, sanitizeDownloadFilename(filename), nil
	}
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("use an https URL or a Hugging Face path such as owner/repo/model.gguf")
	}
	filename := parts[len(parts)-1]
	if filename == "" {
		return "", "", fmt.Errorf("model path must include a filename")
	}
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return "https://huggingface.co/" + strings.Join(parts[:2], "/") + "/resolve/main/" + strings.Join(parts[2:], "/") + "?download=true", sanitizeDownloadFilename(filename), nil
}

func sanitizeDownloadFilename(filename string) string {
	var builder strings.Builder
	for _, character := range filename {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "model.gguf"
	}
	return builder.String()
}

func gatewayPorts(environment map[string]string) (int, int, error) {
	front := manifest.DefaultPort
	if value, exists := environment["OUTRIDER_PORT"]; exists {
		parsed, err := parsePort(value, true)
		if err != nil {
			return 0, 0, err
		}
		front = *parsed
	}
	if front == 65535 {
		return 0, 0, runnerErrorf("OUTRIDER_PORT must leave the next port available for the model backend")
	}
	return front, front + 1, nil
}

func cloneEnvironment(environment map[string]string) map[string]string {
	cloned := make(map[string]string, len(environment))
	for key, value := range environment {
		cloned[key] = value
	}
	return cloned
}
