package eval_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/eval"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

func TestExtractSearchTokens(t *testing.T) {
	tokens := eval.ExtractSearchTokens("Cannondale Topstone 1 Gravel (Talla M)")
	expected := []string{"cannondale", "topstone", "1", "gravel", "talla", "m"}
	if len(tokens) < 4 {
		t.Errorf("expected at least 4 extracted tokens, got %v", tokens)
	}
	if tokens[0] != "cannondale" || tokens[1] != "topstone" {
		t.Errorf("unexpected initial tokens: %v (want %v)", tokens, expected)
	}

	// Stop words filtering
	tokensWithStop := eval.ExtractSearchTokens("Bicicleta de montaña en Madrid con frenos de disco")
	for _, tk := range tokensWithStop {
		if tk == "de" || tk == "en" || tk == "con" {
			t.Errorf("stop word %q not filtered out in %v", tk, tokensWithStop)
		}
	}
}

func TestEvaluateAd_Success(t *testing.T) {
	targetAdID := "492817294"
	targetAd := milanuncios.AdItem{
		ID:         targetAdID,
		Title:      "Cannondale Topstone 1 Gravel (Talla M)",
		Price:      950.0,
		Category:   1102,
		CategoryID: 1102,
		URL:        "https://www.milanuncios.com/bicicletas/cannondale-topstone-1-492817294.htm",
	}

	// Generate 20 comparable ads with median around 1200€, plus outliers
	sampleAds := []milanuncios.AdItem{
		{ID: targetAdID, Title: targetAd.Title, Price: 950.0},       // target ad should be skipped in sample
		{ID: "dummy-1", Title: "Fake 1€ ad", Price: 1.0},            // outlier < 5.0
		{ID: "dummy-crazy", Title: "Extreme price", Price: 99999.0}, // extreme outlier
	}

	// 15 realistic listings around 1100 - 1300€
	prices := []float64{
		1050, 1100, 1150, 1180, 1200, 1200, 1220, 1250,
		1250, 1280, 1300, 1320, 1350, 1380, 1400,
	}
	for i, p := range prices {
		sampleAds = append(sampleAds, milanuncios.AdItem{
			ID:       fmt.Sprintf("ad-%d", i+1),
			Title:    fmt.Sprintf("Cannondale Topstone 1 talla %d", i%3+1),
			Price:    p,
			Category: 1102,
			URL:      fmt.Sprintf("https://www.milanuncios.com/bicicletas/ad-%d.htm", i+1),
		})
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/ads/"):
			_ = json.NewEncoder(w).Encode(targetAd)
		case r.URL.Path == "/api/v1/search":
			res := milanuncios.SearchResult{
				Items: sampleAds,
				Total: len(sampleAds),
			}
			_ = json.NewEncoder(w).Encode(res)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	evaluator := eval.NewEvaluator(client, nil)
	report, err := evaluator.EvaluateAd(context.Background(), targetAdID, true)
	if err != nil {
		t.Fatalf("EvaluateAd failed: %v", err)
	}

	if report.AdID != targetAdID {
		t.Errorf("expected ad_id %q, got %q", targetAdID, report.AdID)
	}
	if report.Price != 950.0 {
		t.Errorf("expected price 950.0, got %v", report.Price)
	}
	if report.MarketMedian != 1250.0 {
		t.Errorf("expected median 1250.0, got %v", report.MarketMedian)
	}
	if report.SampleSize != 15 {
		t.Errorf("expected sample size 15, got %d", report.SampleSize)
	}
	if report.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %v", report.Confidence)
	}
	if report.DealScore < 80 {
		t.Errorf("expected high deal score (>=80), got %d", report.DealScore)
	}
	if len(report.Comparables) == 0 {
		t.Errorf("expected non-empty comparables when includeComparables=true")
	}

	// Verify Pretty output
	var buf bytes.Buffer
	report.Pretty(&buf, false)
	prettyOut := buf.String()
	if !strings.Contains(prettyOut, "Cannondale Topstone 1") {
		t.Errorf("expected title in Pretty output, got:\n%s", prettyOut)
	}
	if !strings.Contains(prettyOut, "Deal Score:") {
		t.Errorf("expected 'Deal Score:' in Pretty output, got:\n%s", prettyOut)
	}
	if !strings.Contains(prettyOut, "BENCHMARK DE MERCADO:") {
		t.Errorf("expected benchmark in Pretty output, got:\n%s", prettyOut)
	}

	// Verify Colored Pretty output does not panic
	buf.Reset()
	report.Pretty(&buf, true)
	coloredOut := buf.String()
	if !strings.Contains(coloredOut, "\x1b[") {
		t.Errorf("expected ANSI color codes when color=true, got:\n%s", coloredOut)
	}
}

func TestEvaluateAd_InsufficientSample(t *testing.T) {
	targetAd := milanuncios.AdItem{
		ID:         "555001",
		Title:      "Rare Antique Clock",
		Price:      500.0,
		Category:   500,
		CategoryID: 500,
	}

	// Only 2 listings in market
	sampleAds := []milanuncios.AdItem{
		{ID: "cand-1", Title: "Antique Clock A", Price: 600.0},
		{ID: "cand-2", Title: "Antique Clock B", Price: 650.0},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/ads/") {
			_ = json.NewEncoder(w).Encode(targetAd)
		} else {
			_ = json.NewEncoder(w).Encode(milanuncios.SearchResult{
				Items: sampleAds,
				Total: len(sampleAds),
			})
		}
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	evaluator := eval.NewEvaluator(client, nil)
	report, err := evaluator.EvaluateAd(context.Background(), "555001", false)
	if err != nil {
		t.Fatalf("EvaluateAd failed: %v", err)
	}

	if report.Confidence >= 0.4 {
		t.Errorf("expected confidence < 0.4 for sample size 2, got %v", report.Confidence)
	}
	if !strings.Contains(report.Verdict, "Muestra insuficiente") {
		t.Errorf("expected verdict to indicate insufficient sample, got: %q", report.Verdict)
	}
}

