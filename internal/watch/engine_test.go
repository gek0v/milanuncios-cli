package watch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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
	if err := st.AddWatch(context.Background(), w); err != nil {
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

	// 4. Fourth check: duplicate promoted ads in single search response should emit only 1 event
	returnItems = append(returnItems,
		milanuncios.AdItem{ID: "ad-promoted", Title: "MacBook Promoted", Price: 700},
		milanuncios.AdItem{ID: "ad-promoted", Title: "MacBook Promoted", Price: 700},
	)
	res4, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("fourth check failed: %v", err)
	}
	if res4.NewItems != 1 {
		t.Fatalf("expected exactly 1 new item event for duplicate ad, got %d", res4.NewItems)
	}

	// 5. Fifth check: price drop to 0 € (free/gratis) should trigger price alert
	returnItems[0].Price = 0
	res5, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("fifth check failed: %v", err)
	}
	if res5.PriceAlerts != 1 {
		t.Fatalf("expected 1 price alert for price drop to 0, got %d", res5.PriceAlerts)
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
	if err := st.AddWatch(context.Background(), w); err != nil {
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

	// 4. Verify watch was paused to prevent infinite removed event storm
	wReloaded, err := st.GetWatch(context.Background(), w.Name)
	if err != nil {
		t.Fatalf("failed to reload watch: %v", err)
	}
	if wReloaded.Status != "paused" {
		t.Fatalf("expected watch status to be 'paused' after ad removal, got %q", wReloaded.Status)
	}

	// 5. Subsequent CheckAll should skip this paused watch
	results, err := eng.CheckAll(context.Background())
	if err != nil {
		t.Fatalf("CheckAll failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results from CheckAll for paused watch, got %d", len(results))
	}
}

type mockSinkSender struct {
	dispatched []store.Event
}

func (m *mockSinkSender) Dispatch(ctx context.Context, ev store.Event) []error {
	m.dispatched = append(m.dispatched, ev)
	return nil
}

func TestEngineFavoritesWatch(t *testing.T) {
	var currentFavs []milanuncios.AdItem

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/me/favorites" {
			_ = json.NewEncoder(w).Encode(currentFavs)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "favorites_test.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	// Initially no session token -> unauthenticated

	sink := &mockSinkSender{}
	eng := watch.NewEngine(st, client)
	eng.Sinks = sink

	w := &store.Watch{
		Type:   "favorites",
		Name:   "my-favorites",
		Target: "favorites",
	}
	if err := st.AddWatch(context.Background(), w); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}

	// 1. Unauthenticated check should fail with KindAuth error
	_, err = eng.CheckWatch(context.Background(), w)
	if err == nil {
		t.Fatal("expected error on unauthenticated favorites check, got nil")
	}

	// Now set session token
	client.SessionToken = "valid-token-123"

	// 2. Initial baseline check
	currentFavs = []milanuncios.AdItem{
		{ID: "fav-1", Title: "VW Golf TDI", Price: 5000, URL: "https://milanuncios.com/anuncio/fav-1"},
		{ID: "fav-2", Title: "Bicicleta Gravel", Price: 800, URL: "https://milanuncios.com/anuncio/fav-2"},
	}

	res1, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("baseline check failed: %v", err)
	}
	if res1.NewItems != 2 {
		t.Fatalf("expected 2 new items recorded, got %d", res1.NewItems)
	}
	if len(res1.Events) != 0 {
		t.Fatalf("expected 0 events on baseline run, got %d", len(res1.Events))
	}

	seenMap, err := st.GetSeenItems(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("reading seen items: %v", err)
	}
	if len(seenMap) != 2 || seenMap["fav-1"].LastPrice != 5000 {
		t.Fatalf("unexpected seen map: %+v", seenMap)
	}

	// 3. Price drops on fav-1 (from 5000 to 4500)
	currentFavs[0].Price = 4500
	res2, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("price drop check failed: %v", err)
	}
	if res2.PriceAlerts != 1 || len(res2.Events) != 1 {
		t.Fatalf("expected 1 price alert event, got %d (alerts: %d)", len(res2.Events), res2.PriceAlerts)
	}
	ev := res2.Events[0]
	if ev.EventType != "ad.price_changed" || ev.AdID != "fav-1" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
		t.Fatalf("failed to parse event payload: %v", err)
	}
	if payload["old_price"] != float64(5000) || payload["new_price"] != float64(4500) {
		t.Fatalf("unexpected payload prices: %+v", payload)
	}
	if payload["title"] != "VW Golf TDI" || payload["url"] != "https://milanuncios.com/anuncio/fav-1" {
		t.Fatalf("missing title or url in payload: %+v", payload)
	}
	if len(sink.dispatched) != 1 {
		t.Fatalf("expected 1 dispatched event to sink, got %d", len(sink.dispatched))
	}

	// 4. Item removed from favorites (fav-2 is removed)
	currentFavs = []milanuncios.AdItem{currentFavs[0]} // only fav-1 remains
	res3, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("item removal check failed: %v", err)
	}
	if res3.Removed != 1 || len(res3.Events) != 1 {
		t.Fatalf("expected 1 removed event, got %d (events: %+v)", res3.Removed, res3.Events)
	}
	if res3.Events[0].EventType != "ad.removed" || res3.Events[0].AdID != "fav-2" {
		t.Fatalf("unexpected removed event: %+v", res3.Events[0])
	}
	if len(sink.dispatched) != 2 {
		t.Fatalf("expected 2 total dispatched events to sink, got %d", len(sink.dispatched))
	}

	// 5. Subsequent run should not trigger ad.removed again
	res4, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("subsequent check failed: %v", err)
	}
	if res4.Removed != 0 || len(res4.Events) != 0 {
		t.Fatalf("expected 0 events on subsequent run, got %d", len(res4.Events))
	}
}

