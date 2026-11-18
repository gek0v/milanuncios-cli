package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/cli"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func setupChatMockServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	lastRequest := make(map[string]any)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		lastRequest["method"] = r.Method
		lastRequest["path"] = r.URL.Path
		lastRequest["header_client_req_id"] = r.Header.Get("X-Client-Request-Id")

		switch {
		case r.URL.Path == "/api/v1/ads/492817294/contact" && r.Method == http.MethodPost:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			lastRequest["body"] = body

			resp := milanuncios.ConversationStartResult{
				ConversationID: "conv-8849201",
				AdID:           "492817294",
				AdTitle:        "Volkswagen Golf VII 2.0 TDI",
				RecipientName:  "Javier R.",
				MessageID:      "msg-9901",
				Text:           body["text"],
				SentAt:         "2026-11-18T09:20:00Z",
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/api/v1/ads/404/contact":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "ad not found"}`))

		case r.URL.Path == "/api/v1/ads/999/contact":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message": "cannot start conversation on your own ad"}`))

		case r.URL.Path == "/api/v1/conversations" && r.Method == http.MethodGet:
			list := milanuncios.ConversationList{
				{
					ID:          "conv-101",
					AdID:        "111",
					AdTitle:     "PlayStation 5",
					AdPrice:     450,
					UserName:    "Carlos",
					LastMessage: "¿Sigue disponible?",
					LastSentAt:  "2026-11-17T18:00:00Z",
					Unread:      true,
				},
				{
					ID:          "conv-102",
					AdID:        "222",
					AdTitle:     "Nintendo Switch",
					AdPrice:     200,
					UserName:    "Elena",
					LastMessage: "Trato cerrado",
					LastSentAt:  "2026-11-16T12:00:00Z",
					Unread:      false,
				},
			}
			_ = json.NewEncoder(w).Encode(list)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return ts, &lastRequest
}

func TestChatStartPositionalJSON(t *testing.T) {
	ts, lastReq := setupChatMockServer(t)
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "test-token")

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"chat", "start", "492817294", "Hola,", "me", "interesa", "el", "coche.", "--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat start failed: %v (stderr: %s)", err, stderr.String())
	}

	body, ok := (*lastReq)["body"].(map[string]string)
	if !ok || body["text"] != "Hola, me interesa el coche." {
		t.Errorf("expected joined message text in body, got: %+v", body)
	}

	var res milanuncios.ConversationStartResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json output: %v, raw: %s", err, stdout.String())
	}

	if res.ConversationID != "conv-8849201" {
		t.Errorf("expected conv-8849201, got %s", res.ConversationID)
	}
	if res.AdTitle != "Volkswagen Golf VII 2.0 TDI" {
		t.Errorf("expected Volkswagen Golf VII 2.0 TDI, got %s", res.AdTitle)
	}
	if res.RecipientName != "Javier R." {
		t.Errorf("expected Javier R., got %s", res.RecipientName)
	}
}

func TestChatStartPretty(t *testing.T) {
	ts, _ := setupChatMockServer(t)
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "test-token")

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"chat", "start", "https://www.milanuncios.com/coches/golf-492817294.htm", "Hola", "--format", "pretty"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat start failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Conversación iniciada (#conv-8849201)") {
		t.Errorf("pretty output missing title: %s", out)
	}
	if !strings.Contains(out, "Volkswagen Golf") {
		t.Errorf("pretty output missing ad title: %s", out)
	}
	if !strings.Contains(out, "Javier R.") {
		t.Errorf("pretty output missing recipient: %s", out)
	}
	if !strings.Contains(out, "Hola") {
		t.Errorf("pretty output missing message text: %s", out)
	}
}

func TestChatStartFlagMessage(t *testing.T) {
	ts, lastReq := setupChatMockServer(t)
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "test-token")

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"chat", "start", "492817294", "-m", "Mensaje via flag -m", "--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat start failed: %v", err)
	}

	body := (*lastReq)["body"].(map[string]string)
	if body["text"] != "Mensaje via flag -m" {
		t.Errorf("expected text 'Mensaje via flag -m', got %q", body["text"])
	}
}

func TestChatStartFromStdin(t *testing.T) {
	ts, lastReq := setupChatMockServer(t)
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "test-token")

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	stdin := strings.NewReader("Mensaje leido desde stdin")
	app.Stdin = stdin
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetIn(stdin)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"chat", "start", "492817294", "--from-stdin", "--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat start failed: %v", err)
	}

	body := (*lastReq)["body"].(map[string]string)
	if body["text"] != "Mensaje leido desde stdin" {
		t.Errorf("expected text 'Mensaje leido desde stdin', got %q", body["text"])
	}
}

