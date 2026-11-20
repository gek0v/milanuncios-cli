package config_test

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestSavePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "sub")
	cfgPath := filepath.Join(cfgDir, "config.toml")

	cfg := config.New()
	cfg.Proxy = "http://user:secret@127.0.0.1:8080"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(cfgPath)
		if err != nil {
			t.Fatalf("stat config file: %v", err)
		}
		if fi.Mode().Perm() != 0600 {
			t.Errorf("expected 0600 file permissions, got %#o", fi.Mode().Perm())
		}

		di, err := os.Stat(cfgDir)
		if err != nil {
			t.Fatalf("stat config dir: %v", err)
		}
		if di.Mode().Perm() != 0700 {
			t.Errorf("expected 0700 dir permissions, got %#o", di.Mode().Perm())
		}
	}
}

func TestTelegramConfig_SaveLoadAndGetSet(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	cfg := config.New()
	cfg.Telegram.Enabled = true
	cfg.Telegram.AllowedChatIDs = []int64{123456, 789012}
	cfg.Telegram.AutoMarkRead = true
	cfg.Sinks["tg-sink"] = config.SinkConfig{
		Type:     "telegram",
		ChatID:   123456,
		ChatIDs:  []int64{123456, 789012},
		BotToken: "test-bot-token",
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if !loaded.Telegram.Enabled {
		t.Errorf("expected telegram.enabled to be true")
	}
	if len(loaded.Telegram.AllowedChatIDs) != 2 || loaded.Telegram.AllowedChatIDs[0] != 123456 {
		t.Errorf("unexpected allowed_chat_ids: %v", loaded.Telegram.AllowedChatIDs)
	}
	if !loaded.Telegram.AutoMarkRead {
		t.Errorf("expected telegram.auto_mark_read to be true")
	}
	sc, ok := loaded.Sinks["tg-sink"]
	if !ok {
		t.Fatalf("tg-sink not found in sinks")
	}
	if sc.ChatID != 123456 || len(sc.ChatIDs) != 2 || sc.BotToken != "test-bot-token" {
		t.Errorf("unexpected sink config: %+v", sc)
	}

	// Test Get / Set for telegram
	if err := loaded.Set("telegram.enabled", "false"); err != nil {
		t.Fatalf("failed to set telegram.enabled: %v", err)
	}
	val, err := loaded.Get("telegram.enabled")
	if err != nil || val != false {
		t.Fatalf("expected telegram.enabled to be false, got %v (err: %v)", val, err)
	}

	if err := loaded.Set("telegram.auto_mark_read", "false"); err != nil {
		t.Fatalf("failed to set telegram.auto_mark_read: %v", err)
	}
	val, err = loaded.Get("telegram.auto_mark_read")
	if err != nil || val != false {
		t.Fatalf("expected telegram.auto_mark_read to be false, got %v (err: %v)", val, err)
	}
}
