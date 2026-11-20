package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/cli"
	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

func setupTestConfig(t *testing.T, allowedChatIDs []int64) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.toml")
	stateDB := filepath.Join(dir, "milanuncios.db")

	var chatIDsStr string
	if len(allowedChatIDs) > 0 {
		var parts []string
		for _, id := range allowedChatIDs {
			parts = append(parts, fmt.Sprintf("%d", id))
		}
		chatIDsStr = fmt.Sprintf("allowed_chat_ids = [%s]", strings.Join(parts, ", "))
	} else {
		chatIDsStr = "allowed_chat_ids = []"
	}

	cfgContent := fmt.Sprintf(`
[telegram]
enabled = true
%s
`, chatIDsStr)

	if err := os.WriteFile(configFile, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	return configFile, stateDB
}

func TestTelegramTest_Success(t *testing.T) {
	var sentToChat atomic.Int64

	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest-token/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         123456789,
					"is_bot":     true,
					"first_name": "Milanuncios Test Bot",
					"username":   "milanuncios_test_bot",
				},
			})
		case "/bottest-token/sendMessage":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if chatID, ok := body["chat_id"].(float64); ok {
				sentToChat.Store(int64(chatID))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": 1001,
					"date":       1700000000,
					"chat":       map[string]any{"id": 123456},
					"text":       "ping",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tgServer.Close()

	configFile, stateDB := setupTestConfig(t, []int64{123456})
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "test-token")
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "test", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("telegram test failed: %v", err)
	}

	if sentToChat.Load() != 123456 {
		t.Errorf("expected ping to chat 123456, got %d", sentToChat.Load())
	}

	var res cli.TelegramTestResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode json output: %v, raw: %s", err, stdout.String())
	}
	if !res.PingSent {
		t.Errorf("expected ping_sent to be true")
	}
	if res.Bot == nil || res.Bot.Username != "milanuncios_test_bot" {
		t.Errorf("unexpected bot in result: %+v", res.Bot)
	}
}

func TestTelegramTest_Pretty(t *testing.T) {
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest-token/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         123456789,
					"is_bot":     true,
					"first_name": "Milanuncios Test Bot",
					"username":   "milanuncios_test_bot",
				},
			})
		case "/bottest-token/sendMessage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 1001, "chat": map[string]any{"id": 123456}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tgServer.Close()

	configFile, stateDB := setupTestConfig(t, []int64{123456})
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "test-token")
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "test", "--format", "pretty"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("telegram test --format pretty failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Telegram Bot: @milanuncios_test_bot") {
		t.Errorf("expected pretty header with bot username, got: %s", out)
	}
	if !strings.Contains(out, "Status: connected (Test ping sent to 1 chat(s))") {
		t.Errorf("expected pretty ping status, got: %s", out)
	}
}

func TestTelegramTest_NoAllowedChats(t *testing.T) {
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/bottest-token/getMe" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         123456789,
					"is_bot":     true,
					"first_name": "Milanuncios Test Bot",
					"username":   "milanuncios_test_bot",
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer tgServer.Close()

	configFile, stateDB := setupTestConfig(t, nil) // no allowed chat IDs
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "test-token")
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "test", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("telegram test failed: %v", err)
	}

	var res cli.TelegramTestResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode json output: %v", err)
	}
	if res.PingSent {
		t.Errorf("expected ping_sent to be false when no chat IDs configured")
	}
}

