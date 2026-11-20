package sink_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/sink"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/gek0v/milanuncios-cli/internal/telegram"
)

func TestTelegramSink_AdNew(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)

		resp := map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 101,
				"chat": map[string]any{
					"id": 123456,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tgClient := telegram.NewClient("token", telegram.WithBaseURL(ts.URL))
	s, err := sink.NewTelegramSink("test-tg", tgClient, []int64{123456}, nil)
	if err != nil {
		t.Fatalf("failed to create telegram sink: %v", err)
	}

	ev := store.Event{
		ID:        1,
		EventType: "ad.new",
		AdID:      "492817294",
		PayloadJSON: `{
			"event": "ad.new",
			"ad": {
				"id": "492817294",
				"title": "Bicicleta Cannondale Topstone 1",
				"price": 950.0,
				"url": "https://www.milanuncios.com/bicicletas/cannondale-492817294.htm",
				"location": {
					"province": "Madrid",
					"municipality": "Alcobendas"
				}
			}
		}`,
		CreatedAt: time.Now(),
	}

	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	text, _ := receivedBody["text"].(string)
	if !strings.Contains(text, "Nuevo anuncio en Milanuncios") {
		t.Errorf("expected header in text: %s", text)
	}
	if !strings.Contains(text, "Bicicleta Cannondale Topstone 1") {
		t.Errorf("expected title in text: %s", text)
	}
	if !strings.Contains(text, "950 €") {
		t.Errorf("expected price in text: %s", text)
	}
	if !strings.Contains(text, "Alcobendas, Madrid") {
		t.Errorf("expected location in text: %s", text)
	}
	if !strings.Contains(text, "https://www.milanuncios.com/bicicletas/cannondale-492817294.htm") {
		t.Errorf("expected link in text: %s", text)
	}

	// Verify inline keyboard markup
	replyMarkup, ok := receivedBody["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("expected reply_markup in request: %v", receivedBody)
	}
	keyboard, ok := replyMarkup["inline_keyboard"].([]any)
	if !ok || len(keyboard) == 0 {
		t.Fatalf("expected inline_keyboard buttons: %v", replyMarkup)
	}
}

func TestTelegramSink_PriceChanged(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)

		resp := map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 102,
				"chat": map[string]any{
					"id": 123456,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tgClient := telegram.NewClient("token", telegram.WithBaseURL(ts.URL))
	s, err := sink.NewTelegramSink("test-tg", tgClient, []int64{123456}, nil)
	if err != nil {
		t.Fatalf("failed to create telegram sink: %v", err)
	}

	ev := store.Event{
		ID:        2,
		EventType: "ad.price_changed",
		AdID:      "492817294",
		PayloadJSON: `{
			"event": "ad.price_changed",
			"old_price": 1100.0,
			"new_price": 950.0,
			"diff": -150.0,
			"ad": {
				"id": "492817294",
				"title": "Bicicleta Cannondale Topstone 1",
				"price": 950.0,
				"url": "https://www.milanuncios.com/bicicletas/cannondale-492817294.htm"
			}
		}`,
		CreatedAt: time.Now(),
	}

	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	text, _ := receivedBody["text"].(string)
	if !strings.Contains(text, "Bajada de precio en Milanuncios") {
		t.Errorf("expected price drop header: %s", text)
	}
	if !strings.Contains(text, "950 €") || !strings.Contains(text, "1100 €") || !strings.Contains(text, "-150 €") {
		t.Errorf("expected price diff details in text: %s", text)
	}
}

func TestTelegramSink_AdRemoved(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)

		resp := map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 103,
				"chat": map[string]any{
					"id": 123456,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tgClient := telegram.NewClient("token", telegram.WithBaseURL(ts.URL))
	s, err := sink.NewTelegramSink("test-tg", tgClient, []int64{123456}, nil)
	if err != nil {
		t.Fatalf("failed to create telegram sink: %v", err)
	}

	ev := store.Event{
		ID:          3,
		EventType:   "ad.removed",
		AdID:        "492817294",
		PayloadJSON: `{"event":"ad.removed","ad_id":"492817294"}`,
		CreatedAt:   time.Now(),
	}

	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	text, _ := receivedBody["text"].(string)
	if !strings.Contains(text, "Anuncio retirado") || !strings.Contains(text, "492817294") {
		t.Errorf("expected removed notice with ad id: %s", text)
	}
}

