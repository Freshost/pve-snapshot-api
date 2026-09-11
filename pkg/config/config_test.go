package config

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("ca_file: ''\nauth_cache_ttl: 0s\n"), 0600))
	c, e := Load(path)
	require.NoError(t, e)
	require.Equal(t, "", c.CAFile)
	require.Equal(t, time.Duration(0), c.AuthCacheTTL)
	require.Equal(t, 8009, c.ListenPort)
	require.NotEmpty(t, c.TaskStateFile)
}
func TestRejectInvalidConfiguration(t *testing.T) {
	for _, body := range []string{"listen_port: -1", "listen_port: 65536", "zfs_timeout: 0s", "pvesh_timeout: -1s", "auth_cache_ttl: -1s", "unknown: true", "proxmox_api_url: 'https://localhost/api2/json'", "proxmox_api_url: 'http://user:pass@localhost'", "tls: {cert_file: /cert}", "task_state_file: relative", "log_level: trace"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(body), 0600))
		_, e := Load(path)
		require.Error(t, e, body)
	}
}
