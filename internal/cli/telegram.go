package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/eval"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/telegram"
	"github.com/spf13/cobra"
)

// TelegramTestResult represents the result of the telegram test command.
type TelegramTestResult struct {
	Bot         *telegram.User `json:"bot"`
	PingSent    bool           `json:"ping_sent"`
	TargetChats []int64        `json:"target_chats,omitempty"`
	Status      string         `json:"status"`
}

// Pretty outputs human-readable status for telegram test.
func (r *TelegramTestResult) Pretty(w io.Writer, color bool) {
	username := r.Bot.Username
	if username == "" {
		username = "sin username"
	}
	fmt.Fprintf(w, "Telegram Bot: @%s (%s, ID: %d)\n", username, r.Bot.FirstName, r.Bot.ID)
	if r.PingSent {
		fmt.Fprintf(w, "Status: %s (Test ping sent to %d chat(s))\n", r.Status, len(r.TargetChats))
	} else {
		fmt.Fprintf(w, "Status: %s (No allowed_chat_ids configured, ping skipped)\n", r.Status)
	}
}

// TelegramSetupResult represents the result of the telegram setup command.
type TelegramSetupResult struct {
	Bot            *telegram.User `json:"bot"`
	ChatID         int64          `json:"chat_id"`
	AllowedChatIDs []int64        `json:"allowed_chat_ids"`
	Confirmed      bool           `json:"confirmed"`
	Status         string         `json:"status"`
}

// Pretty outputs human-readable status for telegram setup.
func (r *TelegramSetupResult) Pretty(w io.Writer, color bool) {
	username := r.Bot.Username
	if username == "" {
		username = "sin username"
	}
	fmt.Fprintf(w, "Telegram Bot: @%s (%s, ID: %d)\n", username, r.Bot.FirstName, r.Bot.ID)
	fmt.Fprintf(w, "Chat ID: %d (Whitelisted in config.toml)\n", r.ChatID)
	if len(r.AllowedChatIDs) > 1 {
		fmt.Fprintf(w, "Allowed Chat IDs: %v\n", r.AllowedChatIDs)
	}
	if r.Confirmed {
		fmt.Fprintf(w, "Confirmation: Sent successfully\n")
	}
	fmt.Fprintf(w, "Status: %s\n", r.Status)
}

func (a *App) getTelegramBotToken() string {
	if creds, err := config.LoadCredentials(a.Paths.CredentialsFile); err == nil && creds != nil {
		if token := creds.TelegramToken(); token != "" {
			return token
		}
	}
	if a.Cfg != nil {
		for _, s := range a.Cfg.Sinks {
			if s.Type == "telegram" && s.BotToken != "" {
				return s.BotToken
			}
		}
	}
	return os.Getenv("MILANUNCIOS_TELEGRAM_TOKEN")
}

func (a *App) newTelegramClient(token string) *telegram.Client {
	var tgOpts []telegram.Option
	if a.Client != nil && a.Client.HTTP != nil && a.Client.HTTP.Transport != nil {
		tgOpts = append(tgOpts, telegram.WithTransport(a.Client.HTTP.Transport))
	}
	if tgBaseURL := os.Getenv("MILANUNCIOS_TELEGRAM_BASE_URL"); tgBaseURL != "" {
		tgOpts = append(tgOpts, telegram.WithBaseURL(tgBaseURL))
	}
	return telegram.NewClient(token, tgOpts...)
}

func (a *App) telegramCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "telegram",
		Aliases: []string{"tg"},
		Short:   "Manage Telegram bot and inbound message daemon",
		Long: `Manage Telegram bot integration, run the inbound long-polling daemon,
test connectivity, and handle swipe-to-reply messaging.`,
	}

	cmd.AddCommand(
		a.telegramRunCmd(),
		a.telegramTestCmd(),
		a.telegramSetupCmd(),
	)

	return cmd
}

func (a *App) telegramRunCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the inbound Telegram long-polling daemon in foreground",
		Long: `Run the inbound Telegram daemon with long polling.
Handles incoming messages, enforces chat ID whitelisting, and forwards
swipe-to-reply responses back to Milanuncios conversations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			token := a.getTelegramBotToken()
			if token == "" {
				return output.Usagef("telegram bot token is required (configure in credentials or set MILANUNCIOS_TELEGRAM_TOKEN)")
			}

			if a.Cfg == nil || len(a.Cfg.Telegram.AllowedChatIDs) == 0 {
				return output.Usagef("telegram.allowed_chat_ids is required in config to run the inbound daemon")
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			tgClient := a.newTelegramClient(token)
			daemonCfg := telegram.DaemonConfig{
				PollTimeout:    timeout,
				AllowedChatIDs: a.Cfg.Telegram.AllowedChatIDs,
				Stderr:         a.Stderr,
			}
			evaluator := eval.NewEvaluator(a.Client, st)
			d := telegram.NewDaemon(tgClient, a.Client, st, daemonCfg, evaluator)

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			fmt.Fprintln(a.Stderr, "milanuncios-cli telegram inbound daemon running. Press Ctrl+C to stop.")

			err = d.Run(ctx)
			if ctx.Err() != nil {
				fmt.Fprintln(a.Stderr, "\nStopping telegram inbound daemon.")
				return &output.DomainError{
					Kind: output.KindInterrupted,
					Msg:  "telegram inbound daemon interrupted by signal",
				}
			}
			return err
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", 30, "long polling timeout in seconds")

	return cmd
}

func (a *App) telegramTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test Telegram Bot token and connection",
		Long: `Validate Telegram Bot API token using getMe and optionally send
