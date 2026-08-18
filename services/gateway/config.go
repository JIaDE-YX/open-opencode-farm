package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Listen        string            `json:"listen"`
	ServerKeys    []string          `json:"server_keys"`
	ZenKeys       []string          `json:"zen_keys"`
	GoKeys        []string          `json:"go_keys"`
	Proxies       []string          `json:"proxies"`
	AdminPassword string            `json:"admin_password,omitempty"`
	Upstream      UpstreamConfig    `json:"upstream"`
	Retry         RetryConfig       `json:"retry"`
	Models        ModelsConfig      `json:"models"`
	Performance   PerformanceConfig `json:"performance"`
	Logging       LoggingConfig     `json:"logging"`
	Prefer        Tier              `json:"prefer"`
}

type UpstreamConfig struct {
	Zen string `json:"zen"`
	Go  string `json:"go"`
}

type RetryConfig struct {
	MaxAttempts    int `json:"max_attempts"`
	TimeoutSeconds int `json:"timeout_seconds"`
}

type ModelsConfig struct {
	RefreshSeconds int               `json:"refresh_seconds"`
	Protocols      map[string]string `json:"protocols"`
}

type LoggingConfig struct {
	Level string `json:"level"`
}

type PerformanceConfig struct {
	MaxIdleConns           int `json:"max_idle_conns"`
	MaxIdleConnsPerHost    int `json:"max_idle_conns_per_host"`
	MaxConnsPerHost        int `json:"max_conns_per_host"`
	IdleConnTimeoutSeconds int `json:"idle_conn_timeout_seconds"`
	ConnectTimeoutSeconds  int `json:"connect_timeout_seconds"`
	FailureCooldownSeconds int `json:"failure_cooldown_seconds"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := Config{
		Listen:      "127.0.0.1:8080",
		Proxies:     []string{"direct"},
		Upstream:    UpstreamConfig{Zen: "https://opencode.ai/zen", Go: "https://opencode.ai/zen/go"},
		Retry:       RetryConfig{MaxAttempts: 3, TimeoutSeconds: 300},
		Models:      ModelsConfig{RefreshSeconds: 300, Protocols: map[string]string{}},
		Performance: PerformanceConfig{MaxIdleConns: 2048, MaxIdleConnsPerHost: 256, MaxConnsPerHost: 0, IdleConnTimeoutSeconds: 120, ConnectTimeoutSeconds: 5, FailureCooldownSeconds: 15},
		Logging:     LoggingConfig{Level: "info"},
		Prefer:      TierGo,
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	trimList(&cfg.ServerKeys)
	trimList(&cfg.ZenKeys)
	trimList(&cfg.GoKeys)
	trimList(&cfg.Proxies)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate 校验配置合法性（LoadConfig 与管理面板保存共用）
func (cfg *Config) Validate() error {
	if cfg.Prefer != TierZen && cfg.Prefer != TierGo {
		return errors.New("prefer must be \"zen\" or \"go\"")
	}
	if cfg.Listen == "" {
		return errors.New("listen must not be empty")
	}
	if len(cfg.ServerKeys) == 0 {
		return errors.New("server_keys must contain at least one local key")
	}
	if len(cfg.ZenKeys) == 0 && len(cfg.GoKeys) == 0 {
		return errors.New("zen_keys or go_keys must contain at least one upstream key")
	}
	if len(cfg.Proxies) == 0 {
		cfg.Proxies = []string{"direct"}
	}
	if cfg.Retry.MaxAttempts < 1 {
		return errors.New("retry.max_attempts must be at least 1")
	}
	if cfg.Retry.TimeoutSeconds < 1 {
		return errors.New("retry.timeout_seconds must be at least 1")
	}
	if cfg.Models.RefreshSeconds < 1 {
		return errors.New("models.refresh_seconds must be at least 1")
	}
	if cfg.Performance.MaxIdleConns < 1 || cfg.Performance.MaxIdleConnsPerHost < 1 || cfg.Performance.MaxConnsPerHost < 0 || cfg.Performance.IdleConnTimeoutSeconds < 1 || cfg.Performance.ConnectTimeoutSeconds < 1 || cfg.Performance.FailureCooldownSeconds < 1 {
		return errors.New("performance values must be positive (max_conns_per_host may be zero for unlimited)")
	}
	for _, raw := range cfg.Proxies {
		if raw == "direct" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return fmt.Errorf("invalid proxy URL %q", redactURL(raw))
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
		}
	}
	for model, protocol := range cfg.Models.Protocols {
		if model == "" || !validProtocol(Protocol(protocol)) {
			return fmt.Errorf("models.protocols contains invalid mapping %q: %q", model, protocol)
		}
	}
	return nil
}

func trimList(items *[]string) {
	out := (*items)[:0]
	for _, item := range *items {
		if value := strings.TrimSpace(item); value != "" {
			out = append(out, value)
		}
	}
	*items = out
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid>"
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	return u.String()
}
