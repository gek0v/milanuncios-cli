package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/output"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("record not found")
)

// Watch represents a monitored search query or individual ad listing.
type Watch struct {
	ID            int64      `json:"id"`
	Type          string     `json:"type"` // "search" or "ad"
	Name          string     `json:"name"`
	Target        string     `json:"target"`
	QueryJSON     string     `json:"query_json,omitempty"`
	IntervalSec   int        `json:"interval_sec"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	Status        string     `json:"status"` // "active", "paused"
}

// SeenItem tracks an ad seen by a specific watch for diffing.
type SeenItem struct {
	WatchID     int64     `json:"watch_id"`
	AdID        string    `json:"ad_id"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	LastPrice   float64   `json:"last_price"`
	LastTitle   string    `json:"last_title"`
	RawJSON     string    `json:"raw_json,omitempty"`
}

// Event records a change detected by the watch engine.
type Event struct {
	ID          int64     `json:"id"`
	WatchID     int64     `json:"watch_id"`
	EventType   string    `json:"event_type"` // "ad.new", "ad.price_changed", "ad.removed"
	AdID        string    `json:"ad_id"`
	PayloadJSON string    `json:"payload_json"`
	CreatedAt   time.Time `json:"created_at"`
	Delivered   bool      `json:"delivered"`
}

// WatchList wraps a slice of watches with Pretty rendering.
type WatchList []Watch

// Pretty renders watches in a clean table.
func (wl WatchList) Pretty(w io.Writer, color bool) {
	if len(wl) == 0 {
		fmt.Fprintln(w, "No watches configured. Add one with: milanuncios watch add search|ad ...")
		return
	}

	header := []string{"ID", "NAME", "TYPE", "TARGET", "INTERVAL", "STATUS", "LAST CHECKED"}
	rows := make([][]string, 0, len(wl))

	for _, wt := range wl {
		lastChecked := "never"
		if wt.LastCheckedAt != nil {
			lastChecked = wt.LastCheckedAt.Local().Format("2006-01-02 15:04:05")
		}

		intervalStr := fmt.Sprintf("%ds", wt.IntervalSec)
		if wt.IntervalSec >= 60 {
			intervalStr = fmt.Sprintf("%dm", wt.IntervalSec/60)
		}

		rows = append(rows, []string{
			fmt.Sprintf("%d", wt.ID),
			output.Truncate(wt.Name, 24),
			wt.Type,
			output.Truncate(wt.Target, 30),
			intervalStr,
			wt.Status,
			lastChecked,
		})
	}

	output.Table(w, color, header, rows)
}

// EventList wraps a slice of events with Pretty rendering.
type EventList []Event

// Pretty renders events in a clean table.
func (el EventList) Pretty(w io.Writer, color bool) {
	if len(el) == 0 {
		fmt.Fprintln(w, "No events recorded.")
		return
	}

	header := []string{"ID", "WATCH", "TYPE", "AD ID", "CREATED AT", "DELIVERED"}
	rows := make([][]string, 0, len(el))

	for _, ev := range el {
		deliv := "no"
		if ev.Delivered {
			deliv = "yes"
		}

		rows = append(rows, []string{
			fmt.Sprintf("%d", ev.ID),
			fmt.Sprintf("%d", ev.WatchID),
			ev.EventType,
			ev.AdID,
			ev.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			deliv,
		})
	}

	output.Table(w, color, header, rows)
}

// Store handles persistent SQLite storage for watches, seen items, and events.
type Store struct {
	dbPath string
	db     *sql.DB
	mu     sync.Mutex
}

