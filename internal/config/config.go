package config

import (
	"os"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Config holds all service settings.
type Config struct {
	DBPath          string // ATC_DB_PATH
	Addr            string // ATC_ADDR
	ExpiryIntervalS int    // ATC_EXPIRY_INTERVAL_S
	DrainTimeoutS   int    // ATC_DRAIN_TIMEOUT_S
	LogFormat       string // ATC_LOG_FORMAT: "json" or "text"
}

// tomlConfig mirrors Config with TOML-friendly keys for file parsing.
type tomlConfig struct {
	DBPath          string `toml:"db_path"`
	Addr            string `toml:"addr"`
	ExpiryIntervalS int    `toml:"expiry_interval_s"`
	DrainTimeoutS   int    `toml:"drain_timeout_s"`
	LogFormat       string `toml:"log_format"`
}

// Load reads configuration from an optional TOML file, then overlays env vars (env wins).
// The TOML file path is read from ATC_CONFIG; a missing file is not an error.
func Load() Config {
	cfg := Config{
		DBPath:          "./agent-task-center.db",
		Addr:            ":8765",
		ExpiryIntervalS: 10,
		DrainTimeoutS:   5,
		LogFormat:       "json",
	}

	// Load optional TOML file first (lowest precedence after defaults).
	if path := os.Getenv("ATC_CONFIG"); path != "" {
		var tc tomlConfig
		if _, err := toml.DecodeFile(path, &tc); err == nil {
			if tc.DBPath != "" {
				cfg.DBPath = tc.DBPath
			}
			if tc.Addr != "" {
				cfg.Addr = tc.Addr
			}
			if tc.ExpiryIntervalS != 0 {
				cfg.ExpiryIntervalS = tc.ExpiryIntervalS
			}
			if tc.DrainTimeoutS != 0 {
				cfg.DrainTimeoutS = tc.DrainTimeoutS
			}
			if tc.LogFormat == "text" || tc.LogFormat == "json" {
				cfg.LogFormat = tc.LogFormat
			}
		}
	}

	// Env vars overlay on top (highest precedence).
	if v := os.Getenv("ATC_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("ATC_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("ATC_EXPIRY_INTERVAL_S"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ExpiryIntervalS = n
		}
	}
	if v := os.Getenv("ATC_DRAIN_TIMEOUT_S"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DrainTimeoutS = n
		}
	}
	if v := os.Getenv("ATC_LOG_FORMAT"); v == "text" || v == "json" {
		cfg.LogFormat = v
	}

	return cfg
}
