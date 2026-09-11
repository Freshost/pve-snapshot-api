package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEffectiveStorageACL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		perms   map[string]map[string]int
		allowed bool
	}{
		{"root cannot override storage denial", map[string]map[string]int{"/": {"Datastore.Allocate": 1}, "/storage/local-zfs": {"Datastore.Audit": 1}}, false},
		{"nonpropagating exact grant", map[string]map[string]int{"/storage/local-zfs": {"Datastore.Allocate": 0}}, true},
		{"empty scope denies", map[string]map[string]int{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/storage/local-zfs", r.URL.Query().Get("path"))
				json.NewEncoder(w).Encode(map[string]any{"data": tc.perms})
			}))
			defer srv.Close()
			a := NewWithClient(time.Second, srv.URL, srv.Client(), 0)
			err := a.Authenticate(context.Background(), validToken, "local-zfs")
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
func TestTaskAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, token, owner    string
		audit, valid, allowed bool
	}{
		{"owner", validToken, "user@pam!mytoken", false, true, true},
		{"another token same user", validToken, "user@pam!other", false, true, false},
		{"forged owner", validToken, "user@pam!mytoken", false, false, false},
		{"auditor", validToken, "someone@pve!token", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/nodes/pve1", r.URL.Query().Get("path"))
				if !tc.valid {
					w.WriteHeader(401)
					return
				}
				p := map[string]int{}
				if tc.audit {
					p["Sys.Audit"] = 0
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"/nodes/pve1": p}})
			}))
			defer srv.Close()
			a := NewWithClient(time.Second, srv.URL, srv.Client(), 0)
			err := a.AuthorizeTask(context.Background(), tc.token, tc.owner, "pve1")
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCacheCapacityAndIsolation(t *testing.T) {
	c := NewCache(time.Minute)
	for i := 0; i < 5000; i++ {
		c.Set(fmt.Sprint(i), "/storage/a", nil)
	}
	require.Len(t, c.entries, 4096)
	_, ok := c.Get("1", "/storage/b")
	require.False(t, ok)
	disabled := NewCache(0)
	disabled.Set("token", "path", nil)
	_, ok = disabled.Get("token", "path")
	require.False(t, ok)
}
