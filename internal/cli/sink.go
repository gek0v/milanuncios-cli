package cli

import (
	"fmt"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/sink"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/spf13/cobra"
)

func (a *App) sinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sink",
		Short: "Manage and test notification sinks (ntfy, webhooks, exec)",
	}

	cmd.AddCommand(
		a.sinkListCmd(),
		a.sinkTestCmd(),
	)

	return cmd
}

func (a *App) sinkListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured notification sinks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var list sink.SinkInfoList
			if a.Cfg != nil && a.Cfg.Sinks != nil {
				for name, sc := range a.Cfg.Sinks {
					list = append(list, sink.SinkInfo{
						Name: name,
						Type: sc.Type,
					})
				}
			}

			return a.Printer.Print(list)
		},
	}
}

func (a *App) sinkTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <name>",
		Short: "Send a mock test event to a configured notification sink",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if a.Cfg == nil || a.Cfg.Sinks == nil {
				return output.NotFoundf("no sinks configured")
			}

			sc, exists := a.Cfg.Sinks[name]
			if !exists {
				return output.NotFoundf("sink %q not found in config", name)
			}

			s, err := sink.FromConfig(name, sc)
			if err != nil {
				return err
			}

			mockEvent := store.Event{
				ID:        999999,
				WatchID:   1,
				EventType: "ad.new",
				AdID:      "test-123456",
				PayloadJSON: store.EncodeJSON(map[string]any{
					"event":    "ad.new",
					"watch_id": 1,
					"name":     "Test Watch",
					"ad": map[string]any{
						"id":    "test-123456",
						"title": "Anuncio de prueba para verificar sink",
						"price": 120.0,
						"url":   "https://www.milanuncios.com/anuncios/test-123456.htm",
					},
				}),
				CreatedAt: time.Now().UTC(),
			}

			if err := s.Send(cmd.Context(), mockEvent); err != nil {
				return fmt.Errorf("sink test failed: %w", err)
			}

			return a.Printer.Print(map[string]any{
				"status":  "ok",
				"message": fmt.Sprintf("test notification sent successfully to sink %q", name),
				"sink":    name,
				"type":    s.Type(),
			})
		},
	}
}
