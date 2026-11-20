package telegram

import (
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/eval"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

// MilanunciosSender sends messages to Milanuncios conversations.
type MilanunciosSender interface {
	SendMessage(ctx context.Context, convID, text string) (*milanuncios.Message, error)
}

// MilanunciosClient defines methods to interact with Milanuncios for interactive bot commands.
type MilanunciosClient interface {
	SendMessage(ctx context.Context, convID, text string) (*milanuncios.Message, error)
	ListChats(ctx context.Context) (milanuncios.ConversationList, error)
	GetChat(ctx context.Context, id string) (*milanuncios.ConversationDetails, error)
	Search(ctx context.Context, q milanuncios.SearchQuery) (*milanuncios.SearchResult, error)
	GetAd(ctx context.Context, idOrURL string) (*milanuncios.AdItem, error)
	Me(ctx context.Context) (*milanuncios.UserProfile, error)
}

// MessageStore retrieves mappings from Telegram message IDs to Milanuncios conversations.
type MessageStore interface {
	GetTelegramMessageMap(ctx context.Context, msgID int64) (*store.TelegramMessageMap, error)
}

// WatchStore provides access to configured watches.
type WatchStore interface {
	ListWatches(ctx context.Context) (store.WatchList, error)
}

// Store combines message map lookup and watch store.
type Store interface {
	MessageStore
	WatchStore
}

// EvaluationEngine evaluates ad pricing against market benchmarks.
type EvaluationEngine interface {
	EvaluateAd(ctx context.Context, adID string, includeComparables bool) (*eval.EvaluationReport, error)
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

// Daemon coordinates long polling, authorization whitelisting, swipe-to-reply routing, and interactive commands.
type Daemon struct {
	tg     TelegramSender
	ma     MilanunciosSender
	st     MessageStore
	eval   EvaluationEngine
	cfg    DaemonConfig
	offset int64
}

// NewDaemon creates a new Daemon instance.
func NewDaemon(tg TelegramSender, ma MilanunciosSender, st MessageStore, cfg DaemonConfig, ev ...EvaluationEngine) *Daemon {
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
	d := &Daemon{
		tg:  tg,
		ma:  ma,
		st:  st,
		cfg: cfg,
	}
	if len(ev) > 0 {
		d.eval = ev[0]
	}
	return d
}

// SetEvaluator sets the evaluation engine on the daemon.
func (d *Daemon) SetEvaluator(ev EvaluationEngine) {
	d.eval = ev
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

func (d *Daemon) getMilanunciosClient() MilanunciosClient {
	if mc, ok := d.ma.(MilanunciosClient); ok {
		return mc
	}
	return nil
}

func (d *Daemon) getWatchStore() WatchStore {
	if ws, ok := d.st.(WatchStore); ok {
		return ws
	}
	return nil
}

// ProcessUpdate handles authorization checking, swipe-to-reply routing, and interactive bot commands.
func (d *Daemon) ProcessUpdate(ctx context.Context, update Update) error {
	// Handle CallbackQuery from inline buttons
	if update.CallbackQuery != nil {
		return d.processCallbackQuery(ctx, update.CallbackQuery)
	}

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
		return nil
	}

	// Interactive Commands (ReplyToMessage == nil)
	return d.handleCommand(ctx, msg)
}

func (d *Daemon) processCallbackQuery(ctx context.Context, cb *CallbackQuery) error {
	var chatID int64
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
	} else {
		chatID = cb.From.ID
	}

	if !d.isAllowed(chatID) {
		fmt.Fprintf(d.cfg.Stderr, "[telegram] ignoring callback query from unauthorized chat_id %d\n", chatID)
		return nil
	}

	data := strings.TrimSpace(cb.Data)
	switch {
	case strings.HasPrefix(data, "/show "):
		convID := strings.TrimSpace(strings.TrimPrefix(data, "/show "))
		return d.handleShow(ctx, chatID, convID)
	case strings.HasPrefix(data, "show:"):
		convID := strings.TrimSpace(strings.TrimPrefix(data, "show:"))
		return d.handleShow(ctx, chatID, convID)
	case strings.HasPrefix(data, "/eval "):
		target := strings.TrimSpace(strings.TrimPrefix(data, "/eval "))
		return d.handleEval(ctx, chatID, target)
	case strings.HasPrefix(data, "eval:"):
		target := strings.TrimSpace(strings.TrimPrefix(data, "eval:"))
		return d.handleEval(ctx, chatID, target)
	default:
		return nil
	}
}

func (d *Daemon) handleCommand(ctx context.Context, msg *Message) error {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return nil
	}

	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}

	cmd := fields[0]
	if atIdx := strings.Index(cmd, "@"); atIdx != -1 {
		cmd = cmd[:atIdx]
	}
	cmd = strings.ToLower(cmd)
	args := fields[1:]

	chatID := msg.Chat.ID

	switch cmd {
	case "/start", "/help":
		return d.handleHelp(ctx, chatID)
	case "/chats":
		return d.handleChats(ctx, chatID)
	case "/show":
		if len(args) == 0 {
			_, err := d.tg.SendMessage(ctx, chatID, "ℹ️ <b>Uso:</b> <code>/show &lt;id_conversacion&gt;</code>", &SendMessageOptions{ParseMode: "HTML"})
			return err
		}
		return d.handleShow(ctx, chatID, args[0])
	case "/search":
		if len(args) == 0 {
			_, err := d.tg.SendMessage(ctx, chatID, "ℹ️ <b>Uso:</b> <code>/search &lt;termino_busqueda&gt;</code>", &SendMessageOptions{ParseMode: "HTML"})
			return err
		}
		query := strings.Join(args, " ")
		return d.handleSearch(ctx, chatID, query)
	case "/eval":
		if len(args) == 0 {
			_, err := d.tg.SendMessage(ctx, chatID, "ℹ️ <b>Uso:</b> <code>/eval &lt;id|url&gt;</code>", &SendMessageOptions{ParseMode: "HTML"})
			return err
		}
		return d.handleEval(ctx, chatID, args[0])
	case "/watches":
		return d.handleWatches(ctx, chatID)
	case "/status":
		return d.handleStatus(ctx, chatID)
	default:
		if strings.HasPrefix(cmd, "/") {
			_, err := d.tg.SendMessage(ctx, chatID, fmt.Sprintf("❓ Comando desconocido: <code>%s</code>\nEscribe /help para ver la lista de comandos disponibles.", html.EscapeString(cmd)), &SendMessageOptions{ParseMode: "HTML"})
			return err
		}
		return nil
	}
}

