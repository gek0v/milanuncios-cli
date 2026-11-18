package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

// SinkSender sends an event to an external notification channel.
type SinkSender interface {
	Dispatch(ctx context.Context, ev store.Event) []error
}

// Engine orchestrates periodic checks, diffing, and event generation for watches.
type Engine struct {
	Store           *store.Store
	Client          *milanuncios.Client
	Sinks           SinkSender
	Notice          func(string)
	SeenItemsTTL    time.Duration
	DispatchTimeout time.Duration
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

	// Automatic retention pruning to prevent unbounded growth / OOM
	if _, err := e.Store.PruneSeenItems(e.SeenItemsTTL); err != nil && e.Notice != nil {
		e.Notice(fmt.Sprintf("pruning seen items: %v", err))
	}

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

	// Dispatch to external sinks concurrently with bounded workers and batch timeout
	if e.Sinks != nil && len(res.Events) > 0 {
		timeout := e.DispatchTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		batchCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		var deliveredMu sync.Mutex
		var deliveredIDs []int64

		maxWorkers := 5
		if len(res.Events) < maxWorkers {
			maxWorkers = len(res.Events)
		}
		sem := make(chan struct{}, maxWorkers)
		var wg sync.WaitGroup

	dispatchLoop:
		for _, ev := range res.Events {
			select {
			case sem <- struct{}{}:
			case <-batchCtx.Done():
				break dispatchLoop
			}

			wg.Add(1)
			go func(ev store.Event) {
				defer func() {
					<-sem
					wg.Done()
				}()
				errs := e.Sinks.Dispatch(batchCtx, ev)
				if len(errs) == 0 {
					deliveredMu.Lock()
					deliveredIDs = append(deliveredIDs, ev.ID)
					deliveredMu.Unlock()
				}
			}(ev)
		}
		wg.Wait()

		if len(deliveredIDs) > 0 {
			_ = e.Store.MarkEventsDelivered(deliveredIDs)
		}
	}

	res.Duration = time.Since(start)
	_ = e.Store.UpdateWatchChecked(w.ID, time.Now().UTC())

	return res, nil
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

	seenMap, err := e.Store.GetSeenItems(w.ID)
	if err != nil {
		return nil, fmt.Errorf("reading seen items: %w", err)
	}

	isFirstRun := (w.LastCheckedAt == nil && len(seenMap) == 0)
	var generatedEvents []store.Event
	now := time.Now().UTC()

	tx, err := e.Store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting diff transaction: %w", err)
	}
	defer tx.Rollback()

	for _, item := range searchRes.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

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
			if err := tx.UpsertSeenItem(seenItem); err != nil {
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
				if err := tx.RecordEvent(&ev); err != nil {
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
				if err := tx.RecordEvent(&ev); err != nil {
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
			if err := tx.UpsertSeenItem(&seen); err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing diff transaction: %w", err)
	}

	return generatedEvents, nil
}

func (e *Engine) checkAdWatch(ctx context.Context, w *store.Watch, res *CheckResult) ([]store.Event, error) {
	ad, err := e.Client.GetAd(ctx, w.Target)
	now := time.Now().UTC()

	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			tx, tErr := e.Store.BeginTx(ctx)
			if tErr != nil {
				return nil, tErr
			}
			defer tx.Rollback()

			_ = tx.UpdateWatchStatus(w.ID, "paused")
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
			if rErr := tx.RecordEvent(&ev); rErr != nil {
				return nil, rErr
			}
			if cErr := tx.Commit(); cErr != nil {
				return nil, cErr
			}
			res.Removed++
			return []store.Event{ev}, nil
		}
		return nil, err
	}

	seenMap, err := e.Store.GetSeenItems(w.ID)
	if err != nil {
		return nil, err
	}

	seen, exists := seenMap[ad.ID]
	var generated []store.Event

	tx, err := e.Store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting diff transaction: %w", err)
	}
	defer tx.Rollback()

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
		if err := tx.UpsertSeenItem(seenItem); err != nil {
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
			if err := tx.RecordEvent(&ev); err != nil {
				return nil, err
			}
			generated = append(generated, ev)
			res.PriceAlerts++
		}

		seen.LastSeenAt = now
		seen.LastPrice = ad.Price
		seen.LastTitle = ad.Title
		seen.RawJSON = store.EncodeJSON(ad)
		if err := tx.UpsertSeenItem(&seen); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing diff transaction: %w", err)
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

	seenMap, err := e.Store.GetSeenItems(w.ID)
	if err != nil {
		return nil, fmt.Errorf("reading seen items: %w", err)
	}

	var generatedEvents []store.Event
	now := time.Now().UTC()
	currentIDs := make(map[string]bool)

	tx, err := e.Store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting diff transaction: %w", err)
	}
	defer tx.Rollback()

	for _, item := range favs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

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
			if err := tx.UpsertSeenItem(seenItem); err != nil {
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
				if err := tx.RecordEvent(&ev); err != nil {
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
			if err := tx.UpsertSeenItem(&seen); err != nil {
				return nil, err
			}
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
			if err := tx.RecordEvent(&ev); err != nil {
				return nil, err
			}
			generatedEvents = append(generatedEvents, ev)
			res.Removed++

			// Remove from seen items so it won't fire ad.removed in subsequent runs
			if err := tx.DeleteSeenItem(w.ID, seenAdID); err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing diff transaction: %w", err)
	}

	return generatedEvents, nil
}

// CheckAll evaluates all active watches in the store sequentially.
func (e *Engine) CheckAll(ctx context.Context) ([]CheckResult, error) {
	watches, err := e.Store.ListWatches()
	if err != nil {
		return nil, fmt.Errorf("listing watches: %w", err)
	}

	var results []CheckResult
	for _, w := range watches {
		if w.Status != "active" {
			continue
		}
		res, err := e.CheckWatch(ctx, &w)
		if err != nil {
			if output.IsKind(err, output.KindBlocked) {
				if e.Notice != nil {
					e.Notice(fmt.Sprintf("watch %d (%s) blocked by anti-bot mitigation; aborting check cycle", w.ID, w.Name))
				}
				return results, err
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
