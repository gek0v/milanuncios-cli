package store_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/store"
)

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
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
	if err := s.AddWatch(ctx, w1); err != nil {
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
	if err := s.AddWatch(ctx, w2); err != nil {
		t.Fatalf("failed to add watch: %v", err)
	}

	// 2. GetWatch by ID and Name
	byName, err := s.GetWatch(ctx, "bici-valencia")
	if err != nil {
		t.Fatalf("failed to get watch by name: %v", err)
	}
	if byName.ID != w1.ID || byName.Target != w1.Target {
		t.Fatalf("unexpected watch by name: %+v", byName)
	}

	byID, err := s.GetWatch(ctx, "1")
	if err != nil {
		t.Fatalf("failed to get watch by id: %v", err)
	}
	if byID.Name != "bici-valencia" {
		t.Fatalf("unexpected watch by id: %+v", byID)
	}

	// 3. ListWatches
	list, err := s.ListWatches(ctx)
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
	if err := s.UpdateWatchStatus(ctx, w1.ID, "paused"); err != nil {
		t.Fatalf("failed to pause watch: %v", err)
	}
	now := time.Now().UTC()
	if err := s.UpdateWatchChecked(ctx, w1.ID, now); err != nil {
		t.Fatalf("failed to update checked: %v", err)
	}

	reloaded, _ := s.GetWatch(ctx, "bici-valencia")
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
	if err := s.UpsertSeenItem(ctx, item1); err != nil {
		t.Fatalf("failed to upsert seen item: %v", err)
	}

	seenMap, err := s.GetSeenItems(ctx, w1.ID)
	if err != nil {
		t.Fatalf("failed to get seen items: %v", err)
	}
	if len(seenMap) != 1 || seenMap["ad-999"].LastPrice != 350 {
		t.Fatalf("unexpected seen map: %+v", seenMap)
	}

	// Update price
	item1.LastPrice = 300
	item1.LastSeenAt = now.Add(5 * time.Minute)
	if err := s.UpsertSeenItem(ctx, item1); err != nil {
		t.Fatalf("failed to update seen item: %v", err)
	}
	seenMap2, _ := s.GetSeenItems(ctx, w1.ID)
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
	if err := s.RecordEvent(ctx, ev1); err != nil {
		t.Fatalf("failed to record event: %v", err)
	}
	if ev1.ID <= 0 {
		t.Fatalf("expected positive event ID, got %d", ev1.ID)
	}

	events, err := s.ListEvents(ctx, w1.ID, 10)
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
	if err := s.MarkEventsDelivered(ctx, []int64{ev1.ID}); err != nil {
		t.Fatalf("failed to mark event delivered: %v", err)
	}
	eventsAfter, _ := s.ListEvents(ctx, w1.ID, 10)
	if !eventsAfter[0].Delivered {
		t.Fatal("expected event to be marked delivered")
	}

	// 7. DeleteWatch
	if err := s.DeleteWatch(ctx, "bici-valencia"); err != nil {
		t.Fatalf("failed to delete watch: %v", err)
	}
	_, err = s.GetWatch(ctx, "bici-valencia")
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound after deletion, got %v", err)
	}
	seenAfterDelete, err := s.GetSeenItems(ctx, w1.ID)
	if err != nil || len(seenAfterDelete) != 0 {
		t.Fatalf("expected 0 seen items after delete, got %d (err: %v)", len(seenAfterDelete), err)
	}
	eventsAfterDelete, err := s.ListEvents(ctx, w1.ID, 10)
	if err != nil || len(eventsAfterDelete) != 0 {
		t.Fatalf("expected 0 events after delete, got %d (err: %v)", len(eventsAfterDelete), err)
	}
}

func TestStoreFavoriteAnnotationsAndChatWatch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "annotations.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// 1. Get non-existent annotation -> ErrNotFound
	_, err = s.GetAnnotation(ctx, "492817294")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. Save annotation
	if err := s.SaveAnnotation(ctx, "492817294", "oferta_enviada", "Ofrecidos 12.500€ el 18/11.", 12500); err != nil {
		t.Fatalf("failed to save annotation: %v", err)
	}

	// 3. Get annotation
	ann, err := s.GetAnnotation(ctx, "492817294")
	if err != nil {
		t.Fatalf("failed to get annotation: %v", err)
	}
	if ann.AdID != "492817294" || ann.Tag != "oferta_enviada" || ann.TargetPrice != 12500 || ann.Note != "Ofrecidos 12.500€ el 18/11." {
		t.Fatalf("unexpected annotation content: %+v", ann)
	}

	// 4. Update annotation
	if err := s.SaveAnnotation(ctx, "492817294", "contraoferta", "Pide 13.000€.", 12800); err != nil {
		t.Fatalf("failed to update annotation: %v", err)
	}
	ann2, err := s.GetAnnotation(ctx, "492817294")
	if err != nil {
		t.Fatalf("failed to get updated annotation: %v", err)
	}
	if ann2.Tag != "contraoferta" || ann2.TargetPrice != 12800 {
		t.Fatalf("unexpected updated annotation: %+v", ann2)
	}

	var buf bytes.Buffer
	ann2.Pretty(&buf, false)
	if !strings.Contains(buf.String(), "contraoferta") {
		t.Fatalf("expected pretty output to contain tag, got %s", buf.String())
	}

	// 5. EnsureChatWatch
	chatW, err := s.EnsureChatWatch(ctx)
	if err != nil {
		t.Fatalf("failed to ensure chat watch: %v", err)
	}
	if chatW.Type != "chat" || chatW.ID <= 0 {
		t.Fatalf("unexpected chat watch: %+v", chatW)
	}

	// EnsureChatWatch second time returns the same watch
	chatW2, err := s.EnsureChatWatch(ctx)
	if err != nil {
		t.Fatalf("second ensure chat watch failed: %v", err)
	}
	if chatW2.ID != chatW.ID {
		t.Fatalf("expected same watch ID %d, got %d", chatW.ID, chatW2.ID)
	}

	// 6. Chat message deduplication
	seen, err := s.HasSeenChatMessage(ctx, chatW.ID, "msg-001")
	if err != nil {
		t.Fatalf("has seen failed: %v", err)
	}
	if seen {
		t.Fatal("expected message not seen yet")
	}

	if err := s.MarkChatMessageSeen(ctx, chatW.ID, "msg-001", "Hola, me interesa"); err != nil {
		t.Fatalf("mark seen failed: %v", err)
	}

	seenAfter, err := s.HasSeenChatMessage(ctx, chatW.ID, "msg-001")
	if err != nil {
		t.Fatalf("has seen failed after mark: %v", err)
	}
	if !seenAfter {
		t.Fatal("expected message to be seen")
	}

	// 7. DeleteSeenItem
	if err := s.DeleteSeenItem(ctx, chatW.ID, "chat_msg:msg-001"); err != nil {
		t.Fatalf("delete seen item failed: %v", err)
	}
	seenAfterDelete, err := s.HasSeenChatMessage(ctx, chatW.ID, "msg-001")
	if err != nil || seenAfterDelete {
		t.Fatalf("expected message not seen after delete, got %v", seenAfterDelete)
	}
}

