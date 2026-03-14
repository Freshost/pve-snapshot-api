package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clusterNode mirrors the unexported type in the cluster package for building
// mock pvesh output.
type clusterNode struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

func fakePveshOutput(nodes []clusterNode) []byte {
	b, _ := json.Marshal(nodes)
	return b
}

// newTestCluster builds a ClusterState with the given nodes already discovered.
func newTestCluster(t *testing.T, nodes []clusterNode) *cluster.ClusterState {
	t.Helper()
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return fakePveshOutput(nodes), nil
	}
	cs := cluster.New(5*time.Second, runner)
	require.NoError(t, cs.Discover(context.Background()))
	return cs
}

func localHostname(t *testing.T) string {
	t.Helper()
	h, err := os.Hostname()
	require.NoError(t, err)
	return h
}

// --- ShouldProxy tests ---

func TestShouldProxy_ReturnsFalseForLocalNode(t *testing.T) {
	hostname := localHostname(t)
	cs := newTestCluster(t, []clusterNode{
		{Name: hostname, IP: "127.0.0.1"},
	})
	p := New(cs, 8080, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots", nil)
	assert.False(t, p.ShouldProxy(req, hostname), "should not proxy requests for the local node")
}

func TestShouldProxy_ReturnsTrueForRemoteNode(t *testing.T) {
	hostname := localHostname(t)
	cs := newTestCluster(t, []clusterNode{
		{Name: hostname, IP: "127.0.0.1"},
		{Name: "remote-node", IP: "10.0.0.2"},
	})
	p := New(cs, 8080, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots", nil)
	assert.True(t, p.ShouldProxy(req, "remote-node"), "should proxy requests for a remote node")
}

func TestShouldProxy_ReturnsFalseWhenAlreadyForwarded(t *testing.T) {
	hostname := localHostname(t)
	cs := newTestCluster(t, []clusterNode{
		{Name: hostname, IP: "127.0.0.1"},
		{Name: "remote-node", IP: "10.0.0.2"},
	})
	p := New(cs, 8080, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots", nil)
	req.Header.Set("X-Forwarded-Node", "remote-node")
	assert.False(t, p.ShouldProxy(req, "remote-node"), "should not proxy when X-Forwarded-Node is set")
}

func TestShouldProxy_ReturnsFalseForEmptyNode(t *testing.T) {
	cs := newTestCluster(t, nil)
	p := New(cs, 8080, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots", nil)
	assert.False(t, p.ShouldProxy(req, ""), "should not proxy when node is empty")
}

// --- Forward tests ---

func TestForward_ProxiesRequestToTargetNode(t *testing.T) {
	// Start a backend server that echoes request details.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "reached")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"method":"%s","path":"%s","forwarded":"%s"}`,
			r.Method, r.URL.Path, r.Header.Get("X-Forwarded-Node"))
	}))
	defer backend.Close()

	// Extract IP and port from the backend server's URL.
	backendHost, backendPortStr, err := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	require.NoError(t, err)
	backendPort, err := strconv.Atoi(backendPortStr)
	require.NoError(t, err)

	// Build cluster state with the remote node pointing to the backend server.
	cs := newTestCluster(t, []clusterNode{
		{Name: "remote-node", IP: backendHost},
	})
	p := New(cs, backendPort, false)

	// Create the request that will be forwarded.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots?vmid=100", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	p.Forward(rec, req, "remote-node")

	resp := rec.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "reached", resp.Header.Get("X-Backend"))

	var respData map[string]string
	require.NoError(t, json.Unmarshal(body, &respData))
	assert.Equal(t, "GET", respData["method"])
	assert.Equal(t, "/api/v1/snapshots", respData["path"])
	assert.Equal(t, "remote-node", respData["forwarded"])
}

func TestForward_PreservesRequestHeaders(t *testing.T) {
	var receivedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendHost, backendPortStr, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	backendPort, _ := strconv.Atoi(backendPortStr)

	cs := newTestCluster(t, []clusterNode{
		{Name: "remote-node", IP: backendHost},
	})
	p := New(cs, backendPort, false)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", strings.NewReader(`{"vmid":100}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	p.Forward(rec, req, "remote-node")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "Bearer test-token", receivedHeaders.Get("Authorization"))
	assert.Equal(t, "remote-node", receivedHeaders.Get("X-Forwarded-Node"))
}

func TestForward_ReturnsErrorForUnknownNode(t *testing.T) {
	cs := newTestCluster(t, []clusterNode{
		{Name: "known-node", IP: "10.0.0.1"},
	})
	p := New(cs, 8080, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots", nil)
	rec := httptest.NewRecorder()

	p.Forward(rec, req, "unknown-node")

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "NODE_NOT_FOUND")
	assert.Contains(t, rec.Body.String(), "unknown-node")
}

func TestForward_PropagatesBackendStatusCode(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer backend.Close()

	backendHost, backendPortStr, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
	backendPort, _ := strconv.Atoi(backendPortStr)

	cs := newTestCluster(t, []clusterNode{
		{Name: "remote-node", IP: backendHost},
	})
	p := New(cs, backendPort, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots", nil)
	rec := httptest.NewRecorder()

	p.Forward(rec, req, "remote-node")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "not found")
}
