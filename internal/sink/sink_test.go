package sink_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	t.Setenv("MILANUNCIOS_ALLOW_PRIVATE_IPS", "true")
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
	t.Setenv("MILANUNCIOS_ALLOW_PRIVATE_IPS", "true")
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

func TestWebhookSink_SSRFProtection(t *testing.T) {
	// Ensure MILANUNCIOS_ALLOW_PRIVATE_IPS is unset
	t.Setenv("MILANUNCIOS_ALLOW_PRIVATE_IPS", "")

	blockedURLs := []struct {
		name string
		url  string
	}{
		{"IPv4 Loopback", "http://127.0.0.1:8080/webhook"},
		{"Localhost hostname", "http://localhost:8080/webhook"},
		{"AWS Instance Metadata", "http://169.254.169.254/latest/meta-data/"},
		{"Cloud Link-Local Subnet", "http://169.254.10.20/endpoint"},
		{"RFC1918 Class A Subnet", "http://10.0.0.1:8080/alert"},
		{"RFC1918 Class B Subnet", "http://172.16.0.5:9000/alert"},
		{"RFC1918 Class C Subnet", "http://192.168.1.100/webhook"},
		{"IPv6 Loopback", "http://[::1]:8080/webhook"},
		{"Unsupported Scheme", "ftp://example.com/webhook"},
	}

	for _, tc := range blockedURLs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sink.NewWebhookSink("test-blocked", tc.url, "")
			if err == nil {
				t.Fatalf("expected NewWebhookSink to reject %s (%s)", tc.name, tc.url)
			}
			if !strings.Contains(err.Error(), "anti-SSRF policy") && !strings.Contains(err.Error(), "unsupported scheme") {
				t.Errorf("expected SSRF or scheme rejection error, got: %v", err)
			}
		})
	}

	// Valid public webhook URL should succeed
	s, err := sink.NewWebhookSink("test-public", "https://example.com/webhook", "")
	if err != nil {
		t.Fatalf("expected valid public webhook URL to succeed, got: %v", err)
	}
	if s == nil {
		t.Fatalf("expected non-nil WebhookSink")
	}
}
