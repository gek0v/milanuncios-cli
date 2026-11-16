package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/cli"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func TestCommandTree(t *testing.T) {
	root := cli.CommandTree("0.1.0-test")
	if root == nil {
		t.Fatal("expected non-nil root command")
	}
	if root.Use != "milanuncios" {
		t.Fatalf("expected Use to be 'milanuncios', got %q", root.Use)
	}
	if root.Version != "0.1.0-test" {
		t.Fatalf("expected Version to be '0.1.0-test', got %q", root.Version)
	}

	flags := []string{"profile", "format", "no-color", "error-format", "no-input", "debug"}
	for _, flagName := range flags {
		if root.PersistentFlags().Lookup(flagName) == nil {
			t.Errorf("missing persistent flag --%s", flagName)
		}
	}
}

func TestExecuteHelp(t *testing.T) {
	app := cli.NewApp("0.1.0-test")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error on --help, got %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "milanuncios is an unofficial terminal client") {
		t.Errorf("expected help text in output, got: %s", out)
	}
}

func TestExecuteVersion(t *testing.T) {
	app := cli.NewApp("1.2.3")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error on --version, got %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "milanuncios 1.2.3") {
		t.Errorf("expected version 'milanuncios 1.2.3', got: %s", out)
	}
}

func TestValidateGlobalFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorSubstr string
	}{
		{
			name:        "valid format json",
			args:        []string{"--format", "json"},
			expectError: false,
		},
		{
			name:        "valid format pretty",
			args:        []string{"--format", "pretty"},
			expectError: false,
		},
		{
			name:        "valid format toon",
			args:        []string{"--format", "toon"},
			expectError: false,
		},
		{
			name:        "invalid format xml",
			args:        []string{"--format", "xml"},
			expectError: true,
			errorSubstr: "unknown format \"xml\"",
		},
		{
			name:        "invalid error format yaml",
			args:        []string{"--error-format", "yaml"},
			expectError: true,
			errorSubstr: "--error-format must be text or json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := cli.NewApp("test")
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			app.Stdout = &stdout
			app.Stderr = &stderr

			cmd := app.RootCmd()
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errorSubstr)
				}
				if !strings.Contains(err.Error(), tt.errorSubstr) {
					t.Fatalf("expected error containing %q, got %q", tt.errorSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfigCommands(t *testing.T) {
	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "config.toml")
	t.Setenv("MILANUNCIOS_CONFIG", cfgFile)

	// Test config path
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"config", "path"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("config path failed: %v", err)
		}
		var res map[string]string
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			t.Fatalf("failed to parse json output: %v", err)
		}
		if res["path"] != cfgFile {
			t.Fatalf("expected config path %q, got %q", cfgFile, res["path"])
		}
	}

	// Test config set
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"config", "set", "proxy", "socks5h://127.0.0.1:9050"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("config set failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "updated") {
			t.Fatalf("expected 'updated' in output, got: %s", stdout.String())
		}
	}

	// Test config get
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"config", "get", "proxy"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("config get failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "socks5h://127.0.0.1:9050") {
			t.Fatalf("expected proxy value in output, got: %s", stdout.String())
		}
	}

	// Test config list
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"config", "list"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("config list failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "socks5h://127.0.0.1:9050") {
			t.Fatalf("expected proxy in config list, got: %s", stdout.String())
		}
	}
}

func TestSearchCommand(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{
					ID:    "3003",
					Title: "Guitarra española",
					Price: 150,
					Location: milanuncios.AdLocation{
						Province: "Sevilla",
					},
					Seller: milanuncios.AdSeller{
						Name: "Manuel",
					},
				},
			},
			Total: 1,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"search", "guitarra", "--province", "Sevilla", "--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("search command failed: %v", err)
	}

	var parsed milanuncios.SearchResult
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to decode search json output: %v", err)
	}
	if len(parsed.Items) != 1 || parsed.Items[0].Title != "Guitarra española" {
		t.Fatalf("unexpected search items in output: %+v", parsed.Items)
	}
}

func TestAdCommands(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/ads/492817294" {
			item := milanuncios.AdItem{
				ID:    "492817294",
				Title: "Mesa de centro",
				Price: 40,
				Photos: []string{
					"https://img.milanuncios.com/1.jpg",
					"https://img.milanuncios.com/2.jpg",
				},
				URL: "https://www.milanuncios.com/anuncios/492817294.htm",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(item)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Test ad show
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"ad", "show", "492817294", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("ad show failed: %v", err)
		}
		var parsed milanuncios.AdItem
		if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to decode ad show output: %v", err)
		}
		if parsed.Title != "Mesa de centro" {
			t.Fatalf("unexpected title: %s", parsed.Title)
		}
	}

	// Test item alias and ad images
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"item", "images", "492817294", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("item images failed: %v", err)
		}
		var parsed milanuncios.ImagesResult
		if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to decode images output: %v", err)
		}
		if len(parsed.Photos) != 2 {
			t.Fatalf("expected 2 photos, got %d", len(parsed.Photos))
		}
	}
}

