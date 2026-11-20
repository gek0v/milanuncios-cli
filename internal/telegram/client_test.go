package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/telegram"
)

func TestNewClient_Options(t *testing.T) {
	customHTTP := &http.Client{Timeout: 5 * time.Second}
	c := telegram.NewClient("test-token",
		telegram.WithBaseURL("https://custom.telegram.org/"),
		telegram.WithHTTPClient(customHTTP),
		telegram.WithTimeout(15*time.Second),
	)

	if c.BaseURL != "https://custom.telegram.org" {
		t.Errorf("expected custom base URL, got %q", c.BaseURL)
	}
	if c.BotToken != "test-token" {
		t.Errorf("expected bot token 'test-token', got %q", c.BotToken)
	}
	if c.HTTP.Timeout != 15*time.Second {
		t.Errorf("expected 15s timeout, got %v", c.HTTP.Timeout)
	}
}

func TestClient_GetMe_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret-token/getMe" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		resp := map[string]any{
			"ok": true,
			"result": map[string]any{
				"id":         123456789,
				"is_bot":     true,
				"first_name": "Milanuncios Bot",
				"username":   "milanuncios_cli_bot",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := telegram.NewClient("secret-token", telegram.WithBaseURL(ts.URL))
	user, err := c.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.ID != 123456789 {
		t.Errorf("expected ID 123456789, got %d", user.ID)
	}
	if !user.IsBot {
		t.Errorf("expected IsBot to be true")
	}
	if user.FirstName != "Milanuncios Bot" {
		t.Errorf("expected first name 'Milanuncios Bot', got %q", user.FirstName)
	}
	if user.Username != "milanuncios_cli_bot" {
		t.Errorf("expected username 'milanuncios_cli_bot', got %q", user.Username)
	}
}

func TestClient_GetMe_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		resp := map[string]any{
			"ok":          false,
			"error_code":  401,
			"description": "Unauthorized: bot token is invalid",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := telegram.NewClient("invalid-token", telegram.WithBaseURL(ts.URL))
	user, err := c.GetMe(context.Background())
	if err == nil {
		t.Fatalf("expected error, got nil user: %v", user)
	}

	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *telegram.APIError, got %T: %v", err, err)
	}
	if apiErr.ErrorCode != 401 {
		t.Errorf("expected error code 401, got %d", apiErr.ErrorCode)
	}
	if !strings.Contains(apiErr.Description, "Unauthorized") {
		t.Errorf("unexpected description: %q", apiErr.Description)
	}
}

func TestClient_SendMessage_Success(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret-token/sendMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		_ = json.Unmarshal(b, &receivedBody)

		resp := map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 4892,
				"date":       1700000000,
				"chat": map[string]any{
					"id":   987654321,
					"type": "private",
				},
				"text": "Hello world",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := telegram.NewClient("secret-token", telegram.WithBaseURL(ts.URL))
	opts := &telegram.SendMessageOptions{
		ParseMode:        "HTML",
		ReplyToMessageID: 1001,
		ReplyMarkup: &telegram.InlineKeyboardMarkup{
			InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{
					{
						Text: "Ver Anuncio",
						URL:  "https://www.milanuncios.com/123",
					},
				},
			},
		},
		DisableWebPagePreview: true,
	}

	msg, err := c.SendMessage(context.Background(), 987654321, "<b>Hello world</b>", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.MessageID != 4892 {
		t.Errorf("expected message_id 4892, got %d", msg.MessageID)
	}
	if msg.Chat.ID != 987654321 {
		t.Errorf("expected chat id 987654321, got %d", msg.Chat.ID)
	}

	// Verify request payload sent to Telegram
	if chatID, ok := receivedBody["chat_id"].(float64); !ok || int64(chatID) != 987654321 {
		t.Errorf("unexpected chat_id in request: %v", receivedBody["chat_id"])
	}
	if text, ok := receivedBody["text"].(string); !ok || text != "<b>Hello world</b>" {
		t.Errorf("unexpected text in request: %v", receivedBody["text"])
	}
	if pm, ok := receivedBody["parse_mode"].(string); !ok || pm != "HTML" {
		t.Errorf("unexpected parse_mode: %v", receivedBody["parse_mode"])
	}
	if replyTo, ok := receivedBody["reply_to_message_id"].(float64); !ok || int64(replyTo) != 1001 {
		t.Errorf("unexpected reply_to_message_id: %v", receivedBody["reply_to_message_id"])
	}
	if d, ok := receivedBody["disable_web_page_preview"].(bool); !ok || !d {
		t.Errorf("expected disable_web_page_preview to be true")
	}
}

