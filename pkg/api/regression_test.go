package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/auth"
	"github.com/freshost/pve-snapshot-api/pkg/cluster"
	"github.com/freshost/pve-snapshot-api/pkg/config"
	"github.com/freshost/pve-snapshot-api/pkg/pool"
	"github.com/freshost/pve-snapshot-api/pkg/proxy"
	"github.com/freshost/pve-snapshot-api/pkg/task"
	"github.com/stretchr/testify/require"
)

const testToken = "PVEAPIToken=root@pam!csi=secret"

func regressionServer(t *testing.T, upstream string, runner pool.CommandRunner, cs *cluster.ClusterState, prx *proxy.Proxy) (http.Handler, *mockBackend, *task.Store) {
	t.Helper()
	a := newAuthServer()
	t.Cleanup(a.Close)
	cfg := &config.Config{ProxmoxAPIURL: upstream, PveshTimeout: time.Second}
	b := &mockBackend{}
	store := task.NewStore()
	return NewServer(b, auth.NewWithClient(time.Second, a.URL, a.Client(), 0), prx, cs, cfg, store, pool.New(time.Second, runner)), b, store
}
func TestNonZFSBodyAndVolumePassThrough(t *testing.T) {
	for _, ct := range []string{"application/json", "application/x-www-form-urlencoded"} {
		for _, volume := range []string{"vm-100-disk-0", "100/vm-100-disk-0.raw", "local:100%2Fvm-100-disk-0.raw"} {
			t.Run(ct+volume, func(t *testing.T) {
				expected := "target=100%2Fvm-200-disk-0.raw"
				if ct == "application/json" {
					expected = `{"target":"100/vm-200-disk-0.raw"}`
				}
				var got string
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, e := io.ReadAll(r.Body)
					require.NoError(t, e)
					got = string(b)
					w.WriteHeader(201)
				}))
				defer up.Close()
				h, b, _ := regressionServer(t, up.URL, func(context.Context, string, ...string) ([]byte, error) { return []byte(`{"type":"dir"}`), nil }, nil, nil)
				r := httptest.NewRequest("POST", "/api2/json/nodes/pve1/storage/local/content/"+volume, strings.NewReader(expected))
				r.Header.Set("Content-Type", ct)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				require.Equal(t, 201, w.Code)
				require.Equal(t, expected, got)
				require.Empty(t, b.clonedSnapshots)
				r = httptest.NewRequest("DELETE", "/api2/json/nodes/pve1/storage/local/content/"+volume, nil)
				w = httptest.NewRecorder()
				h.ServeHTTP(w, r)
				require.Equal(t, 201, w.Code)
				require.Empty(t, b.destroyedVols)
			})
		}
	}
}
func TestRejectUnsafeCopyAndDelete(t *testing.T) {
	for _, body := range []string{`{"target":"other:vm-200-disk-0"}`, `{"target":"vm-200-disk-0","target_node":"pve2"}`, `{"target":"archive"}`, `{"target":"vm-100-disk-0"}`, `{"target":"vm-200-disk-0","unknown":true}`} {
		h, b := newTestServer(t, nil)
		r := httptest.NewRequest("POST", "/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", testToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
		require.Empty(t, b.clonedSnapshots)
	}
	for _, volume := range []string{"archive", "subvol-100-disk-0", "other:vm-100-disk-0"} {
		h, b := newTestServer(t, nil)
		r := httptest.NewRequest("DELETE", "/api2/json/nodes/pve1/storage/local-zfs/content/"+volume, nil)
		r.Header.Set("Authorization", testToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
		require.Empty(t, b.destroyedVols)
	}
}
func TestBodyLimitAndAuthFirst(t *testing.T) {
	h, b := newTestServer(t, nil)
	for _, token := range []string{"", testToken} {
		r := httptest.NewRequest("POST", "/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0", strings.NewReader(`{"target":"`+strings.Repeat("a", maxCopyBody*2)+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if token == "" {
			require.Equal(t, 401, w.Code)
		} else {
			require.Equal(t, 413, w.Code)
		}
	}
	require.Empty(t, b.clonedSnapshots)
}
func TestCrossNodeCopyAndStatus(t *testing.T) {
	for _, ct := range []string{"application/json", "application/x-www-form-urlencoded"} {
		t.Run(ct, func(t *testing.T) {
			dest, b, _ := regressionServer(t, "http://127.0.0.1:1", poolRunner, nil, nil)
			remote := httptest.NewServer(dest)
			defer remote.Close()
			u, _ := url.Parse(remote.URL)
			host, port, _ := net.SplitHostPort(u.Host)
			p, _ := strconv.Atoi(port)
			cs := cluster.New(time.Second, func(context.Context, string, ...string) ([]byte, error) {
				return []byte(fmt.Sprintf(`[{"type":"node","name":"pve2","ip":%q}]`, host)), nil
			})
			require.NoError(t, cs.Discover(context.Background()))
			h, local, _ := regressionServer(t, "http://127.0.0.1:1", poolRunner, cs, proxy.New(cs, p, false))
			body := `{"target":"vm-200-disk-0"}`
			if ct != "application/json" {
				body = "target=vm-200-disk-0"
			}
			r := httptest.NewRequest("POST", "/api2/json/nodes/pve2/storage/local-zfs/content/vm-100-disk-0", strings.NewReader(body))
			r.Header.Set("Content-Type", ct)
			r.Header.Set("Authorization", testToken)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Len(t, b.clonedSnapshots, 1)
			require.Empty(t, local.clonedSnapshots)
			var resp struct{ Data string }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			r = httptest.NewRequest("GET", "/api2/json/nodes/pve2/tasks/"+resp.Data+"/status", nil)
			r.Header.Set("Authorization", testToken)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), `"exitstatus":"OK"`)
			// Spoofing the loop header can only reject the operation, never execute locally.
			r = httptest.NewRequest("DELETE", "/api2/json/nodes/pve2/storage/local-zfs/content/vm-100-disk-0", nil)
			r.Header.Set("Authorization", testToken)
			r.Header.Set("X-Forwarded-Node", "anything")
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, 508, w.Code)
			require.Empty(t, local.destroyedVols)
			require.Empty(t, b.destroyedVols)
		})
	}
}
func TestTaskStatusAuthNodeAndRestart(t *testing.T) {
	h, _, store := regressionServer(t, "http://127.0.0.1:1", poolRunner, nil, nil)
	id := task.GenerateUPID("pve1", "imgcopy", "vm-100-disk-0", "root@pam!csi")
	require.NoError(t, store.Put(&task.TaskResult{UPID: id, Node: "pve1", Status: "stopped", User: "root@pam!csi"}))
	for _, tc := range []struct {
		node, token string
		status      int
	}{{"pve1", "", 401}, {"pve1", testToken, 200}, {"pve1", "PVEAPIToken=root@pam!other=secret", 403}, {"pve2", testToken, 400}} {
		r := httptest.NewRequest("GET", "/api2/json/nodes/"+tc.node+"/tasks/"+id+"/status", nil)
		r.Header.Set("Authorization", tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, tc.status, w.Code)
	}
}
func TestStorageChangeBeforeMutation(t *testing.T) {
	calls := 0
	h, b, _ := regressionServer(t, "http://127.0.0.1:1", func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return []byte(fmt.Sprintf(`{"type":"zfspool","pool":"pool%d/data"}`, calls)), nil
	}, nil, nil)
	r := httptest.NewRequest("DELETE", "/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0", nil)
	r.Header.Set("Authorization", testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 409, w.Code)
	require.Empty(t, b.destroyedVols)
}
func TestWebsocketUpgradeThroughLogging(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, e := http.NewResponseController(w).Hijack()
		if e != nil {
			return
		}
		defer conn.Close()
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		data := make([]byte, 4)
		if _, e := io.ReadFull(rw, data); e == nil {
			rw.Write(data)
			rw.Flush()
		}
	}))
	defer up.Close()
	h, _, _ := regressionServer(t, up.URL, poolRunner, nil, nil)
	front := httptest.NewServer(h)
	defer front.Close()
	u, _ := url.Parse(front.URL)
	conn, e := net.DialTimeout("tcp", u.Host, time.Second)
	require.NoError(t, e)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "GET /api2/json/nodes/pve1/qemu/100/vncwebsocket HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", u.Host)
	reader := bufio.NewReader(conn)
	response, e := http.ReadResponse(reader, nil)
	require.NoError(t, e)
	require.Equal(t, 101, response.StatusCode)
	_, e = conn.Write([]byte("ping"))
	require.NoError(t, e)
	data := make([]byte, 4)
	_, e = io.ReadFull(reader, data)
	require.NoError(t, e)
	require.Equal(t, "ping", string(data))
}