func TestChatStartClientRequestID(t *testing.T) {
	ts, lastReq := setupChatMockServer(t)
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "test-token")

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	reqID := "550e8400-e29b-41d4-a716-446655440001"
	cmd.SetArgs([]string{"chat", "start", "492817294", "Oferta con idempotencia", "--client-request-id", reqID, "--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat start failed: %v", err)
	}

	headerID := (*lastReq)["header_client_req_id"]
	if headerID != reqID {
		t.Errorf("expected X-Client-Request-Id header %q, got %q", reqID, headerID)
	}

	body := (*lastReq)["body"].(map[string]string)
	if body["client_request_id"] != reqID {
		t.Errorf("expected client_request_id in body %q, got %q", reqID, body["client_request_id"])
	}
}

func TestChatStartValidationErrors(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	// Missing message (no positional, no flag, no stdin)
	cmd := app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"chat", "start", "492817294"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no message is provided, got nil")
	}
	if !strings.Contains(err.Error(), "message text is required") {
		t.Errorf("unexpected error: %v", err)
	}

	// Conflicting --from-stdin with positional
	cmd = app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"chat", "start", "492817294", "arg-text", "--from-stdin"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error on conflicting stdin and positional, got nil")
	}
	if !strings.Contains(err.Error(), "cannot combine --from-stdin") {
		t.Errorf("unexpected error: %v", err)
	}

	// Conflicting positional and --message
	cmd = app.RootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"chat", "start", "492817294", "arg-text", "-m", "flag-text"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error on conflicting positional and flag, got nil")
	}
	if !strings.Contains(err.Error(), "cannot specify message as both positional argument and --message flag") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestChatStartExitCodes(t *testing.T) {
	ts, _ := setupChatMockServer(t)
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "test-token")

	// 1. Not Found (404) -> Exit code 4
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		os.Args = []string{"milanuncios", "chat", "start", "404", "Hola"}
		code := app.Execute()
		if code != 4 {
			t.Fatalf("expected exit code 4 for ad not found, got %d (stderr: %s)", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "ad not found: 404") {
			t.Errorf("expected error to mention 'ad not found: 404', got: %s", stderr.String())
		}
	}

	// 2. Own Ad -> Exit code 2 (Usage error)
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		os.Args = []string{"milanuncios", "chat", "start", "999", "Hola"}
		code := app.Execute()
		if code != 2 {
			t.Fatalf("expected exit code 2 for own ad, got %d (stderr: %s)", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "cannot start conversation on your own ad") {
			t.Errorf("expected error to mention 'cannot start conversation on your own ad', got: %s", stderr.String())
		}
	}
}

func TestChatListUnreadOnly(t *testing.T) {
	ts, _ := setupChatMockServer(t)
	defer ts.Close()

	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "test-token")

	// 1. Unfiltered chat list returns 2 conversations
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
			t.Fatalf("failed to decode json: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 conversations, got %d", len(list))
		}
	}

	// 2. chat list --unread-only returns only unread (1 conversation)
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"chat", "list", "--unread-only", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat list --unread-only failed: %v", err)
		}

		var list []milanuncios.Conversation
		if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("expected 1 unread conversation, got %d", len(list))
		}
		if list[0].ID != "conv-101" || !list[0].Unread {
			t.Errorf("unexpected conversation in unread-only list: %+v", list[0])
		}
	}

	// 3. chat list --unread-only --format pretty renders unread row
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"chat", "list", "--unread-only", "--format", "pretty"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat list --unread-only pretty failed: %v", err)
		}

		out := stdout.String()
		if !strings.Contains(out, "conv-101") || !strings.Contains(out, "PlayStation 5") {
			t.Errorf("pretty table output missing unread conversation: %s", out)
		}
		if strings.Contains(out, "Nintendo Switch") {
			t.Errorf("pretty table should not include read conversation: %s", out)
		}
	}

	// 4. When no unread conversations exist
	{
		emptyTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			list := milanuncios.ConversationList{
				{
					ID:     "conv-read-only",
					Unread: false,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
		}))
		defer emptyTS.Close()

		// JSON output should be empty array []
		app := cli.NewApp("test")
		app.Client.BaseURL = emptyTS.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		cmd := app.RootCmd()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"chat", "list", "--unread-only", "--format", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat list empty failed: %v", err)
		}

		var list []milanuncios.Conversation
		if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected 0 conversations, got %d", len(list))
		}

		// Pretty output should say "No conversations found."
		stdout.Reset()
		cmd.SetArgs([]string{"chat", "list", "--unread-only", "--format", "pretty"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat list empty pretty failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "No conversations found.") {
			t.Errorf("expected 'No conversations found.', got: %s", stdout.String())
		}
	}
}
