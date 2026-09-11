package cluster

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestManagementStatusSchema(t *testing.T) {
	cs := New(time.Second, func(_ context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "pvesh", name)
		require.Equal(t, "/cluster/status", args[1])
		return []byte(`[{"type":"cluster","name":"production","quorate":1},{"type":"node","name":"pve1","ip":"2001:db8::1","online":1}]`), nil
	})
	require.NoError(t, cs.Discover(context.Background()))
	ip, e := cs.GetNodeIP("pve1")
	require.NoError(t, e)
	require.Equal(t, "2001:db8::1", ip)
}
func TestDiscoveryRejectsCorosyncOrEmptyAddress(t *testing.T) {
	cs := New(time.Second, mockRunner([]byte(`[{"name":"pve1","ring0_addr":"10.0.0.1"}]`), nil))
	require.Error(t, cs.Discover(context.Background()))
	require.Empty(t, cs.GetNodeList())
}
