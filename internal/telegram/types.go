package telegram

// User represents a Telegram user or bot.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"` // "private", "group", "supergroup", "channel"
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID      int64    `json:"message_id"`
	From           *User    `json:"from,omitempty"`
	Date           int64    `json:"date"`
	Chat           Chat     `json:"chat"`
	Text           string   `json:"text,omitempty"`
	ReplyToMessage *Message `json:"reply_to_message,omitempty"`
}

// Update represents an incoming update from Telegram getUpdates.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// CallbackQuery represents an incoming callback query from an inline keyboard button.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// InlineKeyboardButton represents one button of an inline keyboard.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboardMarkup represents an inline keyboard that appears right next to the message it belongs to.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// SendMessageOptions provides optional parameters for SendMessage.
type SendMessageOptions struct {
	ParseMode             string `json:"parse_mode,omitempty"`
	ReplyMarkup           any    `json:"reply_markup,omitempty"`
	ReplyToMessageID      int64  `json:"reply_to_message_id,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}