func (d *Daemon) handleHelp(ctx context.Context, chatID int64) error {
	helpText := `🤖 <b>milanuncios-cli Bot</b>

<b>Comandos interactivos:</b>
/chats — Lista los últimos 5 chats activos con botones de acceso
/show &lt;id&gt; — Muestra los últimos 10 mensajes de una conversación
/search &lt;query&gt; — Busca los 3 mejores resultados con precio y enlace
/eval &lt;id|url&gt; — Evalúa el precio y calcula el Deal Score (0-100)
/watches — Muestra el estado de los monitores activos en segundo plano
/status — Comprueba la sesión activa de Milanuncios y monitores
/help — Muestra este mensaje de ayuda

💬 <b>¿Cómo responder a compradores?</b>
Cuando recibas una notificación de un mensaje nuevo en Telegram, simplemente <i>desliza el mensaje</i> (Swipe to Reply) y escribe tu respuesta. El bot la enviará automáticamente a Milanuncios.`

	_, err := d.tg.SendMessage(ctx, chatID, helpText, &SendMessageOptions{
		ParseMode: "HTML",
	})
	return err
}

func (d *Daemon) handleChats(ctx context.Context, chatID int64) error {
	mc := d.getMilanunciosClient()
	if mc == nil {
		_, err := d.tg.SendMessage(ctx, chatID, "❌ Cliente de Milanuncios no configurado para comandos interactivos.", nil)
		return err
	}

	chats, err := mc.ListChats(ctx)
	if err != nil {
		_, sendErr := d.tg.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error al obtener conversaciones: %s", html.EscapeString(err.Error())), nil)
		return sendErr
	}

	if len(chats) == 0 {
		_, err := d.tg.SendMessage(ctx, chatID, "📭 No tienes conversaciones activas en Milanuncios.", nil)
		return err
	}

	limit := 5
	if len(chats) < limit {
		limit = len(chats)
	}
	topChats := chats[:limit]

	var sb strings.Builder
	sb.WriteString("💬 <b>Conversaciones recientes:</b>\n\n")

	var keyboard [][]InlineKeyboardButton
	for _, c := range topChats {
		unreadBadge := "⚪"
		if c.Unread {
			unreadBadge = "🔴"
		}

		priceStr := ""
		if c.AdPrice > 0 {
			priceStr = fmt.Sprintf(" (%s)", eval.FormatEuro(c.AdPrice))
		}

		sb.WriteString(fmt.Sprintf("%s <b>%s</b> — <i>%s</i>%s\n",
			unreadBadge,
			html.EscapeString(c.UserName),
			html.EscapeString(c.AdTitle),
			priceStr,
		))
		if c.LastMessage != "" {
			sb.WriteString(fmt.Sprintf("   Último: %q\n", html.EscapeString(c.LastMessage)))
		}
		sb.WriteString("\n")

		btnText := fmt.Sprintf("💬 Ver #%s (%s)", c.ID, c.UserName)
		keyboard = append(keyboard, []InlineKeyboardButton{
			{
				Text:         btnText,
				CallbackData: fmt.Sprintf("/show %s", c.ID),
			},
		})
	}

	opts := &SendMessageOptions{
		ParseMode: "HTML",
		ReplyMarkup: InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		},
	}

	_, err = d.tg.SendMessage(ctx, chatID, strings.TrimSpace(sb.String()), opts)
	return err
}

