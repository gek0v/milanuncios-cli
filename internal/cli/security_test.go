package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func TestValidateAdURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		expectErr bool
	}{
		{
			name:      "valid milanuncios ad URL",
			rawURL:    "https://www.milanuncios.com/coches/golf-492817294.htm",
			expectErr: false,
		},
		{
			name:      "valid ad URL with query params",
			rawURL:    "https://www.milanuncios.com/anuncios/12345.htm?fromSearch=1",
			expectErr: false,
		},
		{
			name:      "insecure HTTP scheme",
			rawURL:    "http://www.milanuncios.com/coches/golf-492817294.htm",
			expectErr: true,
		},
		{
			name:      "different host",
			rawURL:    "https://attacker.com/coches/golf-492817294.htm",
			expectErr: true,
		},
		{
			name:      "subdomain spoofing attempt",
			rawURL:    "https://www.milanuncios.com.attacker.com/ad.htm",
			expectErr: true,
		},
		{
			name:      "file protocol handler attack",
			rawURL:    "file:///C:/Windows/System32/calc.exe",
			expectErr: true,
		},
		{
			name:      "javascript protocol handler attack",
			rawURL:    "javascript:alert(1)",
			expectErr: true,
		},
		{
			name:      "command injection attempt with ampersand",
			rawURL:    "https://www.milanuncios.com/ad & calc.exe",
			expectErr: true,
		},
		{
			name:      "command injection attempt with pipe",
			rawURL:    "https://www.milanuncios.com/ad | calc.exe",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validated, err := validateAdURL(tc.rawURL)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for URL %q, got validated URL %q", tc.rawURL, validated)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for URL %q: %v", tc.rawURL, err)
				}
				if validated != tc.rawURL {
					t.Errorf("expected %q, got %q", tc.rawURL, validated)
				}
			}
		})
	}
}

func TestBuildBrowserCmd_SafeWindowsExecution(t *testing.T) {
	testURL := "https://www.milanuncios.com/anuncios/492817294.htm"
	cmd := buildBrowserCmd(testURL)

	if runtime.GOOS == "windows" {
		// Verify that Windows does NOT use "cmd.exe" or "/c start"
		cmdPath := strings.ToLower(cmd.Path)
		if strings.Contains(cmdPath, "cmd.exe") || strings.Contains(cmdPath, "cmd") {
			t.Errorf("Windows browser command must NOT use cmd.exe shell wrapper: got %s", cmd.Path)
		}
		if len(cmd.Args) < 3 {
			t.Fatalf("expected at least 3 args for rundll32, got %v", cmd.Args)
		}
		if cmd.Args[0] != "rundll32" {
			t.Errorf("expected rundll32 executable, got %s", cmd.Args[0])
		}
		if cmd.Args[1] != "url.dll,FileProtocolHandler" {
			t.Errorf("expected url.dll,FileProtocolHandler entrypoint, got %s", cmd.Args[1])
		}
		if cmd.Args[2] != testURL {
			t.Errorf("expected URL argument %s, got %s", testURL, cmd.Args[2])
		}
	}
}

func TestRedactProxyError(t *testing.T) {
	tests := []struct {
		name           string
		rawURL         string
		secretPassword string
	}{
		{
			name:           "http proxy with username and password",
			rawURL:         "http://admin:SuperSecretPassword123@invalid host:8080",
			secretPassword: "SuperSecretPassword123",
		},
		{
			name:           "socks5 proxy with special characters in password and invalid escape",
			rawURL:         "socks5://user:P@ssword!#99@proxy.example.com:1080%zz",
			secretPassword: "P@ssword!#99",
		},
		{
			name:           "proxy with password only",
			rawURL:         "http://:MyTokenSecret@invalid host:8080",
			secretPassword: "MyTokenSecret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := url.Parse(tc.rawURL)
			if err == nil {
				t.Fatalf("expected url.Parse to fail for invalid URL %q", tc.rawURL)
			}

			redacted := redactProxyError(err)
			if redacted == nil {
				t.Fatalf("expected non-nil error from redactProxyError")
			}

			errMsg := redacted.Error()
			if strings.Contains(errMsg, tc.secretPassword) {
				t.Errorf("password leaked in error message! Got: %s, expected password %q to be redacted", errMsg, tc.secretPassword)
			}
			if !strings.Contains(errMsg, "xxxxx") {
				t.Errorf("expected 'xxxxx' replacement in error message, got: %s", errMsg)
			}
		})
	}
}

func TestAppProxyPasswordLeakageInCLI(t *testing.T) {
	secretPass := "UltraSecretPassword987"
	t.Setenv("MILANUNCIOS_PROXY", "http://myuser:"+secretPass+"@invalid host:8080")

	var stdout, stderr bytes.Buffer
	app := &App{
		Version:         "test",
		Stdin:           strings.NewReader(""),
		Stdout:          &stdout,
		Stderr:          &stderr,
		FlagErrorFormat: "json",
	}

	exitCode := app.Execute()
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code due to invalid proxy")
	}

	stderrStr := stderr.String()
	if strings.Contains(stderrStr, secretPass) {
		t.Fatalf("CRITICAL: proxy password leaked in stderr output!\n%s", stderrStr)
	}
	if !strings.Contains(stderrStr, "xxxxx") {
		t.Errorf("expected redacted credential 'xxxxx' in stderr, got: %s", stderrStr)
	}
}

func TestAdOpen_RejectsMaliciousURL(t *testing.T) {
	// Set up mock server that returns an ad with a malicious URL
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "123456",
			"title": "Malicious Ad",
			"url": "https://attacker.com/malicious & calc.exe",
			"price": 100
		}`))
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	app := &App{
		Version:         "test",
		Stdin:           strings.NewReader(""),
		Stdout:          &stdout,
		Stderr:          &stderr,
		FlagErrorFormat: "text",
	}
	app.Client = milanuncios.NewClient("test")
	app.Client.BaseURL = ts.URL

	cmd := app.adOpenCmd()
	cmd.SetArgs([]string{"123456"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected ad open to reject non-milanuncios URL, but it succeeded")
	}
	if !strings.Contains(err.Error(), "only www.milanuncios.com is allowed") &&
		!strings.Contains(err.Error(), "invalid ad URL") &&
		!strings.Contains(err.Error(), "ad URL contains invalid or dangerous characters") {
		t.Errorf("expected security validation error, got: %v", err)
	}
}