type disconnectBackend struct {
	*mockBackend
	started      chan struct{}
	proceed      chan struct{}
	contextError error
}

func (b *disconnectBackend) CopyVolume(ctx context.Context, source, target string) error {
	close(b.started)
	<-b.proceed
	b.contextError = ctx.Err()
	return b.mockBackend.CopyVolume(ctx, source, target)
}
func TestAcceptedMutationSurvivesDisconnect(t *testing.T) {
	a := newAuthServer()
	defer a.Close()
	b := &disconnectBackend{mockBackend: &mockBackend{}, started: make(chan struct{}), proceed: make(chan struct{})}
	cfg := &config.Config{ProxmoxAPIURL: "http://127.0.0.1:1"}
	store := task.NewStore()
	h := NewServer(b, auth.NewWithClient(time.Second, a.URL, a.Client(), 0), nil, nil, cfg, store, pool.New(time.Second, poolRunner))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0", strings.NewReader(`{"target":"vm-200-disk-0"}`)).WithContext(ctx)
	req.Header.Set("Authorization", testToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(w, req) }()
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not start")
	}
	cancel()
	close(b.proceed)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
	}
	require.NoError(t, b.contextError)
	require.Equal(t, 200, w.Code)
	var result struct{ Data string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "OK", store.Get(result.Data).ExitStatus)
}
