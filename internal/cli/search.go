package cli

import (
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) searchCmd() *cobra.Command {
	var (
		minPrice float64
		maxPrice float64
		province string
		category int
		sort     string
		limit    int
		next     string
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

			q := milanuncios.SearchQuery{
				Keywords: keywords,
				MinPrice: minPrice,
				MaxPrice: maxPrice,
				Province: province,
				Category: category,
				Sort:     sort,
				Limit:    limit,
				Next:     next,
			}

			res, err := a.Client.Search(cmd.Context(), q)
			if err != nil {
				return err
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

	_ = cmd.RegisterFlagCompletionFunc("sort", cobra.FixedCompletions([]string{"relevance", "newest", "price_asc", "price_desc"}, cobra.ShellCompDirectiveNoFileComp))

	return cmd
}
