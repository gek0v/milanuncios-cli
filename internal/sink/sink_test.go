package sink_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	var gotTitle, gotTags, gotBody, gotClick string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		gotTags = r.Header.Get("Tags")
		gotClick = r.Header.Get("Click")
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
		PayloadJSON: `{"event":"ad.new","ad":{"title":"Bicicleta carretera","price":450,"url":"https://www.milanuncios.com/123456"}}`,
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
	if gotClick != "https://www.milanuncios.com/123456" {
		t.Errorf("unexpected click URL: %s", gotClick)
	}
}

func TestNtfySink_ClickHeaderSanitization(t *testing.T) {
	tests := []struct {
		name          string
		rawURL        string
		expectSet     bool
		expectedClick string
	}{
		{
			name:          "Valid https www.milanuncios.com",
			rawURL:        "https://www.milanuncios.com/motor/coche-123.htm",
			expectSet:     true,
			expectedClick: "https://www.milanuncios.com/motor/coche-123.htm",
		},
		{
			name:      "Phishing domain",
			rawURL:    "https://evil-phishing.com/steal-login",
			expectSet: false,
		},
		{
			name:      "Subdomain attack",
			rawURL:    "https://www.milanuncios.com.attacker.com/ad",
			expectSet: false,
		},
		{
			name:      "HTTP unencrypted",
			rawURL:    "http://www.milanuncios.com/ad-123.htm",
			expectSet: false,
		},
		{
			name:      "Javascript pseudo-protocol",
			rawURL:    "javascript:alert(1)",
			expectSet: false,
		},
		{
			name:      "Arbitrary app scheme",
			rawURL:    "intent://scan/#Intent;scheme=zxing;package=com.google.zxing.client.android;end",
			expectSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recordedClick string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recordedClick = r.Header.Get("Click")
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			s, err := sink.NewNtfySink("test-sanitization", ts.URL, "topic")
			if err != nil {
				t.Fatalf("failed to create sink: %v", err)
			}

			payload := fmt.Sprintf(`{"ad":{"title":"Test","price":10,"url":%q}}`, tt.rawURL)
			ev := store.Event{
				ID:          1,
				EventType:   "ad.new",
				AdID:        "123",
				PayloadJSON: payload,
			}

			if err := s.Send(context.Background(), ev); err != nil {
				t.Fatalf("send failed: %v", err)
			}

			if tt.expectSet {
				if recordedClick != tt.expectedClick {
					t.Errorf("expected Click header %q, got %q", tt.expectedClick, recordedClick)
				}
			} else {
				if recordedClick != "" {
					t.Errorf("expected empty Click header for unsafe URL %q, got %q", tt.rawURL, recordedClick)
				}
			}
		})
	}
}

type mockTransport struct {
	called bool
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.called = true
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)
	return rec.Result(), nil
}

func TestSink_WithTransport(t *testing.T) {
	tr := &mockTransport{}
	s, err := sink.NewNtfySink("test-proxy", "https://mock.example.com", "my-topic", sink.WithTransport(tr))
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}

	ev := store.Event{
		ID:          1,
		EventType:   "ad.new",
		AdID:        "100",
		PayloadJSON: `{"ad":{"title":"Item"}}`,
	}

	if err := s.Send(context.Background(), ev); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	if !tr.called {
		t.Fatalf("custom transport was not called by sink client")
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

	names := mgr.SinkNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 sink names, got %d", len(names))
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

	// DispatchPerSink with specific sink
	perSinkRes := mgr.DispatchPerSink(context.Background(), ev, []string{"ntfy-one"})
	if len(perSinkRes) != 1 || perSinkRes["ntfy-one"] != nil {
		t.Fatalf("unexpected DispatchPerSink result: %+v", perSinkRes)
	}

	// SendTo valid sink
	if err := mgr.SendTo(context.Background(), "hook-one", ev); err != nil {
		t.Fatalf("SendTo failed: %v", err)
	}

	// SendTo invalid sink
	if err := mgr.SendTo(context.Background(), "non-existent", ev); err == nil {
		t.Fatalf("expected error sending to non-existent sink")
	}
}
