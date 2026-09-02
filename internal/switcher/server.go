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
)

const maxRequestBytes = 16 << 20

type Model struct {
	ID              string
	ContextWindow   int
	TrainingContext int
	Quantization    string
}

type Backend interface {
	Ensure(context.Context, string) (string, error)
}

type Server struct {
	models  map[string]Model
	ordered []Model
	backend Backend
	client  *http.Client
	mu      sync.Mutex
}

func New(models []Model, backend Backend) (*Server, error) {
	if len(models) == 0 {
		return nil, fmt.Errorf("model switcher requires at least one model")
	}
	byID := make(map[string]Model, len(models))
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
		models: byID, ordered: append([]Model(nil), models...), backend: backend, client: http.DefaultClient,
	}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("GET /v1/models", server.listModels)
	mux.HandleFunc("POST /v1/chat/completions", server.proxyModelRequest)
	mux.HandleFunc("POST /v1/completions", server.proxyModelRequest)
	mux.HandleFunc("POST /v1/embeddings", server.proxyModelRequest)
	mux.HandleFunc("POST /v1/responses", server.proxyModelRequest)
	return mux
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) listModels(writer http.ResponseWriter, _ *http.Request) {
	type meta struct {
		Context         int `json:"n_ctx"`
		TrainingContext int `json:"n_ctx_train,omitempty"`
	}
	type entry struct {
		ID           string `json:"id"`
		Object       string `json:"object"`
		OwnedBy      string `json:"owned_by"`
		Quantization string `json:"quantization,omitempty"`
		Meta         meta   `json:"meta"`
	}
	data := make([]entry, 0, len(server.ordered))
	for _, model := range server.ordered {
		data = append(data, entry{
			ID: model.ID, Object: "model", OwnedBy: "outrider", Quantization: model.Quantization,
			Meta: meta{Context: model.ContextWindow, TrainingContext: model.TrainingContext},
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"object": "list", "data": data})
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
