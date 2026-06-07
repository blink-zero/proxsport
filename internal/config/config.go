package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the runtime configuration loaded from environment variables.
type Config struct {
	// Proxmox connection
	ProxmoxURL         string
	TokenID            string // e.g. "monitoring@pve!proxsport"
	TokenSecret        string // the UUID secret half of the API token
	Username           string // fallback if no token (e.g. "root@pam")
	Password           string // fallback if no token
	InsecureSkipVerify bool

	// Exporter behaviour
	ListenAddr     string
	MetricsPath    string
	PollInterval   time.Duration
	RequestTimeout time.Duration

	// Selective collectors (default: all on)
	CollectNodes   bool
	CollectGuests  bool
	CollectStorage bool
	CollectCluster bool
}

// Load reads configuration from environment variables, applying defaults.
// Returns an error if any required field is missing or malformed.
func Load() (*Config, error) {
	c := &Config{
		ProxmoxURL:     getEnv("PROXMOX_URL", ""),
		TokenID:        getEnv("PROXMOX_TOKEN_ID", ""),
		TokenSecret:    getEnv("PROXMOX_TOKEN_SECRET", ""),
		Username:       getEnv("PROXMOX_USERNAME", ""),
		Password:       getEnv("PROXMOX_PASSWORD", ""),
		ListenAddr:     getEnv("PROXSPORT_LISTEN", ":9221"),
		MetricsPath:    getEnv("PROXSPORT_METRICS_PATH", "/metrics"),
		CollectNodes:   getEnvBool("PROXSPORT_COLLECT_NODES", true),
		CollectGuests:  getEnvBool("PROXSPORT_COLLECT_GUESTS", true),
		CollectStorage: getEnvBool("PROXSPORT_COLLECT_STORAGE", true),
		CollectCluster: getEnvBool("PROXSPORT_COLLECT_CLUSTER", true),
	}

	c.InsecureSkipVerify = getEnvBool("PROXMOX_INSECURE_SKIP_VERIFY", false)

	interval, err := time.ParseDuration(getEnv("PROXSPORT_POLL_INTERVAL", "30s"))
	if err != nil {
		return nil, fmt.Errorf("invalid PROXSPORT_POLL_INTERVAL: %w", err)
	}
	c.PollInterval = interval

	timeout, err := time.ParseDuration(getEnv("PROXSPORT_REQUEST_TIMEOUT", "15s"))
	if err != nil {
		return nil, fmt.Errorf("invalid PROXSPORT_REQUEST_TIMEOUT: %w", err)
	}
	c.RequestTimeout = timeout

	if c.ProxmoxURL == "" {
		return nil, fmt.Errorf("PROXMOX_URL is required (e.g. https://pve.example.com:8006)")
	}
	c.ProxmoxURL = strings.TrimRight(c.ProxmoxURL, "/")

	hasToken := c.TokenID != "" && c.TokenSecret != ""
	hasPassword := c.Username != "" && c.Password != ""
	if !hasToken && !hasPassword {
		return nil, fmt.Errorf("authentication required: set PROXMOX_TOKEN_ID + PROXMOX_TOKEN_SECRET (preferred) or PROXMOX_USERNAME + PROXMOX_PASSWORD")
	}

	return c, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}
