package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

// SinkSender sends an event to an external notification channel.
type SinkSender interface {
	Dispatch(ctx context.Context, ev store.Event) []error
}

// PerSinkSender delivers events to named sinks individually.
type PerSinkSender interface {
	SinkSender
	SinkNames() []string
	SendTo(ctx context.Context, name string, ev store.Event) error
}

// BackoffDuration calculates exponential backoff based on retry attempts.
// 1 attempt: 30s, 2: 1m, 3: 2m, 4: 4m, 5+: 8m (capped at 10m).
func BackoffDuration(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	shift := attempts - 1
	if shift > 5 {
		shift = 5
	}
	dur := 30 * time.Second * (1 << shift)
	if dur > 10*time.Minute {
		dur = 10 * time.Minute
	}
	return dur
}

// Engine orchestrates periodic checks, diffing, and event generation for watches.
type Engine struct {
	Store        *store.Store
	Client       *milanuncios.Client
	Sinks        SinkSender
	Notice       func(string)
	RetryBackoff func(attempts int) time.Duration
}

// NewEngine creates an initialized watch Engine.
func NewEngine(s *store.Store, c *milanuncios.Client) *Engine {
	return &Engine{
		Store:  s,
		Client: c,
	}
}

// CheckResult contains summary of a watch check operation.
type CheckResult struct {
	WatchID     int64         `json:"watch_id"`
	WatchName   string        `json:"watch_name"`
	WatchType   string        `json:"watch_type"`
	NewItems    int           `json:"new_items"`
	PriceAlerts int           `json:"price_alerts"`
	Removed     int           `json:"removed"`
	Events      []store.Event `json:"events"`
	Duration    time.Duration `json:"duration_ms"`
}

// CheckWatch performs a single check cycle on a given watch.
func (e *Engine) CheckWatch(ctx context.Context, w *store.Watch) (*CheckResult, error) {
	start := time.Now()
	res := &CheckResult{
		WatchID:   w.ID,
		WatchName: w.Name,
		WatchType: w.Type,
	}

	// Retry any previously undelivered events whose backoff has elapsed
	_ = e.retryUndeliveredForWatch(ctx, w.ID)

	switch w.Type {
	case "search":
		events, err := e.checkSearchWatch(ctx, w, res)
		if err != nil {
			return nil, err
		}
		res.Events = events

	case "ad":
		events, err := e.checkAdWatch(ctx, w, res)
		if err != nil {
			return nil, err
		}
		res.Events = events

	case "favorites":
		events, err := e.checkFavoritesWatch(ctx, w, res)
		if err != nil {
			return nil, err
		}
		res.Events = events

	default:
		return nil, output.Usagef("unknown watch type %q", w.Type)
	}

	// Dispatch newly generated events to configured sinks
	if e.Sinks != nil && len(res.Events) > 0 {
		for _, ev := range res.Events {
			_ = e.dispatchEvent(ctx, ev)
		}
	}

	res.Duration = time.Since(start)
	_ = e.Store.UpdateWatchChecked(ctx, w.ID, time.Now().UTC())

	return res, nil
}

func (e *Engine) dispatchEvent(ctx context.Context, ev store.Event) error {
	if e.Sinks == nil {
		return nil
	}

	if perSink, ok := e.Sinks.(PerSinkSender); ok {
		sinkNames := perSink.SinkNames()
		if len(sinkNames) == 0 {
			return nil
		}

		deliveries, _ := e.Store.GetEventDeliveries(ctx, ev.ID)
		deliveredMap := make(map[string]bool, len(deliveries))
		for _, d := range deliveries {
			if d.Status == "delivered" {
				deliveredMap[d.SinkName] = true
			}
		}

		allDelivered := true
		for _, name := range sinkNames {
			if deliveredMap[name] {
				continue
			}
			err := perSink.SendTo(ctx, name, ev)
			_ = e.Store.RecordDeliveryAttempt(ctx, ev.ID, name, err)
			if err != nil {
				allDelivered = false
				if e.Notice != nil {
					e.Notice(fmt.Sprintf("sink %s failed for event %d: %v", name, ev.ID, err))
				}
			}
		}

		if allDelivered {
			_ = e.Store.MarkEventsDelivered(ctx, []int64{ev.ID})
		}
		return nil
	}

	// Fallback for simple SinkSender
	errs := e.Sinks.Dispatch(ctx, ev)
	if len(errs) == 0 {
		_ = e.Store.RecordDeliveryAttempt(ctx, ev.ID, "default", nil)
		_ = e.Store.MarkEventsDelivered(ctx, []int64{ev.ID})
	} else {
		_ = e.Store.RecordDeliveryAttempt(ctx, ev.ID, "default", errs[0])
		if e.Notice != nil {
			e.Notice(fmt.Sprintf("sink dispatch failed for event %d: %v", ev.ID, errs[0]))
		}
	}
	return nil
}

