package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

func TestWatchCircuitBreakerPersistence(t *testing.T) {
	reqCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": "rate limit reached"}`))
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "watch_circuit.db")
	t.Setenv("MILANUNCIOS_STATE_DB", dbPath)
	t.Setenv("MILANUNCIOS_CONFIG", filepath.Join(tmpDir, "config.toml"))

	// Setup a watch in SQLite
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	w := &store.Watch{
		Type:   "search",
		Name:   "test-watch",
		Target: "portatil",
	}
	if err := st.AddWatch(context.Background(), w); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}
	st.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"watch", "check", "test-watch"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error on 429 block, got nil")
	}

	// Verify blocked_until was saved in state.db
	st2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer st2.Close()

	until, err := st2.GetBlockedUntil()
	if err != nil {
		t.Fatalf("failed to read blocked until: %v", err)
	}
	if until == nil {
		t.Fatal("expected non-nil blocked_until in state.db")
	}
	if time.Now().After(*until) {
		t.Errorf("expected blocked_until in the future, got %v", *until)
	}

	// Another command run should be rejected by circuit breaker without network call
	app2 := cli.NewApp("test")
	app2.Client.BaseURL = ts.URL
	stdout.Reset()
	stderr.Reset()
	app2.Stdout = &stdout
	app2.Stderr = &stderr
	cmd2 := app2.RootCmd()
	cmd2.SetArgs([]string{"watch", "check", "test-watch"})

	err2 := cmd2.Execute()
	if err2 == nil {
		t.Fatal("expected error from circuit breaker rejection, got nil")
	}
	if reqCount != 1 {
		t.Errorf("expected server to only receive 1 request, got %d", reqCount)
	}
}
