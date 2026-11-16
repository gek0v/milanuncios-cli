package cli

import (
	"strings"

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
		a.chatListCmd(),
		a.chatShowCmd(),
		a.chatSendCmd(),
	)

	return cmd
}

func (a *App) chatListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List active chat conversations",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := a.Client.ListConversations(cmd.Context())
			if err != nil {
				return err
			}
			return a.Printer.Print(list)
		},
	}
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