func TestCategoryAndFilterCommands(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/categories":
			cats := milanuncios.CategoryListResult{
				{ID: 10, Name: "Motor", Slug: "motor"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cats)
		case "/api/v1/categories/10/filters":
			filters := milanuncios.CategoryFiltersResult{
				CategoryID:   10,
				CategoryName: "Motor",
				Filters: []milanuncios.FilterDefinition{
					{Key: "brand", Label: "Marca", Type: "select"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(filters)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	// Test category list
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"category", "list", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("category list failed: %v", err)
		}
		var parsed milanuncios.CategoryListResult
		if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to decode category list output: %v", err)
		}
		if len(parsed) != 1 || parsed[0].Name != "Motor" {
			t.Fatalf("unexpected category list: %+v", parsed)
		}
	}

	// Test search filters
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"search", "filters", "--category", "10", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("search filters failed: %v", err)
		}
		var parsed milanuncios.CategoryFiltersResult
		if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to decode filters output: %v", err)
		}
		if len(parsed.Filters) != 1 || parsed.Filters[0].Key != "brand" {
			t.Fatalf("unexpected filters: %+v", parsed)
		}
	}
}

func TestExecuteExitCodes(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	os.Args = []string{"milanuncios", "--format", "invalid-format"}
	code := app.Execute()
	if code != 2 {
		t.Fatalf("expected exit code 2 for usage error, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown format") {
		t.Fatalf("expected error message on stderr, got: %s", stderr.String())
	}
}

func TestAuthAndProfileCommands(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.toml")
	t.Setenv("MILANUNCIOS_CREDENTIALS_FILE", credFile)
	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "")

	// 1. auth status when not logged in
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"auth", "status", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("auth status failed: %v", err)
		}
		var res map[string]interface{}
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode auth status json: %v", err)
		}
		if res["logged_in"] != false {
			t.Fatalf("expected logged_in: false, got: %v", res["logged_in"])
		}
	}

	// 2. profile list when empty
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"profile", "list", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("profile list failed: %v", err)
		}
	}
}

func TestMeAndFavoriteCommands(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer valid-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		switch {
		case r.URL.Path == "/api/v1/me":
			user := milanuncios.UserProfile{
				ID:       "usr-99",
				Name:     "Ana López",
				Email:    "ana@example.com",
				Phone:    "600112233",
				Verified: true,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(user)

		case r.URL.Path == "/api/v1/me/favorites" && r.Method == http.MethodGet:
			favs := []milanuncios.AdItem{
				{
					ID:    "777",
					Title: "Mesa de centro vintage",
					Price: 80,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favs)

		case r.URL.Path == "/api/v1/ads/777/favorite" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))

		case r.URL.Path == "/api/v1/ads/777/favorite" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "valid-test-token")

	// 1. me show
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"me", "show", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("me show failed: %v", err)
		}
		var user milanuncios.UserProfile
		if err := json.Unmarshal(stdout.Bytes(), &user); err != nil {
			t.Fatalf("failed to decode user profile: %v", err)
		}
		if user.Name != "Ana López" {
			t.Fatalf("unexpected user name: %s", user.Name)
		}
	}

	// 2. me favorites
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"me", "favorites", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("me favorites failed: %v", err)
		}
		var favs []milanuncios.AdItem
		if err := json.Unmarshal(stdout.Bytes(), &favs); err != nil {
			t.Fatalf("failed to decode favorites: %v", err)
		}
		if len(favs) != 1 || favs[0].ID != "777" {
			t.Fatalf("unexpected favorites: %+v", favs)
		}
	}

	// 3. ad favorite & unfavorite
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"ad", "favorite", "777", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("ad favorite failed: %v", err)
		}

		stdout.Reset()
		stderr.Reset()
		cmd = app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"ad", "unfavorite", "777", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("ad unfavorite failed: %v", err)
		}
	}
}

func TestWatchCommands(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")
	t.Setenv("MILANUNCIOS_STATE_DB", dbPath)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{
					ID:    "9001",
					Title: "Bicicleta Specialized Tarmac",
					Price: 1200,
				},
			},
			Total: 1,
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	// 1. watch add search
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "add", "search", "bicicleta carretera", "--name", "bici", "--interval", "3m", "--province", "valencia", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch add search failed: %v", err)
		}
		var w map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &w); err != nil {
			t.Fatalf("failed to decode watch json: %v", err)
		}
		if w["name"] != "bici" || w["type"] != "search" {
			t.Fatalf("unexpected watch: %+v", w)
		}
	}

	// 2. watch add ad
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "add", "ad", "492817294", "--name", "mi-bici", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch add ad failed: %v", err)
		}
	}

	// 3. watch list
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "list", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch list failed: %v", err)
		}
		var list []map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
			t.Fatalf("failed to decode watch list: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 watches, got %d", len(list))
		}
	}

	// 4. watch pause & resume
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "pause", "bici", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch pause failed: %v", err)
		}

		stdout.Reset()
		stderr.Reset()
		cmd = app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "resume", "bici", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch resume failed: %v", err)
		}
	}

	// 5. watch check
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "check", "bici", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch check failed: %v", err)
		}
	}

	// 6. watch events
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "events", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch events failed: %v", err)
		}
	}

	// 7. watch remove
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"watch", "remove", "bici", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("watch remove failed: %v", err)
		}
	}
}