func (e *Engine) retryUndeliveredForWatch(ctx context.Context, watchID int64) error {
	if e.Sinks == nil {
		return nil
	}

	undelivered, err := e.Store.GetUndeliveredEvents(ctx, watchID, 50)
	if err != nil || len(undelivered) == 0 {
		return err
	}

	now := time.Now().UTC()

	calcBackoff := BackoffDuration
	if e.RetryBackoff != nil {
		calcBackoff = e.RetryBackoff
	}

	if perSink, ok := e.Sinks.(PerSinkSender); ok {
		sinkNames := perSink.SinkNames()
		if len(sinkNames) == 0 {
			return nil
		}

		for _, ev := range undelivered {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			deliveries, err := e.Store.GetEventDeliveries(ctx, ev.ID)
			if err != nil {
				continue
			}

			delivMap := make(map[string]store.EventDelivery, len(deliveries))
			for _, d := range deliveries {
				delivMap[d.SinkName] = d
			}

			allDelivered := true
			for _, name := range sinkNames {
				d, exists := delivMap[name]
				if exists && d.Status == "delivered" {
					continue
				}

				// If failed before, check exponential backoff
				if exists && d.Attempts > 0 {
					backoff := calcBackoff(d.Attempts)
					if now.Sub(d.LastAttemptAt) < backoff {
						allDelivered = false
						continue
					}
				}

				// Attempt retry
				sendErr := perSink.SendTo(ctx, name, ev)
				_ = e.Store.RecordDeliveryAttempt(ctx, ev.ID, name, sendErr)
				if sendErr != nil {
					allDelivered = false
					if e.Notice != nil {
						e.Notice(fmt.Sprintf("sink retry %s failed for event %d: %v", name, ev.ID, sendErr))
					}
				}
			}

			if allDelivered {
				_ = e.Store.MarkEventsDelivered(ctx, []int64{ev.ID})
			}
		}
		return nil
	}

	// Fallback for simple SinkSender
	for _, ev := range undelivered {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		deliveries, _ := e.Store.GetEventDeliveries(ctx, ev.ID)
		if len(deliveries) > 0 {
			d := deliveries[0]
			if d.Attempts > 0 {
				backoff := calcBackoff(d.Attempts)
				if now.Sub(d.LastAttemptAt) < backoff {
					continue
				}
			}
		}

		errs := e.Sinks.Dispatch(ctx, ev)
		if len(errs) == 0 {
			_ = e.Store.RecordDeliveryAttempt(ctx, ev.ID, "default", nil)
			_ = e.Store.MarkEventsDelivered(ctx, []int64{ev.ID})
		} else {
			_ = e.Store.RecordDeliveryAttempt(ctx, ev.ID, "default", errs[0])
		}
	}

	return nil
}

