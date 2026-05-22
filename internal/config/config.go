package config

import (
	"os"
	"strconv"
)

// Config holds all service settings.
type Config struct {
	DBPath          string // ATC_DB_PATH
	Addr            string // ATC_ADDR
	ExpiryIntervalS int    // ATC_EXPIRY_INTERVAL_S
	DrainTimeoutS   int    // ATC_DRAIN_TIMEOUT_S
	LogFormat       string // ATC_LOG_FORMAT: "json" or "text"
}

// Load reads configuration from environment variables, applying defaults.
func Load() Config {
	cfg := Config{
		DBPath:          "./agent-task-center.db",
		Addr:            ":8765",
		ExpiryIntervalS: 10,
		DrainTimeoutS:   5,
		LogFormat:       "json",
	}

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
