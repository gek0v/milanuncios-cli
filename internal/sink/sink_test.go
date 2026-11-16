package sink_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/sink"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

func TestNtfySink(t *testing.T) {
	var gotTitle, gotTags, gotBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		gotTags = r.Header.Get("Tags")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	s, err := sink.NewNtfySink("test-ntfy", ts.URL, "alerts-milanuncios")
	if err != nil {
		t.Fatalf("failed to create ntfy sink: %v", err)
	}

	ev := store.Event{
		ID:          1,
		EventType:   "ad.new",
		AdID:        "123456",
		PayloadJSON: `{"event":"ad.new","ad":{"title":"Bicicleta carretera","price":450,"url":"https://milanuncios.com/123456"}}`,
		CreatedAt:   time.Now(),
	}

	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatalf("failed to send ntfy notification: %v", err)
	}

	if gotTitle != "Milanuncios: Nuevo anuncio" {
		t.Errorf("unexpected title: %s", gotTitle)
	}
	if gotTags != "bell,package" {
		t.Errorf("unexpected tags: %s", gotTags)
	}
	if gotBody != "Bicicleta carretera (450 €)" {
		t.Errorf("unexpected body: %s", gotBody)
	}
}

func TestWebhookSink(t *testing.T) {
	var receivedAuth string
	var receivedEvent store.Event

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&receivedEvent)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token.txt")
	_ = os.WriteFile(tokenFile, []byte("secret-token-xyz"), 0600)

	s, err := sink.NewWebhookSink("test-hook", ts.URL, tokenFile)
	if err != nil {
		t.Fatalf("failed to create webhook sink: %v", err)
	}

	ev := store.Event{
		ID:          2,
		EventType:   "ad.price_changed",
		AdID:        "999",
		PayloadJSON: `{"old_price":100,"new_price":80}`,
		CreatedAt:   time.Now(),
	}

	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatalf("failed to send webhook: %v", err)
	}

	if receivedAuth != "Bearer secret-token-xyz" {
		t.Errorf("unexpected auth header: %s", receivedAuth)
	}
	if receivedEvent.ID != 2 || receivedEvent.EventType != "ad.price_changed" {
		t.Errorf("unexpected received event: %+v", receivedEvent)
	}
}

func TestManagerFromConfig(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfgSinks := map[string]config.SinkConfig{
		"ntfy-one": {
			Type:  "ntfy",
			Topic: "my-topic",
			URL:   ts.URL,
		},
		"hook-one": {
			Type: "webhook",
			URL:  ts.URL,
		},
	}

	mgr, err := sink.NewManager(cfgSinks)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if len(mgr.Sinks()) != 2 {
		t.Fatalf("expected 2 sinks, got %d", len(mgr.Sinks()))
	}

	ev := store.Event{
		ID:        1,
		EventType: "ad.new",
		AdID:      "111",
		CreatedAt: time.Now(),
	}

	errs := mgr.Dispatch(context.Background(), ev)
	if len(errs) != 0 {
		t.Fatalf("expected 0 dispatch errors, got: %v", errs)
	}
}
