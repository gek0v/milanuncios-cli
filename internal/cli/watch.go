package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/sink"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/gek0v/milanuncios-cli/internal/watch"
	"github.com/spf13/cobra"
)

func parseIntervalSec(val string) (int, error) {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return 300, nil
	}

	// Try pure seconds integer
	if sec, err := strconv.Atoi(trimmed); err == nil && sec > 0 {
		if sec < 10 {
			return 0, output.Usagef("interval must be at least 10 seconds")
		}
		return sec, nil
	}

	// Try duration string like 5m, 30s, 1h
	dur, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, output.Usagef("invalid interval format %q (use e.g. 5m, 300s, 1h)", val)
	}

	sec := int(dur.Seconds())
	if sec < 10 {
		return 0, output.Usagef("interval must be at least 10 seconds")
	}

	return sec, nil
}

func (a *App) getStore() (*store.Store, error) {
	return store.Open(a.Paths.StateDB)
}

func (a *App) newWatchEngine(st *store.Store) *watch.Engine {
	eng := watch.NewEngine(st, a.Client)
	if a.Cfg != nil && len(a.Cfg.Sinks) > 0 {
		if mgr, err := sink.NewManager(a.Cfg.Sinks); err == nil {
			eng.Sinks = mgr
		}
	}
	return eng
}

func (a *App) watchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Monitor searches and listings with local SQLite engine",
	}

	cmd.AddCommand(
		a.watchAddCmd(),
		a.watchListCmd(),
		a.watchCheckCmd(),
		a.watchPauseCmd(),
		a.watchResumeCmd(),
		a.watchRemoveCmd(),
		a.watchEventsCmd(),
		a.watchRunCmd(),
	)

	return cmd
}

func (a *App) watchAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a search query or ad listing to monitoring",
	}

	cmd.AddCommand(
		a.watchAddSearchCmd(),
		a.watchAddAdCmd(),
		a.watchAddFavoritesCmd(),
	)

	return cmd
}

func (a *App) watchAddFavoritesCmd() *cobra.Command {
	var (
		name     string
		interval string
	)

	defInterval := "10m"
	if a.Cfg != nil && a.Cfg.Watch.Interval != "" {
		defInterval = a.Cfg.Watch.Interval
	}

	cmd := &cobra.Command{
		Use:   "favorites",
		Short: "Watch account favorites for price drops and removals",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := parseIntervalSec(interval)
			if err != nil {
				return err
			}

			if name == "" {
				name = "favorites"
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			w := &store.Watch{
				Type:        "favorites",
				Name:        name,
				Target:      "favorites",
				QueryJSON:   "{}",
				IntervalSec: sec,
			}

			if err := st.AddWatch(w); err != nil {
				return fmt.Errorf("saving watch: %w", err)
			}

			return a.Printer.Print(w)
		},
	}

	cmd.Flags().StringVar(&name, "name", "favorites", "friendly name for this watch")
	cmd.Flags().StringVar(&interval, "interval", defInterval, "check interval (e.g. 10m, 300s, 1h)")

	return cmd
}

func (a *App) watchAddSearchCmd() *cobra.Command {
	var (
		name     string
		interval string
		minPrice float64
		maxPrice float64
		province string
		category int
		sort     string
	)

	cmd := &cobra.Command{
		Use:   "search <keywords>",
		Short: "Watch a search query for new listings and price changes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keywords := strings.TrimSpace(args[0])
			if keywords == "" {
				return output.Usagef("search keywords cannot be empty")
			}

			sec, err := parseIntervalSec(interval)
			if err != nil {
				return err
			}

			if name == "" {
				name = keywords
			}

			q := milanuncios.SearchQuery{
				Keywords: keywords,
				MinPrice: minPrice,
				MaxPrice: maxPrice,
				Province: province,
				Category: category,
				Sort:     sort,
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			w := &store.Watch{
				Type:        "search",
				Name:        name,
				Target:      keywords,
				QueryJSON:   store.EncodeJSON(q),
				IntervalSec: sec,
			}

			if err := st.AddWatch(w); err != nil {
				return fmt.Errorf("saving watch: %w", err)
			}

			return a.Printer.Print(w)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "friendly name for this watch")
	cmd.Flags().StringVar(&interval, "interval", "5m", "check interval (e.g. 5m, 300s, 1h)")
	cmd.Flags().Float64Var(&minPrice, "min-price", 0, "minimum price in EUR")
	cmd.Flags().Float64Var(&maxPrice, "max-price", 0, "maximum price in EUR")
	cmd.Flags().StringVar(&province, "province", "", "filter by province")
	cmd.Flags().IntVar(&category, "category", 0, "filter by category ID")
	cmd.Flags().StringVar(&sort, "sort", "newest", "sorting: newest, relevance, price_asc, price_desc")

	return cmd
}

func (a *App) watchAddAdCmd() *cobra.Command {
	var (
		name     string
		interval string
	)

	cmd := &cobra.Command{
		Use:   "ad <id-or-url>",
		Short: "Watch a specific ad listing for price drops and removal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := milanuncios.ExtractAdID(args[0])
			if err != nil {
				return err
			}

			sec, err := parseIntervalSec(interval)
			if err != nil {
				return err
			}

			if name == "" {
				name = "ad-" + id
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			w := &store.Watch{
				Type:        "ad",
				Name:        name,
				Target:      id,
				IntervalSec: sec,
			}

			if err := st.AddWatch(w); err != nil {
				return fmt.Errorf("saving watch: %w", err)
			}

			return a.Printer.Print(w)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "friendly name for this watch")
	cmd.Flags().StringVar(&interval, "interval", "5m", "check interval (e.g. 5m, 300s, 1h)")

	return cmd
}

func (a *App) watchListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured watches",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			list, err := st.ListWatches()
			if err != nil {
				return err
			}
			if list == nil {
				list = store.WatchList{}
			}

			return a.Printer.Print(list)
		},
	}
}

