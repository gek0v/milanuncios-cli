package watch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
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

	// 4. Verify watch was paused to prevent infinite removed event storm
	wReloaded, err := st.GetWatch(w.Name)
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
	mu         sync.Mutex
	dispatched []store.Event
}

func (m *mockSinkSender) Dispatch(ctx context.Context, ev store.Event) []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatched = append(m.dispatched, ev)
	return nil
}

func (m *mockSinkSender) getDispatched() []store.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]store.Event, len(m.dispatched))
	copy(cp, m.dispatched)
	return cp
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
	if err := st.AddWatch(w); err != nil {
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

	seenMap, err := st.GetSeenItems(w.ID)
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
	if len(sink.getDispatched()) != 1 {
		t.Fatalf("expected 1 dispatched event to sink, got %d", len(sink.getDispatched()))
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
	if len(sink.getDispatched()) != 2 {
		t.Fatalf("expected 2 total dispatched events to sink, got %d", len(sink.getDispatched()))
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

type trackingSinkSender struct {
	mu         sync.Mutex
	active     int
	peakActive int
	sleepDelay time.Duration
	blockOnCtx bool
	dispatched []store.Event
}

func (t *trackingSinkSender) Dispatch(ctx context.Context, ev store.Event) []error {
	t.mu.Lock()
	t.active++
	if t.active > t.peakActive {
		t.peakActive = t.active
	}
	t.dispatched = append(t.dispatched, ev)
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.active--
		t.mu.Unlock()
	}()

	if t.blockOnCtx {
		select {
		case <-ctx.Done():
			return []error{ctx.Err()}
		case <-time.After(2 * time.Second):
			return nil
		}
	}

	if t.sleepDelay > 0 {
		select {
		case <-ctx.Done():
			return []error{ctx.Err()}
		case <-time.After(t.sleepDelay):
		}
	}

	return nil
}

func TestEngineConcurrentSinkDispatchAndTimeout(t *testing.T) {
	var returnItems []milanuncios.AdItem
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(milanuncios.SearchResult{
			Items: returnItems,
			Total: len(returnItems),
		})
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "test_concurrent.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	eng := watch.NewEngine(st, client)
	tracker := &trackingSinkSender{sleepDelay: 50 * time.Millisecond}
	eng.Sinks = tracker

	w := &store.Watch{
		Type:   "search",
		Name:   "test-concurrent",
		Target: "test",
	}
	if err := st.AddWatch(w); err != nil {
		t.Fatalf("add watch: %v", err)
	}

	// 1. Initial run: seed 1 item so watch is no longer first run
	returnItems = []milanuncios.AdItem{
		{ID: "seed-1", Title: "Initial Item", Price: 100},
	}
	if _, err := eng.CheckWatch(context.Background(), w); err != nil {
		t.Fatalf("initial run failed: %v", err)
	}

	// 2. Add 6 new items (which will trigger 6 ad.new events)
	returnItems = []milanuncios.AdItem{
		{ID: "seed-1", Title: "Initial Item", Price: 100},
		{ID: "ad-1", Title: "Item 1", Price: 10},
		{ID: "ad-2", Title: "Item 2", Price: 20},
		{ID: "ad-3", Title: "Item 3", Price: 30},
		{ID: "ad-4", Title: "Item 4", Price: 40},
		{ID: "ad-5", Title: "Item 5", Price: 50},
		{ID: "ad-6", Title: "Item 6", Price: 60},
	}

	res, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("check watch: %v", err)
	}
	if len(res.Events) != 6 {
		t.Fatalf("expected 6 events, got %d", len(res.Events))
	}

	tracker.mu.Lock()
	peak := tracker.peakActive
	dispatchedCount := len(tracker.dispatched)
	tracker.mu.Unlock()

	if dispatchedCount != 6 {
		t.Fatalf("expected 6 dispatched events, got %d", dispatchedCount)
	}
	if peak <= 1 {
		t.Fatalf("expected peak concurrency > 1 for 6 events, got %d", peak)
	}

	// Verify events were marked as delivered
	events, err := st.ListEvents(w.ID, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, ev := range events {
		if !ev.Delivered {
			t.Errorf("expected event %d to be marked delivered", ev.ID)
		}
	}

	// 3. Test timeout: when sinks block, DispatchTimeout aborts within bounded time
	eng.DispatchTimeout = 60 * time.Millisecond
	blockingTracker := &trackingSinkSender{blockOnCtx: true}
	eng.Sinks = blockingTracker

	// Add another new item
	returnItems = append(returnItems, milanuncios.AdItem{ID: "ad-7", Title: "Item 7", Price: 70})

	start := time.Now()
	resTimeout, err := eng.CheckWatch(context.Background(), w)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("check watch with timeout failed: %v", err)
	}
	if len(resTimeout.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resTimeout.Events))
	}
	if duration >= 1500*time.Millisecond {
		t.Fatalf("expected dispatch to timeout quickly (~60ms), but took %v", duration)
	}
}
