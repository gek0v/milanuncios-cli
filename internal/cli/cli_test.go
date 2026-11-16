package cli_test

import (
	"bytes"
	"encoding/json"
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
