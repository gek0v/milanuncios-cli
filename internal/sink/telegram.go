package sink

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/gek0v/milanuncios-cli/internal/telegram"
)

// TelegramSink delivers notification events to Telegram chats.
type TelegramSink struct {
	name    string
	tg      *telegram.Client
	chatIDs []int64
	store   *store.Store
}

// NewTelegramSink creates a new Telegram notification sink.
func NewTelegramSink(name string, tg *telegram.Client, chatIDs []int64, st *store.Store) (*TelegramSink, error) {
	if tg == nil {
		return nil, output.Usagef("telegram sink requires a telegram client")
	}
	if len(chatIDs) == 0 {
		return nil, output.Usagef("telegram sink %q requires at least one chat_id", name)
	}
	return &TelegramSink{
		name:    name,
		tg:      tg,
		chatIDs: chatIDs,
		store:   st,
	}, nil
}

func (s *TelegramSink) Name() string { return s.name }
func (s *TelegramSink) Type() string { return "telegram" }

func (s *TelegramSink) Send(ctx context.Context, ev store.Event) error {
	text, opts, convInfo, err := s.formatEvent(ev)
	if err != nil {
		return err
	}

	var errs []error
	for _, chatID := range s.chatIDs {
		sentMsg, sendErr := s.tg.SendMessage(ctx, chatID, text, opts)
		if sendErr != nil {
			errs = append(errs, sendErr)
			continue
		}
		if s.store != nil && sentMsg != nil && convInfo != nil {
			_ = s.store.SaveTelegramMessageMap(
				ctx,
				sentMsg.MessageID,
				convInfo.ConversationID,
				convInfo.AdID,
				convInfo.AdTitle,
				convInfo.OpponentName,
			)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("sending to telegram: %w", errs[0])
	}
	return nil
}

type conversationCorrelation struct {
	ConversationID string
	AdID           string
	AdTitle        string
	OpponentName   string
}

func (s *TelegramSink) formatEvent(ev store.Event) (string, *telegram.SendMessageOptions, *conversationCorrelation, error) {
	opts := &telegram.SendMessageOptions{
		ParseMode: "HTML",
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
		payload = make(map[string]any)
	}

	switch ev.EventType {
	case "ad.new":
		title, price, locStr, link := extractAdDetails(payload)
		if title == "" {
			title = fmt.Sprintf("Anuncio %s", ev.AdID)
		}

		var lines []string
		lines = append(lines, "🆕 <b>Nuevo anuncio en Milanuncios</b>")
		lines = append(lines, fmt.Sprintf("<b>%s</b>", html.EscapeString(title)))
		if price > 0 {
			lines = append(lines, fmt.Sprintf("💶 <b>Precio:</b> %.0f €", price))
		}
		if locStr != "" {
			lines = append(lines, fmt.Sprintf("📍 <b>Ubicación:</b> %s", html.EscapeString(locStr)))
		}
		if link != "" {
			lines = append(lines, fmt.Sprintf("🔗 <a href=%q>Ver anuncio en Milanuncios</a>", link))
			opts.ReplyMarkup = &telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{
						{
							Text: "🔗 Ver en Milanuncios",
							URL:  link,
						},
					},
				},
			}
		}
		return strings.Join(lines, "\n"), opts, nil, nil

	case "ad.price_changed":
		title, price, _, link := extractAdDetails(payload)
		if title == "" {
			title = fmt.Sprintf("Anuncio %s", ev.AdID)
		}
		oldPrice, _ := payload["old_price"].(float64)
		newPrice, _ := payload["new_price"].(float64)
		if newPrice == 0 {
			newPrice = price
		}
		diff, _ := payload["diff"].(float64)
		if diff == 0 && oldPrice > 0 && newPrice > 0 {
			diff = newPrice - oldPrice
		}

		var lines []string
		lines = append(lines, "📉 <b>Bajada de precio en Milanuncios</b>")
		lines = append(lines, fmt.Sprintf("<b>%s</b>", html.EscapeString(title)))
		if oldPrice > 0 && newPrice > 0 {
			diffStr := fmt.Sprintf("%.0f €", diff)
			if diff > 0 {
				diffStr = fmt.Sprintf("+%.0f €", diff)
			}
			lines = append(lines, fmt.Sprintf("💶 <b>Precio:</b> %.0f € (antes %.0f €, <b>%s</b>)", newPrice, oldPrice, diffStr))
		} else if newPrice > 0 {
			lines = append(lines, fmt.Sprintf("💶 <b>Precio:</b> %.0f €", newPrice))
		}
		if link != "" {
			lines = append(lines, fmt.Sprintf("🔗 <a href=%q>Ver anuncio en Milanuncios</a>", link))
			opts.ReplyMarkup = &telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{
						{
							Text: "🔗 Ver en Milanuncios",
							URL:  link,
						},
					},
				},
			}
		}
		return strings.Join(lines, "\n"), opts, nil, nil

	case "ad.removed":
		adID := ev.AdID
		if id, ok := payload["ad_id"].(string); ok && id != "" {
			adID = id
		}
		text := fmt.Sprintf("🗑️ <b>Anuncio retirado</b>\nEl anuncio <code>%s</code> ha sido retirado o ya no está disponible.", html.EscapeString(adID))
		return text, opts, nil, nil

	case "chat.message_received":
		convID, _ := payload["conversation_id"].(string)
		adID, _ := payload["ad_id"].(string)
		if adID == "" {
			adID = ev.AdID
		}
		adTitle, _ := payload["ad_title"].(string)
		if adTitle == "" {
			adTitle = fmt.Sprintf("Anuncio #%s", adID)
		}
		senderName, _ := payload["sender_name"].(string)
		if senderName == "" {
			senderName = "Usuario"
		}
		msgText, _ := payload["text"].(string)
		price, _ := payload["ad_price"].(float64)
		if price == 0 {
			price, _ = payload["price"].(float64)
		}

		priceStr := ""
		if price > 0 {
			priceStr = fmt.Sprintf(" (%.0f €)", price)
		}

		text := fmt.Sprintf("💬 <b>Mensaje de %s</b> en <i>%s%s</i>:\n\"%s\"\n\n<i>ℹ️ Desliza este mensaje para responder directamente.</i>",
			html.EscapeString(senderName),
			html.EscapeString(adTitle),
			priceStr,
			html.EscapeString(msgText),
		)

		opponentName, _ := payload["opponent_name"].(string)
		if opponentName == "" {
			opponentName = senderName
		}

		corr := &conversationCorrelation{
			ConversationID: convID,
			AdID:           adID,
			AdTitle:        adTitle,
			OpponentName:   opponentName,
		}
		return text, opts, corr, nil

	default:
		text := fmt.Sprintf("📢 <b>Milanuncios: %s</b>\nAnuncio ID: <code>%s</code>", html.EscapeString(ev.EventType), html.EscapeString(ev.AdID))
		return text, opts, nil, nil
	}
}

func extractAdDetails(payload map[string]any) (title string, price float64, locStr string, link string) {
	if ad, ok := payload["ad"].(map[string]any); ok {
		if t, ok := ad["title"].(string); ok {
			title = t
		}
		if p, ok := ad["price"].(float64); ok {
			price = p
		}
		if u, ok := ad["url"].(string); ok {
			link = u
		}
		if loc, ok := ad["location"].(map[string]any); ok {
			prov, _ := loc["province"].(string)
			muni, _ := loc["municipality"].(string)
			if muni != "" && prov != "" && muni != prov {
				locStr = fmt.Sprintf("%s, %s", muni, prov)
			} else if muni != "" {
				locStr = muni
			} else if prov != "" {
				locStr = prov
			}
		} else if s, ok := ad["location"].(string); ok {
			locStr = s
		}
	}
	if title == "" {
		if t, ok := payload["title"].(string); ok {
			title = t
		}
	}
	if price == 0 {
		if p, ok := payload["price"].(float64); ok {
			price = p
		}
	}
	if link == "" {
		if u, ok := payload["url"].(string); ok {
			link = u
		}
	}
	return
}