func (d *Daemon) handleShow(ctx context.Context, chatID int64, convID string) error {
	mc := d.getMilanunciosClient()
	if mc == nil {
		_, err := d.tg.SendMessage(ctx, chatID, "❌ Cliente de Milanuncios no configurado para comandos interactivos.", nil)
		return err
	}

	conv, err := mc.GetChat(ctx, convID)
	if err != nil {
		_, sendErr := d.tg.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error al obtener conversación #%s: %s", html.EscapeString(convID), html.EscapeString(err.Error())), nil)
		return sendErr
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("💬 <b>Conversación #%s</b> con <b>%s</b>\n", html.EscapeString(conv.Conversation.ID), html.EscapeString(conv.Conversation.UserName)))
	if conv.Conversation.AdTitle != "" {
		priceStr := ""
		if conv.Conversation.AdPrice > 0 {
			priceStr = fmt.Sprintf(" (%s)", eval.FormatEuro(conv.Conversation.AdPrice))
		}
		sb.WriteString(fmt.Sprintf("Anuncio: <i>%s</i>%s\n", html.EscapeString(conv.Conversation.AdTitle), priceStr))
	}
	sb.WriteString("\n<b>Historial de mensajes:</b>\n\n")

	messages := conv.Messages
	if len(messages) == 0 {
		sb.WriteString("<i>(No hay mensajes en esta conversación)</i>\n")
	} else {
		limit := 10
		start := 0
		if len(messages) > limit {
			start = len(messages) - limit
		}
		recent := messages[start:]

		for _, m := range recent {
			sender := html.EscapeString(m.SenderName)
			if m.FromMe {
				sender = "Tú"
			}
			timeStr := ""
			if m.SentAt != "" {
				timeStr = fmt.Sprintf(" <i>(%s)</i>", html.EscapeString(m.SentAt))
			}
			sb.WriteString(fmt.Sprintf("👤 <b>%s</b>%s:\n%s\n\n", sender, timeStr, html.EscapeString(m.Text)))
		}
	}

	_, err = d.tg.SendMessage(ctx, chatID, strings.TrimSpace(sb.String()), &SendMessageOptions{
		ParseMode: "HTML",
	})
	return err
}

