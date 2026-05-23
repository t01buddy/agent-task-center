package config_test

import (
	"os"
	"testing"

	"github.com/t01buddy/agent-task-center/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("ATC_CONFIG")
	os.Unsetenv("ATC_DB_PATH")
	os.Unsetenv("ATC_ADDR")
	os.Unsetenv("ATC_LOG_FORMAT")

	cfg := config.Load()
	if cfg.Addr != ":8765" {
		t.Errorf("want :8765, got %s", cfg.Addr)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("want json, got %s", cfg.LogFormat)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	os.Unsetenv("ATC_CONFIG")
	t.Setenv("ATC_ADDR", ":9999")
	t.Setenv("ATC_LOG_FORMAT", "text")

	cfg := config.Load()
	if cfg.Addr != ":9999" {
		t.Errorf("want :9999, got %s", cfg.Addr)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("want text, got %s", cfg.LogFormat)
	}
}

func TestLoad_TOMLFile(t *testing.T) {
	os.Unsetenv("ATC_DB_PATH")
	os.Unsetenv("ATC_ADDR")

	f, err := os.CreateTemp("", "atc-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`addr = ":7777"
db_path = "/tmp/test.db"
log_format = "text"
`)
	f.Close()

	t.Setenv("ATC_CONFIG", f.Name())

	cfg := config.Load()
	if cfg.Addr != ":7777" {
		t.Errorf("want :7777, got %s", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("want /tmp/test.db, got %s", cfg.DBPath)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("want text, got %s", cfg.LogFormat)
	}
}

func TestLoad_EnvWinsOverTOML(t *testing.T) {
	f, err := os.CreateTemp("", "atc-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`addr = ":7777"`)
	f.Close()

	t.Setenv("ATC_CONFIG", f.Name())
	t.Setenv("ATC_ADDR", ":8888")

	cfg := config.Load()
	if cfg.Addr != ":8888" {
		t.Errorf("env should win: want :8888, got %s", cfg.Addr)
	}
}

func TestLoad_MissingTOMLFileNotError(t *testing.T) {
	t.Setenv("ATC_CONFIG", "/tmp/nonexistent-atc-config.toml")
	os.Unsetenv("ATC_ADDR")

	cfg := config.Load()
	// should fall back to defaults without panicking
	if cfg.Addr != ":8765" {
		t.Errorf("want :8765, got %s", cfg.Addr)
	}
}
