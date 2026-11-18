package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication and sessions",
	}

	cmd.AddCommand(
		a.authLoginCmd(),
		a.authStatusCmd(),
		a.authLogoutCmd(),
	)

	return cmd
}

func (a *App) authLoginCmd() *cobra.Command {
	var cookiesFile string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Import session cookies from your web browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var rawCookies string

			if cookiesFile != "" {
				data, err := os.ReadFile(cookiesFile)
				if err != nil {
					return fmt.Errorf("reading cookies file: %w", err)
				}
				rawCookies = string(data)
			} else {
				if !a.Interactive {
					return output.Usagef("pass --cookies <file> in non-interactive mode")
				}

				fmt.Fprintln(a.Stderr, "Paste your exported cookies (JSON or Netscape format), then press Enter and Ctrl+D (or Ctrl+Z on Windows):")
				reader := bufio.NewReader(a.Stdin)
				data, err := io.ReadAll(reader)
				if err != nil {
					return err
				}
				rawCookies = string(data)
			}

			token, wafCookies, err := milanuncios.ParseCookies(rawCookies)
			if err != nil {
				return err
			}

			// Validate JWT expiration before verifying
			if expTime, isExpired, err := milanuncios.ParseJWTExpiration(token); err == nil && expTime != nil && isExpired {
				return output.Authf("session token expired on %s. Please log in to milanuncios.com in your browser and export fresh cookies", expTime.Format(time.RFC3339))
			}

			a.Client.SetSession(token)
			if len(wafCookies) > 0 {
				a.Client.SetCookies(wafCookies)
			}

			// Verify session against Milanuncios API
			me, err := a.Client.Me(cmd.Context())
			if err != nil {
				return fmt.Errorf("verifying session with milanuncios: %w", err)
			}

			// Save to credentials.toml
			creds, err := config.LoadCredentials(a.Paths.CredentialsFile)
			if err != nil {
				return err
			}

			creds.Profiles[a.Profile] = config.ProfileCredentials{
				SessionToken: token,
				UserHash:     me.Hash,
				Email:        me.Email,
				Name:         me.Name,
				Cookies:      wafCookies,
				UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			}

			if err := config.SaveCredentials(a.Paths.CredentialsFile, creds); err != nil {
				return fmt.Errorf("saving credentials: %w", err)
			}

			return a.Printer.Print(map[string]any{
				"status":  "logged_in",
				"profile": a.Profile,
				"name":    me.Name,
				"email":   me.Email,
			})
		},
	}

	cmd.Flags().StringVar(&cookiesFile, "cookies", "", "path to cookie export file (JSON or cookies.txt)")

	return cmd
}

func (a *App) authStatusCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect authentication status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			hasSession := a.Client.SessionToken != ""
			source := "none"
			if os.Getenv("MILANUNCIOS_SESSION_TOKEN") != "" {
				source = "env"
			} else if hasSession {
				source = "credentials.toml"
			}

			res := map[string]any{
				"profile":    a.Profile,
				"logged_in":  hasSession,
				"source":     source,
				"check_live": check,
			}

			var tokenExpired bool
			if hasSession {
				if expTime, isExpired, err := milanuncios.ParseJWTExpiration(a.Client.SessionToken); err == nil && expTime != nil {
					res["expires_at"] = expTime.Format(time.RFC3339)
					res["expired"] = isExpired
					tokenExpired = isExpired
					if isExpired {
						res["valid"] = false
					}
				}
			}

			if hasSession && check {
				if tokenExpired {
					res["valid"] = false
					res["error"] = "session token has expired"
				} else {
					me, err := a.Client.Me(cmd.Context())
					if err != nil {
						res["valid"] = false
						res["error"] = err.Error()
					} else {
						res["valid"] = true
						res["name"] = me.Name
						res["email"] = me.Email
					}
				}
			}

			return a.Printer.Print(res)
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "actively verify the session with Milanuncios")

	return cmd
}

func (a *App) authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear authenticated session for the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.RemoveProfileCredentials(a.Paths.CredentialsFile, a.Profile); err != nil {
				return err
			}
			a.Client.SetSession("")

			return a.Printer.Print(map[string]string{
				"status":  "logged_out",
				"profile": a.Profile,
			})
		},
	}
}
