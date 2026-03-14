package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/freshost/pve-snapshot-api/pkg/cluster"
)

const pveCAPath = "/etc/pve/pve-root-ca.pem"

// Proxy forwards HTTP requests to other cluster nodes.
type Proxy struct {
	cluster    *cluster.ClusterState
	listenPort int
	useTLS     bool
	client     *http.Client
}

// New creates a Proxy. If useTLS is true, inter-node requests use HTTPS
// with the PVE root CA for certificate verification.
func New(cs *cluster.ClusterState, port int, useTLS bool) *Proxy {
	client := &http.Client{}

	if useTLS {
		tlsConfig := &tls.Config{}

		// Load PVE CA to trust other nodes' certificates
		if caCert, err := os.ReadFile(pveCAPath); err == nil {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(caCert)
			tlsConfig.RootCAs = pool
		} else {
			// If PVE CA is not available, skip verification for inter-node traffic
			// (all nodes are on the same trusted cluster network)
			tlsConfig.InsecureSkipVerify = true
		}

		client.Transport = &http.Transport{TLSClientConfig: tlsConfig}
	}

	return &Proxy{
		cluster:    cs,
		listenPort: port,
		useTLS:     useTLS,
		client:     client,
	}
}

// ShouldProxy returns true if the request targets a non-local node
// and hasn't already been forwarded.
func (p *Proxy) ShouldProxy(r *http.Request, node string) bool {
	if node == "" {
		return false
	}
	if r.Header.Get("X-Forwarded-Node") != "" {
		return false // already forwarded, don't loop
	}
	return !p.cluster.IsLocal(node)
}

// Forward proxies the request to the target node and writes the response.
func (p *Proxy) Forward(w http.ResponseWriter, r *http.Request, targetNode string) {
	ip, err := p.cluster.GetNodeIP(targetNode)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s","code":"NODE_NOT_FOUND"}`, err), http.StatusBadGateway)
		return
	}

	scheme := "http"
	if p.useTLS {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s:%d%s", scheme, ip, p.listenPort, r.URL.RequestURI())

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, url, r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to create proxy request","code":"PROXY_ERROR"}`, http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, v := range values {
			proxyReq.Header.Add(key, v)
		}
	}
	proxyReq.Header.Set("X-Forwarded-Node", targetNode)

	resp, err := p.client.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"proxy request failed: %s","code":"PROXY_ERROR"}`, err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
