package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the official Telegram Bot API base URL.
const DefaultBaseURL = "https://api.telegram.org"

// APIError represents an error returned by Telegram Bot API.
type APIError struct {
	StatusCode  int
	ErrorCode   int
	Description string
}

func (e *APIError) Error() string {
	if e.ErrorCode != 0 {
		return fmt.Sprintf("telegram api error (code %d): %s", e.ErrorCode, e.Description)
	}
	return fmt.Sprintf("telegram http error (status %d): %s", e.StatusCode, e.Description)
}

type apiResponse[T any] struct {
	Ok          bool   `json:"ok"`
	Result      T      `json:"result"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// Client is a pure Go client for the Telegram Bot API.
type Client struct {
	BaseURL  string
	BotToken string
	HTTP     *http.Client
}

// Option configures a Telegram Client.
type Option func(*Client)

// WithBaseURL sets a custom base URL (useful for mock servers / local bot API).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.BaseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.HTTP = httpClient
		}
	}
}

// WithTransport configures a custom RoundTripper on the HTTP client.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) {
		if rt != nil {
			c.HTTP.Transport = rt
		}
	}
}

// WithTimeout sets the request timeout on the HTTP client.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.HTTP.Timeout = d
	}
}

// NewClient creates a new Telegram Bot API client.
func NewClient(token string, opts ...Option) *Client {
	c := &Client{
		BaseURL:  DefaultBaseURL,
		BotToken: token,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetMe tests the bot token and returns basic information about the bot.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var user User
	if err := c.postJSON(ctx, "getMe", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

type sendMessagePayload struct {
	ChatID                int64  `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	ReplyToMessageID      int64  `json:"reply_to_message_id,omitempty"`
	ReplyMarkup           any    `json:"reply_markup,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

// SendMessage sends a text message to a specified Telegram chat.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, opts *SendMessageOptions) (*Message, error) {
	req := sendMessagePayload{
		ChatID: chatID,
		Text:   text,
	}
	if opts != nil {
		req.ParseMode = opts.ParseMode
		req.ReplyToMessageID = opts.ReplyToMessageID
		req.ReplyMarkup = opts.ReplyMarkup
		req.DisableWebPagePreview = opts.DisableWebPagePreview
	}

	var msg Message
	if err := c.postJSON(ctx, "sendMessage", req, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *Client) postJSON(ctx context.Context, method string, reqBody any, dest any) error {
	endpoint := fmt.Sprintf("%s/bot%s/%s", c.BaseURL, c.BotToken, method)

	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("encoding telegram request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("creating telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return sanitizeError(err, c.BotToken)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("reading telegram response: %w", err)
	}

	var apiResp apiResponse[json.RawMessage]
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return &APIError{
			StatusCode:  resp.StatusCode,
			Description: fmt.Sprintf("non-json response: %s", string(bodyBytes)),
		}
	}

	if !apiResp.Ok || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{
			StatusCode:  resp.StatusCode,
			ErrorCode:   apiResp.ErrorCode,
			Description: apiResp.Description,
		}
	}

	if dest != nil && len(apiResp.Result) > 0 {
		if err := json.Unmarshal(apiResp.Result, dest); err != nil {
			return fmt.Errorf("decoding result payload: %w", err)
		}
	}

	return nil
}

func sanitizeError(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	sanitized := strings.ReplaceAll(err.Error(), token, "[REDACTED]")
	return fmt.Errorf("%s", sanitized)
}