func TestEngine_CadenceAndCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(milanuncios.SearchResult{
			Items: []milanuncios.AdItem{{ID: "1", Title: "Item 1", Price: 100}},
			Total: 1,
		})
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "cadence.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	eng := watch.NewEngine(st, client)

	now := time.Now().UTC()
	checkedRecently := now.Add(-10 * time.Second) // 10s ago
	checkedLongAgo := now.Add(-70 * time.Second)  // 70s ago

	// w1: interval 300s, checked 10s ago -> should NOT run
	w1 := &store.Watch{
		Type:          "search",
		Name:          "w1-cadence-wait",
		Target:        "search-1",
		IntervalSec:   300,
		LastCheckedAt: &checkedRecently,
		Status:        "active",
	}
	// w2: interval 60s, checked 70s ago -> SHOULD run
	w2 := &store.Watch{
		Type:          "search",
		Name:          "w2-cadence-due",
		Target:        "search-2",
		IntervalSec:   60,
		LastCheckedAt: &checkedLongAgo,
		Status:        "active",
	}
	// w3: interval 300s, LastCheckedAt is nil (first run) -> SHOULD run
	w3 := &store.Watch{
		Type:        "search",
		Name:        "w3-first-run",
		Target:      "search-3",
		IntervalSec: 300,
		Status:      "active",
	}

	ctx := context.Background()
	_ = st.AddWatch(ctx, w1)
	_ = st.AddWatch(ctx, w2)
	_ = st.AddWatch(ctx, w3)
	_ = st.UpdateWatchChecked(ctx, w1.ID, checkedRecently)
	_ = st.UpdateWatchChecked(ctx, w2.ID, checkedLongAgo)

	results, err := eng.CheckAll(ctx)
	if err != nil {
		t.Fatalf("CheckAll failed: %v", err)
	}

	// Only w2 and w3 should have run
	if len(results) != 2 {
		t.Fatalf("expected 2 results respecting cadence, got %d", len(results))
	}
	ranNames := map[string]bool{
		results[0].WatchName: true,
		results[1].WatchName: true,
	}
	if !ranNames["w2-cadence-due"] || !ranNames["w3-first-run"] {
		t.Fatalf("unexpected watches executed: %+v", ranNames)
	}

	// Verify cancellation: canceled context should return ctx.Err() cleanly and run 0 watches
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	canceledResults, cancelErr := eng.CheckAll(canceledCtx)
	if cancelErr == nil {
		t.Fatalf("expected context canceled error, got nil")
	}
	if len(canceledResults) != 0 {
		t.Fatalf("expected 0 results on canceled context, got %d", len(canceledResults))
	}
}

type mockPerSinkSender struct {
	names      []string
	failSinks  map[string]bool
	deliveries map[string][]store.Event
}

func newMockPerSinkSender(names []string) *mockPerSinkSender {
	return &mockPerSinkSender{
		names:      names,
		failSinks:  make(map[string]bool),
		deliveries: make(map[string][]store.Event),
	}
}

func (m *mockPerSinkSender) SinkNames() []string {
	return m.names
}

