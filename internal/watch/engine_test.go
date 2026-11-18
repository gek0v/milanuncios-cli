package watch_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
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

	// 1. Unauthenticated check should fail with KindAuth error
	wUnauth := &store.Watch{
		Type:   "favorites",
		Name:   "unauth-favorites",
		Target: "favorites",
	}
	if err := st.AddWatch(context.Background(), wUnauth); err != nil {
		t.Fatalf("failed to add unauth watch: %v", err)
	}
	_, err = eng.CheckWatch(context.Background(), wUnauth)
	if err == nil {
		t.Fatal("expected error on unauthenticated favorites check, got nil")
	}

	// Now set session token
	client.SessionToken = "valid-token-123"

	w := &store.Watch{
		Type:   "favorites",
		Name:   "my-favorites",
		Target: "favorites",
	}
	if err := st.AddWatch(context.Background(), w); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}

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

func TestEngineCheckAllAbortOnBlocked(t *testing.T) {
	reqCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Rate limited by Milanuncios"))
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

	var onBlockTriggered bool
	client.OnBlock = func(until time.Time) {
		onBlockTriggered = true
	}

	eng := watch.NewEngine(st, client)

	// Add 3 active watches
	for i := 1; i <= 3; i++ {
		w := &store.Watch{
			Type:   "search",
			Name:   fmt.Sprintf("watch-%d", i),
			Target: fmt.Sprintf("target-%d", i),
		}
		if err := st.AddWatch(context.Background(), w); err != nil {
			t.Fatalf("failed to add watch %d: %v", i, err)
		}
	}

	results, err := eng.CheckAll(context.Background())
	if err == nil {
		t.Fatal("expected error from CheckAll, got nil")
	}
	if !output.IsKind(err, output.KindBlocked) {
		t.Fatalf("expected KindBlocked error, got %v", err)
	}

	// Must have only made 1 request and aborted immediately
	if reqCount != 1 {
		t.Fatalf("expected exactly 1 request before aborting, got %d", reqCount)
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 successful results, got %d", len(results))
	}

	if !onBlockTriggered {
		t.Fatal("expected OnBlock hook to be triggered")
	}

	if !client.IsBlocked() {
		t.Fatal("expected client.IsBlocked() to be true")
	}
}