func (e *Engine) checkSearchWatch(ctx context.Context, w *store.Watch, res *CheckResult) ([]store.Event, error) {
	var q milanuncios.SearchQuery
	if w.QueryJSON != "" {
		_ = json.Unmarshal([]byte(w.QueryJSON), &q)
	}
	if q.Keywords == "" && w.Target != "" {
		q.Keywords = w.Target
	}

	searchRes, err := e.Client.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("executing search query: %w", err)
	}

	seenMap, err := e.Store.GetSeenItems(ctx, w.ID)
	if err != nil {
		return nil, fmt.Errorf("reading seen items: %w", err)
	}

	isFirstRun := (w.LastCheckedAt == nil && len(seenMap) == 0)
	var generatedEvents []store.Event
	now := time.Now().UTC()

	for _, item := range searchRes.Items {
		seen, exists := seenMap[item.ID]

		if !exists {
			// New ad detected
			seenItem := &store.SeenItem{
				WatchID:     w.ID,
				AdID:        item.ID,
				FirstSeenAt: now,
				LastSeenAt:  now,
				LastPrice:   item.Price,
				LastTitle:   item.Title,
				RawJSON:     store.EncodeJSON(item),
			}
			if err := e.Store.UpsertSeenItem(ctx, seenItem); err != nil {
				return nil, err
			}
			seenMap[item.ID] = *seenItem

			// Do not flood new events on the very first baseline check of a brand new watch
			if !isFirstRun {
				ev := store.Event{
					WatchID:   w.ID,
					EventType: "ad.new",
					AdID:      item.ID,
					PayloadJSON: store.EncodeJSON(map[string]any{
						"event":    "ad.new",
						"watch_id": w.ID,
						"name":     w.Name,
						"ad":       item,
					}),
					CreatedAt: now,
				}
				if err := e.Store.RecordEvent(ctx, &ev); err != nil {
					return nil, err
				}
				generatedEvents = append(generatedEvents, ev)
				res.NewItems++
			}
		} else {
			// Existing ad - check price changes
			if seen.LastPrice != item.Price && item.Price >= 0 && seen.LastPrice >= 0 {
				diff := item.Price - seen.LastPrice
				ev := store.Event{
					WatchID:   w.ID,
					EventType: "ad.price_changed",
					AdID:      item.ID,
					PayloadJSON: store.EncodeJSON(map[string]any{
						"event":     "ad.price_changed",
						"watch_id":  w.ID,
						"name":      w.Name,
						"old_price": seen.LastPrice,
						"new_price": item.Price,
						"diff":      diff,
						"ad":        item,
					}),
					CreatedAt: now,
				}
				if err := e.Store.RecordEvent(ctx, &ev); err != nil {
					return nil, err
				}
				generatedEvents = append(generatedEvents, ev)
				res.PriceAlerts++
			}

			// Update timestamp and latest title/price
			seen.LastSeenAt = now
			seen.LastPrice = item.Price
			seen.LastTitle = item.Title
			seen.RawJSON = store.EncodeJSON(item)
			_ = e.Store.UpsertSeenItem(ctx, &seen)
		}
	}

	return generatedEvents, nil
}

func (e *Engine) checkAdWatch(ctx context.Context, w *store.Watch, res *CheckResult) ([]store.Event, error) {
	ad, err := e.Client.GetAd(ctx, w.Target)
	now := time.Now().UTC()

	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			_ = e.Store.UpdateWatchStatus(ctx, w.ID, "paused")
			ev := store.Event{
				WatchID:   w.ID,
				EventType: "ad.removed",
				AdID:      w.Target,
				PayloadJSON: store.EncodeJSON(map[string]any{
					"event":    "ad.removed",
					"watch_id": w.ID,
					"name":     w.Name,
					"ad_id":    w.Target,
					"notice":   "ad listing removed; watch paused",
				}),
				CreatedAt: now,
			}
			if rErr := e.Store.RecordEvent(ctx, &ev); rErr == nil {
				res.Removed++
				return []store.Event{ev}, nil
			}
			return nil, err
		}
		return nil, err
	}

	seenMap, err := e.Store.GetSeenItems(ctx, w.ID)
	if err != nil {
		return nil, err
	}

	seen, exists := seenMap[ad.ID]
	var generated []store.Event

	if !exists {
		seenItem := &store.SeenItem{
			WatchID:     w.ID,
			AdID:        ad.ID,
			FirstSeenAt: now,
			LastSeenAt:  now,
			LastPrice:   ad.Price,
			LastTitle:   ad.Title,
			RawJSON:     store.EncodeJSON(ad),
		}
		if err := e.Store.UpsertSeenItem(ctx, seenItem); err != nil {
			return nil, err
		}
	} else {
		if seen.LastPrice != ad.Price && ad.Price >= 0 && seen.LastPrice >= 0 {
			diff := ad.Price - seen.LastPrice
			ev := store.Event{
				WatchID:   w.ID,
				EventType: "ad.price_changed",
				AdID:      ad.ID,
				PayloadJSON: store.EncodeJSON(map[string]any{
					"event":     "ad.price_changed",
					"watch_id":  w.ID,
					"name":      w.Name,
					"old_price": seen.LastPrice,
					"new_price": ad.Price,
					"diff":      diff,
					"ad":        ad,
				}),
				CreatedAt: now,
			}
			if err := e.Store.RecordEvent(ctx, &ev); err != nil {
				return nil, err
			}
			generated = append(generated, ev)
			res.PriceAlerts++
		}

		seen.LastSeenAt = now
		seen.LastPrice = ad.Price
		seen.LastTitle = ad.Title
		seen.RawJSON = store.EncodeJSON(ad)
		_ = e.Store.UpsertSeenItem(ctx, &seen)
	}

	return generated, nil
}

