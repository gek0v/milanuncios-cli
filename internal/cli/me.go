package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) meCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "me",
		Short: "Manage own account, listings and favorites",
	}

	cmd.AddCommand(
		a.meShowCmd(),
		a.meAdsCmd(),
		a.meFavoritesCmd(),
	)

	return cmd
}

func (a *App) meShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display your account details",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, err := a.Client.Me(cmd.Context())
			if err != nil {
				return err
			}
			return a.Printer.Print(profile)
		},
	}
}

func (a *App) meAdsCmd() *cobra.Command {
	var sold bool

	cmd := &cobra.Command{
		Use:   "ads",
		Short: "List your published ads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ads, err := a.Client.MyAds(cmd.Context(), sold)
			if err != nil {
				return err
			}
			return a.Printer.Print(ads)
		},
	}

	cmd.Flags().BoolVar(&sold, "sold", false, "include or filter by sold items")

	return cmd
}

func (a *App) meFavoritesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "favorites",
		Short: "List your favorite ads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			favs, err := a.Client.MyFavorites(cmd.Context())
			if err != nil {
				return err
			}
			return a.Printer.Print(favs)
		},
	}
}
