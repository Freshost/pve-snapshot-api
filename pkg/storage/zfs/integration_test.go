package zfs

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/config"
	"github.com/stretchr/testify/require"
)

// Run only against the disposable pool created by scripts/test-zfs.sh.
func TestRealZFSLifecycle(t *testing.T) {
	root := os.Getenv("PVE_SNAPSHOT_TEST_POOL")
	if root == "" {
		t.Skip("run scripts/test-zfs.sh on a Linux host with ZFS")
	}
	require.True(t, strings.HasPrefix(root, "psatest"))
	require.NotContains(t, root, "/")
	ctx := context.Background()
	z := New(&config.Config{ZFSTimeout: 30 * time.Second}, nil)
	run := func(args ...string) string {
		out, err := DefaultRunner(ctx, "zfs", args...)
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	for index, order := range permutations([]int{0, 1, 2, 3}) {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			parent := fmt.Sprintf("%s/case%d", root, index)
			run("create", parent)
			names := []string{parent + "/vm-100-disk-0", parent + "/vm-200-disk-0", parent + "/vm-300-disk-0", parent + "/vm-400-disk-0"}
			run("create", "-s", "-V", "8M", names[0])
			require.NoError(t, z.CopyVolume(ctx, names[0], names[1]))
			require.NoError(t, z.CopyVolume(ctx, names[0], names[2]))
			require.NoError(t, z.CopyVolume(ctx, names[1], names[3]))
			require.NoError(t, z.CopyVolume(ctx, names[0], names[1]))
			alive := map[int]bool{0: true, 1: true, 2: true, 3: true}
			for _, i := range order {
				require.NoError(t, z.DestroyVolume(ctx, names[i]))
				delete(alive, i)
				for j := range alive {
					require.Equal(t, "8388608", run("get", "-Hp", "-o", "value", "volsize", names[j]))
				}
			}
			require.Equal(t, "", run("list", "-H", "-o", "name", "-r", "-t", "snapshot", parent))
			run("destroy", parent)
		})
	}
}