func TestSinkCommands(t *testing.T) {
	var receivedWebhook bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedWebhook = true
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "config.toml")
	cfgContent := fmt.Sprintf(`
[sinks.alerts-hook]
type = "webhook"
url = "%s"
`, ts.URL)
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	t.Setenv("MILANUNCIOS_CONFIG", cfgFile)

	// 1. sink list
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"sink", "list", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("sink list failed: %v", err)
		}
		var list []map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
			t.Fatalf("failed to decode sink list: %v", err)
		}
		if len(list) != 1 || list[0]["name"] != "alerts-hook" {
			t.Fatalf("unexpected sink list: %+v", list)
		}
	}

	// 2. sink test
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"sink", "test", "alerts-hook", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("sink test failed: %v", err)
		}
		if !receivedWebhook {
			t.Fatal("expected webhook to receive mock event")
		}
	}
}

func TestMCPAndSkillsCommands(t *testing.T) {
	// 1. skills get
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"skills", "get", "milanuncios-usage"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("skills get failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "Milanuncios CLI Agent Guide") {
			t.Fatalf("expected skill guide in output, got: %s", stdout.String())
		}
	}

	// 2. mcp install cursor
	{
		tmpHome := t.TempDir()
		t.Setenv("USERPROFILE", tmpHome)
		t.Setenv("HOME", tmpHome)

		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"mcp", "install", "cursor", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("mcp install cursor failed: %v", err)
		}
		var res map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode install json: %v", err)
		}
		if res["status"] != "installed" || res["client"] != "cursor" {
			t.Fatalf("unexpected install result: %+v", res)
		}
	}
}

func TestChatCommands(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/v1/conversations" && r.Method == http.MethodGet:
			convs := milanuncios.ConversationList{
				{
					ID:          "conv-101",
					AdID:        "492817294",
					AdTitle:     "Bicicleta carretera",
					AdPrice:     450,
					UserName:    "Carlos",
					LastMessage: "¿Aceptas 400€?",
					Unread:      true,
				},
			}
			_ = json.NewEncoder(w).Encode(convs)

		case r.URL.Path == "/api/v1/conversations/conv-101" && r.Method == http.MethodGet:
			details := milanuncios.ConversationDetails{
				Conversation: milanuncios.Conversation{
					ID:          "conv-101",
					AdID:        "492817294",
					AdTitle:     "Bicicleta carretera",
					AdPrice:     450,
					UserName:    "Carlos",
					LastMessage: "¿Aceptas 400€?",
				},
				Messages: []milanuncios.Message{
					{
						ID:             "msg-1",
						ConversationID: "conv-101",
						SenderName:     "Carlos",
						Text:           "Hola, ¿sigue disponible?",
						SentAt:         "10:00",
					},
					{
						ID:             "msg-2",
						ConversationID: "conv-101",
						SenderName:     "Carlos",
						Text:           "¿Aceptas 400€?",
						SentAt:         "10:05",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(details)

		case r.URL.Path == "/api/v1/conversations/conv-101/messages" && r.Method == http.MethodPost:
			msg := milanuncios.Message{
				ID:             "msg-3",
				ConversationID: "conv-101",
				SenderName:     "Tú",
				Text:           "Hola Carlos, sí, por 420€ cerramos trato",
				SentAt:         "10:15",
				FromMe:         true,
			}
			_ = json.NewEncoder(w).Encode(msg)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "valid-chat-token")

	// 1. chat list
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"chat", "list", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat list failed: %v", err)
		}
		var list []milanuncios.Conversation
		if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
			t.Fatalf("failed to decode conversation list: %v", err)
		}
		if len(list) != 1 || list[0].ID != "conv-101" {
			t.Fatalf("unexpected conversation list: %+v", list)
		}
	}

	// 2. chat show
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"chat", "show", "conv-101", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat show failed: %v", err)
		}
		var details milanuncios.ConversationDetails
		if err := json.Unmarshal(stdout.Bytes(), &details); err != nil {
			t.Fatalf("failed to decode conversation details: %v", err)
		}
		if len(details.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(details.Messages))
		}
	}

	// 3. chat send
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"chat", "send", "conv-101", "Hola Carlos, sí, por 420€ cerramos trato", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat send failed: %v", err)
		}
		var msg milanuncios.Message
		if err := json.Unmarshal(stdout.Bytes(), &msg); err != nil {
			t.Fatalf("failed to decode sent message: %v", err)
		}
		if !msg.FromMe || !strings.Contains(msg.Text, "420€") {
			t.Fatalf("unexpected sent message: %+v", msg)
		}
	}
}