a test ping message if allowed_chat_ids are configured.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			token := a.getTelegramBotToken()
			if token == "" {
				return output.Usagef("telegram bot token is required (configure in credentials or set MILANUNCIOS_TELEGRAM_TOKEN)")
			}

			tgClient := a.newTelegramClient(token)
			user, err := tgClient.GetMe(cmd.Context())
			if err != nil {
				return err
			}

			res := &TelegramTestResult{
				Bot:    user,
				Status: "connected",
			}

			if a.Cfg != nil && len(a.Cfg.Telegram.AllowedChatIDs) > 0 {
				pingText := fmt.Sprintf("🔔 <b>milanuncios-cli</b>: Conexión con Telegram Bot exitosa.\nBot: @%s", user.Username)
				for _, chatID := range a.Cfg.Telegram.AllowedChatIDs {
					_, err := tgClient.SendMessage(cmd.Context(), chatID, pingText, &telegram.SendMessageOptions{
						ParseMode: "HTML",
					})
					if err != nil {
						return fmt.Errorf("sending test ping to chat %d: %w", chatID, err)
					}
				}
				res.PingSent = true
				res.TargetChats = a.Cfg.Telegram.AllowedChatIDs
			}

			return a.Printer.Print(res)
		},
	}

	return cmd
}

func (a *App) telegramSetupCmd() *cobra.Command {
	var (
		tokenFlag  string
		chatIDFlag int64
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure Telegram bot token and allowed chat ID",
		Long: `Configure Telegram bot credentials and chat whitelist.
Validates the token with Telegram Bot API, saves the bot token in credentials.toml,
saves the chat ID in config.toml, and sends a confirmation test message.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			token := strings.TrimSpace(tokenFlag)
			if token == "" {
				token = a.getTelegramBotToken()
			}
			if token == "" {
				return output.Usagef("telegram bot token is required (--token or MILANUNCIOS_TELEGRAM_TOKEN)")
			}

			if chatIDFlag == 0 {
				return output.Usagef("telegram chat ID is required (--chat-id)")
			}

			tgClient := a.newTelegramClient(token)
			user, err := tgClient.GetMe(cmd.Context())
			if err != nil {
				return fmt.Errorf("validating telegram bot token: %w", err)
			}

			// Save token in credentials.toml
			creds, err := config.LoadCredentials(a.Paths.CredentialsFile)
			if err != nil {
				return fmt.Errorf("loading credentials: %w", err)
			}
			creds.Telegram.BotToken = token
			if err := config.SaveCredentials(a.Paths.CredentialsFile, creds); err != nil {
				return fmt.Errorf("saving credentials: %w", err)
			}

			// Save chat ID in config.toml
			cfg, err := config.Load(a.Paths.ConfigFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg.Telegram.Enabled = true
			alreadyPresent := false
			for _, id := range cfg.Telegram.AllowedChatIDs {
				if id == chatIDFlag {
					alreadyPresent = true
					break
				}
			}
			if !alreadyPresent {
				cfg.Telegram.AllowedChatIDs = append(cfg.Telegram.AllowedChatIDs, chatIDFlag)
			}
			if err := config.Save(a.Paths.ConfigFile, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			a.Cfg = cfg

			// Send confirmation message to Telegram chat
			confirmText := fmt.Sprintf("🎉 <b>milanuncios-cli</b>: ¡Bot configurado con éxito!\nBot: @%s (ID: %d)\nChat ID autorizado: %d", user.Username, user.ID, chatIDFlag)
			_, err = tgClient.SendMessage(cmd.Context(), chatIDFlag, confirmText, &telegram.SendMessageOptions{
				ParseMode: "HTML",
			})
			if err != nil {
				return fmt.Errorf("sending confirmation message to chat %d: %w", chatIDFlag, err)
			}

			res := &TelegramSetupResult{
				Bot:            user,
				ChatID:         chatIDFlag,
				AllowedChatIDs: cfg.Telegram.AllowedChatIDs,
				Confirmed:      true,
				Status:         "configured",
			}

			return a.Printer.Print(res)
		},
	}

	cmd.Flags().StringVar(&tokenFlag, "token", "", "Telegram bot API token")
	cmd.Flags().Int64Var(&chatIDFlag, "chat-id", 0, "Telegram chat ID to whitelist and notify")

	return cmd
}