func TestTelegramTest_MissingToken(t *testing.T) {
	configFile, stateDB := setupTestConfig(t, []int64{123456})
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "")

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "test"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error due to missing token, got nil")
	}
	if !strings.Contains(err.Error(), "telegram bot token is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTelegramRun_MissingToken(t *testing.T) {
	configFile, stateDB := setupTestConfig(t, []int64{123456})
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "")

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "run"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error due to missing token, got nil")
	}
	if !strings.Contains(err.Error(), "telegram bot token is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTelegramRun_MissingAllowedChatIDs(t *testing.T) {
	configFile, stateDB := setupTestConfig(t, nil) // no allowed chat IDs
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "test-token")

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "run"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error due to missing allowed_chat_ids, got nil")
	}
	if !strings.Contains(err.Error(), "telegram.allowed_chat_ids is required in config") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTelegramRun_SwipeToReply_Success(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var tgMu sync.Mutex
	var sentConfirmation string
	var replyToMsgID int64
	updateDelivered := false

	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest-token/getUpdates":
			tgMu.Lock()
			defer tgMu.Unlock()
			if !updateDelivered {
				updateDelivered = true
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 100,
							"message": map[string]any{
								"message_id": 9002,
								"date":       1700000010,
								"chat": map[string]any{
									"id":   123456,
									"type": "private",
								},
								"text": "Sí, todavía está disponible",
								"reply_to_message": map[string]any{
									"message_id": 4892,
									"date":       1699999900,
									"chat": map[string]any{
										"id":   123456,
										"type": "private",
									},
								},
							},
						},
					},
				})
				return
			}
			// Subsequent calls: empty result
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": []any{},
			})

		case "/bottest-token/sendMessage":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			tgMu.Lock()
			if text, ok := body["text"].(string); ok {
				sentConfirmation = text
			}
			if rep, ok := body["reply_to_message_id"].(float64); ok {
				replyToMsgID = int64(rep)
			}
			tgMu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 9003, "chat": map[string]any{"id": 123456}},
			})

			// Message sent back to Telegram: cancel context to exit daemon gracefully
			cancel()

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tgServer.Close()

	var maMu sync.Mutex
	var maConvID, maText string

	maServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/conversations/conv-8849201/messages" && r.Method == http.MethodPost {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			maMu.Lock()
			maConvID = "conv-8849201"
			maText = body["text"]
			maMu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "ma-msg-99",
				"text": body["text"],
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer maServer.Close()

	configFile, stateDB := setupTestConfig(t, []int64{123456})
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "test-token")
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)
	t.Setenv("MILANUNCIOS_BASE_URL", maServer.URL)
	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "valid-ma-token")

	// Pre-seed the telegram_message_map table
	st, err := store.Open(stateDB)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	err = st.SaveTelegramMessageMap(context.Background(), 4892, "conv-8849201", "ad-555", "Bicicleta Cannondale", "Carlos M.")
	_ = st.Close()
	if err != nil {
		t.Fatalf("failed to save telegram message map: %v", err)
	}

	app := cli.NewApp("test")
	app.Client.BaseURL = maServer.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"telegram", "run", "--timeout", "1"})

	execErr := cmd.Execute()
	if execErr == nil {
		t.Fatal("expected KindInterrupted error on cancellation, got nil")
	}

	maMu.Lock()
	convID := maConvID
	text := maText
	maMu.Unlock()

	if convID != "conv-8849201" || text != "Sí, todavía está disponible" {
		t.Errorf("unexpected milanuncios message sent: convID=%q, text=%q", convID, text)
	}

	tgMu.Lock()
	confText := sentConfirmation
	replyID := replyToMsgID
	tgMu.Unlock()

	if confText != "✓ Enviado a Carlos M." {
		t.Errorf("expected confirmation '✓ Enviado a Carlos M.', got %q", confText)
	}
	if replyID != 9002 {
		t.Errorf("expected reply_to_message_id 9002, got %d", replyID)
	}
}

