package task

import (
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableTasksAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	s, e := OpenStore(path)
	require.NoError(t, e)
	done := &TaskResult{UPID: GenerateUPID("pve1", "imgcopy", "vm-100-disk-0", "csi@pve!one"), Status: "stopped", ExitStatus: "OK", User: "csi@pve!one"}
	running := &TaskResult{UPID: GenerateUPID("pve1", "imgdel", "vm-100-disk-0", "csi@pve!one"), Status: "running"}
	require.NoError(t, s.Put(done))
	require.NoError(t, s.Put(running))
	restored, e := OpenStore(path)
	require.NoError(t, e)
	require.Equal(t, "OK", restored.Get(done.UPID).ExitStatus)
	require.Equal(t, "stopped", restored.Get(running.UPID).Status)
	require.Contains(t, restored.Get(running.UPID).ExitStatus, "restarted")
	copy := restored.Get(done.UPID)
	copy.User = "attacker"
	require.Equal(t, "csi@pve!one", restored.Get(done.UPID).User)
}
func TestRetentionAndUniqueUPIDs(t *testing.T) {
	s := NewStore()
	s.limit = 3
	ids := map[string]bool{}
	for i := 0; i < 10000; i++ {
		id := GenerateUPID("pve1", "imgcopy", "vm-100-disk-0", "u@pve!t")
		require.False(t, ids[id])
		ids[id] = true
	}
	for i := 0; i < 10; i++ {
		require.NoError(t, s.Put(&TaskResult{UPID: GenerateUPID("pve1", "imgdel", "vm-100-disk-0", "u@pve!t"), Status: "stopped"}))
	}
	require.LessOrEqual(t, len(s.tasks), 3)
	require.NoError(t, s.Put(&TaskResult{UPID: "expired", Status: "stopped", StartTime: time.Now().Add(-8 * 24 * time.Hour).Unix()}))
	require.Nil(t, s.Get("expired"))
}