func TestEngineDiffingTransactionalRollback(t *testing.T) {
	var returnItems []milanuncios.AdItem
	var cancelOnRequest func()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cancelOnRequest != nil {
			cancelOnRequest()
		}
		w.Header().Set("Content-Type", "application/json")
		res := milanuncios.SearchResult{
			Items: returnItems,
			Total: len(returnItems),
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "tx_rollback.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	eng := watch.NewEngine(st, client)

	w := &store.Watch{
		Type:   "search",
		Name:   "tx-watch",
		Target: "tx target",
	}
	if err := st.AddWatch(context.Background(), w); err != nil {
		t.Fatalf("add watch: %v", err)
	}

	// 1. Baseline run with 1 item
	returnItems = []milanuncios.AdItem{
		{ID: "ad-base", Title: "Base ad", Price: 100},
	}
	if _, err := eng.CheckWatch(context.Background(), w); err != nil {
		t.Fatalf("baseline run failed: %v", err)
	}

	// 2. Next run: 10 new ads arrive, but context is canceled mid-flight
	returnItems = []milanuncios.AdItem{
		{ID: "ad-base", Title: "Base ad", Price: 100},
	}
	for i := 1; i <= 10; i++ {
		returnItems = append(returnItems, milanuncios.AdItem{
			ID:    fmt.Sprintf("ad-new-%d", i),
			Title: fmt.Sprintf("New ad %d", i),
			Price: float64(200 + i*10),
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelOnRequest = cancel // cancel context when HTTP search is being handled

	_, err = eng.CheckWatch(ctx, w)
	if err == nil {
		t.Fatal("expected error from canceled context during diffing, got nil")
	}

	// Verify rollback: seen items must still only contain "ad-base"
	seenMap, err := st.GetSeenItems(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("get seen items: %v", err)
	}
	if len(seenMap) != 1 {
		t.Fatalf("expected transaction rollback leaving only 1 seen item, but found %d items", len(seenMap))
	}
	if _, ok := seenMap["ad-base"]; !ok {
		t.Fatal("expected ad-base to be present")
	}

	// Events must be 0
	events, err := st.ListEvents(context.Background(), w.ID, 50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events after rollback, got %d", len(events))
	}

	// 3. Next normal run with healthy context: all 10 new ads should be successfully committed
	cancelOnRequest = nil
	resHealthy, err := eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("healthy run failed: %v", err)
	}
	if resHealthy.NewItems != 10 || len(resHealthy.Events) != 10 {
		t.Fatalf("expected 10 new items and 10 events, got %d items and %d events", resHealthy.NewItems, len(resHealthy.Events))
	}

	seenMapAfter, err := st.GetSeenItems(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("get seen items after healthy: %v", err)
	}
	if len(seenMapAfter) != 11 {
		t.Fatalf("expected 11 seen items total, got %d", len(seenMapAfter))
	}
}

func TestEngineAutomaticRetentionPruning(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{ID: "ad-active", Title: "Active ad", Price: 150},
			},
			Total: 1,
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "auto_prune.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	w := &store.Watch{
		Type:   "search",
		Name:   "prune-watch",
		Target: "prune target",
	}
	if err := st.AddWatch(context.Background(), w); err != nil {
		t.Fatalf("add watch: %v", err)
	}

	// Insert an expired seen item directly (45 days old)
	now := time.Now().UTC()
	oldItem := &store.SeenItem{
		WatchID:     w.ID,
		AdID:        "ad-expired",
		FirstSeenAt: now.Add(-50 * 24 * time.Hour),
		LastSeenAt:  now.Add(-45 * 24 * time.Hour),
		LastPrice:   50,
		LastTitle:   "Expired item",
		RawJSON:     `{}`,
	}
	if err := st.UpsertSeenItem(context.Background(), oldItem); err != nil {
		t.Fatalf("upsert old item: %v", err)
	}

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	eng := watch.NewEngine(st, client)
	eng.SeenItemsTTL = 30 * 24 * time.Hour

	// Check watch: this should automatically prune the expired item
	_, err = eng.CheckWatch(context.Background(), w)
	if err != nil {
		t.Fatalf("check watch failed: %v", err)
	}

	seenMap, err := st.GetSeenItems(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("get seen items: %v", err)
	}
	if err != nil {
		t.Fatalf("get seen items: %v", err)
	}
	if _, ok := seenMap["ad-expired"]; ok {
		t.Error("expected ad-expired to be pruned during CheckWatch")
	}
	if _, ok := seenMap["ad-active"]; !ok {
		t.Error("expected ad-active to be present in seen items")
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
	if err := st.AddWatch(context.Background(), w); err != nil {
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
	events, err := st.ListEvents(context.Background(), w.ID, 100)
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

func TestEngineFavoritesAuthExpiredPause(t *testing.T) {
	// 1. Server returns HTTP 401 Unauthorized
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "fav_expired.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("dummy-active-token")

	eng := watch.NewEngine(st, client)

	w := &store.Watch{
		Type:   "favorites",
		Name:   "my-favorites",
		Target: "favorites",
		Status: "active",
	}
	if err := st.AddWatch(context.Background(), w); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}

	// 1. Run check on watch returning HTTP 401
	_, err = eng.CheckWatch(context.Background(), w)
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}

	// 2. Verify watch was automatically paused in store
	reloaded, err := st.GetWatch(context.Background(), w.Name)
	if err != nil {
		t.Fatalf("failed to reload watch: %v", err)
	}
	if reloaded.Status != "paused" {
		t.Fatalf("expected watch status to be 'paused', got %q", reloaded.Status)
	}

	// 3. Verify event watch.paused was recorded with reason session_expired
	events, err := st.ListEvents(context.Background(), w.ID, 10)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "watch.paused" {
		t.Fatalf("expected event type 'watch.paused', got %q", events[0].EventType)
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(events[0].PayloadJSON), &payload)
	if payload["reason"] != "session_expired" {
		t.Fatalf("expected payload reason 'session_expired', got %v", payload["reason"])
	}

	// 4. Verify CheckAll skips this paused watch
	results, err := eng.CheckAll(context.Background())
	if err != nil {
		t.Fatalf("CheckAll failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected CheckAll to skip paused watch, got %d results", len(results))
	}

	// 5. Test preventive check with expired JWT claim
	pastExp := time.Now().Add(-1 * time.Hour).Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"exp": pastExp})
	payloadB64 := base64.RawURLEncoding.EncodeToString(claims)
	expiredJWT := fmt.Sprintf("%s.%s.sig", header, payloadB64)

	client.SetSession(expiredJWT)
	w2 := &store.Watch{
		Type:   "favorites",
		Name:   "my-favorites-2",
		Target: "favorites",
		Status: "active",
	}
	if err := st.AddWatch(context.Background(), w2); err != nil {
		t.Fatalf("failed to add watch 2: %v", err)
	}

	_, err = eng.CheckWatch(context.Background(), w2)
	if err == nil {
		t.Fatal("expected error on expired jwt check, got nil")
	}

	reloaded2, err := st.GetWatch(context.Background(), w2.Name)
	if err != nil {
		t.Fatalf("failed to reload watch 2: %v", err)
	}
	if reloaded2.Status != "paused" {
		t.Fatalf("expected watch 2 to be paused, got %q", reloaded2.Status)
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
