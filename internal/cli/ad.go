package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/spf13/cobra"
)

func (a *App) adCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ad",
		Aliases: []string{"item"},
		Short:   "Inspect and interact with Milanuncios ads",
	}

	cmd.AddCommand(
		a.adShowCmd(),
		a.adImagesCmd(),
		a.adOpenCmd(),
	)

	return cmd
}

func (a *App) adShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id|url>",
		Short: "Display full details of an ad",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := a.Client.GetAd(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.Printer.Print(item)
		},
	}
}

func (a *App) adImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "images <id|url>",
		Short: "List all photo URLs for an ad",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := a.Client.GetAd(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			result := &milanuncios.ImagesResult{
				AdID:   item.ID,
				Photos: item.Photos,
			}
			return a.Printer.Print(result)
		},
	}
}

func (a *App) adOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <id|url>",
		Short: "Open an ad listing in your default browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := a.Client.GetAd(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if a.Interactive {
				fmt.Fprintf(a.Stderr, "Opening %s in browser...\n", item.URL)
			}

			if err := openBrowser(item.URL); err != nil {
				return fmt.Errorf("opening browser: %w", err)
			}

			return a.Printer.Print(map[string]string{
				"status": "opened",
				"url":    item.URL,
			})
		},
	}
}

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}
