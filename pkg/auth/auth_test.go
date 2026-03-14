package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validToken = "PVEAPIToken=user@pam!mytoken=aaaa-bbbb-cccc"

// pvePermissionsHandler returns an http.HandlerFunc that serves PVE-style
// permissions JSON: {"data": ...}
func pvePermissionsHandler(perms map[string]map[string]int, callCount *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if callCount != nil {
			callCount.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"data": perms}
		json.NewEncoder(w).Encode(resp)
	}
}

// ---------------------------------------------------------------------------
// 1. Token parsing -- invalid format rejected
// ---------------------------------------------------------------------------

func TestAuthenticate_InvalidToken_NoPVEPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("PVE API should not be called for a malformed token")
	}))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), "BadToken=foo", "local-zfs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token format")
}

func TestAuthenticate_InvalidToken_MissingSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("PVE API should not be called for a malformed token")
	}))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), "PVEAPIToken=user@pam!tokenid", "local-zfs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token format")
}

// ---------------------------------------------------------------------------
// 2. Successful authentication with Datastore.Allocate permission
// ---------------------------------------------------------------------------

func TestAuthenticate_Success_RootPath(t *testing.T) {
	perms := map[string]map[string]int{
		"/": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, nil))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
}

func TestAuthenticate_Success_SpecificStoragePath(t *testing.T) {
	perms := map[string]map[string]int{
		"/storage/local-zfs": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, nil))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
}

func TestAuthenticate_Success_EmptyStorageWithRootPerm(t *testing.T) {
	perms := map[string]map[string]int{
		"/": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, nil))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// 3. Failed authentication without required permission
// ---------------------------------------------------------------------------

func TestAuthenticate_Fail_NoDatastoreAllocate(t *testing.T) {
	perms := map[string]map[string]int{
		"/": {"Sys.Audit": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, nil))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient permissions")
	assert.Contains(t, err.Error(), "local-zfs")
}

func TestAuthenticate_Fail_AllocateOnWrongStorage(t *testing.T) {
	perms := map[string]map[string]int{
		"/storage/local-zfs": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, nil))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "ceph-pool")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient permissions")
}

func TestAuthenticate_Fail_AllocateOnNonStoragePath(t *testing.T) {
	perms := map[string]map[string]int{
		"/vms/100": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, nil))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient permissions")
}

func TestAuthenticate_Fail_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "connection refused", http.StatusBadGateway)
	}))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PVE auth request failed")
}

func TestAuthenticate_Fail_ConnectionError(t *testing.T) {
	// Use a server that's immediately closed to simulate connection error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	a := NewWithClient(5*time.Second, url, &http.Client{}, time.Minute)
	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PVE auth request failed")
}

// ---------------------------------------------------------------------------
// 4. Cache hit -- second call does NOT invoke PVE API
// ---------------------------------------------------------------------------

func TestAuthenticate_CacheHit(t *testing.T) {
	var callCount atomic.Int32
	perms := map[string]map[string]int{
		"/": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, &callCount))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)

	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	err = a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load(), "second call should use cache")
}

func TestAuthenticate_CacheHit_DifferentStorageMisses(t *testing.T) {
	var callCount atomic.Int32
	perms := map[string]map[string]int{
		"/": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, &callCount))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)

	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	err = a.Authenticate(context.Background(), validToken, "ceph-pool")
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load(), "different storage should miss cache")
}

func TestAuthenticate_CacheHit_ErrorNotCached(t *testing.T) {
	var callCount atomic.Int32
	perms := map[string]map[string]int{
		"/": {"Sys.Audit": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, &callCount))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)

	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.Error(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	err = a.Authenticate(context.Background(), validToken, "local-zfs")
	require.Error(t, err)
	assert.Equal(t, int32(2), callCount.Load(), "failed auth should not be cached")
}

// ---------------------------------------------------------------------------
// 5. Cache expiry -- after TTL, PVE API is called again
// ---------------------------------------------------------------------------

func TestAuthenticate_CacheExpiry(t *testing.T) {
	var callCount atomic.Int32
	perms := map[string]map[string]int{
		"/": {"Datastore.Allocate": 1},
	}
	srv := httptest.NewServer(pvePermissionsHandler(perms, &callCount))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), 50*time.Millisecond)

	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	time.Sleep(100 * time.Millisecond)

	err = a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load(), "after TTL expiry PVE API should be called again")
}

// ---------------------------------------------------------------------------
// 6. Token is forwarded correctly as Authorization header
// ---------------------------------------------------------------------------

func TestAuthenticate_TokenForwarded(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"/":{"Datastore.Allocate":1}}}`)
	}))
	defer srv.Close()

	a := NewWithClient(5*time.Second, srv.URL, srv.Client(), time.Minute)
	err := a.Authenticate(context.Background(), validToken, "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, validToken, receivedAuth)
}
