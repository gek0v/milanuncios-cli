package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/config"
)

func TestLoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error loading non-existent config: %v", err)
	}
	if cfg.DefaultProfile != "default" {
		t.Errorf("expected default profile 'default', got %q", cfg.DefaultProfile)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := config.New()
	cfg.DefaultProfile = "work"
	cfg.Proxy = "socks5h://127.0.0.1:1080"
	cfg.Watch.Interval = "10m"

	err := config.Save(cfgPath, cfg)
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	if loaded.DefaultProfile != "work" {
		t.Errorf("expected default_profile 'work', got %q", loaded.DefaultProfile)
	}
	if loaded.Proxy != "socks5h://127.0.0.1:1080" {
		t.Errorf("expected proxy 'socks5h://127.0.0.1:1080', got %q", loaded.Proxy)
	}
	if loaded.Watch.Interval != "10m" {
		t.Errorf("expected watch interval '10m', got %q", loaded.Watch.Interval)
	}
}

func TestGetAndSet(t *testing.T) {
	cfg := config.New()

	if err := cfg.Set("proxy", "http://proxy.local:8080"); err != nil {
		t.Fatalf("failed to set proxy: %v", err)
	}
	val, err := cfg.Get("proxy")
	if err != nil || val != "http://proxy.local:8080" {
		t.Fatalf("expected proxy 'http://proxy.local:8080', got %v (err: %v)", val, err)
	}

	if err := cfg.Set("profiles.home.location.province", "Madrid"); err != nil {
		t.Fatalf("failed to set province: %v", err)
	}
	val, err = cfg.Get("profiles.home.location.province")
	if err != nil || val != "Madrid" {
		t.Fatalf("expected province 'Madrid', got %v (err: %v)", val, err)
	}

	if err := cfg.Set("profiles.home.location.lat", "40.4168"); err != nil {
		t.Fatalf("failed to set lat: %v", err)
	}
	val, err = cfg.Get("profiles.home.location.lat")
	if err != nil || val != 40.4168 {
		t.Fatalf("expected lat 40.4168, got %v (err: %v)", val, err)
	}
}

func TestResolveProfile(t *testing.T) {
	cfg := config.New()
	cfg.DefaultProfile = "configured-default"

	if p := config.ResolveProfile("flag-profile", cfg); p != "flag-profile" {
		t.Errorf("expected 'flag-profile', got %q", p)
	}

	os.Setenv("MILANUNCIOS_PROFILE", "env-profile")
	defer os.Unsetenv("MILANUNCIOS_PROFILE")

	if p := config.ResolveProfile("", cfg); p != "env-profile" {
		t.Errorf("expected 'env-profile', got %q", p)
	}

	os.Unsetenv("MILANUNCIOS_PROFILE")
	if p := config.ResolveProfile("", cfg); p != "configured-default" {
		t.Errorf("expected 'configured-default', got %q", p)
	}
}
