package telegram_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/eval"
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

type mockMilanunciosClient struct {
	mockMilanunciosSender
	chats       milanuncios.ConversationList
	chatsErr    error
	convDetails map[string]*milanuncios.ConversationDetails
	getChatErr  error
	searchRes   *milanuncios.SearchResult
	searchErr   error
	adItem      *milanuncios.AdItem
	getAdErr    error
	userProfile *milanuncios.UserProfile
	meErr       error
}

func (m *mockMilanunciosClient) ListChats(ctx context.Context) (milanuncios.ConversationList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chatsErr != nil {
		return nil, m.chatsErr
	}
	return m.chats, nil
}

func (m *mockMilanunciosClient) GetChat(ctx context.Context, id string) (*milanuncios.ConversationDetails, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getChatErr != nil {
		return nil, m.getChatErr
	}
	if cd, ok := m.convDetails[id]; ok {
		return cd, nil
	}
	return nil, errors.New("conversation not found")
}

func (m *mockMilanunciosClient) Search(ctx context.Context, q milanuncios.SearchQuery) (*milanuncios.SearchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.searchRes, nil
}

func (m *mockMilanunciosClient) GetAd(ctx context.Context, idOrURL string) (*milanuncios.AdItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getAdErr != nil {
		return nil, m.getAdErr
	}
	return m.adItem, nil
}

func (m *mockMilanunciosClient) Me(ctx context.Context) (*milanuncios.UserProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.meErr != nil {
		return nil, m.meErr
	}
	return m.userProfile, nil
}

type mockMessageStore struct {
	mu         sync.Mutex
	mappings   map[int64]*store.TelegramMessageMap
	watches    store.WatchList
	watchesErr error
}

func (m *mockMessageStore) GetTelegramMessageMap(ctx context.Context, msgID int64) (*store.TelegramMessageMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.mappings[msgID]; ok {
		return entry, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockMessageStore) ListWatches(ctx context.Context) (store.WatchList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.watchesErr != nil {
		return nil, m.watchesErr
	}
	return m.watches, nil
}

type mockEvaluationEngine struct {
	report *eval.EvaluationReport
	err    error
}

func (m *mockEvaluationEngine) EvaluateAd(ctx context.Context, adID string, includeComparables bool) (*eval.EvaluationReport, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.report, nil
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

func TestDaemon_Interactive_Help(t *testing.T) {
	tg := &mockTelegramSender{}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	for _, cmd := range []string{"/start", "/help", "/help@my_bot"} {
		tg.sentMessages = nil
		up := telegram.Update{
			UpdateID: 1,
			Message: &telegram.Message{
				MessageID: 100,
				Chat:      telegram.Chat{ID: 12345},
				Text:      cmd,
			},
		}

		err := d.ProcessUpdate(context.Background(), up)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", cmd, err)
		}
		if len(tg.sentMessages) != 1 {
			t.Fatalf("expected 1 response for %s, got %d", cmd, len(tg.sentMessages))
		}
		resText := tg.sentMessages[0].Text
		if !strings.Contains(resText, "/chats") || !strings.Contains(resText, "/eval") || !strings.Contains(resText, "Swipe to Reply") {
			t.Errorf("help text missing required information: %s", resText)
		}
	}
}

func TestDaemon_Interactive_Chats_Success(t *testing.T) {
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosClient{
		chats: milanuncios.ConversationList{
			{
				ID:          "c1",
				AdTitle:     "MacBook Pro 14",
				AdPrice:     1400,
				UserName:    "Alvaro R.",
				LastMessage: "¿Haces envíos?",
				Unread:      true,
			},
			{
				ID:          "c2",
				AdTitle:     "Bicicleta Orbea",
				AdPrice:     350,
				UserName:    "Marta G.",
				LastMessage: "Trato hecho",
				Unread:      false,
			},
		},
	}
	d := telegram.NewDaemon(tg, ma, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 101,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/chats",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response, got %d", len(tg.sentMessages))
	}

	msg := tg.sentMessages[0]
	if !strings.Contains(msg.Text, "Alvaro R.") || !strings.Contains(msg.Text, "🔴") {
		t.Errorf("expected unread indicator and username: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "Marta G.") || !strings.Contains(msg.Text, "⚪") {
		t.Errorf("expected read indicator and username: %s", msg.Text)
	}

	// Verify inline buttons
	if msg.Opts == nil || msg.Opts.ReplyMarkup == nil {
		t.Fatalf("expected inline keyboard markup")
	}
	markup, ok := msg.Opts.ReplyMarkup.(telegram.InlineKeyboardMarkup)
	if !ok || len(markup.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 inline buttons, got: %+v", msg.Opts.ReplyMarkup)
	}
	if markup.InlineKeyboard[0][0].CallbackData != "/show c1" {
		t.Errorf("unexpected callback data: %s", markup.InlineKeyboard[0][0].CallbackData)
	}
}

func TestDaemon_Interactive_Chats_Empty(t *testing.T) {
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosClient{
		chats: milanuncios.ConversationList{},
	}
	d := telegram.NewDaemon(tg, ma, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 102,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/chats",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "No tienes conversaciones activas") {
		t.Errorf("unexpected empty chats response: %+v", tg.sentMessages)
	}
}

func TestDaemon_Interactive_Show_Success(t *testing.T) {
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosClient{
		convDetails: map[string]*milanuncios.ConversationDetails{
			"c1": {
				Conversation: milanuncios.Conversation{
					ID:       "c1",
					AdTitle:  "iPhone 15",
					AdPrice:  800,
					UserName: "Lucas M.",
				},
				Messages: []milanuncios.Message{
					{
						ID:         "m1",
						SenderName: "Lucas M.",
						Text:       "Hola, ¿sigue en venta?",
						SentAt:     "10:00",
						FromMe:     false,
					},
					{
						ID:         "m2",
						SenderName: "Yo",
						Text:       "Sí, disponible.",
						SentAt:     "10:05",
						FromMe:     true,
					},
				},
			},
		},
	}
	d := telegram.NewDaemon(tg, ma, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 103,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/show c1",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response, got %d", len(tg.sentMessages))
	}
	out := tg.sentMessages[0].Text
	if !strings.Contains(out, "Conversación #c1") || !strings.Contains(out, "Lucas M.") || !strings.Contains(out, "iPhone 15") {
		t.Errorf("unexpected show response header: %s", out)
	}
	if !strings.Contains(out, "Hola, ¿sigue en venta?") || !strings.Contains(out, "Tú") {
		t.Errorf("unexpected show response messages: %s", out)
	}
}

