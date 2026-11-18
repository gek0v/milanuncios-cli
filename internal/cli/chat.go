package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) chatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "chat",
		Aliases: []string{"messages", "msg"},
		Short:   "View and send messages on Milanuncios",
	}

	cmd.AddCommand(
		a.chatStartCmd(),
		a.chatListCmd(),
		a.chatShowCmd(),
		a.chatSendCmd(),
	)

	return cmd
}

func (a *App) chatStartCmd() *cobra.Command {
	var (
		msgFlag         string
		fromStdin       bool
		clientRequestID string
	)

	cmd := &cobra.Command{
		Use:   "start <ad-id|url> [message]",
		Short: "Start a conversation with the seller of an ad",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			adID := args[0]
			var text string

			if fromStdin {
				if msgFlag != "" || len(args) > 1 {
					return output.Usagef("cannot combine --from-stdin with positional message or --message")
				}
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading message from stdin: %w", err)
				}
				text = string(data)
			} else if msgFlag != "" {
				if len(args) > 1 {
					return output.Usagef("cannot specify message as both positional argument and --message flag")
				}
				text = msgFlag
			} else if len(args) > 1 {
				text = strings.Join(args[1:], " ")
			} else {
				return output.Usagef("message text is required (provide via argument, --message, or --from-stdin)")
			}

			if strings.TrimSpace(text) == "" {
				return output.Usagef("message text cannot be empty")
			}

			var opts []string
			if clientRequestID != "" {
				opts = append(opts, clientRequestID)
			}

			res, err := a.Client.StartConversation(cmd.Context(), adID, text, opts...)
			if err != nil {
				return err
			}
			return a.Printer.Print(res)
		},
	}

	cmd.Flags().StringVarP(&msgFlag, "message", "m", "", "Text message to send")
	cmd.Flags().BoolVar(&fromStdin, "from-stdin", false, "Read message from stdin")
	cmd.Flags().StringVar(&clientRequestID, "client-request-id", "", "UUIDv4 idempotency key")

	return cmd
}

func (a *App) chatListCmd() *cobra.Command {
	var unreadOnly bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List active chat conversations",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := a.Client.ListConversations(cmd.Context())
			if err != nil {
				return err
			}

			if unreadOnly {
				filtered := make(milanuncios.ConversationList, 0)
				for _, c := range list {
					if c.Unread {
						filtered = append(filtered, c)
					}
				}
				return a.Printer.Print(filtered)
			}

			return a.Printer.Print(list)
		},
	}

	cmd.Flags().BoolVar(&unreadOnly, "unread-only", false, "Filter conversations to only include unread threads")

	return cmd
}

func (a *App) chatShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <conversation-id>",
		Short: "View full message history for a conversation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			details, err := a.Client.GetConversation(cmd.Context(), id)
			if err != nil {
				return err
			}
			return a.Printer.Print(details)
		},
	}
}

func (a *App) chatSendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send <conversation-id> <message>",
		Short: "Send a text message in a conversation",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			text := strings.Join(args[1:], " ")
			if strings.TrimSpace(text) == "" {
				return output.Usagef("message text cannot be empty")
			}

			msg, err := a.Client.SendMessage(cmd.Context(), id, text)
			if err != nil {
				return err
			}
			return a.Printer.Print(msg)
		},
	}
}
