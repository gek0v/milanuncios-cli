package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
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
		a.adFavoriteCmd(),
		a.adUnfavoriteCmd(),
		a.adSchemaCmd(),
		a.adCreateCmd(),
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

func (a *App) adFavoriteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "favorite <id|url>",
		Short: "Add an ad to your account favorites",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.Client.Favorite(cmd.Context(), args[0]); err != nil {
				return err
			}
			id, _ := milanuncios.ExtractAdID(args[0])
			return a.Printer.Print(map[string]string{
				"status": "favorited",
				"ad_id":  id,
			})
		},
	}
}

func (a *App) adUnfavoriteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unfavorite <id|url>",
		Short: "Remove an ad from your account favorites",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.Client.Unfavorite(cmd.Context(), args[0]); err != nil {
				return err
			}
			id, _ := milanuncios.ExtractAdID(args[0])
			return a.Printer.Print(map[string]string{
				"status": "unfavorited",
				"ad_id":  id,
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

func (a *App) adSchemaCmd() *cobra.Command {
	var categoryID int

	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Inspect JSON Schema Draft-07 publication contract for a category",
		Long: `Retrieve the JSON Schema Draft-07 publication contract for a category.

Maps required fields, types, constraints and dynamic taxonomy filters under 'attributes'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if categoryID <= 0 {
				return output.Usagef("--category ID must be greater than 0")
			}

			schema, err := a.Client.GetCategorySchema(cmd.Context(), categoryID)
			if err != nil {
				return err
			}

			return a.Printer.Print(schema)
		},
	}

	cmd.Flags().IntVarP(&categoryID, "category", "c", 0, "target category ID (required)")
	_ = cmd.MarkFlagRequired("category")

	return cmd
}

func (a *App) adCreateCmd() *cobra.Command {
	var (
		fromFile   string
		inlineSpec string
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Validate or publish an ad listing from declarative specification",
		Long: `Create or validate an ad listing using a declarative AdSpec JSON document.

Supports file input (--from-file <path>), stdin (--from-file -), or inline payload (--spec '<json>').
By default, --dry-run is true to simulate validation without creating the listing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fromFile == "" && inlineSpec == "" {
				return output.Usagef("must provide ad spec via --from-file or --spec")
			}
			if fromFile != "" && inlineSpec != "" {
				return output.Usagef("cannot specify both --from-file and --spec")
			}

			var specBytes []byte
			if inlineSpec != "" {
				specBytes = []byte(inlineSpec)
			} else if fromFile == "-" {
				var err error
				specBytes, err = io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
			} else {
				var err error
				specBytes, err = os.ReadFile(fromFile)
				if err != nil {
					return output.Usagef("reading ad spec file %q: %v", fromFile, err)
				}
			}

			if len(bytes.TrimSpace(specBytes)) == 0 {
				return output.Usagef("ad spec payload is empty")
			}

			var spec milanuncios.AdSpec
			if err := json.Unmarshal(specBytes, &spec); err != nil {
				return output.Usagef("invalid JSON ad spec: %v", err)
			}

			report, err := a.Client.ValidateAdSpec(cmd.Context(), &spec)
			if err != nil {
				return err
			}

			if err := a.Printer.Print(report); err != nil {
				return err
			}

			if !report.Valid {
				return output.Usagef("ad validation failed with %d error(s)", len(report.Errors))
			}

			if dryRun {
				return nil
			}

			fmt.Fprintln(a.Stderr, "Notice: dry-run validation succeeded. Real ad publishing endpoint will activate in Slice 13.")
			return nil
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to ad spec JSON file or '-' for stdin")
	cmd.Flags().StringVar(&inlineSpec, "spec", "", "inline ad spec JSON payload")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "simulate validation without creating listing")

	return cmd
}
