package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/pve-snapshot-api/config.yaml"

type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type Config struct {
	ListenPort     int           `yaml:"listen_port"`
	ZFSTimeout     time.Duration `yaml:"zfs_timeout"`
	PveshTimeout   time.Duration `yaml:"pvesh_timeout"`
	AuthCacheTTL   time.Duration `yaml:"auth_cache_ttl"`
	TLS            *TLSConfig    `yaml:"tls"`
	LogLevel       string        `yaml:"log_level"`
	ProxmoxAPIURL  string        `yaml:"proxmox_api_url"`
}

type rawConfig struct {
	ListenPort     int        `yaml:"listen_port"`
	ZFSTimeout     string     `yaml:"zfs_timeout"`
	PveshTimeout   string     `yaml:"pvesh_timeout"`
	AuthCacheTTL   string     `yaml:"auth_cache_ttl"`
	TLS            *TLSConfig `yaml:"tls"`
	LogLevel       string     `yaml:"log_level"`
	ProxmoxAPIURL  string     `yaml:"proxmox_api_url"`
}

func defaults() *Config {
	return &Config{
		ListenPort:   8009,
		ZFSTimeout:   30 * time.Second,
		PveshTimeout: 15 * time.Second,
		AuthCacheTTL: 60 * time.Second,
		TLS: &TLSConfig{
			CertFile: "/etc/pve/local/pve-ssl.pem",
			KeyFile:  "/etc/pve/local/pve-ssl.key",
		},
		LogLevel:      "info",
		ProxmoxAPIURL: "https://localhost:8006",
	}
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if raw.ListenPort != 0 {
		cfg.ListenPort = raw.ListenPort
	}
	if raw.ZFSTimeout != "" {
		d, err := time.ParseDuration(raw.ZFSTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid zfs_timeout: %w", err)
		}
		cfg.ZFSTimeout = d
	}
	if raw.PveshTimeout != "" {
		d, err := time.ParseDuration(raw.PveshTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid pvesh_timeout: %w", err)
		}
		cfg.PveshTimeout = d
	}
	if raw.AuthCacheTTL != "" {
		d, err := time.ParseDuration(raw.AuthCacheTTL)
		if err != nil {
			return nil, fmt.Errorf("invalid auth_cache_ttl: %w", err)
		}
		cfg.AuthCacheTTL = d
	}
	if raw.TLS != nil {
		cfg.TLS = raw.TLS
	}
	if raw.LogLevel != "" {
		cfg.LogLevel = raw.LogLevel
	}
	if raw.ProxmoxAPIURL != "" {
		cfg.ProxmoxAPIURL = raw.ProxmoxAPIURL
	}

	return cfg, nil
}
