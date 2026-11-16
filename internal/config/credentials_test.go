package config_test

import (
	"path/filepath"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/config"
)

func TestLoadNonExistentCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.toml")

	creds, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("unexpected error loading non-existent credentials: %v", err)
	}
	if len(creds.Profiles) != 0 {
		t.Fatalf("expected 0 profiles, got %d", len(creds.Profiles))
	}
}

func TestSaveAndLoadCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "credentials.toml")

	creds := config.NewCredentials()
	creds.Profiles["default"] = config.ProfileCredentials{
		SessionToken: "token-secret-12345",
		UserHash:     "u-hash-abc",
		Email:        "user@example.com",
		Name:         "Pedro",
		UpdatedAt:    "2026-11-16T18:00:00Z",
	}

	err := config.SaveCredentials(path, creds)
	if err != nil {
		t.Fatalf("failed to save credentials: %v", err)
	}

	loaded, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("failed to reload credentials: %v", err)
	}

	p, ok := loaded.Profiles["default"]
	if !ok {
		t.Fatal("profile 'default' not found in loaded credentials")
	}
	if p.SessionToken != "token-secret-12345" || p.UserHash != "u-hash-abc" || p.Email != "user@example.com" {
		t.Fatalf("unexpected credentials content: %+v", p)
	}

	// Test remove profile
	err = config.RemoveProfileCredentials(path, "default")
	if err != nil {
		t.Fatalf("failed to remove profile: %v", err)
	}

	reloaded, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("failed to reload credentials after removal: %v", err)
	}
	if _, ok := reloaded.Profiles["default"]; ok {
		t.Fatal("expected profile 'default' to be removed")
	}
}