func TestClient_SendMessage_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]any{
			"ok":          false,
			"error_code":  400,
			"description": "Bad Request: chat not found",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := telegram.NewClient("secret-token", telegram.WithBaseURL(ts.URL))
	msg, err := c.SendMessage(context.Background(), 999999, "test", nil)
	if err == nil {
		t.Fatalf("expected error, got msg: %v", msg)
	}

	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *telegram.APIError, got %T: %v", err, err)
	}
	if apiErr.ErrorCode != 400 {
		t.Errorf("expected error code 400, got %d", apiErr.ErrorCode)
	}
	if !strings.Contains(apiErr.Description, "chat not found") {
		t.Errorf("unexpected description: %q", apiErr.Description)
	}
}

func TestClient_TokenRedactionInErrors(t *testing.T) {
	// Point to an invalid unreachable address so http.Client.Do returns a transport error containing URL
	secretToken := "very-secret-bot-token-12345"
	c := telegram.NewClient(secretToken, telegram.WithBaseURL("http://127.0.0.1:1"))

	_, err := c.GetMe(context.Background())
	if err == nil {
		t.Fatal("expected error connecting to closed port")
	}

	errStr := err.Error()
	if strings.Contains(errStr, secretToken) {
		t.Fatalf("CRITICAL SECURITY LEAK: bot token found in error message: %q", errStr)
	}
	if !strings.Contains(errStr, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in error message: %q", errStr)
	}
}

func TestClient_GetUpdates_Success(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret-token/getUpdates" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		_ = json.Unmarshal(b, &receivedBody)

		resp := map[string]any{
			"ok": true,
			"result": []map[string]any{
				{
					"update_id": 100,
					"message": map[string]any{
						"message_id": 501,
						"date":       1700000000,
						"chat": map[string]any{
							"id":   123456,
							"type": "private",
						},
						"text": "Hola",
					},
				},
				{
					"update_id": 101,
					"message": map[string]any{
						"message_id": 502,
						"date":       1700000010,
						"chat": map[string]any{
							"id":   123456,
							"type": "private",
						},
						"text": "Respondo al mensaje",
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
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := telegram.NewClient("secret-token", telegram.WithBaseURL(ts.URL))
	opts := &telegram.GetUpdatesOptions{
		Offset:         100,
		Limit:          50,
		Timeout:        30,
		AllowedUpdates: []string{"message", "callback_query"},
	}

	updates, err := c.GetUpdates(context.Background(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if updates[0].UpdateID != 100 || updates[0].Message.Text != "Hola" {
		t.Errorf("unexpected update 0: %+v", updates[0])
	}
	if updates[1].UpdateID != 101 || updates[1].Message.ReplyToMessage == nil || updates[1].Message.ReplyToMessage.MessageID != 4892 {
		t.Errorf("unexpected update 1: %+v", updates[1])
	}

	// Verify request payload sent to Telegram
	if off, ok := receivedBody["offset"].(float64); !ok || int64(off) != 100 {
		t.Errorf("unexpected offset: %v", receivedBody["offset"])
	}
	if lim, ok := receivedBody["limit"].(float64); !ok || int(lim) != 50 {
		t.Errorf("unexpected limit: %v", receivedBody["limit"])
	}
	if tout, ok := receivedBody["timeout"].(float64); !ok || int(tout) != 30 {
		t.Errorf("unexpected timeout: %v", receivedBody["timeout"])
	}
}

func TestClient_GetUpdates_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		resp := map[string]any{
			"ok":          false,
			"error_code":  409,
			"description": "Conflict: terminated by other getUpdates request",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := telegram.NewClient("secret-token", telegram.WithBaseURL(ts.URL))
	updates, err := c.GetUpdates(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error, got updates: %v", updates)
	}

	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *telegram.APIError, got %T: %v", err, err)
	}
	if apiErr.ErrorCode != 409 {
		t.Errorf("expected error code 409, got %d", apiErr.ErrorCode)
	}
}