func TestDaemon_Interactive_Show_MissingArg(t *testing.T) {
	tg := &mockTelegramSender{}
	d := telegram.NewDaemon(tg, &mockMilanunciosClient{}, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 104,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/show",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Uso:") {
		t.Errorf("expected usage message, got: %+v", tg.sentMessages)
	}
}

func TestDaemon_Interactive_Search_Success(t *testing.T) {
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosClient{
		searchRes: &milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{
					ID:       "ad-101",
					Title:    "Sony WH-1000XM4 Auriculares",
					Price:    180,
					Location: milanuncios.AdLocation{Province: "Madrid"},
					URL:      "https://www.milanuncios.com/auriculares/sony-101.htm",
				},
			},
		},
	}
	d := telegram.NewDaemon(tg, ma, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 105,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/search sony xm4",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response, got %d", len(tg.sentMessages))
	}
	msg := tg.sentMessages[0]
	if !strings.Contains(msg.Text, "Sony WH-1000XM4") || !strings.Contains(msg.Text, "180 €") || !strings.Contains(msg.Text, "Madrid") {
		t.Errorf("search result missing item info: %s", msg.Text)
	}
	if msg.Opts == nil || msg.Opts.ReplyMarkup == nil {
		t.Fatalf("expected inline keyboard with evaluate button")
	}
}

func TestDaemon_Interactive_Search_MissingArg(t *testing.T) {
	tg := &mockTelegramSender{}
	d := telegram.NewDaemon(tg, &mockMilanunciosClient{}, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 106,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/search",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Uso:") {
		t.Errorf("expected usage message for /search, got: %+v", tg.sentMessages)
	}
}

func TestDaemon_Interactive_Eval_Success(t *testing.T) {
	tg := &mockTelegramSender{}
	ev := &mockEvaluationEngine{
		report: &eval.EvaluationReport{
			AdID:            "492817294",
			Title:           "Cannondale Topstone 1",
			Price:           950,
			DealScore:       84,
			Rating:          eval.RatingGreatPrice,
			DiscountPercent: 20.8,
			MarketMedian:    1200,
			SampleSize:      26,
			Verdict:         "El anuncio se encuentra 250 € por debajo de la mediana del mercado.",
			URL:             "https://www.milanuncios.com/bicicletas/cannondale-492817294.htm",
		},
	}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	}, ev)

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 107,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/eval 492817294",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response, got %d", len(tg.sentMessages))
	}
	res := tg.sentMessages[0].Text
	if !strings.Contains(res, "Cannondale Topstone 1") || !strings.Contains(res, "84 / 100") {
		t.Errorf("eval response missing deal score: %s", res)
	}
	if !strings.Contains(res, "1.200 €") || !strings.Contains(res, "20.8%") {
		t.Errorf("eval response missing median/discount: %s", res)
	}
	if !strings.Contains(res, "Veredicto:") {
		t.Errorf("eval response missing verdict: %s", res)
	}
}

func TestDaemon_Interactive_Eval_MissingArg(t *testing.T) {
	tg := &mockTelegramSender{}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 108,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/eval",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Uso:") {
		t.Errorf("expected usage message for /eval, got: %+v", tg.sentMessages)
	}
}

