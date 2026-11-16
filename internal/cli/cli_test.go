package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/cli"
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
