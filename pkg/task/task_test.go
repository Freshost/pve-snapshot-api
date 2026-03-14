package task

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateUPID(t *testing.T) {
	upid := GenerateUPID("pve1", "imgcopy", "vm-100-disk-0", "root@pam")

	assert.True(t, strings.HasPrefix(upid, "UPID:pve1:"))
	assert.True(t, strings.HasSuffix(upid, ":imgcopy:vm-100-disk-0:root@pam:"))

	// Should have 8 colon-separated fields (with trailing colon)
	parts := strings.Split(strings.TrimSuffix(upid, ":"), ":")
	require.Len(t, parts, 8)
	assert.Equal(t, "UPID", parts[0])
	assert.Equal(t, "pve1", parts[1])
	// parts[2], [3], [4] are hex pid, pstart, time
	assert.Len(t, parts[2], 8) // hex pid
	assert.Len(t, parts[3], 8) // hex pstart
	assert.Len(t, parts[4], 8) // hex time
	assert.Equal(t, "imgcopy", parts[5])
	assert.Equal(t, "vm-100-disk-0", parts[6])
	assert.Equal(t, "root@pam", parts[7])
}

func TestGenerateUPID_DifferentTypes(t *testing.T) {
	upid := GenerateUPID("node2", "imgdel", "vm-200-disk-1", "user@pve")
	assert.Contains(t, upid, ":imgdel:")
	assert.Contains(t, upid, ":vm-200-disk-1:")
	assert.Contains(t, upid, ":user@pve:")
}

func TestExtractUserFromToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		expected string
	}{
		{
			name:     "full token with prefix",
			token:    "PVEAPIToken=root@pam!csi=secret123",
			expected: "root@pam",
		},
		{
			name:     "token without prefix",
			token:    "user@pve!mytoken=abc",
			expected: "user@pve",
		},
		{
			name:     "no exclamation mark",
			token:    "root@pam",
			expected: "root@pam",
		},
		{
			name:     "empty string",
			token:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractUserFromToken(tt.token)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStore(t *testing.T) {
	store := NewStore()

	t.Run("get returns nil for unknown UPID", func(t *testing.T) {
		result := store.Get("UPID:unknown")
		assert.Nil(t, result)
	})

	t.Run("IsOurs returns false for unknown UPID", func(t *testing.T) {
		assert.False(t, store.IsOurs("UPID:unknown"))
	})

	t.Run("put and get", func(t *testing.T) {
		tr := &TaskResult{
			UPID:       "UPID:pve1:00000001:00000001:00000001:imgcopy:vm-100-disk-0:root@pam:",
			Node:       "pve1",
			Status:     "stopped",
			ExitStatus: "OK",
			Type:       "imgcopy",
			User:       "root@pam",
			ID:         "vm-100-disk-0",
		}
		store.Put(tr)

		got := store.Get(tr.UPID)
		require.NotNil(t, got)
		assert.Equal(t, "pve1", got.Node)
		assert.Equal(t, "stopped", got.Status)
		assert.Equal(t, "OK", got.ExitStatus)
	})

	t.Run("IsOurs returns true for known UPID", func(t *testing.T) {
		assert.True(t, store.IsOurs("UPID:pve1:00000001:00000001:00000001:imgcopy:vm-100-disk-0:root@pam:"))
	})
}