func TestEvaluateAd_EmptyMarketSample(t *testing.T) {
	targetAd := milanuncios.AdItem{
		ID:         "888001",
		Title:      "Completely unique prototype",
		Price:      250.0,
		CategoryID: 999,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/ads/") {
			_ = json.NewEncoder(w).Encode(targetAd)
		} else {
			_ = json.NewEncoder(w).Encode(milanuncios.SearchResult{
				Items: nil,
				Total: 0,
			})
		}
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	evaluator := eval.NewEvaluator(client, nil)
	report, err := evaluator.EvaluateAd(context.Background(), "888001", false)
	if err != nil {
		t.Fatalf("EvaluateAd failed: %v", err)
	}

	if report.SampleSize != 0 {
		t.Errorf("expected sample size 0, got %d", report.SampleSize)
	}
	if report.DealScore != 50 {
		t.Errorf("expected default deal score 50 on empty sample, got %d", report.DealScore)
	}
	if report.Rating != eval.RatingFairPrice {
		t.Errorf("expected default fair_price on empty sample, got %v", report.Rating)
	}
	if !strings.Contains(report.Verdict, "insuficiente") {
		t.Errorf("expected verdict warning on empty sample, got: %q", report.Verdict)
	}
}

func TestEvaluateAd_ErrorHandling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	evaluator := eval.NewEvaluator(client, nil)
	_, err := evaluator.EvaluateAd(context.Background(), "123456", false)
	if err == nil {
		t.Fatalf("expected error from failed GetAd, got nil")
	}
}

func TestEvaluateAd_WithStorePriceHistory(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	// Add watch to satisfy foreign keys
	watch := &store.Watch{
		Type:        "ad",
		Name:        "cannondale_watch",
		Target:      "492817294",
		IntervalSec: 60,
		CreatedAt:   time.Now().UTC(),
		Status:      "active",
	}
	if err := st.AddWatch(ctx, watch); err != nil {
		t.Fatalf("add watch: %v", err)
	}

	// Insert historical events: ad.new and ad.price_changed
	ev1 := &store.Event{
		WatchID:   watch.ID,
		EventType: "ad.new",
		AdID:      "492817294",
		PayloadJSON: store.EncodeJSON(map[string]any{
			"event": "ad.new",
			"ad": map[string]any{
				"id":    "492817294",
				"price": 1100.0,
			},
		}),
		CreatedAt: time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC),
	}
	if err := st.RecordEvent(ctx, ev1); err != nil {
		t.Fatalf("record ev1: %v", err)
	}

	ev2 := &store.Event{
		WatchID:   watch.ID,
		EventType: "ad.price_changed",
		AdID:      "492817294",
		PayloadJSON: store.EncodeJSON(map[string]any{
			"event":     "ad.price_changed",
			"old_price": 1100.0,
			"new_price": 950.0,
			"diff":      -150.0,
		}),
		CreatedAt: time.Date(2026, 11, 18, 20, 25, 0, 0, time.UTC),
	}
	if err := st.RecordEvent(ctx, ev2); err != nil {
		t.Fatalf("record ev2: %v", err)
	}

	targetAd := milanuncios.AdItem{
		ID:         "492817294",
		Title:      "Cannondale Topstone 1",
		Price:      950.0,
		CategoryID: 1102,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/ads/") {
			_ = json.NewEncoder(w).Encode(targetAd)
		} else {
			_ = json.NewEncoder(w).Encode(milanuncios.SearchResult{
				Items: []milanuncios.AdItem{
					{ID: "other-1", Title: "Cannondale Topstone 1", Price: 1200.0},
					{ID: "other-2", Title: "Cannondale Topstone 1", Price: 1250.0},
				},
				Total: 2,
			})
		}
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	evaluator := eval.NewEvaluator(client, st)
	report, err := evaluator.EvaluateAd(ctx, "492817294", false)
	if err != nil {
		t.Fatalf("EvaluateAd failed: %v", err)
	}

	if len(report.PriceHistory) != 2 {
		t.Fatalf("expected 2 price history points, got %d (%v)", len(report.PriceHistory), report.PriceHistory)
	}
	if report.PriceHistory[0].Price != 1100.0 || report.PriceHistory[1].Price != 950.0 {
		t.Errorf("unexpected price history values: %v", report.PriceHistory)
	}

	// Verify pretty output renders price drop
	var buf bytes.Buffer
	report.Pretty(&buf, false)
	out := buf.String()
	if !strings.Contains(out, "HISTÓRICO DEL ANUNCIO:") {
		t.Errorf("expected HISTÓRICO DEL ANUNCIO in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Bajada de 150 €") {
		t.Errorf("expected 'Bajada de 150 €' in output, got:\n%s", out)
	}
}

func TestFormatEuro(t *testing.T) {
	tests := []struct {
		val      float64
		expected string
	}{
		{0.0, "0 €"},
		{950.0, "950 €"},
		{1200.0, "1.200 €"},
		{1050.0, "1.050 €"},
		{1500000.0, "1.500.000 €"},
		{950.50, "950,50 €"},
	}

	for _, tt := range tests {
		got := eval.FormatEuro(tt.val)
		if got != tt.expected {
			t.Errorf("FormatEuro(%v) = %q, want %q", tt.val, got, tt.expected)
		}
	}
}
