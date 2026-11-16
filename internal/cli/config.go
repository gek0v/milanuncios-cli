package cli

import (
	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and modify configuration settings",
	}

	cmd.AddCommand(
		a.configPathCmd(),
		a.configListCmd(),
		a.configGetCmd(),
		a.configSetCmd(),
	)

	return cmd
}

func (a *App) configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path to the config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Printer.Print(map[string]string{
				"path": a.Paths.ConfigFile,
			})
		},
	}
}

func (a *App) configListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configuration values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Printer.Print(a.Cfg)
		},
	}
}

func (a *App) configGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			val, err := a.Cfg.Get(args[0])
			if err != nil {
				return output.Usagef("%v", err)
			}
			return a.Printer.Print(map[string]any{
				"key":   args[0],
				"value": val,
			})
		},
	}
}

func (a *App) configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value by key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.Cfg.Set(args[0], args[1]); err != nil {
				return output.Usagef("%v", err)
			}
			if err := config.Save(a.Paths.ConfigFile, a.Cfg); err != nil {
				return err
			}
			return a.Printer.Print(map[string]any{
				"status": "updated",
				"key":    args[0],
				"value":  args[1],
			})
		},
	}
}
