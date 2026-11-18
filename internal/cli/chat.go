package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/store"
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
		a.chatWatchCmd(),
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

func (a *App) chatWatchCmd() *cobra.Command {
	var (
		interval   string
		once       bool
		unreadOnly bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Monitor chat conversations for incoming unread messages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sec, err := parseIntervalSec(interval)
			if err != nil {
				return err
			}

			st, err := a.getStore()
			if err != nil {
				return err
			}
			defer st.Close()

			chatWatch, err := st.EnsureChatWatch()
			if err != nil {
				return fmt.Errorf("initializing chat watch record: %w", err)
			}

			eng := a.newWatchEngine(st)

			checkCycle := func(ctx context.Context) error {
				convs, err := a.Client.ListConversations(ctx)
				if err != nil {
					return err
				}

				now := time.Now().UTC()

				for _, c := range convs {
					if unreadOnly && !c.Unread {
						continue
					}

					details, err := a.Client.GetConversation(ctx, c.ID)
					if err != nil {
						if output.IsKind(err, output.KindBlocked) {
							return err
						}
						if !once {
							fmt.Fprintf(a.Stderr, "failed to get conversation %s: %v\n", c.ID, err)
						}
						continue
					}

					for _, msg := range details.Messages {
						if msg.FromMe {
							continue
						}

						seen, err := st.HasSeenChatMessage(chatWatch.ID, msg.ID)
						if err != nil {
							return fmt.Errorf("checking seen status for message %s: %w", msg.ID, err)
						}
						if seen {
							continue
						}

						// Mark as seen
						if err := st.MarkChatMessageSeen(chatWatch.ID, msg.ID, msg.Text); err != nil {
							return fmt.Errorf("marking message %s as seen: %w", msg.ID, err)
						}

						evPayload := milanuncios.ChatMessageReceivedEvent{
							EventType:      "chat.message_received",
							ConversationID: c.ID,
							AdID:           c.AdID,
							SenderName:     msg.SenderName,
							Text:           msg.Text,
							SentAt:         msg.SentAt,
						}

						// Record event in SQLite events table
						storeEv := store.Event{
							WatchID:     chatWatch.ID,
							EventType:   "chat.message_received",
							AdID:        c.AdID,
							PayloadJSON: store.EncodeJSON(evPayload),
							CreatedAt:   now,
						}
						if err := st.RecordEvent(&storeEv); err != nil {
							return fmt.Errorf("recording chat event: %w", err)
						}

						// Dispatch to external sinks
						if eng.Sinks != nil {
							errs := eng.Sinks.Dispatch(ctx, storeEv)
							if len(errs) == 0 {
								_ = st.MarkEventsDelivered([]int64{storeEv.ID})
							}
						}

						// Print event
						if err := a.Printer.Print(evPayload); err != nil {
							return err
						}
					}
				}

				return nil
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			var blockedUntil time.Time
			if stored, err := st.GetBlockedUntil(); err == nil && stored != nil {
				if time.Now().Before(*stored) {
					blockedUntil = *stored
					a.Client.SetBlockedUntil(*stored)
					fmt.Fprintf(a.Stderr, "circuit breaker active: cooling down until %s\n", stored.Local().Format("15:04:05"))
				}
			}

			prevOnBlock := a.Client.OnBlock
			a.Client.OnBlock = func(until time.Time) {
				blockedUntil = until
				_ = st.SetBlockedUntil(until)
				if prevOnBlock != nil {
					prevOnBlock(until)
				}
				fmt.Fprintf(a.Stderr, "circuit breaker triggered: paused until %s\n", until.Local().Format("15:04:05"))
			}

			runCycle := func() error {
				if !blockedUntil.IsZero() && time.Now().Before(blockedUntil) {
					fmt.Fprintf(a.Stderr, "chat check paused (cooling down until %s)\n", blockedUntil.Local().Format("15:04:05"))
					return nil
				}
				err := checkCycle(ctx)
				if err != nil {
					if output.IsKind(err, output.KindBlocked) {
						fmt.Fprintf(a.Stderr, "chat check aborted due to anti-bot block (cooling down until %s)\n", blockedUntil.Local().Format("15:04:05"))
						return err
					}
					return err
				}
				if !blockedUntil.IsZero() && time.Now().After(blockedUntil) {
					blockedUntil = time.Time{}
					_ = st.ClearBlockedUntil()
					a.Client.ClearBlock()
				}
				return nil
			}

			if once {
				return runCycle()
			}

			fmt.Fprintf(a.Stderr, "milanuncios-cli chat monitor running (interval: %ds). Press Ctrl+C to stop.\n", sec)

			// Initial check
			if err := runCycle(); err != nil {
				if !output.IsKind(err, output.KindBlocked) {
					fmt.Fprintf(a.Stderr, "initial chat check error: %v\n", err)
				}
			}

			ticker := time.NewTicker(time.Duration(sec) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					fmt.Fprintln(a.Stderr, "\nStopping chat monitor.")
					return &output.DomainError{
						Kind: output.KindInterrupted,
						Msg:  "chat monitor interrupted by signal",
					}
				case <-ticker.C:
					if err := runCycle(); err != nil {
						if !output.IsKind(err, output.KindBlocked) {
							fmt.Fprintf(a.Stderr, "chat check error: %v\n", err)
						}
					}
				}
			}
		},
	}

	cmd.Flags().StringVar(&interval, "interval", "30s", "polling interval (e.g. 30s, 1m; min: 10s)")
	cmd.Flags().BoolVar(&once, "once", false, "run single check cycle and exit immediately")
	cmd.Flags().BoolVar(&unreadOnly, "unread-only", true, "only check conversations marked as unread")

	return cmd
}