func TestStore_EventDeliveriesAndUndelivered(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "deliveries.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	w := &store.Watch{
		Type:   "search",
		Name:   "test-watch",
		Target: "reloj",
	}
	if err := s.AddWatch(ctx, w); err != nil {
		t.Fatalf("add watch failed: %v", err)
	}

	ev := &store.Event{
		WatchID:     w.ID,
		EventType:   "ad.new",
		AdID:        "ad-100",
		PayloadJSON: `{"title":"Rolex"}`,
		CreatedAt:   time.Now().UTC(),
		Delivered:   false,
	}
	if err := s.RecordEvent(ctx, ev); err != nil {
		t.Fatalf("record event failed: %v", err)
	}

	// 1. GetUndeliveredEvents
	undelivered, err := s.GetUndeliveredEvents(ctx, w.ID, 10)
	if err != nil {
		t.Fatalf("get undelivered failed: %v", err)
	}
	if len(undelivered) != 1 || undelivered[0].ID != ev.ID {
		t.Fatalf("expected 1 undelivered event, got %d", len(undelivered))
	}

	// 2. Record failed delivery for webhook
	deliveryErr := errors.New("connection refused")
	if err := s.RecordDeliveryAttempt(ctx, ev.ID, "webhook-1", deliveryErr); err != nil {
		t.Fatalf("record delivery failed: %v", err)
	}

	// Record success for ntfy
	if err := s.RecordDeliveryAttempt(ctx, ev.ID, "ntfy-1", nil); err != nil {
		t.Fatalf("record delivery success failed: %v", err)
	}

	// 3. GetEventDeliveries
	deliveries, err := s.GetEventDeliveries(ctx, ev.ID)
	if err != nil {
		t.Fatalf("get deliveries failed: %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(deliveries))
	}

	var webhookDeliv, ntfyDeliv *store.EventDelivery
	for i := range deliveries {
		if deliveries[i].SinkName == "webhook-1" {
			webhookDeliv = &deliveries[i]
		}
		if deliveries[i].SinkName == "ntfy-1" {
			ntfyDeliv = &deliveries[i]
		}
	}

	if webhookDeliv == nil || webhookDeliv.Status != "failed" || webhookDeliv.Attempts != 1 || webhookDeliv.LastError != "connection refused" {
		t.Fatalf("unexpected webhook delivery: %+v", webhookDeliv)
	}
	if ntfyDeliv == nil || ntfyDeliv.Status != "delivered" || ntfyDeliv.Attempts != 1 {
		t.Fatalf("unexpected ntfy delivery: %+v", ntfyDeliv)
	}

	// 4. Retry webhook -> increments attempts
	if err := s.RecordDeliveryAttempt(ctx, ev.ID, "webhook-1", nil); err != nil {
		t.Fatalf("record delivery retry failed: %v", err)
	}
	deliveries2, err := s.GetEventDeliveries(ctx, ev.ID)
	if err != nil {
		t.Fatalf("get deliveries 2 failed: %v", err)
	}
	for _, d := range deliveries2 {
		if d.SinkName == "webhook-1" {
			if d.Status != "delivered" || d.Attempts != 2 {
				t.Fatalf("expected status 'delivered' and attempts 2, got status %q attempts %d", d.Status, d.Attempts)
			}
		}
	}

	// 5. Mark as delivered in events table
	if err := s.MarkEventsDelivered(ctx, []int64{ev.ID}); err != nil {
		t.Fatalf("mark delivered failed: %v", err)
	}
	undeliveredAfter, err := s.GetUndeliveredEvents(ctx, w.ID, 10)
	if err != nil {
		t.Fatalf("get undelivered after failed: %v", err)
	}
	if len(undeliveredAfter) != 0 {
		t.Fatalf("expected 0 undelivered events, got %d", len(undeliveredAfter))
	}
}
