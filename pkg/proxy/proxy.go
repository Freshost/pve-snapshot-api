package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"

	"github.com/freshost/pve-snapshot-api/pkg/cluster"
	"github.com/freshost/pve-snapshot-api/pkg/pvetls"
)

type Proxy struct {
	cluster    *cluster.ClusterState
	listenPort int
	useTLS     bool
	transport  http.RoundTripper
}

func New(cs *cluster.ClusterState, port int, useTLS bool, caPaths ...string) *Proxy {
	ca := "/etc/pve/pve-root-ca.pem"
	if len(caPaths) > 0 {
		ca = caPaths[0]
	}
	var tr http.RoundTripper = http.DefaultTransport
	if useTLS {
		tr = pvetls.Transport(ca)
	}
	return &Proxy{cluster: cs, listenPort: port, useTLS: useTLS, transport: tr}
}
func (p *Proxy) ShouldProxy(_ *http.Request, node string) bool {
	return node != "" && !p.cluster.IsLocal(node)
}
func (p *Proxy) Forward(w http.ResponseWriter, r *http.Request, node string) {
	// A forwarded request arriving at a different node must fail, never run locally.
	if r.Header.Get("X-Forwarded-Node") != "" {
		http.Error(w, "cluster routing loop or wrong destination", http.StatusLoopDetected)
		return
	}
	ip, err := p.cluster.GetNodeIP(node)
	if err != nil {
		http.Error(w, fmt.Sprint(err), http.StatusBadGateway)
		return
	}
	scheme := "http"
	if p.useTLS {
		scheme = "https"
	}
	target := &url.URL{Scheme: scheme, Host: net.JoinHostPort(ip, strconv.Itoa(p.listenPort))}
	prx := httputil.NewSingleHostReverseProxy(target)
	director := prx.Director
	prx.Director = func(req *http.Request) { director(req); req.Header.Set("X-Forwarded-Node", node) }
	prx.Transport = p.transport
	prx.ServeHTTP(w, r)
}
