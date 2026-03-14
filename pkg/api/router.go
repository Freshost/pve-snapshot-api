package api

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/freshost/pve-snapshot-api/pkg/auth"
	"github.com/freshost/pve-snapshot-api/pkg/cluster"
	"github.com/freshost/pve-snapshot-api/pkg/config"
	"github.com/freshost/pve-snapshot-api/pkg/pool"
	"github.com/freshost/pve-snapshot-api/pkg/proxy"
	"github.com/freshost/pve-snapshot-api/pkg/storage"
	"github.com/freshost/pve-snapshot-api/pkg/task"
)

const pveCAPath = "/etc/pve/pve-root-ca.pem"

// Server holds all dependencies for the API reverse proxy.
type Server struct {
	backend      storage.StorageBackend
	auth         *auth.Authenticator
	proxy        *proxy.Proxy
	cluster      *cluster.ClusterState
	config       *config.Config
	taskStore    *task.Store
	poolResolver *pool.Resolver
	pveProxy     *httputil.ReverseProxy
}

// NewServer creates the HTTP handler with intercepted routes and a PVE reverse proxy fallback.
func NewServer(
	backend storage.StorageBackend,
	authenticator *auth.Authenticator,
	prx *proxy.Proxy,
	cs *cluster.ClusterState,
	cfg *config.Config,
	taskStore *task.Store,
	poolResolver *pool.Resolver,
) http.Handler {
	target, _ := url.Parse(cfg.ProxmoxAPIURL)

	pveProxy := httputil.NewSingleHostReverseProxy(target)
	// Trust PVE CA for the upstream connection
	tlsConfig := &tls.Config{}
	if caCert, err := os.ReadFile(pveCAPath); err == nil {
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = certPool
	} else {
		tlsConfig.InsecureSkipVerify = true
	}
	pveProxy.Transport = &http.Transport{TLSClientConfig: tlsConfig}

	s := &Server{
		backend:      backend,
		auth:         authenticator,
		proxy:        prx,
		cluster:      cs,
		config:       cfg,
		taskStore:    taskStore,
		poolResolver: poolResolver,
		pveProxy:     pveProxy,
	}

	mux := http.NewServeMux()

	// Intercepted Proxmox-compatible routes
	mux.HandleFunc("POST /api2/json/nodes/{node}/storage/{storage}/content/{volume}", s.handleCopyVolume)
	mux.HandleFunc("DELETE /api2/json/nodes/{node}/storage/{storage}/content/{disk}", s.handleDeleteVolume)
	mux.HandleFunc("GET /api2/json/nodes/{node}/tasks/{upid}/status", s.handleTaskStatus)

	// Health check
	mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Catch-all: proxy to Proxmox
	mux.HandleFunc("/", s.handleProxy)

	var handler http.Handler = mux
	handler = s.recoveryMiddleware(handler)
	handler = s.loggingMiddleware(handler)

	return handler
}
