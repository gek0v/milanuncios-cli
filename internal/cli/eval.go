package cli

import (
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/eval"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/spf13/cobra"
)

func (a *App) evaluateCmd() *cobra.Command {
	var includeComparables bool

	cmd := &cobra.Command{
		Use:     "evaluate <id|url>",
		Aliases: []string{"eval"},
		Short:   "Evaluate the fair market price and calculate Deal Score for an ad",
		Long: `Evaluate the fair market price and calculate the Deal Score (0-100) of an ad listing.

Uses robust non-parametric statistical benchmarks (IQR outlier cleaning and median comparison)
to detect real bargains without AI hallucinations.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.TrimSpace(args[0])
			if target == "" {
				return output.Usagef("ad ID or URL is required")
			}

			var st *store.Store
			if s, err := a.getStore(); err == nil && s != nil {
				st = s
				defer st.Close()
			}

			evaluator := eval.NewEvaluator(a.Client, st)
			report, err := evaluator.EvaluateAd(cmd.Context(), target, includeComparables)
			if err != nil {
				return err
			}

			return a.Printer.Print(report)
		},
	}

	cmd.Flags().BoolVar(&includeComparables, "include-comparables", false, "include top comparable listings in the output")

	return cmd
}