func (a *App) watchCheckCmd() *cobra.Command {
	var checkAll bool

	cmd := &cobra.Command{
		Use:   "check [id-or-name]",
		Short: "Execute a check cycle immediately",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			eng := a.newWatchEngine(st)

			if len(args) == 1 {
				w, err := st.GetWatch(args[0])
				if err != nil {
					if err == store.ErrNotFound {
						return output.NotFoundf("watch %q not found", args[0])
					}
					return err
				}

				res, err := eng.CheckWatch(cmd.Context(), w)
				if err != nil {
					return err
				}
				return a.Printer.Print(res)
			}

			// Check all active watches
			results, err := eng.CheckAll(cmd.Context())
			if err != nil {
				return err
			}

			return a.Printer.Print(results)
		},
	}

	cmd.Flags().BoolVar(&checkAll, "all", false, "check all active watches")

	return cmd
}

func (a *App) watchPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <id-or-name>",
		Short: "Pause an active watch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			w, err := st.GetWatch(args[0])
			if err != nil {
				if err == store.ErrNotFound {
					return output.NotFoundf("watch %q not found", args[0])
				}
				return err
			}

			if err := st.UpdateWatchStatus(w.ID, "paused"); err != nil {
				return err
			}

			return a.Printer.Print(map[string]any{
				"status": "paused",
				"id":     w.ID,
				"name":   w.Name,
			})
		},
	}
}

func (a *App) watchResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <id-or-name>",
		Short: "Resume a paused watch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			w, err := st.GetWatch(args[0])
			if err != nil {
				if err == store.ErrNotFound {
					return output.NotFoundf("watch %q not found", args[0])
				}
				return err
			}

			if err := st.UpdateWatchStatus(w.ID, "active"); err != nil {
				return err
			}

			return a.Printer.Print(map[string]any{
				"status": "active",
				"id":     w.ID,
				"name":   w.Name,
			})
		},
	}
}

func (a *App) watchRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <id-or-name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Delete a watch and its recorded history",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			if err := st.DeleteWatch(args[0]); err != nil {
				if err == store.ErrNotFound {
					return output.NotFoundf("watch %q not found", args[0])
				}
				return err
			}

			return a.Printer.Print(map[string]any{
				"status": "removed",
				"watch":  args[0],
			})
		},
	}
}

func (a *App) watchEventsCmd() *cobra.Command {
	var (
		watchID int64
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Query recorded change events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			events, err := st.ListEvents(watchID, limit)
			if err != nil {
				return err
			}
			if events == nil {
				events = store.EventList{}
			}

			return a.Printer.Print(events)
		},
	}

	cmd.Flags().Int64Var(&watchID, "watch", 0, "filter events by watch ID")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum number of events to show")

	return cmd
}

func (a *App) watchRunCmd() *cobra.Command {
	var interval string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start daemon runner checking active watches periodically",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := parseIntervalSec(interval)
			if err != nil {
				return err
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			eng := a.newWatchEngine(st)
			ticker := time.NewTicker(time.Duration(sec) * time.Second)
			defer ticker.Stop()

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			var blockedUntil time.Time
			if stored, err := st.GetBlockedUntil(); err == nil && stored != nil {
				if time.Now().Before(*stored) {
					blockedUntil = *stored
					a.Client.SetBlockedUntil(*stored)
					fmt.Fprintf(a.Stderr, "circuit breaker active: cooling down until %s\n", stored.Local().Format("15:04:05"))
				}
			}

			prevOnBlock := a.Client.OnBlock
			a.Client.OnBlock = func(until time.Time) {
				blockedUntil = until
				_ = st.SetBlockedUntil(until)
				if prevOnBlock != nil {
					prevOnBlock(until)
				}
				fmt.Fprintf(a.Stderr, "circuit breaker triggered: paused until %s\n", until.Local().Format("15:04:05"))
			}

			runCycle := func() {
				if !blockedUntil.IsZero() && time.Now().Before(blockedUntil) {
					fmt.Fprintf(a.Stderr, "watch check paused (cooling down until %s)\n", blockedUntil.Local().Format("15:04:05"))
					return
				}
				results, err := eng.CheckAll(ctx)
				if err != nil {
					if output.IsKind(err, output.KindBlocked) {
						fmt.Fprintf(a.Stderr, "watch check aborted due to anti-bot block (cooling down until %s)\n", blockedUntil.Local().Format("15:04:05"))
						return
					}
					fmt.Fprintf(a.Stderr, "watch check error: %v\n", err)
					return
				}
				if !blockedUntil.IsZero() && time.Now().After(blockedUntil) {
					blockedUntil = time.Time{}
					_ = st.ClearBlockedUntil()
					a.Client.ClearBlock()
				}
				for _, r := range results {
					if len(r.Events) > 0 {
						_ = a.Printer.Print(r.Events)
					}
				}
			}

			fmt.Fprintf(a.Stderr, "milanuncios-cli watch engine running (interval: %ds). Press Ctrl+C to stop.\n", sec)

			// Initial check on start
			runCycle()

			for {
				select {
				case <-ctx.Done():
					fmt.Fprintln(a.Stderr, "\nStopping watch engine.")
					return &output.DomainError{
						Kind: output.KindInterrupted,
						Msg:  "watch daemon interrupted by signal",
					}
				case <-ticker.C:
					runCycle()
				}
			}
		},
	}

	cmd.Flags().StringVar(&interval, "interval", "5m", "polling interval (e.g. 5m, 300s, 1h)")

	return cmd
}
