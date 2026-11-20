package store

import (
	"context"
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

// EventDelivery tracks notification delivery state and retry attempts per sink.
type EventDelivery struct {
	EventID       int64     `json:"event_id"`
	SinkName      string    `json:"sink_name"`
	Status        string    `json:"status"` // "delivered", "failed"
	Attempts      int       `json:"attempts"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
	LastError     string    `json:"last_error,omitempty"`
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

// FavoriteAnnotation stores local agent metadata and notes for an ad.
type FavoriteAnnotation struct {
	AdID        string    `json:"ad_id"`
	Tag         string    `json:"tag"`
	Note        string    `json:"note"`
	TargetPrice float64   `json:"target_price"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Pretty renders the annotation for human reading.
func (fa *FavoriteAnnotation) Pretty(w io.Writer, color bool) {
	if color {
		fmt.Fprintf(w, "\x1b[1mAnotación de Favorito #%s\x1b[0m\n", fa.AdID)
	} else {
		fmt.Fprintf(w, "Anotación de Favorito #%s\n", fa.AdID)
	}
	fmt.Fprintf(w, "Tag:          %s\n", fa.Tag)
	if fa.Note != "" {
		fmt.Fprintf(w, "Nota:         %s\n", fa.Note)
	}
	if fa.TargetPrice > 0 {
		fmt.Fprintf(w, "Precio Obj:   %.2f €\n", fa.TargetPrice)
	}
	fmt.Fprintf(w, "Actualizado:  %s\n", fa.UpdatedAt.Local().Format("2006-01-02 15:04:05"))
}

// Store handles persistent SQLite storage for watches, seen items, and events.
type Store struct {
	dbPath string
	db     *sql.DB
	mu     sync.Mutex
}

// Open initializes SQLite connection and runs migrations.
func Open(dbPath string) (*Store, error) {
	var dsn string
	if dbPath == ":memory:" {
		dsn = ":memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	} else {
		cleanPath := filepath.ToSlash(dbPath)
		if strings.HasPrefix(cleanPath, "file:") {
			cleanPath = strings.TrimPrefix(cleanPath, "file:")
		}
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating db directory: %w", err)
		}
		if strings.Contains(cleanPath, "?") {
			dsn = fmt.Sprintf("file:%s&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", cleanPath)
		} else {
			dsn = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", cleanPath)
		}
	}

	db, err := sql.Open("sqlite", dsn)
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

	CREATE TABLE IF NOT EXISTS event_deliveries (
		event_id INTEGER NOT NULL,
		sink_name TEXT NOT NULL,
		status TEXT NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0,
		last_attempt_at TEXT NOT NULL,
		last_error TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (event_id, sink_name),
		FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS favorite_annotations (
		ad_id TEXT PRIMARY KEY,
		tag TEXT NOT NULL,
		note TEXT NOT NULL DEFAULT '',
		target_price REAL NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS app_state (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS telegram_message_map (
		telegram_msg_id INTEGER PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		ad_id           TEXT NOT NULL,
		ad_title        TEXT NOT NULL,
		opponent_name   TEXT NOT NULL,
		created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_watches_name ON watches(name);
	CREATE INDEX IF NOT EXISTS idx_seen_items_watch ON seen_items(watch_id);
	CREATE INDEX IF NOT EXISTS idx_seen_items_last_seen ON seen_items(last_seen_at);
	CREATE INDEX IF NOT EXISTS idx_events_watch ON events(watch_id);
	CREATE INDEX IF NOT EXISTS idx_events_delivered ON events(delivered);
	CREATE INDEX IF NOT EXISTS idx_events_ad ON events(ad_id);
	CREATE INDEX IF NOT EXISTS idx_event_deliveries_sink ON event_deliveries(sink_name, status);
	CREATE INDEX IF NOT EXISTS idx_fav_annotations_tag ON favorite_annotations(tag);
	CREATE INDEX IF NOT EXISTS idx_tg_map_conv ON telegram_message_map(conversation_id);
	`
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		_, err = s.db.Exec(schema)
		if err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}

// AddWatch inserts a new watch.
func (s *Store) AddWatch(ctx context.Context, w *Watch) error {
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
	res, err := s.db.ExecContext(ctx, query, w.Type, w.Name, w.Target, w.QueryJSON, w.IntervalSec, w.CreatedAt.Format(time.RFC3339), w.Status)
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
func (s *Store) GetWatch(ctx context.Context, idOrName string) (*Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getWatchLocked(ctx, idOrName)
}

// ListWatches returns all configured watches.
func (s *Store) ListWatches(ctx context.Context) (WatchList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT id, type, name, target, query_json, interval_sec, last_checked_at, created_at, status FROM watches ORDER BY id ASC`)
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
func (s *Store) UpdateWatchStatus(ctx context.Context, id int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.ExecContext(ctx, `UPDATE watches SET status = ? WHERE id = ?`, status, id)
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
func (s *Store) UpdateWatchChecked(ctx context.Context, id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `UPDATE watches SET last_checked_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), id)
	return err
}

// DeleteWatch removes a watch and its associated seen items and events.
func (s *Store) DeleteWatch(ctx context.Context, idOrName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, err := s.getWatchLocked(ctx, idOrName)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM seen_items WHERE watch_id = ?`, w.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE watch_id = ?`, w.ID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM watches WHERE id = ?`, w.ID)
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
	return tx.Commit()
}

func (s *Store) getWatchLocked(ctx context.Context, idOrName string) (*Watch, error) {
	trimmed := strings.TrimSpace(idOrName)
	query := `SELECT id, type, name, target, query_json, interval_sec, last_checked_at, created_at, status FROM watches WHERE id = ? OR name = ? LIMIT 1`

	row := s.db.QueryRowContext(ctx, query, trimmed, trimmed)
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

// GetSeenItems returns map of ad_id -> SeenItem for a given watch.
// Omits raw_json to conserve memory during diffing cycles.
func (s *Store) GetSeenItems(ctx context.Context, watchID int64) (map[string]SeenItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT watch_id, ad_id, first_seen_at, last_seen_at, last_price, last_title FROM seen_items WHERE watch_id = ?`, watchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]SeenItem)
	for rows.Next() {
		var it SeenItem
		var firstStr, lastStr string

		if err := rows.Scan(&it.WatchID, &it.AdID, &firstStr, &lastStr, &it.LastPrice, &it.LastTitle); err != nil {
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

// GetSeenItem retrieves a single seen ad with its full raw_json.
func (s *Store) GetSeenItem(watchID int64, adID string) (*SeenItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var it SeenItem
	var firstStr, lastStr string
	query := `SELECT watch_id, ad_id, first_seen_at, last_seen_at, last_price, last_title, raw_json FROM seen_items WHERE watch_id = ? AND ad_id = ? LIMIT 1`
	err := s.db.QueryRow(query, watchID, adID).Scan(&it.WatchID, &it.AdID, &firstStr, &lastStr, &it.LastPrice, &it.LastTitle, &it.RawJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339, firstStr); err == nil {
		it.FirstSeenAt = t
	}
	if t, err := time.Parse(time.RFC3339, lastStr); err == nil {
		it.LastSeenAt = t
	}

	return &it, nil
}

// DefaultSeenItemsTTL is the default retention period for seen ads (30 days).
const DefaultSeenItemsTTL = 30 * 24 * time.Hour

// PruneSeenItems removes seen items not seen since the given duration (e.g. 30 days).
// If olderThan <= 0, DefaultSeenItemsTTL (30 days) is used.
// Returns the number of pruned items.
func (s *Store) PruneSeenItems(olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		olderThan = DefaultSeenItemsTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM seen_items WHERE last_seen_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertSeenItem records or updates a seen ad.
func (s *Store) UpsertSeenItem(ctx context.Context, it *SeenItem) error {
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
	_, err := s.db.ExecContext(ctx, query,
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
func (s *Store) RecordEvent(ctx context.Context, ev *Event) error {
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
	res, err := s.db.ExecContext(ctx, query, ev.WatchID, ev.EventType, ev.AdID, ev.PayloadJSON, ev.CreatedAt.Format(time.RFC3339), delivInt)
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
func (s *Store) ListEvents(ctx context.Context, watchID int64, limit int) (EventList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if watchID > 0 {
		rows, err = s.db.QueryContext(ctx, `SELECT id, watch_id, event_type, ad_id, payload_json, created_at, delivered FROM events WHERE watch_id = ? ORDER BY id DESC LIMIT ?`, watchID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT id, watch_id, event_type, ad_id, payload_json, created_at, delivered FROM events ORDER BY id DESC LIMIT ?`, limit)
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
func (s *Store) MarkEventsDelivered(ctx context.Context, ids []int64) error {
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
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// GetUndeliveredEvents returns events with delivered = 0 for a watch (or all watches if watchID == 0).
func (s *Store) GetUndeliveredEvents(ctx context.Context, watchID int64, limit int) (EventList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error
	if watchID > 0 {
		rows, err = s.db.QueryContext(ctx, `SELECT id, watch_id, event_type, ad_id, payload_json, created_at, delivered FROM events WHERE delivered = 0 AND watch_id = ? ORDER BY id ASC LIMIT ?`, watchID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT id, watch_id, event_type, ad_id, payload_json, created_at, delivered FROM events WHERE delivered = 0 ORDER BY id ASC LIMIT ?`, limit)
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

// RecordDeliveryAttempt tracks or updates delivery status and backoff for a sink and event.
func (s *Store) RecordDeliveryAttempt(ctx context.Context, eventID int64, sinkName string, deliveryErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	status := "delivered"
	errMsg := ""
	if deliveryErr != nil {
		status = "failed"
		errMsg = deliveryErr.Error()
	}

	query := `
	INSERT INTO event_deliveries (event_id, sink_name, status, attempts, last_attempt_at, last_error)
	VALUES (?, ?, ?, 1, ?, ?)
	ON CONFLICT(event_id, sink_name) DO UPDATE SET
		status = excluded.status,
		attempts = event_deliveries.attempts + 1,
		last_attempt_at = excluded.last_attempt_at,
		last_error = excluded.last_error
	`
	_, err := s.db.ExecContext(ctx, query, eventID, sinkName, status, now, errMsg)
	return err
}

// GetEventDeliveries returns delivery attempts per sink for a given event ID.
func (s *Store) GetEventDeliveries(ctx context.Context, eventID int64) ([]EventDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT event_id, sink_name, status, attempts, last_attempt_at, last_error FROM event_deliveries WHERE event_id = ?`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []EventDelivery
	for rows.Next() {
		var ed EventDelivery
		var lastAttemptStr string
		if err := rows.Scan(&ed.EventID, &ed.SinkName, &ed.Status, &ed.Attempts, &lastAttemptStr, &ed.LastError); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, lastAttemptStr); err == nil {
			ed.LastAttemptAt = t
		}
		list = append(list, ed)
	}
	return list, rows.Err()
}

// DeleteSeenItem removes a seen item record from SQLite (e.g. when an ad is removed from favorites).
func (s *Store) DeleteSeenItem(ctx context.Context, watchID int64, adID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM seen_items WHERE watch_id = ? AND ad_id = ?`, watchID, adID)
	return err
}

// Tx represents an active database transaction in Store.
type Tx struct {
	tx     *sql.Tx
	st     *Store
	closed bool
	mu     sync.Mutex
}

// BeginTx starts a database transaction wrapped in Store Tx.
func (s *Store) BeginTx(ctx context.Context) (*Tx, error) {
	s.mu.Lock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	return &Tx{
		tx: tx,
		st: s,
	}, nil
}

// Commit commits the transaction and releases the store lock.
func (t *Tx) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	defer t.st.mu.Unlock()
	return t.tx.Commit()
}

// Rollback aborts the transaction and releases the store lock.
func (t *Tx) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	defer t.st.mu.Unlock()
	return t.tx.Rollback()
}

// UpsertSeenItem records or updates a seen ad inside the transaction.
func (t *Tx) UpsertSeenItem(it *SeenItem) error {
	query := `
	INSERT INTO seen_items (watch_id, ad_id, first_seen_at, last_seen_at, last_price, last_title, raw_json)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(watch_id, ad_id) DO UPDATE SET
		last_seen_at = excluded.last_seen_at,
		last_price = excluded.last_price,
		last_title = excluded.last_title,
		raw_json = excluded.raw_json
	`
	_, err := t.tx.Exec(query,
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

// RecordEvent saves a detected event inside the transaction.
func (t *Tx) RecordEvent(ev *Event) error {
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
	res, err := t.tx.Exec(query, ev.WatchID, ev.EventType, ev.AdID, ev.PayloadJSON, ev.CreatedAt.Format(time.RFC3339), delivInt)
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

// DeleteSeenItem removes a seen ad inside the transaction.
func (t *Tx) DeleteSeenItem(watchID int64, adID string) error {
	_, err := t.tx.Exec(`DELETE FROM seen_items WHERE watch_id = ? AND ad_id = ?`, watchID, adID)
	return err
}

// UpdateWatchChecked records the timestamp of a completed watch check inside the transaction.
func (t *Tx) UpdateWatchChecked(id int64, at time.Time) error {
	_, err := t.tx.Exec(`UPDATE watches SET last_checked_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), id)
	return err
}

// UpdateWatchStatus sets status ("active", "paused") for a watch inside the transaction.
func (t *Tx) UpdateWatchStatus(id int64, status string) error {
	res, err := t.tx.Exec(`UPDATE watches SET status = ? WHERE id = ?`, status, id)
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

// SaveAnnotation stores or updates agent notes and metadata for an ad.
func (s *Store) SaveAnnotation(ctx context.Context, adID, tag, note string, targetPrice float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	query := `
	INSERT INTO favorite_annotations (ad_id, tag, note, target_price, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(ad_id) DO UPDATE SET
		tag = excluded.tag,
		note = excluded.note,
		target_price = excluded.target_price,
		updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, query, adID, tag, note, targetPrice, now)
	return err
}

// GetAnnotation retrieves the local annotation for an ad, or ErrNotFound if absent.
func (s *Store) GetAnnotation(ctx context.Context, adID string) (*FavoriteAnnotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var fa FavoriteAnnotation
	var updatedStr string
	query := `SELECT ad_id, tag, note, target_price, updated_at FROM favorite_annotations WHERE ad_id = ? LIMIT 1`
	err := s.db.QueryRowContext(ctx, query, adID).Scan(&fa.AdID, &fa.Tag, &fa.Note, &fa.TargetPrice, &updatedStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339, updatedStr); err == nil {
		fa.UpdatedAt = t
	}
	return &fa, nil
}

// EnsureChatWatch returns the existing chat watch or creates one atomically if it doesn't exist.
func (s *Store) EnsureChatWatch(ctx context.Context) (*Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO watches (type, name, target, query_json, interval_sec, created_at, status)
		VALUES ('chat', 'chat_monitor', 'conversations', '{}', 30, ?, 'active')
		ON CONFLICT(name) DO NOTHING
	`, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("ensuring chat watch: %w", err)
	}

	var w Watch
	var lastCheckedStr sql.NullString
	var createdStr string

	err = s.db.QueryRowContext(ctx, `
		SELECT id, type, name, target, query_json, interval_sec, last_checked_at, created_at, status
		FROM watches WHERE name = 'chat_monitor' LIMIT 1
	`).Scan(&w.ID, &w.Type, &w.Name, &w.Target, &w.QueryJSON, &w.IntervalSec, &lastCheckedStr, &createdStr, &w.Status)
	if err != nil {
		return nil, fmt.Errorf("fetching chat watch: %w", err)
	}

	if lastCheckedStr.Valid && lastCheckedStr.String != "" {
		if t, err := time.Parse(time.RFC3339, lastCheckedStr.String); err == nil {
			w.LastCheckedAt = &t
		}
	}
	if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
		w.CreatedAt = t
	}
	return &w, nil
}

// HasSeenChatMessage returns true if a message ID has already been recorded for this chat watch.
func (s *Store) HasSeenChatMessage(ctx context.Context, watchID int64, msgID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seen_items WHERE watch_id = ? AND ad_id = ?`, watchID, "chat_msg:"+msgID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// MarkChatMessageSeen records a message ID as seen for this chat watch.
func (s *Store) MarkChatMessageSeen(ctx context.Context, watchID int64, msgID string, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO seen_items (watch_id, ad_id, first_seen_at, last_seen_at, last_price, last_title, raw_json)
		VALUES (?, ?, ?, ?, 0, ?, '{}')
		ON CONFLICT(watch_id, ad_id) DO UPDATE SET last_seen_at = excluded.last_seen_at
	`, watchID, "chat_msg:"+msgID, now, now, output.Truncate(text, 50))
	return err
}

// SetState records a key-value pair in app_state.
func (s *Store) SetState(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO app_state (key, value, updated_at)
	VALUES (?, ?, ?)
	ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = excluded.updated_at
	`
	_, err := s.db.Exec(query, key, value, time.Now().UTC().Format(time.RFC3339))
	return err
}

// GetState reads a key-value pair from app_state. Returns ErrNotFound if not present.
func (s *Store) GetState(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var val string
	err := s.db.QueryRow(`SELECT value FROM app_state WHERE key = ?`, key).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return val, nil
}

// SetBlockedUntil persists the cooldown expiration time in app_state.
func (s *Store) SetBlockedUntil(until time.Time) error {
	return s.SetState("blocked_until", until.UTC().Format(time.RFC3339))
}

// GetBlockedUntil retrieves the active cooldown expiration time.
// Returns nil if no cooldown is stored.
func (s *Store) GetBlockedUntil() (*time.Time, error) {
	val, err := s.GetState("blocked_until")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ClearBlockedUntil clears any persisted cooldown.
func (s *Store) ClearBlockedUntil() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM app_state WHERE key = 'blocked_until'`)
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

// PricePoint represents an ad's recorded price at a specific date.
type PricePoint struct {
	Date  string  `json:"date"`
	Price float64 `json:"price"`
}

// GetPriceHistory retrieves the chronological price history recorded for an ad.
func (s *Store) GetPriceHistory(ctx context.Context, adID string) ([]PricePoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	SELECT event_type, payload_json, created_at
	FROM events
	WHERE ad_id = ? AND event_type IN ('ad.new', 'ad.price_changed')
	ORDER BY id ASC
	`
	rows, err := s.db.QueryContext(ctx, query, adID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []PricePoint
	for rows.Next() {
		var eventType, payloadJSON, createdStr string
		if err := rows.Scan(&eventType, &payloadJSON, &createdStr); err != nil {
			return nil, err
		}

		dateStr := createdStr
		if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
			dateStr = t.Format("02/01/2006")
		} else if t, err := time.Parse("2006-01-02 15:04:05", createdStr); err == nil {
			dateStr = t.Format("02/01/2006")
		} else if len(dateStr) > 10 {
			dateStr = dateStr[:10]
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			continue
		}

		switch eventType {
		case "ad.new":
			var price float64
			if adMap, ok := payload["ad"].(map[string]any); ok {
				if p, ok := adMap["price"].(float64); ok {
					price = p
				}
			} else if p, ok := payload["price"].(float64); ok {
				price = p
			}
			if price > 0 {
				points = append(points, PricePoint{Date: dateStr, Price: price})
			}
		case "ad.price_changed":
			oldPrice, _ := payload["old_price"].(float64)
			newPrice, _ := payload["new_price"].(float64)
			if len(points) == 0 && oldPrice > 0 {
				points = append(points, PricePoint{Date: dateStr, Price: oldPrice})
			}
			if newPrice > 0 {
				points = append(points, PricePoint{Date: dateStr, Price: newPrice})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(points) == 0 {
		var firstSeenStr string
		var lastPrice float64
		seenQuery := `SELECT first_seen_at, last_price FROM seen_items WHERE ad_id = ? ORDER BY first_seen_at ASC LIMIT 1`
		err := s.db.QueryRowContext(ctx, seenQuery, adID).Scan(&firstSeenStr, &lastPrice)
		if err == nil && lastPrice > 0 {
			dateStr := firstSeenStr
			if t, err := time.Parse(time.RFC3339, firstSeenStr); err == nil {
				dateStr = t.Format("02/01/2006")
			} else if t, err := time.Parse("2006-01-02 15:04:05", firstSeenStr); err == nil {
				dateStr = t.Format("02/01/2006")
			} else if len(dateStr) > 10 {
				dateStr = dateStr[:10]
			}
			points = append(points, PricePoint{Date: dateStr, Price: lastPrice})
		}
	}

	return points, nil
}

// TelegramMessageMap records a sent Telegram message mapped to a Milanuncios conversation.
type TelegramMessageMap struct {
	TelegramMsgID  int64     `json:"telegram_msg_id"`
	ConversationID string    `json:"conversation_id"`
	AdID           string    `json:"ad_id"`
	AdTitle        string    `json:"ad_title"`
	OpponentName   string    `json:"opponent_name"`
	CreatedAt      time.Time `json:"created_at"`
}

// SaveTelegramMessageMap records a mapping between a sent Telegram message and a Milanuncios conversation.
func (s *Store) SaveTelegramMessageMap(ctx context.Context, msgID int64, convID, adID, adTitle, opponentName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	INSERT INTO telegram_message_map (telegram_msg_id, conversation_id, ad_id, ad_title, opponent_name, created_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(telegram_msg_id) DO UPDATE SET
		conversation_id = excluded.conversation_id,
		ad_id = excluded.ad_id,
		ad_title = excluded.ad_title,
		opponent_name = excluded.opponent_name
	`
	_, err := s.db.ExecContext(ctx, query, msgID, convID, adID, adTitle, opponentName, time.Now().UTC().Format(time.RFC3339))
	return err
}

// GetTelegramMessageMap retrieves the mapping for a Telegram message ID, or ErrNotFound if absent.
func (s *Store) GetTelegramMessageMap(ctx context.Context, msgID int64) (*TelegramMessageMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var m TelegramMessageMap
	var createdStr string
	query := `SELECT telegram_msg_id, conversation_id, ad_id, ad_title, opponent_name, created_at FROM telegram_message_map WHERE telegram_msg_id = ? LIMIT 1`
	err := s.db.QueryRowContext(ctx, query, msgID).Scan(&m.TelegramMsgID, &m.ConversationID, &m.AdID, &m.AdTitle, &m.OpponentName, &createdStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
		m.CreatedAt = t
	} else if t, err := time.Parse("2006-01-02 15:04:05", createdStr); err == nil {
		m.CreatedAt = t
	}
	return &m, nil
}
