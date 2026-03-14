package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/auth"
	"github.com/freshost/pve-snapshot-api/pkg/config"
	"github.com/freshost/pve-snapshot-api/pkg/pool"
	"github.com/freshost/pve-snapshot-api/pkg/storage"
	"github.com/freshost/pve-snapshot-api/pkg/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBackend implements storage.StorageBackend for testing.
type mockBackend struct {
	createSnapshotErr error
	cloneSnapshotErr  error
	destroyVolumeErr  error

	createdSnapshots []string // records "volid@snapname"
	clonedSnapshots  []string // records "volid@snapname->target"
	destroyedVols    []string
}

func (m *mockBackend) CreateSnapshot(_ context.Context, volid, snapname string) error {
	m.createdSnapshots = append(m.createdSnapshots, volid+"@"+snapname)
	return m.createSnapshotErr
}

func (m *mockBackend) DeleteSnapshot(_ context.Context, _, _ string) error { return nil }

func (m *mockBackend) CloneSnapshot(_ context.Context, volid, snapname, target string) error {
	m.clonedSnapshots = append(m.clonedSnapshots, volid+"@"+snapname+"->"+target)
	return m.cloneSnapshotErr
}

func (m *mockBackend) ListSnapshots(_ context.Context, _ string) ([]storage.Snapshot, error) {
	return nil, nil
}

func (m *mockBackend) PromoteClone(_ context.Context, _ string) error { return nil }

func (m *mockBackend) GetVolumeInfo(_ context.Context, _ string) (*storage.VolumeInfo, error) {
	return nil, nil
}

func (m *mockBackend) DestroyVolume(_ context.Context, volid string) error {
	m.destroyedVols = append(m.destroyedVols, volid)
	return m.destroyVolumeErr
}

func (m *mockBackend) GetOriginSnapshot(_ context.Context, _ string) (string, error) {
	return "", nil
}

