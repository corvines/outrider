package switcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/corvines/outrider/internal/catalog"
	"github.com/corvines/outrider/internal/endpoint"
)

const maxRequestBytes = 16 << 20

type Backend interface {
	Ensure(context.Context, string) (string, error)
}

// Loader is an optional Backend capability: saying what is loaded right now
// without loading anything. A backend that does not implement it makes every
// model report itself as not loaded, which is honest and costs nothing.
type Loader interface {
	Loaded(ctx context.Context) (modelID string, endpointURL string, ok bool)
}

// ResolveFunc reads a running backend's own view of itself.
type ResolveFunc func(ctx context.Context, endpointURL string, modelID string) (endpoint.Resolved, error)

// AvailabilityFunc answers for one model id. It runs per request so the
// catalog reflects a download or deletion without a gateway restart.
type AvailabilityFunc func(modelID string) (catalog.Weights, error)

type Server struct {
	// Build identifies the binary serving this catalog. A long-running
	// gateway answers from the catalog compiled into it, so a client that
	// upgraded the binary needs a way to see it is talking to the old one.
	Build Build

	models       map[string]catalog.Entry
	ordered      []catalog.Entry
	backend      Backend
	availability AvailabilityFunc
	client       *http.Client
	resolve      ResolveFunc
	mu           sync.Mutex
}

// New builds the switcher. availability may be nil, in which case /v1/models
// reports the weights state carried by the entries themselves.
func New(models []catalog.Entry, backend Backend, availability AvailabilityFunc) (*Server, error) {
	if len(models) == 0 {
		return nil, fmt.Errorf("model switcher requires at least one model")
	}
	byID := make(map[string]catalog.Entry, len(models))
	for _, model := range models {
		if strings.TrimSpace(model.ID) == "" {
			return nil, fmt.Errorf("model switcher received an empty model id")
		}
		if _, exists := byID[model.ID]; exists {
			return nil, fmt.Errorf("model switcher received duplicate model id %q", model.ID)
		}
		byID[model.ID] = model
	}
	return &Server{
		models: byID, ordered: append([]catalog.Entry(nil), models...), backend: backend,
		availability: availability, client: http.DefaultClient,
		resolve: endpoint.FetchResolved,
	}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("GET /v1/models", server.listModels)
	mux.HandleFunc("GET /v1/models/{id}", server.showModel)
	mux.HandleFunc("POST /v1/chat/completions", server.proxyModelRequest)
	mux.HandleFunc("POST /v1/completions", server.proxyModelRequest)
	mux.HandleFunc("POST /v1/embeddings", server.proxyModelRequest)
	mux.HandleFunc("POST /v1/responses", server.proxyModelRequest)
	return mux
}

// Build identifies the running binary.
type Build struct {
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, struct {
		Status  string `json:"status"`
		Version string `json:"version,omitempty"`
		Commit  string `json:"commit,omitempty"`
	}{Status: "ok", Version: server.Build.Version, Commit: server.Build.Commit})
}

func (server *Server) listModels(writer http.ResponseWriter, _ *http.Request) {
	data := make([]modelEntry, 0, len(server.ordered))
	for _, model := range server.ordered {
		weights := model.Weights
		if server.availability != nil {
			// A caller cannot tell an omitted field from a failed lookup, and
			// reporting "missing" for a model that is on disk would provoke a
			// needless multi-gigabyte download. Fail the request instead.
			current, err := server.availability(model.ID)
			if err != nil {
				writeError(writer, http.StatusInternalServerError, "could not inspect model weights")
				return
			}
			weights = current
		}
		data = append(data, newModelEntry(model, weights))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"object": "list", "data": data})
}

// showModel answers for one model: what it is, what outrider asked the
// backend for, and what a running backend reports back. The two halves are
// kept apart because only some of a request is confirmed.
func (server *Server) showModel(writer http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	model, exists := server.models[id]
	if !exists {
		writeError(writer, http.StatusNotFound, fmt.Sprintf("unknown model %q", id))
		return
	}
	weights := model.Weights
	if server.availability != nil {
		current, err := server.availability(model.ID)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "could not inspect model weights")
			return
		}
		weights = current
	}
	detail := modelDetail{
		modelEntry: newModelEntry(model, weights),
		Requested:  newRequestedSettings(model.Requested),
	}
	if resolved, ok := server.resolveLoaded(request.Context(), model.ID); ok {
		detail.Resolved = newResolvedState(resolved)
	}
	writeJSON(writer, http.StatusOK, detail)
}

// resolveLoaded asks the backend about a model only when that model is the
// one already serving. Reporting nothing is better than starting a load for
// a request that reads like a lookup.
func (server *Server) resolveLoaded(ctx context.Context, modelID string) (endpoint.Resolved, bool) {
	loader, ok := server.backend.(Loader)
	if !ok || server.resolve == nil {
		return endpoint.Resolved{}, false
	}
	loaded, backendURL, ok := loader.Loaded(ctx)
	if !ok || loaded != modelID {
		return endpoint.Resolved{}, false
	}
	resolved, err := server.resolve(ctx, backendURL, modelID)
	if err != nil {
		// A backend that will not describe itself is reported as not
		// described, never as a failed lookup of the model itself.
		return endpoint.Resolved{}, false
	}
	return resolved, true
}

func (server *Server) proxyModelRequest(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(io.LimitReader(request.Body, maxRequestBytes+1))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "could not read request")
		return
	}
	if len(body) > maxRequestBytes {
		writeError(writer, http.StatusRequestEntityTooLarge, "request is too large")
		return
	}
	var envelope struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || strings.TrimSpace(envelope.Model) == "" {
		writeError(writer, http.StatusBadRequest, "request must name a model")
		return
	}
	if _, exists := server.models[envelope.Model]; !exists {
		writeError(writer, http.StatusNotFound, fmt.Sprintf("unknown model %q", envelope.Model))
		return
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	backendURL, err := server.backend.Ensure(request.Context(), envelope.Model)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, err.Error())
		return
	}
	upstream, err := http.NewRequestWithContext(
		request.Context(), request.Method, strings.TrimRight(backendURL, "/")+request.URL.Path,
		bytes.NewReader(body),
	)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "could not build backend request")
		return
	}
	copyHeaders(upstream.Header, request.Header)
	response, err := server.client.Do(upstream)
	if err != nil {
		writeError(writer, http.StatusBadGateway, fmt.Sprintf("model backend failed: %v", err))
		return
	}
	defer response.Body.Close()
	copyHeaders(writer.Header(), response.Header)
	writer.Header().Set("X-Outrider-Model", envelope.Model)
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(flushingWriter{writer: writer}, response.Body)
}

type flushingWriter struct {
	writer http.ResponseWriter
}

func (writer flushingWriter) Write(payload []byte) (int, error) {
	written, err := writer.writer.Write(payload)
	if flusher, ok := writer.writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return written, err
}

func copyHeaders(destination http.Header, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]any{
		"error": map[string]string{"message": message, "type": "outrider_model_error"},
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
