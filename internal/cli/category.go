package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) categoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "category",
		Short: "Browse Milanuncios category taxonomy",
	}

	cmd.AddCommand(
		a.categoryListCmd(),
	)

	return cmd
}

func (a *App) categoryListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all categories and subcategories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cats, err := a.Client.ListCategories(cmd.Context())
			if err != nil {
				return err
			}
			return a.Printer.Print(cats)
		},
	}
}
