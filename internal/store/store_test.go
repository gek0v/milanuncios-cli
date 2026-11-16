package store_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/store"
)

func TestStoreLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// 1. Add watches
	w1 := &store.Watch{
		Type:        "search",
		Name:        "bici-valencia",
		Target:      "bicicleta carretera",
		QueryJSON:   `{"keywords":"bicicleta carretera","province":"valencia"}`,
		IntervalSec: 180,
	}
	if err := s.AddWatch(w1); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}
	if w1.ID <= 0 {
		t.Fatalf("expected positive ID, got %d", w1.ID)
	}

	w2 := &store.Watch{
		Type:        "ad",
		Name:        "furgoneta",
		Target:      "123456",
		IntervalSec: 600,
	}
	if err := s.AddWatch(w2); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}

	// 2. GetWatch by ID and Name
	byName, err := s.GetWatch("bici-valencia")
	if err != nil {
		t.Fatalf("failed to get watch by name: %v", err)
	}
	if byName.ID != w1.ID || byName.Target != w1.Target {
		t.Fatalf("unexpected watch by name: %+v", byName)
	}

	byID, err := s.GetWatch("1")
	if err != nil {
		t.Fatalf("failed to get watch by id: %v", err)
	}
	if byID.Name != "bici-valencia" {
		t.Fatalf("unexpected watch by id: %+v", byID)
	}

	// 3. ListWatches
	list, err := s.ListWatches()
	if err != nil {
		t.Fatalf("failed to list watches: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 watches, got %d", len(list))
	}

	var buf bytes.Buffer
	list.Pretty(&buf, false)
	if buf.Len() == 0 {
		t.Fatal("expected non-empty pretty table")
	}

	// 4. Update status and checked
	if err := s.UpdateWatchStatus(w1.ID, "paused"); err != nil {
		t.Fatalf("failed to pause watch: %v", err)
	}
	now := time.Now().UTC()
	if err := s.UpdateWatchChecked(w1.ID, now); err != nil {
		t.Fatalf("failed to update checked: %v", err)
	}

	reloaded, _ := s.GetWatch("bici-valencia")
	if reloaded.Status != "paused" {
		t.Fatalf("expected status 'paused', got %s", reloaded.Status)
	}
	if reloaded.LastCheckedAt == nil {
		t.Fatal("expected non-nil LastCheckedAt")
	}

	// 5. Seen items
	item1 := &store.SeenItem{
		WatchID:     w1.ID,
		AdID:        "ad-999",
		FirstSeenAt: now,
		LastSeenAt:  now,
		LastPrice:   350,
		LastTitle:   "Orbea Orca M30",
		RawJSON:     `{"id":"ad-999","price":350}`,
	}
	if err := s.UpsertSeenItem(item1); err != nil {
		t.Fatalf("failed to upsert seen item: %v", err)
	}

	seenMap, err := s.GetSeenItems(w1.ID)
	if err != nil {
		t.Fatalf("failed to get seen items: %v", err)
	}
	if len(seenMap) != 1 || seenMap["ad-999"].LastPrice != 350 {
		t.Fatalf("unexpected seen map: %+v", seenMap)
	}

	// Update price
	item1.LastPrice = 300
	item1.LastSeenAt = now.Add(5 * time.Minute)
	if err := s.UpsertSeenItem(item1); err != nil {
		t.Fatalf("failed to update seen item: %v", err)
	}
	seenMap2, _ := s.GetSeenItems(w1.ID)
	if seenMap2["ad-999"].LastPrice != 300 {
		t.Fatalf("expected updated price 300, got %f", seenMap2["ad-999"].LastPrice)
	}

	// 6. Events
	ev1 := &store.Event{
		WatchID:     w1.ID,
		EventType:   "ad.price_changed",
		AdID:        "ad-999",
		PayloadJSON: `{"old_price":350,"new_price":300}`,
		CreatedAt:   now,
	}
	if err := s.RecordEvent(ev1); err != nil {
		t.Fatalf("failed to record event: %v", err)
	}
	if ev1.ID <= 0 {
		t.Fatalf("expected positive event ID, got %d", ev1.ID)
	}

	events, err := s.ListEvents(w1.ID, 10)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "ad.price_changed" {
		t.Fatalf("unexpected events: %+v", events)
	}

	var evBuf bytes.Buffer
	events.Pretty(&evBuf, false)
	if evBuf.Len() == 0 {
		t.Fatal("expected non-empty events table")
	}

	// Mark delivered
	if err := s.MarkEventsDelivered([]int64{ev1.ID}); err != nil {
		t.Fatalf("failed to mark event delivered: %v", err)
	}
	eventsAfter, _ := s.ListEvents(w1.ID, 10)
	if !eventsAfter[0].Delivered {
		t.Fatal("expected event to be marked delivered")
	}

	// 7. DeleteWatch
	if err := s.DeleteWatch("bici-valencia"); err != nil {
		t.Fatalf("failed to delete watch: %v", err)
	}
	_, err = s.GetWatch("bici-valencia")
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound after deletion, got %v", err)
	}
}
