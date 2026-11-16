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

// Engine orchestrates periodic checks, diffing, and event generation for watches.
type Engine struct {
	Store  *store.Store
	Client *milanuncios.Client
	Notice func(string)
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

	default:
		return nil, output.Usagef("unknown watch type %q", w.Type)
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

	isFirstRun := len(seenMap) == 0
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
			if err := e.Store.UpsertSeenItem(seenItem); err != nil {
				return nil, err
			}

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
				if err := e.Store.RecordEvent(&ev); err != nil {
					return nil, err
				}
				generatedEvents = append(generatedEvents, ev)
				res.NewItems++
			}
		} else {
			// Existing ad - check price changes
			if seen.LastPrice != item.Price && item.Price > 0 {
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
				if err := e.Store.RecordEvent(&ev); err != nil {
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
			_ = e.Store.UpsertSeenItem(&seen)
		}
	}

	return generatedEvents, nil
}

func (e *Engine) checkAdWatch(ctx context.Context, w *store.Watch, res *CheckResult) ([]store.Event, error) {
	ad, err := e.Client.GetAd(ctx, w.Target)
	now := time.Now().UTC()

	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			ev := store.Event{
				WatchID:   w.ID,
				EventType: "ad.removed",
				AdID:      w.Target,
				PayloadJSON: store.EncodeJSON(map[string]any{
					"event":    "ad.removed",
					"watch_id": w.ID,
					"name":     w.Name,
					"ad_id":    w.Target,
				}),
				CreatedAt: now,
			}
			if rErr := e.Store.RecordEvent(&ev); rErr == nil {
				res.Removed++
				return []store.Event{ev}, nil
			}
			return nil, err
		}
		return nil, err
	}

	seenMap, err := e.Store.GetSeenItems(w.ID)
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
		if err := e.Store.UpsertSeenItem(seenItem); err != nil {
			return nil, err
		}
	} else {
		if seen.LastPrice != ad.Price && ad.Price > 0 {
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
			if err := e.Store.RecordEvent(&ev); err != nil {
				return nil, err
			}
			generated = append(generated, ev)
			res.PriceAlerts++
		}

		seen.LastSeenAt = now
		seen.LastPrice = ad.Price
		seen.LastTitle = ad.Title
		seen.RawJSON = store.EncodeJSON(ad)
		_ = e.Store.UpsertSeenItem(&seen)
	}

	return generated, nil
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
			if e.Notice != nil {
				e.Notice(fmt.Sprintf("watch %d (%s) failed: %v", w.ID, w.Name, err))
			}
			continue
		}
		results = append(results, *res)
	}

	return results, nil
}
