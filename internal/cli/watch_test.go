package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/cli"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

func TestWatchAddFavorites(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")
	t.Setenv("MILANUNCIOS_STATE_DB", dbPath)
	t.Setenv("MILANUNCIOS_CONFIG", filepath.Join(tmpDir, "config.toml"))

	// 1. Add favorites with defaults
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetArgs([]string{"watch", "add", "favorites", "--format", "json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch add favorites failed: %v (stderr: %s)", err, stderr.String())
		}

		var w store.Watch
		if err := json.Unmarshal(stdout.Bytes(), &w); err != nil {
			t.Fatalf("failed to decode watch json: %v, stdout: %s", err, stdout.String())
		}
		if w.Type != "favorites" {
			t.Fatalf("expected type 'favorites', got %q", w.Type)
		}
		if w.Name != "favorites" {
			t.Fatalf("expected name 'favorites', got %q", w.Name)
		}
		if w.IntervalSec != 600 {
			t.Fatalf("expected default interval 600s (10m), got %d", w.IntervalSec)
		}
	}

	// 2. Add favorites with custom flags
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetArgs([]string{"watch", "add", "favorites", "--name", "my-favorites", "--interval", "15m", "--format", "json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch add favorites with custom flags failed: %v", err)
		}

		var w store.Watch
		if err := json.Unmarshal(stdout.Bytes(), &w); err != nil {
			t.Fatalf("failed to decode watch json: %v", err)
		}
		if w.Name != "my-favorites" {
			t.Fatalf("expected name 'my-favorites', got %q", w.Name)
		}
		if w.IntervalSec != 900 {
			t.Fatalf("expected interval 900s, got %d", w.IntervalSec)
		}
	}

	// 3. Validation failure: interval below 10 seconds
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetArgs([]string{"watch", "add", "favorites", "--interval", "5s"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for interval < 10s, got nil")
		}
	}

	// 4. Verify watch list reflects favorites watches
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetArgs([]string{"watch", "list", "--format", "json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch list failed: %v", err)
		}

		var list []store.Watch
		if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
			t.Fatalf("failed to decode watch list: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 watches, got %d", len(list))
		}
	}
}
