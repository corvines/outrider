package chat

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Another listener is a dead end: the session cannot switch a server it does
// not drive, so without --debug nothing but the configured endpoint is read.
func TestDiscoveryReadsOnlyTheConfiguredEndpoint(t *testing.T) {
	gateway := modelServer(t, "outrider", "qwen35-0.8b")
	other := modelServer(t, "library", "llama3")
	rows := runDiscovery(t, gateway.URL, []int{portOf(t, other.URL)}, false)
	if rows.err != nil {
		t.Fatal(rows.err)
	}
	if len(rows.rows) != 1 || rows.rows[0].id != "qwen35-0.8b" {
		t.Fatalf("rows = %+v", rows.rows)
	}
}

func TestDebugDiscoveryReadsTheOtherPorts(t *testing.T) {
	gateway := modelServer(t, "outrider", "qwen35-0.8b")
	other := modelServer(t, "library", "llama3")
	rows := runDiscovery(t, gateway.URL, []int{portOf(t, other.URL)}, true)
	if rows.err != nil {
		t.Fatal(rows.err)
	}
	if len(rows.rows) != 2 {
		t.Fatalf("rows = %+v", rows.rows)
	}
}

// The gateway says which models are its own, and the picker groups by that.
func TestGatewayModelsGroupUnderOutrider(t *testing.T) {
	gateway := modelServer(t, "outrider", "qwen35-0.8b")
	rows := runDiscovery(t, gateway.URL, nil, false)
	if rows.err != nil {
		t.Fatal(rows.err)
	}
	if rows.rows[0].group != groupOutrider {
		t.Fatalf("group = %q", rows.rows[0].group)
	}
}

func runDiscovery(t *testing.T, endpoint string, ports []int, debug bool) discoveredMsg {
	t.Helper()
	msg, ok := discoverModels(endpoint, ports, debug)().(discoveredMsg)
	if !ok {
		t.Fatalf("discovery returned %T", tea.Msg(nil))
	}
	return msg
}

func portOf(t *testing.T, endpoint string) int {
	t.Helper()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return number
}

func modelServer(t *testing.T, ownedBy string, id string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/models" {
				http.NotFound(response, request)
				return
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"data": []map[string]any{{"id": id, "owned_by": ownedBy}},
			})
		}))
	t.Cleanup(server.Close)
	return server
}
