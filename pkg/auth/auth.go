package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/pvetls"
)

type Authenticator struct {
	timeout time.Duration
	apiURL  string
	client  *http.Client
	cache   *AuthCache
}

func New(timeout time.Duration, apiURL string, ttl time.Duration, caPaths ...string) *Authenticator {
	ca := "/etc/pve/pve-root-ca.pem"
	if len(caPaths) > 0 {
		ca = caPaths[0]
	}
	return NewWithClient(timeout, apiURL, &http.Client{Timeout: timeout, Transport: pvetls.Transport(ca), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, ttl)
}
func NewWithClient(timeout time.Duration, apiURL string, client *http.Client, ttl time.Duration) *Authenticator {
	return &Authenticator{timeout: timeout, apiURL: strings.TrimRight(apiURL, "/"), client: client, cache: NewCache(ttl)}
}

// Identity preserves the full token ID; separate tokens are separate principals.
func Identity(token string) (string, error) {
	if !strings.HasPrefix(token, "PVEAPIToken=") {
		return "", fmt.Errorf("invalid token format")
	}
	id, secret, ok := strings.Cut(strings.TrimPrefix(token, "PVEAPIToken="), "=")
	user, tokenID, hasToken := strings.Cut(id, "!")
	if !ok || secret == "" || !hasToken || tokenID == "" || !strings.Contains(user, "@") || strings.ContainsAny(id, " :\r\n\t=") {
		return "", fmt.Errorf("invalid token format")
	}
	return id, nil
}

func (a *Authenticator) permissions(ctx context.Context, token, path string) (map[string]int, error) {
	if _, err := Identity(token); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", a.apiURL+"/api2/json/access/permissions?"+url.Values{"path": {path}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PVE auth request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PVE auth request failed: HTTP %d", resp.StatusCode)
	}
	var wrapper struct {
		Data map[string]map[string]int `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("parsing permissions: %w", err)
	}
	return wrapper.Data[path], nil
}
func (a *Authenticator) Authenticate(ctx context.Context, token, storageID string) error {
	path := "/"
	if storageID != "" {
		path = "/storage/" + storageID
	}
	if err, ok := a.cache.Get(token, path); ok {
		return err
	}
	perms, err := a.permissions(ctx, token, path)
	if err != nil {
		return err
	}
	if _, ok := perms["Datastore.Allocate"]; !ok {
		return fmt.Errorf("insufficient permissions: Datastore.Allocate required on %s", path)
	}
	a.cache.Set(token, path, nil)
	return nil
}

func (a *Authenticator) AuthorizeTask(ctx context.Context, token, owner, node string) error {
	id, err := Identity(token)
	if err != nil {
		return err
	}
	// Always validate the token, even when the supplied ID matches the owner.
	perms, err := a.permissions(ctx, token, "/nodes/"+node)
	if err != nil {
		return err
	}
	if id == owner {
		return nil
	}
	if _, ok := perms["Sys.Audit"]; ok {
		return nil
	}
	return fmt.Errorf("insufficient permissions: task owner or Sys.Audit required")
}
