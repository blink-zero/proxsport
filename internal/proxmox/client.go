package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a thin Proxmox VE HTTP API client. Supports API-token and
// username/password auth. Read-only operations only — proxsport never
// changes anything on the cluster.
type Client struct {
	baseURL string
	http    *http.Client

	// Auth (one of the two pairs is set)
	tokenID     string // e.g. "monitoring@pve!proxsport"
	tokenSecret string

	username string
	password string

	ticket    string // PVEAuthCookie value after ticket auth
	csrfToken string // for ticket auth (we don't need CSRF for GETs but kept for future)
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets API-token auth. tokenID format: "user@realm!tokenname".
func WithToken(tokenID, tokenSecret string) Option {
	return func(c *Client) {
		c.tokenID = tokenID
		c.tokenSecret = tokenSecret
	}
}

// WithPassword sets username/password auth. username must include realm
// (e.g. "root@pam").
func WithPassword(username, password string) Option {
	return func(c *Client) {
		c.username = username
		c.password = password
	}
}

// WithInsecureSkipVerify disables TLS certificate verification. Useful for
// self-signed Proxmox installs.
func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Client) {
		if t, ok := c.http.Transport.(*http.Transport); ok && skip {
			t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
	}
}

// WithTimeout sets the per-request HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.http.Timeout = d
	}
}

// New constructs a Client for the given Proxmox base URL (e.g.
// "https://pve.example.com:8006").
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{}},
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Login obtains a ticket when using username/password auth. No-op for
// token auth. Call once before issuing requests; ticket lifetime is two
// hours per Proxmox docs — call again after that.
func (c *Client) Login(ctx context.Context) error {
	if c.tokenID != "" {
		return nil
	}
	if c.username == "" || c.password == "" {
		return fmt.Errorf("no credentials configured")
	}

	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api2/json/access/ticket", nil)
	if err != nil {
		return fmt.Errorf("build login request: %w", err)
	}
	req.URL.RawQuery = form.Encode()
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("login failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var wrapped struct {
		Data struct {
			Ticket              string `json:"ticket"`
			CSRFPreventionToken string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	c.ticket = wrapped.Data.Ticket
	c.csrfToken = wrapped.Data.CSRFPreventionToken
	return nil
}

// get issues a GET against a Proxmox API path and decodes the "data"
// field of the response envelope into v.
func (c *Client) get(ctx context.Context, path string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api2/json"+path, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", path, err)
	}

	if c.tokenID != "" {
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", c.tokenID, c.tokenSecret))
	} else if c.ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: c.ticket})
	} else {
		return fmt.Errorf("client not authenticated (call Login first or configure a token)")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}

	wrapper := struct {
		Data json.RawMessage `json:"data"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return fmt.Errorf("decode envelope for %s: %w", path, err)
	}
	if len(wrapper.Data) == 0 || string(wrapper.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(wrapper.Data, v); err != nil {
		return fmt.Errorf("decode data for %s: %w", path, err)
	}
	return nil
}

// ClusterResources fetches /cluster/resources — the consolidated view of
// every node, VM, container, and storage pool in the cluster. This is
// the workhorse endpoint for an exporter: one call returns enough data
// to render most metrics without hammering per-node endpoints.
func (c *Client) ClusterResources(ctx context.Context) ([]Resource, error) {
	var out []Resource
	if err := c.get(ctx, "/cluster/resources", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ClusterStatus fetches /cluster/status — the cluster's quorum and node
// membership state.
func (c *Client) ClusterStatus(ctx context.Context) ([]ClusterStatusEntry, error) {
	var out []ClusterStatusEntry
	if err := c.get(ctx, "/cluster/status", &out); err != nil {
		return nil, err
	}
	return out, nil
}
