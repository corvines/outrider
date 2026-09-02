package chat

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// modelRow is one model as offered by one server.
type modelRow struct {
	id       string
	label    string
	endpoint string
	source   string
	quant    string
	ctx      int
	path     string
}

type discoveredMsg struct {
	rows []modelRow
	err  error
}

// defaultScanPorts are the loopback ports a local model server tends to hold.
var defaultScanPorts = []int{11434, 11435, 11436, 11437, 11438, 11439, 11440}

type rawModel struct {
	ID            string `json:"id"`
	OwnedBy       string `json:"owned_by"`
	Quantization  string `json:"quantization"`
	ContextWindow int    `json:"context_window"`
	Meta          struct {
		NCtx  int    `json:"n_ctx"`
		FType string `json:"ftype"`
	} `json:"meta"`
}

type rawModelsResp struct {
	Data []rawModel `json:"data"`
}

var probeClient = &http.Client{Timeout: 600 * time.Millisecond}

// discoverModels lists what every reachable local server offers, so a row can
// say where it came from. The configured endpoint is always included.
func discoverModels(endpoint string, ports []int) tea.Cmd {
	return func() tea.Msg {
		targets := scanTargets(endpoint, ports)
		var mu sync.Mutex
		var rows []modelRow
		var wg sync.WaitGroup
		for _, target := range targets {
			wg.Add(1)
			go func(target string) {
				defer wg.Done()
				found := probe(target)
				mu.Lock()
				rows = append(rows, found...)
				mu.Unlock()
			}(target)
		}
		wg.Wait()
		if len(rows) == 0 {
			return discoveredMsg{err: endpointUnreachable{
				url: endpoint,
				err: fmt.Errorf("no models on any local server"),
			}}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].label != rows[j].label {
				return rows[i].label < rows[j].label
			}
			return rows[i].endpoint < rows[j].endpoint
		})
		return discoveredMsg{rows: rows}
	}
}

// scanTargets puts the configured endpoint first and adds the loopback ports
// worth probing on the same host.
func scanTargets(endpoint string, ports []int) []string {
	out := []string{endpoint}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return out
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return out
	}
	for _, candidate := range ports {
		if strconv.Itoa(candidate) == port {
			continue
		}
		out = append(out, fmt.Sprintf("%s://%s:%d", parsed.Scheme, host, candidate))
	}
	return out
}

func probe(endpoint string) []modelRow {
	res, err := probeClient.Get(endpoint + "/v1/models")
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil
	}
	var parsed rawModelsResp
	if json.NewDecoder(res.Body).Decode(&parsed) != nil {
		return nil
	}
	path := weightsPath(endpoint)
	rows := make([]modelRow, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if item.ID == "" {
			continue
		}
		rows = append(rows, modelRow{
			id:       item.ID,
			label:    modelLabel(item.ID),
			endpoint: endpoint,
			source:   sourceLabel(endpoint, item.OwnedBy),
			quant:    shortQuant(ifEmpty(item.Meta.FType, item.Quantization)),
			ctx:      firstNonZero(item.Meta.NCtx, item.ContextWindow),
			path:     path,
		})
	}
	return rows
}

// sourceLabel names the port and the server software behind it.
func sourceLabel(endpoint, ownedBy string) string {
	port := endpointPort(endpoint)
	switch ownedBy {
	case "llamacpp":
		return port + " llama.cpp"
	case "library":
		return port + " ollama"
	case "":
		return port
	}
	return port + " " + ownedBy
}

func endpointPort(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return parsed.Host
	}
	return ":" + port
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

// formatContext shortens a context length to thousands once it is large enough
// for the exact figure to stop mattering in a table.
func formatContext(n int) string {
	if n == 0 {
		return "?"
	}
	if n >= 1024 && n%1024 == 0 {
		return strconv.Itoa(n/1024) + "k"
	}
	return formatInt(n)
}

// shortQuant turns llama.cpp's spelled-out file type ("Q4_K - Medium") into the
// form the weights are published under ("Q4_K_M").
func shortQuant(ftype string) string {
	base, size, found := strings.Cut(ftype, " - ")
	if !found {
		return strings.TrimSpace(ftype)
	}
	size = strings.TrimSpace(size)
	if size == "" {
		return strings.TrimSpace(base)
	}
	return strings.TrimSpace(base) + "_" + strings.ToUpper(size[:1])
}

type propsResp struct {
	ModelPath string `json:"model_path"`
}

// weightsPath asks a server which file it loaded, so a row can point at the
// weights on disk. Servers that host many models do not answer.
func weightsPath(endpoint string) string {
	res, err := probeClient.Get(endpoint + "/props")
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return ""
	}
	var props propsResp
	if json.NewDecoder(res.Body).Decode(&props) != nil {
		return ""
	}
	return props.ModelPath
}

// shortPath swaps the home directory for a tilde so a path fits a panel.
func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home+"/") {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}
