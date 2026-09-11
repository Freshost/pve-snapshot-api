package pool

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestMappingChangeAndAvailability(t *testing.T) {
	out := `{"type":"zfspool","pool":"old/data"}`
	r := New(time.Second, func(context.Context, string, ...string) ([]byte, error) { return []byte(out), nil })
	first, e := r.Fetch(context.Background(), "zfs")
	require.NoError(t, e)
	out = `{"type":"zfspool","pool":"new/data","nodes":"pve2","disable":1}`
	second, e := r.Fetch(context.Background(), "zfs")
	require.NoError(t, e)
	require.NotEqual(t, first.Pool, second.Pool)
	require.Error(t, second.Available("pve1"))
	second.Disable = 0
	require.Error(t, second.Available("pve1"))
	require.NoError(t, second.Available("pve2"))
}
func TestVolumeBoundaries(t *testing.T) {
	for _, id := range []string{"archive", "../vm-100-disk-0", "other:vm-100-disk-0", "subvol-100-disk-0", "vm-0-disk-0", "base-100-disk-0", "vm-100-disk-0/child"} {
		_, e := NormalizeVolume("zfs", id)
		require.Error(t, e, id)
	}
	v, e := NormalizeVolume("zfs", "zfs:vm-100-disk-0")
	require.NoError(t, e)
	require.Equal(t, "vm-100-disk-0", v)
}
