package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type clusterNode struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// ClusterState tracks known cluster nodes.
type ClusterState struct {
	mu        sync.RWMutex
	nodes     map[string]string // name → IP
	localName string
	timeout   time.Duration
	run       CommandRunner
}

// New creates a ClusterState.
func New(timeout time.Duration, runner CommandRunner) *ClusterState {
	hostname, _ := os.Hostname()
	return &ClusterState{
		nodes:     make(map[string]string),
		localName: hostname,
		timeout:   timeout,
		run:       runner,
	}
}

// Discover fetches the current cluster node list from pvesh.
func (cs *ClusterState) Discover(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, cs.timeout)
	defer cancel()

	out, err := cs.run(ctx, "pvesh", "get", "/cluster/config/nodes", "--output-format", "json")
	if err != nil {
		return fmt.Errorf("cluster discovery: %s: %w", string(out), err)
	}

	var nodes []clusterNode
	if err := json.Unmarshal(out, &nodes); err != nil {
		return fmt.Errorf("parsing cluster nodes: %w", err)
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.nodes = make(map[string]string, len(nodes))
	for _, n := range nodes {
		cs.nodes[n.Name] = n.IP
	}

	return nil
}

// GetNodeIP returns the IP address for a given node name.
func (cs *ClusterState) GetNodeIP(name string) (string, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	ip, ok := cs.nodes[name]
	if !ok {
		return "", fmt.Errorf("node %s not found in cluster", name)
	}
	return ip, nil
}

// GetNodeList returns all known node names.
func (cs *ClusterState) GetNodeList() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	names := make([]string, 0, len(cs.nodes))
	for name := range cs.nodes {
		names = append(names, name)
	}
	return names
}

// IsLocal returns true if the given node name matches the local hostname.
func (cs *ClusterState) IsLocal(name string) bool {
	return name == cs.localName
}

// StartPeriodicRefresh runs Discover on a timer until ctx is cancelled.
func (cs *ClusterState) StartPeriodicRefresh(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := cs.Discover(ctx); err != nil {
				slog.Error("cluster refresh failed", "error", err)
			}
		}
	}
}