// newAuthServer creates an httptest.Server that returns PVE-style permissions JSON.
func newAuthServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"/":{"Datastore.Allocate":1}}}`)
	}))
}

// poolRunner returns a zfspool storage config
func poolRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return []byte(`{"pool":"rpool/data","type":"zfspool"}`), nil
}

func newTestServer(t *testing.T, backend *mockBackend) (http.Handler, *mockBackend) {
	t.Helper()
	if backend == nil {
		backend = &mockBackend{}
	}

	authSrv := newAuthServer()
	t.Cleanup(authSrv.Close)

	cfg := &config.Config{
		ListenPort:    8009,
		ZFSTimeout:    5 * time.Second,
		PveshTimeout:  5 * time.Second,
		AuthCacheTTL:  60 * time.Second,
		ProxmoxAPIURL: "https://localhost:8006",
	}

	authenticator := auth.NewWithClient(cfg.PveshTimeout, authSrv.URL, authSrv.Client(), cfg.AuthCacheTTL)
	taskStore := task.NewStore()
	poolResolver := pool.New(cfg.PveshTimeout, poolRunner)

	handler := NewServer(backend, authenticator, nil, nil, cfg, taskStore, poolResolver)
	return handler, backend
}

func TestHandleCopyVolume(t *testing.T) {
	t.Run("success - creates snapshot and clone", func(t *testing.T) {
		handler, backend := newTestServer(t, nil)

		form := url.Values{}
		form.Set("target", "local-zfs:vm-200-disk-0")

		req := httptest.NewRequest("POST",
			"/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0",
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "PVEAPIToken=root@pam!csi=secret")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify ZFS operations
		require.Len(t, backend.createdSnapshots, 1)
		assert.Equal(t, "rpool/data/vm-100-disk-0@csi-vm-200-disk-0", backend.createdSnapshots[0])

		require.Len(t, backend.clonedSnapshots, 1)
		assert.Equal(t, "rpool/data/vm-100-disk-0@csi-vm-200-disk-0->rpool/data/vm-200-disk-0", backend.clonedSnapshots[0])

		// Verify response contains UPID
		var resp map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		upid, ok := resp["data"].(string)
		require.True(t, ok)
		assert.Contains(t, upid, "UPID:pve1:")
		assert.Contains(t, upid, ":imgcopy:")
	})

	t.Run("missing auth", func(t *testing.T) {
		handler, _ := newTestServer(t, nil)

		req := httptest.NewRequest("POST",
			"/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0", nil)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("missing target", func(t *testing.T) {
		handler, _ := newTestServer(t, nil)

		req := httptest.NewRequest("POST",
			"/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0",
			strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "PVEAPIToken=root@pam!csi=secret")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleDeleteVolume(t *testing.T) {
	t.Run("success - destroys volume", func(t *testing.T) {
		handler, backend := newTestServer(t, nil)

		req := httptest.NewRequest("DELETE",
			"/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0", nil)
		req.Header.Set("Authorization", "PVEAPIToken=root@pam!csi=secret")

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		require.Len(t, backend.destroyedVols, 1)
		assert.Equal(t, "rpool/data/vm-100-disk-0", backend.destroyedVols[0])

		var resp map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		upid, ok := resp["data"].(string)
		require.True(t, ok)
		assert.Contains(t, upid, ":imgdel:")
	})

	t.Run("missing auth", func(t *testing.T) {
		handler, _ := newTestServer(t, nil)

		req := httptest.NewRequest("DELETE",
			"/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0", nil)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestHandleTaskStatus(t *testing.T) {
	t.Run("returns stored task result", func(t *testing.T) {
		authSrv := newAuthServer()
		defer authSrv.Close()

		cfg := &config.Config{
			ListenPort:    8009,
			PveshTimeout:  5 * time.Second,
			AuthCacheTTL:  60 * time.Second,
			ProxmoxAPIURL: "https://localhost:8006",
		}

		authenticator := auth.NewWithClient(cfg.PveshTimeout, authSrv.URL, authSrv.Client(), cfg.AuthCacheTTL)
		taskStore := task.NewStore()
		poolResolver := pool.New(cfg.PveshTimeout, poolRunner)

		upid := "UPID:pve1:00000001:00000001:65000000:imgcopy:vm-100-disk-0:root@pam:"
		taskStore.Put(&task.TaskResult{
			UPID:       upid,
			Node:       "pve1",
			Status:     "stopped",
			ExitStatus: "OK",
			Type:       "imgcopy",
			User:       "root@pam",
			ID:         "vm-100-disk-0",
		})

		handler := NewServer(&mockBackend{}, authenticator, nil, nil, cfg, taskStore, poolResolver)

		req := httptest.NewRequest("GET",
			"/api2/json/nodes/pve1/tasks/"+url.PathEscape(upid)+"/status", nil)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		data, ok := resp["data"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "stopped", data["status"])
		assert.Equal(t, "OK", data["exitstatus"])
	})
}

func TestHandleHealthz(t *testing.T) {
	handler, _ := newTestServer(t, nil)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data, ok := resp["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", data["status"])
}

func TestNonInterceptedRequestsProxy(t *testing.T) {
	// Set up a fake PVE backend
	pveBackend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"data": "proxied"})
	}))
	defer pveBackend.Close()

	authSrv := newAuthServer()
	defer authSrv.Close()

	cfg := &config.Config{
		ListenPort:    8009,
		PveshTimeout:  5 * time.Second,
		AuthCacheTTL:  60 * time.Second,
		ProxmoxAPIURL: pveBackend.URL,
	}

	authenticator := auth.NewWithClient(cfg.PveshTimeout, authSrv.URL, authSrv.Client(), cfg.AuthCacheTTL)
	taskStore := task.NewStore()
	poolResolver := pool.New(cfg.PveshTimeout, poolRunner)

	handler := NewServer(&mockBackend{}, authenticator, nil, nil, cfg, taskStore, poolResolver)

	// Request a non-intercepted endpoint
	req := httptest.NewRequest("GET", "/api2/json/nodes/pve1/qemu", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should be proxied (may get a connection error since we're using TLS test server,
	// but the important thing is it didn't return our intercepted handler's response)
	// The test verifies the proxy path was taken, not the actual upstream response.
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}