func (d *Daemon) handleSearch(ctx context.Context, chatID int64, query string) error {
	mc := d.getMilanunciosClient()
	if mc == nil {
		_, err := d.tg.SendMessage(ctx, chatID, "❌ Cliente de Milanuncios no configurado para comandos interactivos.", nil)
		return err
	}

	searchRes, err := mc.Search(ctx, milanuncios.SearchQuery{
		Keywords: query,
		Limit:    3,
	})
	if err != nil {
		_, sendErr := d.tg.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error al realizar la búsqueda: %s", html.EscapeString(err.Error())), nil)
		return sendErr
	}

	if searchRes == nil || len(searchRes.Items) == 0 {
		_, err := d.tg.SendMessage(ctx, chatID, fmt.Sprintf("🔍 No se encontraron resultados para: <i>%s</i>", html.EscapeString(query)), &SendMessageOptions{ParseMode: "HTML"})
		return err
	}

	items := searchRes.Items
	if len(items) > 3 {
		items = items[:3]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 <b>Resultados para:</b> <i>%s</i>\n\n", html.EscapeString(query)))

	var keyboard [][]InlineKeyboardButton
	for i, it := range items {
		priceStr := eval.FormatEuro(it.Price)
		sb.WriteString(fmt.Sprintf("<b>%d. %s</b>\n", i+1, html.EscapeString(it.Title)))
		sb.WriteString(fmt.Sprintf("💰 <b>%s</b>", priceStr))
		loc := it.Location.Municipality
		if loc == "" {
			loc = it.Location.Province
		} else if it.Location.Province != "" {
			loc += " (" + it.Location.Province + ")"
		}
		if loc != "" {
			sb.WriteString(fmt.Sprintf(" 📍 %s", html.EscapeString(loc)))
		}
		sb.WriteString("\n")
		if it.URL != "" {
			sb.WriteString(fmt.Sprintf("🔗 <a href=\"%s\">Ver en Milanuncios</a>\n", html.EscapeString(it.URL)))
		}
		sb.WriteString("\n")

		if it.ID != "" {
			keyboard = append(keyboard, []InlineKeyboardButton{
				{
					Text:         fmt.Sprintf("📊 Evaluar #%s", it.ID),
					CallbackData: fmt.Sprintf("/eval %s", it.ID),
				},
			})
		}
	}

	opts := &SendMessageOptions{
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	}
	if len(keyboard) > 0 {
		opts.ReplyMarkup = InlineKeyboardMarkup{InlineKeyboard: keyboard}
	}

	_, err = d.tg.SendMessage(ctx, chatID, strings.TrimSpace(sb.String()), opts)
	return err
}

func (d *Daemon) handleEval(ctx context.Context, chatID int64, target string) error {
	if d.eval == nil {
		_, err := d.tg.SendMessage(ctx, chatID, "❌ Motor de evaluación (Deal Score) no configurado.", nil)
		return err
	}

	report, err := d.eval.EvaluateAd(ctx, target, false)
	if err != nil {
		_, sendErr := d.tg.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error al evaluar anuncio <code>%s</code>: %s", html.EscapeString(target), html.EscapeString(err.Error())), &SendMessageOptions{ParseMode: "HTML"})
		return sendErr
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 <b>Evaluación: %s</b>\n\n", html.EscapeString(report.Title)))
	sb.WriteString(fmt.Sprintf("💰 <b>Precio:</b> %s\n", eval.FormatEuro(report.Price)))
	sb.WriteString(fmt.Sprintf("⭐ <b>Deal Score:</b> %d / 100 (<code>%s</code>)\n", report.DealScore, report.Rating))
	sb.WriteString(fmt.Sprintf("📈 <b>Mediana de mercado:</b> %s (muestra: %d anuncios)\n", eval.FormatEuro(report.MarketMedian), report.SampleSize))
	if report.DiscountPercent != 0 {
		if report.DiscountPercent > 0 {
			sb.WriteString(fmt.Sprintf("🏷️ <b>Descuento:</b> +%.1f%% sobre la mediana\n", report.DiscountPercent))
		} else {
			sb.WriteString(fmt.Sprintf("🏷️ <b>Sobreprecio:</b> %.1f%% sobre la mediana\n", -report.DiscountPercent))
		}
	}
	if report.Verdict != "" {
		sb.WriteString(fmt.Sprintf("\n💡 <b>Veredicto:</b> %s\n", html.EscapeString(report.Verdict)))
	}
	if report.URL != "" {
		sb.WriteString(fmt.Sprintf("\n🔗 <a href=\"%s\">Ver anuncio en Milanuncios</a>\n", html.EscapeString(report.URL)))
	}

	_, err = d.tg.SendMessage(ctx, chatID, strings.TrimSpace(sb.String()), &SendMessageOptions{
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	return err
}

func (d *Daemon) handleWatches(ctx context.Context, chatID int64) error {
	ws := d.getWatchStore()
	if ws == nil {
		_, err := d.tg.SendMessage(ctx, chatID, "❌ Base de datos no disponible para consultar monitores.", nil)
		return err
	}

	watches, err := ws.ListWatches(ctx)
	if err != nil {
		_, sendErr := d.tg.SendMessage(ctx, chatID, fmt.Sprintf("❌ Error al listar monitores: %s", html.EscapeString(err.Error())), nil)
		return sendErr
	}

	if len(watches) == 0 {
		_, err := d.tg.SendMessage(ctx, chatID, "⏱️ No hay monitores activos configurados.", nil)
		return err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⏱️ <b>Monitores configurados (%d):</b>\n\n", len(watches)))

	for _, w := range watches {
		statusIcon := "🟢"
		if w.Status != "active" {
			statusIcon = "⏸️"
		}

		targetStr := w.Target
		if targetStr == "" {
			targetStr = w.Name
		}

		sb.WriteString(fmt.Sprintf("%s <b>%s</b> [<code>%s</code>]\n", statusIcon, html.EscapeString(w.Name), html.EscapeString(w.Type)))
		sb.WriteString(fmt.Sprintf("   🎯 Objetivo: <i>%s</i>\n", html.EscapeString(targetStr)))
		sb.WriteString(fmt.Sprintf("   ⏳ Intervalo: %ds | Estado: %s\n\n", w.IntervalSec, html.EscapeString(w.Status)))
	}

	_, err = d.tg.SendMessage(ctx, chatID, strings.TrimSpace(sb.String()), &SendMessageOptions{
		ParseMode: "HTML",
	})
	return err
}

func (d *Daemon) handleStatus(ctx context.Context, chatID int64) error {
	var userStr string
	mc := d.getMilanunciosClient()
	if mc != nil {
		me, err := mc.Me(ctx)
		if err != nil {
			userStr = "⚠️ No autenticado o sesión expirada"
		} else if me != nil {
			if me.Email != "" && me.Name != "" {
				userStr = fmt.Sprintf("✅ Conectado como <b>%s</b> (<i>%s</i>)", html.EscapeString(me.Name), html.EscapeString(me.Email))
			} else if me.Name != "" {
				userStr = fmt.Sprintf("✅ Conectado como <b>%s</b>", html.EscapeString(me.Name))
			} else if me.Email != "" {
				userStr = fmt.Sprintf("✅ Conectado como <i>%s</i>", html.EscapeString(me.Email))
			} else {
				userStr = "✅ Sesión activa"
			}
		}
	} else {
		userStr = "❌ Cliente Milanuncios no inicializado"
	}

	watchCount := 0
	activeWatchCount := 0
	ws := d.getWatchStore()
	if ws != nil {
		if watches, err := ws.ListWatches(ctx); err == nil {
			watchCount = len(watches)
			for _, w := range watches {
				if w.Status == "active" {
					activeWatchCount++
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("🤖 <b>Estado del Sistema</b>\n\n")
	sb.WriteString(fmt.Sprintf("👤 <b>Sesión Milanuncios:</b> %s\n", userStr))
	sb.WriteString(fmt.Sprintf("⏱️ <b>Monitores activos:</b> %d (total: %d)\n", activeWatchCount, watchCount))
	sb.WriteString("📡 <b>Inbound Daemon:</b> Long Polling activo y conectado\n")

	_, err := d.tg.SendMessage(ctx, chatID, strings.TrimSpace(sb.String()), &SendMessageOptions{
		ParseMode: "HTML",
	})
	return err
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