func (m *mockPerSinkSender) Dispatch(ctx context.Context, ev store.Event) []error {
	var errs []error
	for _, n := range m.names {
		if err := m.SendTo(ctx, n, ev); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (m *mockPerSinkSender) SendTo(ctx context.Context, name string, ev store.Event) error {
	if m.failSinks[name] {
		return fmt.Errorf("sink %s simulated failure", name)
	}
	m.deliveries[name] = append(m.deliveries[name], ev)
	return nil
}

func TestEngine_PartialDeliveryRetryQueue(t *testing.T) {
	var currentPrice float64 = 100
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(milanuncios.SearchResult{
			Items: []milanuncios.AdItem{{ID: "item-retry", Title: "Reloj", Price: currentPrice}},
			Total: 1,
		})
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "retry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	eng := watch.NewEngine(st, client)

	// Configure mock per-sink sender with 2 sinks: sink-ok and sink-flaky
	sinkSender := newMockPerSinkSender([]string{"sink-ok", "sink-flaky"})
	eng.Sinks = sinkSender

	ctx := context.Background()
	w := &store.Watch{
		Type:   "search",
		Name:   "retry-watch",
		Target: "reloj",
	}
	if err := st.AddWatch(ctx, w); err != nil {
		t.Fatalf("add watch: %v", err)
	}

	// 1. First run establishes baseline seen items (no events)
	if _, err := eng.CheckWatch(ctx, w); err != nil {
		t.Fatalf("baseline check failed: %v", err)
	}

	// 2. Make sink-flaky fail, and trigger a price change event
	sinkSender.failSinks["sink-flaky"] = true
	currentPrice = 90 // price drop triggers event

	res, err := eng.CheckWatch(ctx, w)
	if err != nil {
		t.Fatalf("price drop check: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	evID := res.Events[0].ID

	// Verify sink-ok received the event, but sink-flaky failed
	if len(sinkSender.deliveries["sink-ok"]) != 1 {
		t.Fatalf("expected sink-ok to receive event, got %d", len(sinkSender.deliveries["sink-ok"]))
	}
	if len(sinkSender.deliveries["sink-flaky"]) != 0 {
		t.Fatalf("expected sink-flaky to receive 0 successful deliveries, got %d", len(sinkSender.deliveries["sink-flaky"]))
	}

	// Check SQLite events table: event must be delivered = 0
	undelivered, err := st.GetUndeliveredEvents(ctx, w.ID, 10)
	if err != nil || len(undelivered) != 1 {
		t.Fatalf("expected 1 undelivered event in store, got %d (err: %v)", len(undelivered), len(undelivered))
	}
	if undelivered[0].Delivered {
		t.Fatalf("expected event delivered to be false")
	}

	// Check deliveries table
	delivs, err := st.GetEventDeliveries(ctx, evID)
	if err != nil || len(delivs) != 2 {
		t.Fatalf("expected 2 delivery records, got %d", len(delivs))
	}

	// 3. Fix sink-flaky. Configure RetryBackoff to 0 for instant retry in test
	sinkSender.failSinks["sink-flaky"] = false
	eng.RetryBackoff = func(attempts int) time.Duration { return 0 }

	// In the next check, retryUndeliveredForWatch runs
	if _, err := eng.CheckWatch(ctx, w); err != nil {
		t.Fatalf("retry check failed: %v", err)
	}

	// Verify sink-ok was NOT duplicated (still 1 delivery)
	if len(sinkSender.deliveries["sink-ok"]) != 1 {
		t.Fatalf("sink-ok received duplicate delivery! got %d, expected 1", len(sinkSender.deliveries["sink-ok"]))
	}
	// Verify sink-flaky was retried and received delivery
	if len(sinkSender.deliveries["sink-flaky"]) != 1 {
		t.Fatalf("sink-flaky was not retried! got %d, expected 1", len(sinkSender.deliveries["sink-flaky"]))
	}

	// Check SQLite events table: event should now be marked delivered = 1!
	undeliveredAfter, err := st.GetUndeliveredEvents(ctx, w.ID, 10)
	if err != nil || len(undeliveredAfter) != 0 {
		t.Fatalf("expected 0 undelivered events after retry, got %d", len(undeliveredAfter))
	}
}

func TestEngine_BackoffRespected(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "backoff.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	eng := watch.NewEngine(st, client)
	eng.RetryBackoff = func(attempts int) time.Duration {
		return 10 * time.Minute // require 10 minutes wait
	}

	sinkSender := newMockPerSinkSender([]string{"flaky"})
	sinkSender.failSinks["flaky"] = true
	eng.Sinks = sinkSender

	ctx := context.Background()
	w := &store.Watch{
		Type:   "search",
		Name:   "backoff-watch",
		Target: "test",
	}
	_ = st.AddWatch(ctx, w)

	ev := &store.Event{
		WatchID:     w.ID,
		EventType:   "ad.new",
		AdID:        "item-1",
		PayloadJSON: `{"ad":{"title":"Test"}}`,
		CreatedAt:   time.Now().UTC(),
		Delivered:   false,
	}
	_ = st.RecordEvent(ctx, ev)
	_ = st.RecordDeliveryAttempt(ctx, ev.ID, "flaky", fmt.Errorf("initial failure"))

	// Now unblock flaky sink, but run CheckWatch immediately (0s elapsed < 10m backoff)
	sinkSender.failSinks["flaky"] = false

	// Retry should be skipped because backoff has not elapsed
	_, _ = eng.CheckWatch(ctx, w)
	if len(sinkSender.deliveries["flaky"]) != 0 {
		t.Fatalf("expected 0 deliveries while in backoff cooldown, got %d", len(sinkSender.deliveries["flaky"]))
	}

	// Now lower backoff requirement to 0
	eng.RetryBackoff = func(attempts int) time.Duration { return 0 }
	_, _ = eng.CheckWatch(ctx, w)
	if len(sinkSender.deliveries["flaky"]) != 1 {
		t.Fatalf("expected delivery after backoff expired, got %d", len(sinkSender.deliveries["flaky"]))
	}
}
