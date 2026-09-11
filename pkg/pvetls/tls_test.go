package pvetls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"github.com/stretchr/testify/require"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifiedTrustAndFailClosed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600))
	client := &http.Client{Transport: Transport(path)}
	resp, e := client.Get(srv.URL)
	require.NoError(t, e)
	resp.Body.Close()
	require.Equal(t, 204, resp.StatusCode)
	for _, ca := range []string{"", path + ".missing"} {
		_, e = (&http.Client{Transport: Transport(ca)}).Get(srv.URL)
		require.Error(t, e)
	}
	require.NoError(t, os.WriteFile(path, []byte("invalid PEM"), 0600))
	_, e = Config(path)
	require.Error(t, e)
}
func TestCertificateReload(t *testing.T) {
	dir := t.TempDir()
	cert, key := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	private, e := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)}), 0600))
	write := func(serial int64) {
		tpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "test"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
		der, e := x509.CreateCertificate(rand.Reader, tpl, tpl, &private.PublicKey, private)
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	}
	reload := ReloadCertificate(cert, key)
	write(1)
	a, e := reload(nil)
	require.NoError(t, e)
	write(2)
	b, e := reload(nil)
	require.NoError(t, e)
	require.NotEqual(t, a.Certificate, b.Certificate)
	require.NoError(t, os.Remove(key))
	_, e = reload(nil)
	require.Error(t, e)
}