func TestTelegramRun_SwipeToReply_MilanunciosError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var tgMu sync.Mutex
	var sentFailureNotice string
	var replyToMsgID int64
	updateDelivered := false

	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest-token/getUpdates":
			tgMu.Lock()
			defer tgMu.Unlock()
			if !updateDelivered {
				updateDelivered = true
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 100,
							"message": map[string]any{
								"message_id": 9002,
								"date":       1700000010,
								"chat": map[string]any{
									"id":   123456,
									"type": "private",
								},
								"text": "Sí, todavía está disponible",
								"reply_to_message": map[string]any{
									"message_id": 4892,
									"date":       1699999900,
									"chat": map[string]any{
										"id":   123456,
										"type": "private",
									},
								},
							},
						},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": []any{},
			})

		case "/bottest-token/sendMessage":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			tgMu.Lock()
			if text, ok := body["text"].(string); ok {
				sentFailureNotice = text
			}
			if rep, ok := body["reply_to_message_id"].(float64); ok {
				replyToMsgID = int64(rep)
			}
			tgMu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 9003, "chat": map[string]any{"id": 123456}},
			})

			cancel()

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tgServer.Close()

	maServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "milanuncios internal error"}`))
	}))
	defer maServer.Close()

	configFile, stateDB := setupTestConfig(t, []int64{123456})
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "test-token")
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)
	t.Setenv("MILANUNCIOS_BASE_URL", maServer.URL)
	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "valid-ma-token")

	st, err := store.Open(stateDB)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	err = st.SaveTelegramMessageMap(context.Background(), 4892, "conv-8849201", "ad-555", "Bicicleta Cannondale", "Carlos M.")
	_ = st.Close()
	if err != nil {
		t.Fatalf("failed to save telegram message map: %v", err)
	}

	app := cli.NewApp("test")
	app.Client.BaseURL = maServer.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"telegram", "run", "--timeout", "1"})

	_ = cmd.Execute()

	tgMu.Lock()
	failureText := sentFailureNotice
	replyID := replyToMsgID
	tgMu.Unlock()

	if !strings.Contains(failureText, "❌ Error al enviar mensaje") {
		t.Errorf("expected failure notice in telegram, got %q", failureText)
	}
	if replyID != 9002 {
		t.Errorf("expected reply_to_message_id 9002, got %d", replyID)
	}
}

func TestTelegramRun_UnauthorizedChatID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var tgCalls atomic.Int32
	var maCalls atomic.Int32
	updateDelivered := false

	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bottest-token/getUpdates":
			if !updateDelivered {
				updateDelivered = true
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 100,
							"message": map[string]any{
								"message_id": 9002,
								"date":       1700000010,
								"chat": map[string]any{
									"id":   999999, // Unauthorized!
									"type": "private",
								},
								"text": "Intento de hack",
								"reply_to_message": map[string]any{
									"message_id": 4892,
									"chat":       map[string]any{"id": 999999},
								},
							},
						},
					},
				})
				return
			}
			// Give a short moment before cancelling
			time.Sleep(50 * time.Millisecond)
			cancel()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": []any{},
			})

		case "/bottest-token/sendMessage":
			tgCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer tgServer.Close()

	maServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer maServer.Close()

	configFile, stateDB := setupTestConfig(t, []int64{123456}) // Allowed is 123456, not 999999
	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "test-token")
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)
	t.Setenv("MILANUNCIOS_BASE_URL", maServer.URL)
	t.Setenv("MILANUNCIOS_SESSION_TOKEN", "valid-ma-token")

	st, err := store.Open(stateDB)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	_ = st.SaveTelegramMessageMap(context.Background(), 4892, "conv-8849201", "ad-555", "Bicicleta Cannondale", "Carlos M.")
	_ = st.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = maServer.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"telegram", "run", "--timeout", "1"})

	_ = cmd.Execute()

	if maCalls.Load() != 0 {
		t.Errorf("expected 0 calls to Milanuncios for unauthorized chat, got %d", maCalls.Load())
	}
	if tgCalls.Load() != 0 {
		t.Errorf("expected 0 sendMessage calls to Telegram for unauthorized chat, got %d", tgCalls.Load())
	}

	if !strings.Contains(stderr.String(), "[telegram] ignoring message from unauthorized chat_id 999999") {
		t.Errorf("expected warning in stderr about unauthorized chat_id, got: %s", stderr.String())
	}
}

func TestTelegramSetup_Success(t *testing.T) {
	var sentToChat atomic.Int64

	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/botvalid-token/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         987654321,
					"is_bot":     true,
					"first_name": "Setup Bot",
					"username":   "setup_test_bot",
				},
			})
		case "/botvalid-token/sendMessage":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if chatID, ok := body["chat_id"].(float64); ok {
				sentToChat.Store(int64(chatID))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": 999,
					"date":       1700000000,
					"chat":       map[string]any{"id": 555123},
					"text":       "confirmation",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tgServer.Close()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.toml")
	credsFile := filepath.Join(dir, "credentials.toml")
	stateDB := filepath.Join(dir, "milanuncios.db")

	t.Setenv("MILANUNCIOS_CONFIG", configFile)
	t.Setenv("MILANUNCIOS_CREDENTIALS_FILE", credsFile)
	t.Setenv("MILANUNCIOS_STATE_DB", stateDB)
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "")

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "setup", "--token", "valid-token", "--chat-id", "555123", "--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("telegram setup failed: %v", err)
	}

	// Verify sent confirmation message
	if sentToChat.Load() != 555123 {
		t.Errorf("expected confirmation to chat 555123, got %d", sentToChat.Load())
	}

	// Verify JSON output
	var res cli.TelegramSetupResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode setup json output: %v, raw: %s", err, stdout.String())
	}
	if !res.Confirmed || res.Status != "configured" {
		t.Errorf("unexpected setup result: %+v", res)
	}
	if res.ChatID != 555123 {
		t.Errorf("expected chat_id 555123, got %d", res.ChatID)
	}
	if res.Bot == nil || res.Bot.Username != "setup_test_bot" {
		t.Errorf("unexpected bot info in setup result: %+v", res.Bot)
	}

	// Verify credentials.toml was written
	creds, err := config.LoadCredentials(credsFile)
	if err != nil {
		t.Fatalf("failed to load credentials file: %v", err)
	}
	if creds.Telegram.BotToken != "valid-token" {
		t.Errorf("expected bot_token 'valid-token' in credentials, got %q", creds.Telegram.BotToken)
	}

	// Verify config.toml was written
	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("failed to load config file: %v", err)
	}
	if !cfg.Telegram.Enabled {
		t.Errorf("expected telegram.enabled = true in config")
	}
	if len(cfg.Telegram.AllowedChatIDs) != 1 || cfg.Telegram.AllowedChatIDs[0] != 555123 {
		t.Errorf("expected allowed_chat_ids = [555123], got %v", cfg.Telegram.AllowedChatIDs)
	}
}

func TestTelegramSetup_Pretty(t *testing.T) {
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/botvalid-token/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         987654321,
					"is_bot":     true,
					"first_name": "Setup Bot",
					"username":   "setup_test_bot",
				},
			})
		case "/botvalid-token/sendMessage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": 999,
					"date":       1700000000,
					"chat":       map[string]any{"id": 555123},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tgServer.Close()

	dir := t.TempDir()
	t.Setenv("MILANUNCIOS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("MILANUNCIOS_CREDENTIALS_FILE", filepath.Join(dir, "credentials.toml"))
	t.Setenv("MILANUNCIOS_STATE_DB", filepath.Join(dir, "milanuncios.db"))
	t.Setenv("MILANUNCIOS_TELEGRAM_BASE_URL", tgServer.URL)

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "setup", "--token", "valid-token", "--chat-id", "555123", "--format", "pretty"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("telegram setup --format pretty failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Telegram Bot: @setup_test_bot") {
		t.Errorf("expected bot username in pretty output: %s", out)
	}
	if !strings.Contains(out, "Chat ID: 555123") || !strings.Contains(out, "Status: configured") {
		t.Errorf("expected chat id and configured status: %s", out)
	}
}

func TestTelegramSetup_MissingFlags(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MILANUNCIOS_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("MILANUNCIOS_CREDENTIALS_FILE", filepath.Join(dir, "credentials.toml"))
	t.Setenv("MILANUNCIOS_STATE_DB", filepath.Join(dir, "milanuncios.db"))
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "")

	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	// Missing token
	cmd := app.RootCmd()
	cmd.SetArgs([]string{"telegram", "setup", "--chat-id", "12345"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "telegram bot token is required") {
		t.Errorf("expected missing token error, got: %v", err)
	}

	// Missing chat id
	cmd = app.RootCmd()
	cmd.SetArgs([]string{"telegram", "setup", "--token", "some-token"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "telegram chat ID is required") {
		t.Errorf("expected missing chat-id error, got: %v", err)
	}
}
