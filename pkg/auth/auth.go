package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const pveCAPath = "/etc/pve/pve-root-ca.pem"

// Authenticator validates PVE API tokens via the Proxmox HTTP API.
type Authenticator struct {
	timeout time.Duration
	apiURL  string
	client  *http.Client
	cache   *AuthCache
}

// New creates an Authenticator that validates tokens against the PVE API at pveAPIURL.
func New(timeout time.Duration, pveAPIURL string, cacheTTL time.Duration) *Authenticator {
	tlsConfig := &tls.Config{}
	if caCert, err := os.ReadFile(pveCAPath); err == nil {
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = certPool
	} else {
		tlsConfig.InsecureSkipVerify = true
	}

	return &Authenticator{
		timeout: timeout,
		apiURL:  strings.TrimRight(pveAPIURL, "/"),
		client: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
		cache: NewCache(cacheTTL),
	}
}

// NewWithClient creates an Authenticator with a custom http.Client (for testing).
func NewWithClient(timeout time.Duration, pveAPIURL string, client *http.Client, cacheTTL time.Duration) *Authenticator {
	return &Authenticator{
		timeout: timeout,
		apiURL:  strings.TrimRight(pveAPIURL, "/"),
		client:  client,
		cache:   NewCache(cacheTTL),
	}
}

// Authenticate validates a PVE API token has Datastore.Allocate permission
// on the given storage. storageID is the PVE storage name (e.g. "local-zfs").
// If storageID is empty, only root-level permission is accepted.
// Token format: PVEAPIToken=user@realm!tokenid=secret
func (a *Authenticator) Authenticate(ctx context.Context, token, storageID string) error {
	cacheKey := token + ":" + storageID
	if err, ok := a.cache.Get(cacheKey, storageID); ok {
		return err
	}

	err := a.authenticate(ctx, token, storageID)
	if err == nil {
		a.cache.Set(cacheKey, storageID, nil)
	}
	return err
}

func (a *Authenticator) authenticate(ctx context.Context, token, storageID string) error {
	if !strings.HasPrefix(token, "PVEAPIToken=") {
		return fmt.Errorf("invalid token format: must start with PVEAPIToken=")
	}

	parts := strings.SplitN(token, "=", 3)
	if len(parts) < 3 {
		return fmt.Errorf("invalid token format")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", a.apiURL+"/api2/json/access/permissions", nil)
	if err != nil {
		return fmt.Errorf("creating auth request: %w", err)
	}
	req.Header.Set("Authorization", token)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("PVE auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading auth response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PVE auth request failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Data map[string]map[string]int `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return fmt.Errorf("parsing permissions: %w", err)
	}

	perms := wrapper.Data

	// Root-level permission covers everything
	if privs, ok := perms["/"]; ok {
		if privs["Datastore.Allocate"] == 1 {
			return nil
		}
	}

	// Check specific storage path: /storage/{storageID}
	if storageID != "" {
		storagePath := "/storage/" + storageID
		if privs, ok := perms[storagePath]; ok {
			if privs["Datastore.Allocate"] == 1 {
				return nil
			}
		}
	}

	if storageID != "" {
		return fmt.Errorf("insufficient permissions: Datastore.Allocate required on /storage/%s", storageID)
	}
	return fmt.Errorf("insufficient permissions: Datastore.Allocate required")
}