// Open initializes SQLite connection and runs migrations.
func Open(dbPath string) (*Store, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	// Single writer to prevent locking errors in modernc sqlite
	db.SetMaxOpenConns(1)

	s := &Store{
		dbPath: dbPath,
		db:     db,
	}

	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}

	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS watches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL,
		name TEXT NOT NULL,
		target TEXT NOT NULL,
		query_json TEXT NOT NULL DEFAULT '',
		interval_sec INTEGER NOT NULL DEFAULT 300,
		last_checked_at TEXT,
		created_at TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active'
	);

	CREATE TABLE IF NOT EXISTS seen_items (
		watch_id INTEGER NOT NULL,
		ad_id TEXT NOT NULL,
		first_seen_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		last_price REAL NOT NULL,
		last_title TEXT NOT NULL,
		raw_json TEXT NOT NULL,
		PRIMARY KEY (watch_id, ad_id),
		FOREIGN KEY (watch_id) REFERENCES watches(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		watch_id INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		ad_id TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		delivered INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (watch_id) REFERENCES watches(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_seen_items_watch ON seen_items(watch_id);
	CREATE INDEX IF NOT EXISTS idx_events_watch ON events(watch_id);
	CREATE INDEX IF NOT EXISTS idx_events_delivered ON events(delivered);
	`
	_, err := s.db.Exec(schema)
	return err
}

// AddWatch inserts a new watch.
func (s *Store) AddWatch(w *Watch) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	if w.Status == "" {
		w.Status = "active"
	}
	if w.IntervalSec <= 0 {
		w.IntervalSec = 300
	}

	query := `
	INSERT INTO watches (type, name, target, query_json, interval_sec, created_at, status)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	res, err := s.db.Exec(query, w.Type, w.Name, w.Target, w.QueryJSON, w.IntervalSec, w.CreatedAt.Format(time.RFC3339), w.Status)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	w.ID = id
	return nil
}

// GetWatch retrieves a watch by numeric ID or exact name.
func (s *Store) GetWatch(idOrName string) (*Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var query string

	trimmed := strings.TrimSpace(idOrName)
	query = `SELECT id, type, name, target, query_json, interval_sec, last_checked_at, created_at, status FROM watches WHERE id = ? OR name = ? LIMIT 1`

	row := s.db.QueryRow(query, trimmed, trimmed)

	var w Watch
	var lastCheckedStr sql.NullString
	var createdStr string

	err := row.Scan(&w.ID, &w.Type, &w.Name, &w.Target, &w.QueryJSON, &w.IntervalSec, &lastCheckedStr, &createdStr, &w.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
		w.CreatedAt = t
	}
	if lastCheckedStr.Valid && lastCheckedStr.String != "" {
		if t, err := time.Parse(time.RFC3339, lastCheckedStr.String); err == nil {
			w.LastCheckedAt = &t
		}
	}

	return &w, nil
}

// ListWatches returns all configured watches.
func (s *Store) ListWatches() (WatchList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, type, name, target, query_json, interval_sec, last_checked_at, created_at, status FROM watches ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list WatchList
	for rows.Next() {
		var w Watch
		var lastCheckedStr sql.NullString
		var createdStr string

		if err := rows.Scan(&w.ID, &w.Type, &w.Name, &w.Target, &w.QueryJSON, &w.IntervalSec, &lastCheckedStr, &createdStr, &w.Status); err != nil {
			return nil, err
		}

		if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
			w.CreatedAt = t
		}
		if lastCheckedStr.Valid && lastCheckedStr.String != "" {
			if t, err := time.Parse(time.RFC3339, lastCheckedStr.String); err == nil {
				w.LastCheckedAt = &t
			}
		}

		list = append(list, w)
	}

	return list, rows.Err()
}

// UpdateWatchStatus sets status ("active", "paused") for a watch.
func (s *Store) UpdateWatchStatus(id int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE watches SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWatchChecked records the timestamp of a completed watch check.
func (s *Store) UpdateWatchChecked(id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`UPDATE watches SET last_checked_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), id)
	return err
}

// DeleteWatch removes a watch and its associated seen items and events.
func (s *Store) DeleteWatch(idOrName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, err := s.getWatchLocked(idOrName)
	if err != nil {
		return err
	}

	_, _ = s.db.Exec(`DELETE FROM seen_items WHERE watch_id = ?`, w.ID)
	_, _ = s.db.Exec(`DELETE FROM events WHERE watch_id = ?`, w.ID)
	res, err := s.db.Exec(`DELETE FROM watches WHERE id = ?`, w.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) getWatchLocked(idOrName string) (*Watch, error) {
	trimmed := strings.TrimSpace(idOrName)
	query := `SELECT id, type, name, target, query_json, interval_sec, last_checked_at, created_at, status FROM watches WHERE id = ? OR name = ? LIMIT 1`

	row := s.db.QueryRow(query, trimmed, trimmed)
	var w Watch
	var lastCheckedStr sql.NullString
	var createdStr string

	err := row.Scan(&w.ID, &w.Type, &w.Name, &w.Target, &w.QueryJSON, &w.IntervalSec, &lastCheckedStr, &createdStr, &w.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &w, nil
}

// GetSeenItems returns map of ad_id -> SeenItem for a given watch.
func (s *Store) GetSeenItems(watchID int64) (map[string]SeenItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT watch_id, ad_id, first_seen_at, last_seen_at, last_price, last_title, raw_json FROM seen_items WHERE watch_id = ?`, watchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]SeenItem)
	for rows.Next() {
		var it SeenItem
		var firstStr, lastStr string

		if err := rows.Scan(&it.WatchID, &it.AdID, &firstStr, &lastStr, &it.LastPrice, &it.LastTitle, &it.RawJSON); err != nil {
			return nil, err
		}

		if t, err := time.Parse(time.RFC3339, firstStr); err == nil {
			it.FirstSeenAt = t
		}
		if t, err := time.Parse(time.RFC3339, lastStr); err == nil {
			it.LastSeenAt = t
		}

		res[it.AdID] = it
	}

	return res, rows.Err()
}

