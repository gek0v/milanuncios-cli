package cli

import (
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/eval"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) searchCmd() *cobra.Command {
	var (
		minPrice     float64
		maxPrice     float64
		province     string
		category     int
		sort         string
		limit        int
		next         string
		filters      []string
		minDealScore int
	)

	cmd := &cobra.Command{
		Use:   "search [keywords]",
		Short: "Search listings on Milanuncios",
		Long: `Search through listings on Milanuncios with filters.

Results are printed as JSON by default on stdout (ideal for jq),
or formatted in a clean table with --format pretty.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			keywords := strings.Join(args, " ")

			// If no province flag is provided, check active profile location
			if province == "" && a.Cfg != nil {
				if prof, ok := a.Cfg.Profiles[a.Profile]; ok && prof.Location != nil {
					province = prof.Location.Province
				}
			}

			if sort != "" {
				switch sort {
				case "relevance", "newest", "price_asc", "price_desc":
				default:
					return output.Usagef("unknown sort %q (allowed: relevance, newest, price_asc, price_desc)", sort)
				}
			}

			if minDealScore < 0 || minDealScore > 100 {
				return output.Usagef("--min-deal-score must be between 0 and 100 (got %d)", minDealScore)
			}

			filterMap, err := parseFilterFlags(filters)
			if err != nil {
				return err
			}

			q := milanuncios.SearchQuery{
				Keywords: keywords,
				MinPrice: minPrice,
				MaxPrice: maxPrice,
				Province: province,
				Category: category,
				Sort:     sort,
				Limit:    limit,
				Next:     next,
				Filters:  filterMap,
			}

			res, err := a.Client.Search(cmd.Context(), q)
			if err != nil {
				return err
			}

			if minDealScore > 0 && res != nil && len(res.Items) > 0 {
				var rawPrices []float64
				for _, it := range res.Items {
					if it.Price > 0 {
						rawPrices = append(rawPrices, it.Price)
					}
				}
				cleanPrices, _ := eval.FilterIQROutliers(rawPrices)
				median := eval.CalculateMedian(cleanPrices)
				sampleSize := len(cleanPrices)

				filtered := make([]milanuncios.AdItem, 0)
				for _, it := range res.Items {
					score, _ := eval.CalculateDealScore(it.Price, median, sampleSize)
					if score >= minDealScore {
						filtered = append(filtered, it)
					}
				}
				res.Items = filtered
				res.Total = len(filtered)
			}

			return a.Printer.Print(res)
		},
	}

	flags := cmd.Flags()
	flags.Float64Var(&minPrice, "min-price", 0, "minimum price in EUR")
	flags.Float64Var(&maxPrice, "max-price", 0, "maximum price in EUR")
	flags.StringVar(&province, "province", "", "filter by Spanish province (e.g. Madrid, Barcelona)")
	flags.IntVar(&category, "category", 0, "filter by category numeric ID")
	flags.StringVar(&sort, "sort", "relevance", "sort order: relevance, newest, price_asc, price_desc")
	flags.IntVar(&limit, "limit", 20, "maximum number of results per page")
	flags.StringVar(&next, "next", "", "pagination cursor for the next page")
	flags.StringArrayVar(&filters, "filter", nil, "category-specific filter in key=value format (repeatable)")
	flags.IntVar(&minDealScore, "min-deal-score", 0, "filter results by minimum Deal Score (0-100)")

	_ = cmd.RegisterFlagCompletionFunc("sort", cobra.FixedCompletions([]string{"relevance", "newest", "price_asc", "price_desc"}, cobra.ShellCompDirectiveNoFileComp))

	cmd.AddCommand(a.searchFiltersCmd())

	return cmd
}

func (a *App) searchFiltersCmd() *cobra.Command {
	var categoryID int

	cmd := &cobra.Command{
		Use:   "filters --category <id>",
		Short: "List available attribute filters for a given category",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if categoryID <= 0 {
				return output.Usagef("--category ID is required (e.g. milanuncios search filters --category 10)")
			}

			res, err := a.Client.GetCategoryFilters(cmd.Context(), categoryID)
			if err != nil {
				return err
			}

			return a.Printer.Print(res)
		},
	}

	cmd.Flags().IntVar(&categoryID, "category", 0, "numeric category ID")
	_ = cmd.MarkFlagRequired("category")

	return cmd
}

func parseFilterFlags(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(flags))
	for _, f := range flags {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, output.Usagef("invalid filter format %q (expected key=value)", f)
		}
		out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return out, nil
}
