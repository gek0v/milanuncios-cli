package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/store"
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
		a.adUpdateCmd(),
		a.adDeleteCmd(),
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
				Photos: item.PhotoURLs(),
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
	cmd := &cobra.Command{
		Use:   "favorite <id|url>",
		Short: "Add an ad to your account favorites, or manage local annotations",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if len(args) > 1 {
				return output.Usagef("accepts 1 arg(s), received %d", len(args))
			}
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

	cmd.AddCommand(
		a.favoriteTagCmd(),
		a.favoriteNotesCmd(),
	)

	return cmd
}

type favoriteAnnotationConfirmation struct {
	Status      string    `json:"status"`
	AdID        string    `json:"ad_id"`
	Tag         string    `json:"tag"`
	Note        string    `json:"note,omitempty"`
	TargetPrice float64   `json:"target_price,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (c favoriteAnnotationConfirmation) Pretty(w io.Writer, color bool) {
	if color {
		fmt.Fprintf(w, "\x1b[32m✔ Anotación guardada para anuncio #%s\x1b[0m\n", c.AdID)
	} else {
		fmt.Fprintf(w, "✔ Anotación guardada para anuncio #%s\n", c.AdID)
	}
	fmt.Fprintf(w, "Tag:         %s\n", c.Tag)
	if c.Note != "" {
		fmt.Fprintf(w, "Nota:        %s\n", c.Note)
	}
	if c.TargetPrice > 0 {
		fmt.Fprintf(w, "Precio Obj:  %.2f €\n", c.TargetPrice)
	}
}

func (a *App) favoriteTagCmd() *cobra.Command {
	var (
		note        string
		targetPrice float64
	)

	cmd := &cobra.Command{
		Use:   "tag <ad-id|url> <tag>",
		Short: "Save local agent annotation, note and target price for an ad",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			adID, err := milanuncios.ExtractAdID(args[0])
			if err != nil {
				return output.Usagef("invalid ad ID or URL: %v", err)
			}
			tag := strings.TrimSpace(args[1])
			if tag == "" {
				return output.Usagef("tag cannot be empty")
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			if err := st.SaveAnnotation(cmd.Context(), adID, tag, note, targetPrice); err != nil {
				return fmt.Errorf("saving annotation: %w", err)
			}

			ann, err := st.GetAnnotation(cmd.Context(), adID)
			if err != nil {
				return fmt.Errorf("reading annotation: %w", err)
			}

			return a.Printer.Print(favoriteAnnotationConfirmation{
				Status:      "annotated",
				AdID:        ann.AdID,
				Tag:         ann.Tag,
				Note:        ann.Note,
				TargetPrice: ann.TargetPrice,
				UpdatedAt:   ann.UpdatedAt,
			})
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Agent private note or negotiation status")
	cmd.Flags().Float64Var(&targetPrice, "target-price", 0, "Target buy/negotiation price in EUR")

	return cmd
}

func (a *App) favoriteNotesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notes <ad-id|url>",
		Short: "Display local agent annotation and notes for an ad",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adID, err := milanuncios.ExtractAdID(args[0])
			if err != nil {
				return output.Usagef("invalid ad ID or URL: %v", err)
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			ann, err := st.GetAnnotation(cmd.Context(), adID)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return output.NotFoundf("annotation for ad %s not found", adID)
				}
				return fmt.Errorf("reading annotation: %w", err)
			}

			return a.Printer.Print(ann)
		},
	}

	return cmd
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

			if !dryRun && a.Client.SessionToken == "" {
				return &output.DomainError{
					Kind:     output.KindAuth,
					Endpoint: "POST /api/v1/ads",
					Msg:      "publishing an ad requires an authenticated profile. Run `milanuncios auth login`",
				}
			}

			if dryRun {
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

				return nil
			}

			// Non-dry-run: validate first so invalid spec prints report with exit code 2
			report, err := a.Client.ValidateAdSpec(cmd.Context(), &spec)
			if err != nil {
				return err
			}
			if !report.Valid {
				_ = a.Printer.Print(report)
				return output.Usagef("ad validation failed with %d error(s)", len(report.Errors))
			}

			ad, err := a.Client.CreateAd(cmd.Context(), &spec)
			if err != nil {
				return err
			}
			return a.Printer.Print(ad)
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to ad spec JSON file or '-' for stdin")
	cmd.Flags().StringVar(&inlineSpec, "spec", "", "inline ad spec JSON payload")
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "simulate validation without creating listing")

	return cmd
}

func (a *App) adUpdateCmd() *cobra.Command {
	var (
		inlineSpec  string
		patchFile   string
		price       float64
		title       string
		description string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an existing ad listing using RFC 7396 JSON merge patch",
		Long: `Update an existing ad listing with partial patch object.

Supports inline patch (--spec '{"price": 1200}'), patch file (--patch-file <path> or - for stdin),
or shortcut flags (--price, --title, --description).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if inlineSpec != "" && patchFile != "" {
				return output.Usagef("cannot specify both --spec and --patch-file")
			}

			patch := make(map[string]interface{})

			if inlineSpec != "" {
				if err := json.Unmarshal([]byte(inlineSpec), &patch); err != nil {
					return output.Usagef("invalid JSON patch in --spec: %v", err)
				}
			} else if patchFile != "" {
				var patchBytes []byte
				if patchFile == "-" {
					var err error
					patchBytes, err = io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return fmt.Errorf("reading stdin: %w", err)
					}
				} else {
					var err error
					patchBytes, err = os.ReadFile(patchFile)
					if err != nil {
						return output.Usagef("reading patch file %q: %v", patchFile, err)
					}
				}
				if len(bytes.TrimSpace(patchBytes)) == 0 {
					return output.Usagef("patch file payload is empty")
				}
				if err := json.Unmarshal(patchBytes, &patch); err != nil {
					return output.Usagef("invalid JSON patch in --patch-file: %v", err)
				}
			}

			if cmd.Flags().Changed("price") {
				if price <= 0 {
					return output.Usagef("price must be greater than 0")
				}
				patch["price"] = price
			}
			if cmd.Flags().Changed("title") {
				if strings.TrimSpace(title) == "" {
					return output.Usagef("title cannot be empty")
				}
				patch["title"] = strings.TrimSpace(title)
			}
			if cmd.Flags().Changed("description") {
				if strings.TrimSpace(description) == "" {
					return output.Usagef("description cannot be empty")
				}
				patch["description"] = strings.TrimSpace(description)
			}

			if len(patch) == 0 {
				return output.Usagef("no update fields provided. Specify --spec, --patch-file, or shortcuts (--price, --title, --description)")
			}

			result, err := a.Client.UpdateAd(cmd.Context(), args[0], patch)
			if err != nil {
				return err
			}

			return a.Printer.Print(result)
		},
	}

	cmd.Flags().StringVar(&inlineSpec, "spec", "", "inline JSON patch object")
	cmd.Flags().StringVar(&patchFile, "patch-file", "", "path to JSON patch file or '-' for stdin")
	cmd.Flags().Float64Var(&price, "price", 0, "new price in Euros")
	cmd.Flags().StringVar(&title, "title", "", "new ad title")
	cmd.Flags().StringVar(&description, "description", "", "new ad description")

	return cmd
}

func (a *App) adDeleteCmd() *cobra.Command {
	var (
		reason   string
		feedback string
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete or withdraw an active ad listing",
		Long: `Withdraw or remove an active ad listing with a structured reason.

Valid reasons:
  - sold_milanuncios: Item sold through Milanuncios
  - sold_other: Item sold via another platform or offline
  - withdrawn: Listing withdrawn by seller (default)
  - expired: Listing is no longer available or valid`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.Client.DeleteAd(cmd.Context(), args[0], reason, feedback)
			if err != nil {
				return err
			}
			return a.Printer.Print(res)
		},
	}

	cmd.Flags().StringVarP(&reason, "reason", "r", "withdrawn", "reason for deletion (sold_milanuncios, sold_other, withdrawn, expired)")
	cmd.Flags().StringVar(&feedback, "feedback", "", "optional closing feedback or comments")

	return cmd
}