func TestDaemon_Interactive_Watches_Success(t *testing.T) {
	tg := &mockTelegramSender{}
	st := &mockMessageStore{
		watches: store.WatchList{
			{
				ID:          1,
				Name:        "alerta_macbook",
				Type:        "search",
				Target:      "macbook air m1",
				IntervalSec: 60,
				Status:      "active",
			},
			{
				ID:          2,
				Name:        "alerta_orbea",
				Type:        "ad",
				Target:      "ad:998877",
				IntervalSec: 300,
				Status:      "paused",
			},
		},
	}
	d := telegram.NewDaemon(tg, nil, st, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 109,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/watches",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response, got %d", len(tg.sentMessages))
	}
	res := tg.sentMessages[0].Text
	if !strings.Contains(res, "alerta_macbook") || !strings.Contains(res, "alerta_orbea") {
		t.Errorf("watches response missing watch names: %s", res)
	}
	if !strings.Contains(res, "60s") || !strings.Contains(res, "300s") {
		t.Errorf("watches response missing interval: %s", res)
	}
}

func TestDaemon_Interactive_Status_Success(t *testing.T) {
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosClient{
		userProfile: &milanuncios.UserProfile{
			ID:    "u-123",
			Name:  "Geko Developer",
			Email: "geko@example.com",
		},
	}
	st := &mockMessageStore{
		watches: store.WatchList{
			{ID: 1, Name: "w1", Status: "active"},
			{ID: 2, Name: "w2", Status: "paused"},
		},
	}
	d := telegram.NewDaemon(tg, ma, st, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 110,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/status",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response, got %d", len(tg.sentMessages))
	}
	res := tg.sentMessages[0].Text
	if !strings.Contains(res, "Geko Developer") || !strings.Contains(res, "geko@example.com") {
		t.Errorf("status response missing user info: %s", res)
	}
	if !strings.Contains(res, "1 (total: 2)") {
		t.Errorf("status response missing active watches count: %s", res)
	}
}

func TestDaemon_CallbackQuery_Show(t *testing.T) {
	tg := &mockTelegramSender{}
	ma := &mockMilanunciosClient{
		convDetails: map[string]*milanuncios.ConversationDetails{
			"c99": {
				Conversation: milanuncios.Conversation{
					ID:       "c99",
					AdTitle:  "Casco Moto",
					AdPrice:  60,
					UserName: "Ana P.",
				},
				Messages: []milanuncios.Message{
					{
						ID:         "m1",
						SenderName: "Ana P.",
						Text:       "¿Qué talla es?",
					},
				},
			},
		},
	}
	d := telegram.NewDaemon(tg, ma, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	// Test with data: "show:c99"
	up := telegram.Update{
		UpdateID: 1,
		CallbackQuery: &telegram.CallbackQuery{
			ID: "cb-1",
			From: telegram.User{
				ID: 12345,
			},
			Data: "show:c99",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response for callback query, got %d", len(tg.sentMessages))
	}
	if !strings.Contains(tg.sentMessages[0].Text, "Conversación #c99") || !strings.Contains(tg.sentMessages[0].Text, "Ana P.") {
		t.Errorf("unexpected callback query show response: %s", tg.sentMessages[0].Text)
	}

	// Test with data: "/show c99"
	tg.sentMessages = nil
	up2 := telegram.Update{
		UpdateID: 2,
		CallbackQuery: &telegram.CallbackQuery{
			ID: "cb-2",
			Message: &telegram.Message{
				Chat: telegram.Chat{ID: 12345},
			},
			Data: "/show c99",
		},
	}
	err = d.ProcessUpdate(context.Background(), up2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 {
		t.Fatalf("expected 1 response for callback query, got %d", len(tg.sentMessages))
	}
	if !strings.Contains(tg.sentMessages[0].Text, "¿Qué talla es?") {
		t.Errorf("unexpected callback query show response: %s", tg.sentMessages[0].Text)
	}
}

func TestDaemon_CallbackQuery_Unauthorized(t *testing.T) {
	var stderr bytes.Buffer
	tg := &mockTelegramSender{}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
		Stderr:         &stderr,
	})

	up := telegram.Update{
		UpdateID: 1,
		CallbackQuery: &telegram.CallbackQuery{
			ID: "cb-hacked",
			From: telegram.User{
				ID: 99999,
			},
			Data: "show:c1",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) > 0 {
		t.Errorf("expected no response to unauthorized callback query")
	}
	if !strings.Contains(stderr.String(), "ignoring callback query from unauthorized chat_id 99999") {
		t.Errorf("expected unauthorized stderr log, got: %s", stderr.String())
	}
}

func TestDaemon_UnknownCommand(t *testing.T) {
	tg := &mockTelegramSender{}
	d := telegram.NewDaemon(tg, nil, nil, telegram.DaemonConfig{
		AllowedChatIDs: []int64{12345},
	})

	up := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 111,
			Chat:      telegram.Chat{ID: 12345},
			Text:      "/foo_bar_command",
		},
	}

	err := d.ProcessUpdate(context.Background(), up)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tg.sentMessages) != 1 || !strings.Contains(tg.sentMessages[0].Text, "Comando desconocido") {
		t.Errorf("expected unknown command error notice, got: %+v", tg.sentMessages)
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
