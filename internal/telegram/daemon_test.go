package telegram_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/gek0v/milanuncios-cli/internal/telegram"
)

type mockTelegramSender struct {
	mu           sync.Mutex
	updates      [][]telegram.Update
	callIdx      int
	sentMessages []mockSentMsg
	getUpdateErr error
}

type mockSentMsg struct {
	ChatID int64
	Text   string
	Opts   *telegram.SendMessageOptions
}

func (m *mockTelegramSender) GetUpdates(ctx context.Context, opts *telegram.GetUpdatesOptions) ([]telegram.Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getUpdateErr != nil {
		return nil, m.getUpdateErr
	}
	if m.callIdx < len(m.updates) {
		res := m.updates[m.callIdx]
		m.callIdx++
		return res, nil
	}
	return nil, nil
}

func (m *mockTelegramSender) SendMessage(ctx context.Context, chatID int64, text string, opts *telegram.SendMessageOptions) (*telegram.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, mockSentMsg{
		ChatID: chatID,
		Text:   text,
		Opts:   opts,
	})
	return &telegram.Message{
		MessageID: 8888,
		Chat:      telegram.Chat{ID: chatID},
		Text:      text,
	}, nil
}

type mockMilanunciosSender struct {
	mu      sync.Mutex
	sent    []mockMaMsg
	sendErr error
}

type mockMaMsg struct {
	ConvID string
	Text   string
}

func (m *mockMilanunciosSender) SendMessage(ctx context.Context, convID, text string) (*milanuncios.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	m.sent = append(m.sent, mockMaMsg{ConvID: convID, Text: text})
	return &milanuncios.Message{
		ID:   "ma-msg-1",
		Text: text,
	}, nil
}

type mockMessageStore struct {
	mu       sync.Mutex
	mappings map[int64]*store.TelegramMessageMap
}

func (m *mockMessageStore) GetTelegramMessageMap(ctx context.Context, msgID int64) (*store.TelegramMessageMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.mappings[msgID]; ok {
		return entry, nil
	}
	return nil, store.ErrNotFound
}

func TestDaemon_ProcessUpdate_Unauthorized(t *testing.T) {
	var stderr bytes.Buffer
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosSender{}
	st := &mockMessageStore{mappings: make(map[int64]*store.TelegramMessageMap)}

	d := telegram.NewDaemon(tg, ma, st, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
		Stderr:         &stderr,
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 10,
			Chat:      telegram.Chat{ID: 99999},
			Text:      "Hacked message",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stderr.String(), "[telegram] ignoring message from unauthorized chat_id 99999") {
		t.Errorf("expected unauthorized notice in stderr, got: %s", stderr.String())
	}
	if len(ma.sent) > 0 {
		t.Errorf("expected no message to milanuncios")
	}
	if len(tg.sentMessages) > 0 {
		t.Errorf("expected no reply to telegram")
	}
}

func TestDaemon_ProcessUpdate_SwipeToReply_Success(t *testing.T) {
	var stderr bytes.Buffer
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosSender{}
	st := &mockMessageStore{
		mappings: map[int64]*store.TelegramMessageMap{
			4892: {
				TelegramMsgID:  4892,
				ConversationID: "conv-8849201",
				AdID:           "492817294",
				AdTitle:        "Bicicleta Cannondale",
				OpponentName:   "Carlos M.",
			},
		},
	}

	d := telegram.NewDaemon(tg, ma, st, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
		Stderr:         &stderr,
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 5001,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "Sí, sigue disponible",
			ReplyToMessage: &telegram.Message{
				MessageID: 4892,
			},
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Milanuncios received the message
	if len(ma.sent) != 1 {
		t.Fatalf("expected 1 message to milanuncios, got %d", len(ma.sent))
	}
	if ma.sent[0].ConvID != "conv-8849201" || ma.sent[0].Text != "Sí, sigue disponible" {
		t.Errorf("unexpected milanuncios message: %+v", ma.sent[0])
	}

	// Telegram received confirmation
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 confirmation to telegram, got %d", len(tg.sentMessages))
	}
	if tg.sentMessages[0].ChatID != 12345 {
		t.Errorf("expected confirmation to chat 12345, got %d", tg.sentMessages[0].ChatID)
	}
	if tg.sentMessages[0].Text != "✓ Enviado a Carlos M." {
		t.Errorf("unexpected confirmation text: %q", tg.sentMessages[0].Text)
	}
	if tg.sentMessages[0].Opts == nil || tg.sentMessages[0].Opts.ReplyToMessageID != 5001 {
		t.Errorf("expected reply_to_message_id 5001, got %+v", tg.sentMessages[0].Opts)
	}
}

func TestDaemon_ProcessUpdate_SwipeToReply_Error(t *testing.T) {
	var stderr bytes.Buffer
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosSender{
		sendErr: errors.New("auth token expired"),
	}
	st := &mockMessageStore{
		mappings: map[int64]*store.TelegramMessageMap{
			4892: {
				TelegramMsgID:  4892,
				ConversationID: "conv-8849201",
				AdID:           "492817294",
				AdTitle:        "Bicicleta Cannondale",
				OpponentName:   "Carlos M.",
			},
		},
	}

	d := telegram.NewDaemon(tg, ma, st, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
		Stderr:         &stderr,
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 5001,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "Sí, sigue disponible",
			ReplyToMessage: &telegram.Message{
				MessageID: 4892,
			},
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Telegram received error notice
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 error notice to telegram, got %d", len(tg.sentMessages))
	}
	if !strings.Contains(tg.sentMessages[0].Text, "❌ Error al enviar mensaje: auth token expired") {
		t.Errorf("unexpected error notice text: %q", tg.sentMessages[0].Text)
	}
	if tg.sentMessages[0].Opts == nil || tg.sentMessages[0].Opts.ReplyToMessageID != 5001 {
		t.Errorf("expected reply_to_message_id 5001, got %+v", tg.sentMessages[0].Opts)
	}
}

func TestDaemon_RunOnce_OffsetTracking(t *testing.T) {
	tg := &mockTelegramSender{
		updates: [][]telegram.Update{
			{
				{UpdateID: 10, Message: &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 12345}}},
				{UpdateID: 11, Message: &telegram.Message{MessageID: 2, Chat: telegram.Chat{ID: 12345}}},
			},
		},
	}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	count, err := d.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
	if d.Offset() != 12 {
		t.Errorf("expected offset 12, got %d", d.Offset())
	}
}

func TestDaemon_Run_ContextCancel(t *testing.T) {
	tg := &mockTelegramSender{}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	err := d.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDaemon_Run_BackoffOnNetworkError(t *testing.T) {
	var stderr bytes.Buffer
	tg := &mockTelegramSender{
		getUpdateErr: errors.New("connection reset by peer"),
	}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Stderr:         &stderr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_ = d.Run(ctx)

	if !strings.Contains(stderr.String(), "[telegram] network error: connection reset by peer") {
		t.Errorf("expected network error in stderr, got: %s", stderr.String())
	}
}