// UpsertSeenItem records or updates a seen ad.
func (s *Store) UpsertSeenItem(it *SeenItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO seen_items (watch_id, ad_id, first_seen_at, last_seen_at, last_price, last_title, raw_json)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(watch_id, ad_id) DO UPDATE SET
		last_seen_at = excluded.last_seen_at,
		last_price = excluded.last_price,
		last_title = excluded.last_title,
		raw_json = excluded.raw_json
	`
	_, err := s.db.Exec(query,
		it.WatchID,
		it.AdID,
		it.FirstSeenAt.UTC().Format(time.RFC3339),
		it.LastSeenAt.UTC().Format(time.RFC3339),
		it.LastPrice,
		it.LastTitle,
		it.RawJSON,
	)
	return err
}

// RecordEvent saves a detected event to SQLite.
func (s *Store) RecordEvent(ev *Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = now
	}

	delivInt := 0
	if ev.Delivered {
		delivInt = 1
	}

	query := `
	INSERT INTO events (watch_id, event_type, ad_id, payload_json, created_at, delivered)
	VALUES (?, ?, ?, ?, ?, ?)
	`
	res, err := s.db.Exec(query, ev.WatchID, ev.EventType, ev.AdID, ev.PayloadJSON, ev.CreatedAt.Format(time.RFC3339), delivInt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	ev.ID = id
	return nil
}

// ListEvents queries events with optional watchID filter and limit.
func (s *Store) ListEvents(watchID int64, limit int) (EventList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if watchID > 0 {
		rows, err = s.db.Query(`SELECT id, watch_id, event_type, ad_id, payload_json, created_at, delivered FROM events WHERE watch_id = ? ORDER BY id DESC LIMIT ?`, watchID, limit)
	} else {
		rows, err = s.db.Query(`SELECT id, watch_id, event_type, ad_id, payload_json, created_at, delivered FROM events ORDER BY id DESC LIMIT ?`, limit)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list EventList
	for rows.Next() {
		var ev Event
		var createdStr string
		var delivInt int

		if err := rows.Scan(&ev.ID, &ev.WatchID, &ev.EventType, &ev.AdID, &ev.PayloadJSON, &createdStr, &delivInt); err != nil {
			return nil, err
		}

		if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
			ev.CreatedAt = t
		}
		ev.Delivered = delivInt == 1
		list = append(list, ev)
	}

	return list, rows.Err()
}

// MarkEventsDelivered marks event IDs as delivered.
func (s *Store) MarkEventsDelivered(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	query := fmt.Sprintf(`UPDATE events SET delivered = 1 WHERE id IN (%s)`, placeholders)
	_, err := s.db.Exec(query, args...)
	return err
}

// EncodeJSON marshals any object to pretty or compact JSON.
func EncodeJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
