package watch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/gek0v/milanuncios-cli/internal/watch"
)

func TestEngineSearchDiffing(t *testing.T) {
	var currentPrice float64 = 500
	var returnItems []milanuncios.AdItem

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		res := milanuncios.SearchResult{
			Items: returnItems,
			Total: len(returnItems),
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	eng := watch.NewEngine(st, client)

	w := &store.Watch{
		Type:   "search",
		Name:   "macbook-m1",
		Target: "macbook air m1",
	}
	if err := st.AddWatch(w); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}

	// 1. Initial check (baseline)
	returnItems = []milanuncios.AdItem{
		{ID: "ad-1", Title: "MacBook Air M1 8GB", Price: currentPrice},
	}
	res1, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("baseline check failed: %v", err)
	}
	if res1.NewItems != 0 || len(res1.Events) != 0 {
		t.Fatalf("expected 0 events on baseline run, got %d", len(res1.Events))
	}

	// 2. Second check: new ad appears
	returnItems = append(returnItems, milanuncios.AdItem{
		ID:    "ad-2",
		Title: "MacBook Air M1 16GB",
		Price: 650,
	})
	res2, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("second check failed: %v", err)
	}
	if res2.NewItems != 1 || len(res2.Events) != 1 {
		t.Fatalf("expected 1 new item event, got %d (events: %+v)", res2.NewItems, res2.Events)
	}
	if res2.Events[0].EventType != "ad.new" || res2.Events[0].AdID != "ad-2" {
		t.Fatalf("unexpected event: %+v", res2.Events[0])
	}

	// 3. Third check: price drops on ad-1
	currentPrice = 450
	returnItems[0].Price = currentPrice
	res3, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("third check failed: %v", err)
	}
	if res3.PriceAlerts != 1 || len(res3.Events) != 1 {
		t.Fatalf("expected 1 price alert, got %d", res3.PriceAlerts)
	}
	if res3.Events[0].EventType != "ad.price_changed" || res3.Events[0].AdID != "ad-1" {
		t.Fatalf("unexpected price alert event: %+v", res3.Events[0])
	}
}

func TestEngineAdDiffing(t *testing.T) {
	statusCode := http.StatusOK
	var itemPrice float64 = 200

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}

		item := milanuncios.AdItem{
			ID:    "999888",
			Title: "Taladro percutor Bosch",
			Price: itemPrice,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "test_ad.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	eng := watch.NewEngine(st, client)

	w := &store.Watch{
		Type:   "ad",
		Name:   "taladro",
		Target: "999888",
	}
	if err := st.AddWatch(w); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}

	// 1. Initial check
	res1, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("initial ad check failed: %v", err)
	}
	if len(res1.Events) != 0 {
		t.Fatalf("expected 0 events on first run, got %d", len(res1.Events))
	}

	// 2. Price change
	itemPrice = 175
	res2, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("price change check failed: %v", err)
	}
	if res2.PriceAlerts != 1 || len(res2.Events) != 1 {
		t.Fatalf("expected 1 price alert, got %d", res2.PriceAlerts)
	}

	// 3. Ad removed (404)
	statusCode = http.StatusNotFound
	res3, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("removed ad check failed: %v", err)
	}
	if res3.Removed != 1 || len(res3.Events) != 1 {
		t.Fatalf("expected 1 removed event, got %d", res3.Removed)
	}
	if res3.Events[0].EventType != "ad.removed" {
		t.Fatalf("expected ad.removed event, got %s", res3.Events[0].EventType)
	}
}
