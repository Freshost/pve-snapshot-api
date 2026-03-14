package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRunner returns a CommandRunner that captures calls and returns canned data.
func mockRunner(output []byte, err error) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return output, err
	}
}

func fakePveshOutput(nodes []clusterNode) []byte {
	b, _ := json.Marshal(nodes)
	return b
}

func TestDiscover_ParsesNodesCorrectly(t *testing.T) {
	nodes := []clusterNode{
		{Name: "pve1", IP: "10.0.0.1"},
		{Name: "pve2", IP: "10.0.0.2"},
		{Name: "pve3", IP: "10.0.0.3"},
	}
	runner := mockRunner(fakePveshOutput(nodes), nil)
	cs := New(5*time.Second, runner)

	err := cs.Discover(context.Background())
	require.NoError(t, err)

	ip1, err := cs.GetNodeIP("pve1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ip1)

	ip2, err := cs.GetNodeIP("pve2")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ip2)

	ip3, err := cs.GetNodeIP("pve3")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.3", ip3)
}

func TestDiscover_ReturnsErrorOnCommandFailure(t *testing.T) {
	runner := mockRunner([]byte("some stderr output"), fmt.Errorf("exit status 1"))
	cs := New(5*time.Second, runner)

	err := cs.Discover(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster discovery")
	assert.Contains(t, err.Error(), "some stderr output")
}

func TestDiscover_ReturnsErrorOnInvalidJSON(t *testing.T) {
	runner := mockRunner([]byte("not valid json"), nil)
	cs := New(5*time.Second, runner)

	err := cs.Discover(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing cluster nodes")
}

func TestDiscover_ReplacesOldNodes(t *testing.T) {
	// First discovery with two nodes.
	nodes1 := []clusterNode{
		{Name: "pve1", IP: "10.0.0.1"},
		{Name: "pve2", IP: "10.0.0.2"},
	}
	callCount := 0
	nodes2 := []clusterNode{
		{Name: "pve3", IP: "10.0.0.3"},
	}
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		callCount++
		if callCount == 1 {
			return fakePveshOutput(nodes1), nil
		}
		return fakePveshOutput(nodes2), nil
	}
	cs := New(5*time.Second, runner)

	require.NoError(t, cs.Discover(context.Background()))
	assert.Len(t, cs.GetNodeList(), 2)

	// Second discovery replaces the node map entirely.
	require.NoError(t, cs.Discover(context.Background()))
	nodeList := cs.GetNodeList()
	assert.Len(t, nodeList, 1)
	assert.Equal(t, "pve3", nodeList[0])

	// Old nodes are gone.
	_, err := cs.GetNodeIP("pve1")
	assert.Error(t, err)
}

func TestGetNodeIP_ReturnsCorrectIP(t *testing.T) {
	nodes := []clusterNode{
		{Name: "alpha", IP: "192.168.1.10"},
		{Name: "beta", IP: "192.168.1.20"},
	}
	runner := mockRunner(fakePveshOutput(nodes), nil)
	cs := New(5*time.Second, runner)
	require.NoError(t, cs.Discover(context.Background()))

	ip, err := cs.GetNodeIP("alpha")
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.10", ip)

	ip, err = cs.GetNodeIP("beta")
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.20", ip)
}

func TestGetNodeIP_ErrorForUnknownNode(t *testing.T) {
	nodes := []clusterNode{
		{Name: "alpha", IP: "192.168.1.10"},
	}
	runner := mockRunner(fakePveshOutput(nodes), nil)
	cs := New(5*time.Second, runner)
	require.NoError(t, cs.Discover(context.Background()))

	_, err := cs.GetNodeIP("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "not found")
}

func TestGetNodeList_ReturnsAllNodeNames(t *testing.T) {
	nodes := []clusterNode{
		{Name: "node-a", IP: "10.0.0.1"},
		{Name: "node-b", IP: "10.0.0.2"},
		{Name: "node-c", IP: "10.0.0.3"},
	}
	runner := mockRunner(fakePveshOutput(nodes), nil)
	cs := New(5*time.Second, runner)
	require.NoError(t, cs.Discover(context.Background()))

	names := cs.GetNodeList()
	sort.Strings(names)
	assert.Equal(t, []string{"node-a", "node-b", "node-c"}, names)
}

func TestGetNodeList_EmptyBeforeDiscover(t *testing.T) {
	runner := mockRunner(nil, nil)
	cs := New(5*time.Second, runner)

	names := cs.GetNodeList()
	assert.Empty(t, names)
}

func TestIsLocal_MatchesHostname(t *testing.T) {
	runner := mockRunner(nil, nil)
	cs := New(5*time.Second, runner)

	hostname, err := os.Hostname()
	require.NoError(t, err)

	assert.True(t, cs.IsLocal(hostname), "IsLocal should return true for the local hostname")
	assert.False(t, cs.IsLocal("some-other-node"), "IsLocal should return false for a different hostname")
}

func TestIsLocal_EmptyString(t *testing.T) {
	runner := mockRunner(nil, nil)
	cs := New(5*time.Second, runner)

	// Empty string should not match unless the hostname is empty.
	hostname, _ := os.Hostname()
	if hostname != "" {
		assert.False(t, cs.IsLocal(""))
	}
}