func (e *Engine) checkFavoritesWatch(ctx context.Context, w *store.Watch, res *CheckResult) ([]store.Event, error) {
	if e.Client == nil || e.Client.SessionToken == "" {
		return nil, &output.DomainError{
			Kind:     output.KindAuth,
			Endpoint: "GET /api/v1/me/favorites",
			Msg:      "this command requires an authenticated profile. Run `milanuncios auth login`",
		}
	}

	favs, err := e.Client.MyFavorites(ctx)
	if err != nil {
		return nil, err
	}

	seenMap, err := e.Store.GetSeenItems(ctx, w.ID)
	if err != nil {
		return nil, fmt.Errorf("reading seen items: %w", err)
	}

	var generatedEvents []store.Event
	now := time.Now().UTC()
	currentIDs := make(map[string]bool)

	for _, item := range favs {
		currentIDs[item.ID] = true
		seen, exists := seenMap[item.ID]

		if !exists {
			// Newly seen favorite ad
			seenItem := &store.SeenItem{
				WatchID:     w.ID,
				AdID:        item.ID,
				FirstSeenAt: now,
				LastSeenAt:  now,
				LastPrice:   item.Price,
				LastTitle:   item.Title,
				RawJSON:     store.EncodeJSON(item),
			}
			if err := e.Store.UpsertSeenItem(ctx, seenItem); err != nil {
				return nil, err
			}
			seenMap[item.ID] = *seenItem
			res.NewItems++
		} else {
			// Existing item: check if price dropped
			if item.Price < seen.LastPrice && seen.LastPrice >= 0 && item.Price >= 0 {
				diff := item.Price - seen.LastPrice
				ev := store.Event{
					WatchID:   w.ID,
					EventType: "ad.price_changed",
					AdID:      item.ID,
					PayloadJSON: store.EncodeJSON(map[string]any{
						"event":     "ad.price_changed",
						"watch_id":  w.ID,
						"name":      w.Name,
						"old_price": seen.LastPrice,
						"new_price": item.Price,
						"diff":      diff,
						"title":     item.Title,
						"url":       item.URL,
						"ad":        item,
					}),
					CreatedAt: now,
				}
				if err := e.Store.RecordEvent(ctx, &ev); err != nil {
					return nil, err
				}
				generatedEvents = append(generatedEvents, ev)
				res.PriceAlerts++
			}

			// Update timestamp and latest title/price
			seen.LastSeenAt = now
			seen.LastPrice = item.Price
			seen.LastTitle = item.Title
			seen.RawJSON = store.EncodeJSON(item)
			_ = e.Store.UpsertSeenItem(ctx, &seen)
		}
	}

	// Detect if an ad was removed from favorites
	for seenAdID, seen := range seenMap {
		if !currentIDs[seenAdID] {
			ev := store.Event{
				WatchID:   w.ID,
				EventType: "ad.removed",
				AdID:      seenAdID,
				PayloadJSON: store.EncodeJSON(map[string]any{
					"event":     "ad.removed",
					"watch_id":  w.ID,
					"name":      w.Name,
					"ad_id":     seenAdID,
					"title":     seen.LastTitle,
					"old_price": seen.LastPrice,
					"notice":    "ad removed from favorites",
				}),
				CreatedAt: now,
			}
			if err := e.Store.RecordEvent(ctx, &ev); err != nil {
				return nil, err
			}
			generatedEvents = append(generatedEvents, ev)
			res.Removed++

			// Remove from seen items so it won't fire ad.removed in subsequent runs
			_ = e.Store.DeleteSeenItem(ctx, w.ID, seenAdID)
		}
	}

	return generatedEvents, nil
}

// CheckAll evaluates all active watches in the store sequentially, respecting each watch's IntervalSec cadence.
func (e *Engine) CheckAll(ctx context.Context) ([]CheckResult, error) {
	watches, err := e.Store.ListWatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing watches: %w", err)
	}

	var results []CheckResult
	for _, w := range watches {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		if w.Status != "active" {
			continue
		}
		if w.LastCheckedAt != nil && time.Since(*w.LastCheckedAt) < time.Duration(w.IntervalSec)*time.Second {
			continue
		}
		res, err := e.CheckWatch(ctx, &w)
		if err != nil {
			if ctx.Err() != nil {
				return results, ctx.Err()
			}
			if e.Notice != nil {
				e.Notice(fmt.Sprintf("watch %d (%s) failed: %v", w.ID, w.Name, err))
			}
			continue
		}
		results = append(results, *res)
	}

	return results, nil
}
