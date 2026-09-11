// Package pvetls provides verified TLS using system roots plus the PVE CA.
package pvetls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

func Config(caPath string) (*tls.Config, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA file contains no certificates")
		}
	}
	return &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
}

// Transport fails closed when the explicitly configured CA cannot be loaded.
func Transport(caPath string) http.RoundTripper {
	cfg, err := Config(caPath)
	if err != nil {
		return failedTransport{err}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	tr.ResponseHeaderTimeout = 6 * time.Minute
	return tr
}

type failedTransport struct{ err error }

func (t failedTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

// ReloadCertificate reads the current key pair for every new TLS connection.
func ReloadCertificate(cert, key string) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		pair, err := tls.LoadX509KeyPair(cert, key)
		return &pair, err
	}
}