func TestTelegramSink_ChatMessageReceived_WithStoreMapping(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)

		resp := map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 4892,
				"chat": map[string]any{
					"id": 987654321,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	tgClient := telegram.NewClient("token", telegram.WithBaseURL(ts.URL))
	s, err := sink.NewTelegramSink("test-tg", tgClient, []int64{987654321}, st)
	if err != nil {
		t.Fatalf("failed to create telegram sink: %v", err)
	}

	ev := store.Event{
		ID:        4,
		EventType: "chat.message_received",
		AdID:      "492817294",
		PayloadJSON: `{
			"event_type": "chat.message_received",
			"conversation_id": "conv-8849201",
			"ad_id": "492817294",
			"ad_title": "Bicicleta Cannondale",
			"ad_price": 1250.0,
			"sender_name": "Carlos M.",
			"text": "¿Sigue disponible? Me interesa recogerla mañana.",
			"sent_at": "2026-11-20T14:30:00Z"
		}`,
		CreatedAt: time.Now(),
	}

	if err := s.Send(ctx, ev); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	// Verify formatted text
	text, _ := receivedBody["text"].(string)
	if !strings.Contains(text, "Mensaje de Carlos M.") {
		t.Errorf("expected sender in text: %s", text)
	}
	if !strings.Contains(text, "Bicicleta Cannondale (1250 €)") {
		t.Errorf("expected ad title & price in text: %s", text)
	}
	if !strings.Contains(text, "¿Sigue disponible? Me interesa recogerla mañana.") {
		t.Errorf("expected message body in text: %s", text)
	}
	if !strings.Contains(text, "Desliza este mensaje para responder directamente") {
		t.Errorf("expected reply hint in text: %s", text)
	}

	// Verify mapping saved in SQLite telegram_message_map
	mapping, err := st.GetTelegramMessageMap(ctx, 4892)
	if err != nil {
		t.Fatalf("expected mapping for msgID 4892 in sqlite store: %v", err)
	}
	if mapping.ConversationID != "conv-8849201" {
		t.Errorf("expected convID 'conv-8849201', got %q", mapping.ConversationID)
	}
	if mapping.AdID != "492817294" {
		t.Errorf("expected adID '492817294', got %q", mapping.AdID)
	}
	if mapping.AdTitle != "Bicicleta Cannondale" {
		t.Errorf("expected adTitle 'Bicicleta Cannondale', got %q", mapping.AdTitle)
	}
	if mapping.OpponentName != "Carlos M." {
		t.Errorf("expected opponentName 'Carlos M.', got %q", mapping.OpponentName)
	}
}

func TestTelegramSink_FromConfig(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"ok": true, "result": map[string]any{"message_id": 1}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// 1. Success from config with ChatID and BotToken
	cfg := config.SinkConfig{
		Type:     "telegram",
		URL:      ts.URL,
		ChatID:   12345,
		BotToken: "secret-bot-token",
	}
	s, err := sink.FromConfig("my-telegram", cfg)
	if err != nil {
		t.Fatalf("unexpected error creating sink from config: %v", err)
	}
	if s.Type() != "telegram" {
		t.Errorf("expected type 'telegram', got %q", s.Type())
	}
	if s.Name() != "my-telegram" {
		t.Errorf("expected name 'my-telegram', got %q", s.Name())
	}

	// 2. Fallback to MILANUNCIOS_TELEGRAM_TOKEN env var
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "env-token-xyz")
	cfgNoToken := config.SinkConfig{
		Type:    "telegram",
		URL:     ts.URL,
		ChatIDs: []int64{12345, 67890},
	}
	s2, err := sink.FromConfig("env-tg", cfgNoToken)
	if err != nil {
		t.Fatalf("unexpected error with env token: %v", err)
	}
	if s2 == nil {
		t.Fatal("expected non-nil sink")
	}

	// 3. Error when missing chat_id
	cfgNoChat := config.SinkConfig{
		Type:     "telegram",
		BotToken: "token",
	}
	_, err = sink.FromConfig("no-chat", cfgNoChat)
	if err == nil {
		t.Fatal("expected error when chat_id is missing")
	}

	// 4. Error when missing bot token
	t.Setenv("MILANUNCIOS_TELEGRAM_TOKEN", "")
	cfgNoTok := config.SinkConfig{
		Type:   "telegram",
		ChatID: 12345,
	}
	_, err = sink.FromConfig("no-tok", cfgNoTok)
	if err == nil {
		t.Fatal("expected error when bot token is missing")
	}
}
