package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

const ProtocolVersion = "1.53.1"
const DefaultProjectDir = `C:\Users\dev\projects\app`
const DefaultPath = "configs/config.json"

type Config struct {
	Port                   int           `json:"port"`
	Host                   string        `json:"host"`
	APIBase                string        `json:"apiBase"`
	ProjectSlug            string        `json:"projectSlug"`
	LogFile                string        `json:"logFile"`
	LogLevel               string        `json:"logLevel"`
	UseProviderModels      bool          `json:"useProviderModels"`
	ModelRefreshIntervalMS int           `json:"modelRefreshIntervalMs"`
	ZDR                    bool          `json:"zdr"`
	CLIMode                string        `json:"cliMode"`
	CLISessionMode         string        `json:"cliSessionMode"`
	FingerprintSalt        string        `json:"fingerprintSalt"`
	DeviceProjectDir       string        `json:"deviceProjectDir"`
	DevicePlatform         string        `json:"-"`
	EmptySystemPlaceholder bool          `json:"emptySystemPlaceholder"`
	UpstreamProxy          string        `json:"upstreamProxy"`
	CheckProtocolDrift     bool          `json:"checkProtocolDrift"`
	MaxBodyBytes           int64         `json:"-"`
	MaxInflight            int           `json:"-"`
	StreamIdle             time.Duration `json:"-"`
	NonstreamIdle          time.Duration `json:"-"`
	ClientDrainTimeout     time.Duration `json:"-"`
	KeepAliveTimeout       time.Duration `json:"-"`
}

// Default returns the upstream-compatible service defaults.
func Default() Config {
	return Config{Port: 3050, Host: "0.0.0.0", APIBase: "https://api.commandcode.ai", ProjectSlug: "cc-proxy", LogLevel: "info", UseProviderModels: true, ModelRefreshIntervalMS: 300000, CLIMode: "agent", CLISessionMode: "interactive", DeviceProjectDir: DefaultProjectDir, DevicePlatform: "win32", EmptySystemPlaceholder: true, CheckProtocolDrift: true, MaxBodyBytes: 100 << 20, StreamIdle: 30 * time.Second, NonstreamIdle: 90 * time.Second, KeepAliveTimeout: 65 * time.Second}
}

// Load applies the optional JSON file followed by environment overrides.
func Load(path string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return c, err
	}
	for name, dst := range map[string]*string{"HOST": &c.Host, "CC_API_BASE": &c.APIBase, "PROJECT_SLUG": &c.ProjectSlug, "LOG_FILE": &c.LogFile, "LOG_LEVEL": &c.LogLevel, "CC_FINGERPRINT_SALT": &c.FingerprintSalt, "CC_DEVICE_PROJECT_DIR": &c.DeviceProjectDir, "CC_CLI_MODE": &c.CLIMode, "CC_CLI_SESSION_MODE": &c.CLISessionMode, "CC_UPSTREAM_PROXY": &c.UpstreamProxy} {
		if value, ok := os.LookupEnv(name); ok {
			*dst = value
		}
	}
	for name, dst := range map[string]*bool{"CC_USE_PROVIDER_MODELS": &c.UseProviderModels, "CC_EMPTY_SYSTEM_PLACEHOLDER": &c.EmptySystemPlaceholder, "CC_CHECK_PROTOCOL_DRIFT": &c.CheckProtocolDrift} {
		if value, ok := os.LookupEnv(name); ok {
			*dst = value != "false" && value != "0"
		}
	}
	if v, ok := os.LookupEnv("CMD_ZDR"); ok {
		c.ZDR = v == "1"
	}
	if v := os.Getenv("PORT"); v != "" {
		c.Port, err = strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("PORT must be a port number")
		}
	}
	positive := func(name string, fallback int64) int64 {
		n, e := strconv.ParseInt(os.Getenv(name), 10, 64)
		if e != nil || n <= 0 {
			return fallback
		}
		return n
	}
	c.MaxBodyBytes = positive("CC_MAX_BODY_MB", 100) * 1024 * 1024
	c.MaxInflight = int(positive("CC_MAX_INFLIGHT", 0))
	c.StreamIdle = time.Duration(positive("CC_STREAM_IDLE_MS", 30000)) * time.Millisecond
	c.NonstreamIdle = time.Duration(positive("CC_NONSTREAM_IDLE_MS", 90000)) * time.Millisecond
	c.ClientDrainTimeout = time.Duration(positive("CC_CLIENT_DRAIN_TIMEOUT_MS", 0)) * time.Millisecond
	c.KeepAliveTimeout = time.Duration(positive("CC_KEEPALIVE_TIMEOUT_MS", 65000)) * time.Millisecond
	if c.Port < 1 || c.Port > 65535 {
		return c, fmt.Errorf("port must be between 1 and 65535")
	}
	if c.MaxBodyBytes <= 0 || c.StreamIdle <= 0 || c.NonstreamIdle <= 0 || c.KeepAliveTimeout <= 0 {
		return c, fmt.Errorf("configured limits exceed supported range")
	}
	if c.DeviceProjectDir == "" {
		c.DeviceProjectDir = DefaultProjectDir
	}
	if c.CLIMode == "" {
		c.CLIMode = "agent"
	}
	if c.CLISessionMode == "" {
		c.CLISessionMode = "interactive"
	}
	if c.ModelRefreshIntervalMS <= 0 {
		c.ModelRefreshIntervalMS = 300000
	}
	return c, nil
}
