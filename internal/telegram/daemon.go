package telegram

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

// MilanunciosSender sends messages to Milanuncios conversations.
type MilanunciosSender interface {
	SendMessage(ctx context.Context, convID, text string) (*milanuncios.Message, error)
}

// MessageStore retrieves mappings from Telegram message IDs to Milanuncios conversations.
type MessageStore interface {
	GetTelegramMessageMap(ctx context.Context, msgID int64) (*store.TelegramMessageMap, error)
}

// TelegramSender is an interface for sending messages and receiving updates from Telegram.
type TelegramSender interface {
	GetUpdates(ctx context.Context, opts *GetUpdatesOptions) ([]Update, error)
	SendMessage(ctx context.Context, chatID int64, text string, opts *SendMessageOptions) (*Message, error)
}

// DaemonConfig holds configuration options for the inbound Telegram long-polling daemon.
type DaemonConfig struct {
	PollTimeout    int           // Long-polling timeout in seconds (default: 30)
	AllowedChatIDs []int64       // Whitelist of allowed chat IDs
	Stderr         io.Writer     // Diagnostics and notice logger (default: os.Stderr)
	InitialBackoff time.Duration // Backoff on network errors (default: 1s)
	MaxBackoff     time.Duration // Max backoff duration (default: 30s)
}

// Daemon coordinates long polling, authorization whitelisting, and swipe-to-reply routing.
type Daemon struct {
	tg     TelegramSender
	ma     MilanunciosSender
	st     MessageStore
	cfg    DaemonConfig
	offset int64
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(tg TelegramSender, ma MilanunciosSender, st MessageStore, cfg DaemonConfig) *Daemon {
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 30
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 1 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	return &Daemon{
		tg:  tg,
		ma:  ma,
		st:  st,
		cfg: cfg,
	}
}

// Offset returns the current update offset.
func (d *Daemon) Offset() int64 {
	return d.offset
}

// SetOffset sets the update offset.
func (d *Daemon) SetOffset(offset int64) {
	d.offset = offset
}

func (d *Daemon) isAllowed(chatID int64) bool {
	for _, id := range d.cfg.AllowedChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

// ProcessUpdate handles authorization checking and swipe-to-reply routing for a single update.
func (d *Daemon) ProcessUpdate(ctx context.Context, update Update) error {
	if update.Message == nil {
		return nil
	}

	msg := update.Message
	chatID := msg.Chat.ID

	// Whitelist enforcement
	if !d.isAllowed(chatID) {
		fmt.Fprintf(d.cfg.Stderr, "[telegram] ignoring message from unauthorized chat_id %d\n", chatID)
		return nil
	}

	// Swipe-to-Reply
	if msg.ReplyToMessage != nil {
		replyToID := msg.ReplyToMessage.MessageID
		if d.st == nil {
			return nil
		}
		mapping, err := d.st.GetTelegramMessageMap(ctx, replyToID)
		if err != nil || mapping == nil {
			// Not a recorded Milanuncios message map
			return nil
		}

		if d.ma == nil {
			return nil
		}

		_, sendErr := d.ma.SendMessage(ctx, mapping.ConversationID, msg.Text)
		if sendErr != nil {
			_, _ = d.tg.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error al enviar mensaje: %s", sendErr.Error()), &SendMessageOptions{
				ReplyToMessageID: msg.MessageID,
			})
			return nil
		}

		_, _ = d.tg.SendMessage(ctx, chatID, fmt.Sprintf("✓ Enviado a %s", mapping.OpponentName), &SendMessageOptions{
			ReplyToMessageID: msg.MessageID,
		})
	}

	return nil
}

// RunOnce fetches one round of updates from Telegram and processes them.
func (d *Daemon) RunOnce(ctx context.Context) (int, error) {
	opts := &GetUpdatesOptions{
		Offset:         d.offset,
		Timeout:        d.cfg.PollTimeout,
		AllowedUpdates: []string{"message", "callback_query"},
	}

	updates, err := d.tg.GetUpdates(ctx, opts)
	if err != nil {
		return 0, err
	}

	count := len(updates)
	for _, u := range updates {
		if u.UpdateID >= d.offset {
			d.offset = u.UpdateID + 1
		}
		if err := d.ProcessUpdate(ctx, u); err != nil {
			fmt.Fprintf(d.cfg.Stderr, "[telegram] error processing update %d: %v\n", u.UpdateID, err)
		}
	}
	return count, nil
}

// Run starts the long-polling daemon loop with reconnection and backoff on errors.
func (d *Daemon) Run(ctx context.Context) error {
	backoff := d.cfg.InitialBackoff

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, err := d.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(d.cfg.Stderr, "[telegram] network error: %v, retrying in %v\n", err, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > d.cfg.MaxBackoff {
				backoff = d.cfg.MaxBackoff
			}
			continue
		}

		// Reset backoff upon successful poll
		backoff = d.cfg.InitialBackoff
	}
}
