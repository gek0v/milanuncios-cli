package milanuncios_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
)

func TestClientSearchSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("s") != "bicicleta" {
			t.Errorf("unexpected query 's': %s", r.URL.Query().Get("s"))
		}

		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{
					ID:       "1001",
					Title:    "Bicicleta de montaña",
					Price:    120,
					Currency: "€",
					Location: milanuncios.AdLocation{
						Province:     "Madrid",
						Municipality: "Alcalá",
					},
					Seller: milanuncios.AdSeller{
						Name: "Carlos",
						Type: "particular",
					},
					URL: "https://www.milanuncios.com/bicicletas/1001.htm",
				},
			},
			Total:    1,
			NextPage: "cursor-page-2",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	ctx := context.Background()
	res, err := client.Search(ctx, milanuncios.SearchQuery{Keywords: "bicicleta"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Items))
	}
	if res.Items[0].Title != "Bicicleta de montaña" {
		t.Errorf("unexpected item title: %s", res.Items[0].Title)
	}
	if res.NextPage != "cursor-page-2" {
		t.Errorf("unexpected next page: %s", res.NextPage)
	}

	var buf bytes.Buffer
	res.Pretty(&buf, false)
	out := buf.String()
	if !strings.Contains(out, "Bicicleta de montaña") || !strings.Contains(out, "120 €") {
		t.Errorf("expected pretty table to contain item info, got: %s", out)
	}
}

func TestClientWAFRetry(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// Simulate Cloudflare / Akamai blocking honest CLI UA
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><head><title>Attention Required! | Cloudflare</title></head><body>Access Denied</body></html>"))
			return
		}

		// Second attempt with browser UA succeeds
		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{ID: "2002", Title: "Mesa comedor", Price: 50},
			},
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	var noticeMsg string
	client.Notice = func(msg string) {
		noticeMsg = msg
	}

	res, err := client.Search(context.Background(), milanuncios.SearchQuery{})
	if err != nil {
		t.Fatalf("unexpected error after WAF retry: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Items))
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if !strings.Contains(noticeMsg, "retrying with browser user agent") {
		t.Errorf("expected notice message, got: %s", noticeMsg)
	}
}

func TestClientErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "404":
			w.WriteHeader(http.StatusNotFound)
		case "429":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limit exceeded"}`))
		case "invalid_json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not a valid json response"))
		}
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	// 404 test
	_, err := client.Search(context.Background(), milanuncios.SearchQuery{Keywords: "test", Next: "404"})
	// Using custom param by changing query or testing path
	// Let's pass keywords as the case selector if we set case in query
	u, _ := url.Parse(ts.URL)
	_ = u

	// Test 404
	{
		ts404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		client.BaseURL = ts404.URL
		_, err = client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error on 404, got nil")
		}
		if ok := output.Classify(err, "1.0", "search"); ok.Code != 4 {
			t.Fatalf("expected exit code 4 for not found, got %d", ok.Code)
		}
		ts404.Close()
	}

	// Test 429
	{
		ts429 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		client.BaseURL = ts429.URL
		_, err = client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error on 429, got nil")
		}
		if ok := output.Classify(err, "1.0", "search"); ok.Code != 5 {
			t.Fatalf("expected exit code 5 for rate limit, got %d", ok.Code)
		}
		ts429.Close()
	}

	// Test invalid JSON (Exit 7 - API Changed)
	{
		tsBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("malformed json payload"))
		}))
		client.BaseURL = tsBad.URL
		_, err = client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error on bad json, got nil")
		}
		if ok := output.Classify(err, "1.0", "search"); ok.Code != 7 {
			t.Fatalf("expected exit code 7 for api changed, got %d", ok.Code)
		}
		tsBad.Close()
	}
}
