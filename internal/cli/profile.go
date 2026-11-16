package cli

import (
	"fmt"

	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage multiple Milanuncios accounts side by side",
	}

	cmd.AddCommand(
		a.profileListCmd(),
		a.profileUseCmd(),
		a.profileRemoveCmd(),
	)

	return cmd
}

func (a *App) profileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, _ := config.LoadCredentials(a.Paths.CredentialsFile)

			type ProfileItem struct {
				Name      string `json:"name"`
				Default   bool   `json:"default"`
				LoggedIn  bool   `json:"logged_in"`
				Email     string `json:"email,omitempty"`
				UpdatedAt string `json:"updated_at,omitempty"`
			}

			profilesMap := make(map[string]bool)
			profilesMap[a.Cfg.DefaultProfile] = true
			if creds != nil {
				for p := range creds.Profiles {
					profilesMap[p] = true
				}
			}
			for p := range a.Cfg.Profiles {
				profilesMap[p] = true
			}

			var items []ProfileItem
			for name := range profilesMap {
				isDefault := (name == a.Cfg.DefaultProfile)
				loggedIn := false
				email := ""
				updated := ""
				if creds != nil {
					if c, ok := creds.Profiles[name]; ok && c.SessionToken != "" {
						loggedIn = true
						email = c.Email
						updated = c.UpdatedAt
					}
				}
				items = append(items, ProfileItem{
					Name:      name,
					Default:   isDefault,
					LoggedIn:  loggedIn,
					Email:     email,
					UpdatedAt: updated,
				})
			}

			return a.Printer.Print(items)
		},
	}
}

func (a *App) profileUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Switch default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			newName := args[0]
			a.Cfg.DefaultProfile = newName
			if err := config.Save(a.Paths.ConfigFile, a.Cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			return a.Printer.Print(map[string]string{
				"status":          "updated",
				"default_profile": newName,
			})
		},
	}
}

func (a *App) profileRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a profile and its credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			if target == a.Cfg.DefaultProfile {
				return output.Usagef("cannot remove default profile %q. Switch to another profile first", target)
			}

			_ = config.RemoveProfileCredentials(a.Paths.CredentialsFile, target)
			delete(a.Cfg.Profiles, target)
			_ = config.Save(a.Paths.ConfigFile, a.Cfg)

			return a.Printer.Print(map[string]string{
				"status":  "removed",
				"profile": target,
			})
		},
	}
}
