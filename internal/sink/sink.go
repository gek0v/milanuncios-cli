package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/config"
	"github.com/gek0v/milanuncios-cli/internal/output"
	"github.com/gek0v/milanuncios-cli/internal/store"
)

// Sink represents an external notification target for watch events.
type Sink interface {
	Name() string
	Type() string
	Send(ctx context.Context, ev store.Event) error
}

// NtfySink delivers notifications via ntfy.sh or self-hosted ntfy server.
type NtfySink struct {
	name   string
	url    string
	topic  string
	client *http.Client
}

// NewNtfySink creates an ntfy notification sink.
func NewNtfySink(name string, serverURL, topic string) (*NtfySink, error) {
	if topic == "" {
		return nil, output.Usagef("ntfy sink requires a topic")
	}
	if serverURL == "" {
		serverURL = "https://ntfy.sh"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	return &NtfySink{
		name:   name,
		url:    serverURL,
		topic:  topic,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (s *NtfySink) Name() string { return s.name }
func (s *NtfySink) Type() string { return "ntfy" }

func (s *NtfySink) Send(ctx context.Context, ev store.Event) error {
	endpoint := fmt.Sprintf("%s/%s", s.url, s.topic)

	var payload map[string]any
	_ = json.Unmarshal([]byte(ev.PayloadJSON), &payload)

	title := fmt.Sprintf("Milanuncios: %s", ev.EventType)
	body := fmt.Sprintf("Evento: %s | Anuncio ID: %s", ev.EventType, ev.AdID)
	tags := "milanuncios"
	clickURL := ""

	if ev.EventType == "ad.new" {
		title = "Milanuncios: Nuevo anuncio"
		tags = "bell,package"
	} else if ev.EventType == "ad.price_changed" {
		title = "Milanuncios: Bajada de precio"
		tags = "moneybag,chart_with_downwards_trend"
	} else if ev.EventType == "ad.removed" {
		title = "Milanuncios: Anuncio retirado"
		tags = "wastebasket"
	}

	if ad, ok := payload["ad"].(map[string]any); ok {
		if t, ok := ad["title"].(string); ok && t != "" {
			body = t
		}
		if p, ok := ad["price"].(float64); ok && p > 0 {
			body += fmt.Sprintf(" (%.0f €)", p)
		}
		if u, ok := ad["url"].(string); ok && u != "" {
			clickURL = u
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Title", title)
	req.Header.Set("Tags", tags)
	if clickURL != "" {
		req.Header.Set("Click", clickURL)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending to ntfy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("ntfy returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// WebhookSink delivers raw event JSON to any HTTP webhook endpoint.
type WebhookSink struct {
	name            string
	url             string
	tokenFile       string
	client          *http.Client
	allowPrivateIPs bool
}

// NewWebhookSink creates an HTTP webhook sink.
func NewWebhookSink(name, targetURL, tokenFile string) (*WebhookSink, error) {
	if targetURL == "" {
		return nil, output.Usagef("webhook sink requires a url")
	}

	allowPrivate := os.Getenv("MILANUNCIOS_ALLOW_PRIVATE_IPS") == "true"
	if err := ValidateWebhookURL(targetURL, allowPrivate); err != nil {
		return nil, output.Usagef("webhook destination url %q blocked: %v", targetURL, err)
	}

	return &WebhookSink{
		name:            name,
		url:             targetURL,
		tokenFile:       tokenFile,
		client:          createSafeWebhookClient(allowPrivate),
		allowPrivateIPs: allowPrivate,
	}, nil
}

// SetAllowPrivateIPs configures whether private IP ranges are permitted.
func (s *WebhookSink) SetAllowPrivateIPs(allow bool) {
	s.allowPrivateIPs = allow
	s.client = createSafeWebhookClient(allow)
}

func (s *WebhookSink) Name() string { return s.name }
func (s *WebhookSink) Type() string { return "webhook" }

func (s *WebhookSink) Send(ctx context.Context, ev store.Event) error {
	allowPrivate := s.allowPrivateIPs || os.Getenv("MILANUNCIOS_ALLOW_PRIVATE_IPS") == "true"
	if !allowPrivate {
		if err := ValidateWebhookURL(s.url, false); err != nil {
			return fmt.Errorf("webhook destination blocked by anti-SSRF policy: %w", err)
		}
	}

	reqBody, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "milanuncios-cli/sink")

	if s.tokenFile != "" {
		tokenData, err := os.ReadFile(s.tokenFile)
		if err != nil {
			return fmt.Errorf("reading webhook token_file %q: %w", s.tokenFile, err)
		}
		token := strings.TrimSpace(string(tokenData))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, string(raw))
	}

	return nil
}

// ExecSink passes the event JSON via stdin to a local script/command.
type ExecSink struct {
	name    string
	command []string
}

// NewExecSink creates a command execution sink.
func NewExecSink(name string, command []string) (*ExecSink, error) {
	if len(command) == 0 {
		return nil, output.Usagef("exec sink requires at least one command argument")
	}

	return &ExecSink{
		name:    name,
		command: command,
	}, nil
}

func (s *ExecSink) Name() string { return s.name }
func (s *ExecSink) Type() string { return "exec" }

func (s *ExecSink) Send(ctx context.Context, ev store.Event) error {
	execCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, s.command[0], s.command[1:]...)
	cmd.Stdin = strings.NewReader(ev.PayloadJSON)

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("MILANUNCIOS_EVENT_ID=%d", ev.ID),
		fmt.Sprintf("MILANUNCIOS_EVENT_TYPE=%s", ev.EventType),
		fmt.Sprintf("MILANUNCIOS_WATCH_ID=%d", ev.WatchID),
		fmt.Sprintf("MILANUNCIOS_AD_ID=%s", ev.AdID),
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("executing %v failed: %w (stderr: %s)", s.command, err, stderr.String())
	}

	return nil
}

// FromConfig instantiates a Sink from configuration.
func FromConfig(name string, cfg config.SinkConfig) (Sink, error) {
	switch strings.ToLower(cfg.Type) {
	case "ntfy":
		return NewNtfySink(name, cfg.URL, cfg.Topic)
	case "webhook":
		return NewWebhookSink(name, cfg.URL, cfg.TokenFile)
	case "exec":
		return NewExecSink(name, cfg.Command)
	default:
		return nil, output.Usagef("unknown sink type %q for sink %q (supported: ntfy, webhook, exec)", cfg.Type, name)
	}
}

// Manager aggregates configured sinks.
type Manager struct {
	sinks []Sink
}

// NewManager builds a Manager from config sinks map.
func NewManager(cfgSinks map[string]config.SinkConfig) (*Manager, error) {
	var sinks []Sink
	for name, sc := range cfgSinks {
		s, err := FromConfig(name, sc)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, s)
	}
	return &Manager{sinks: sinks}, nil
}

// Sinks returns the list of loaded sinks.
func (m *Manager) Sinks() []Sink {
	return m.sinks
}

// Dispatch sends the event to all configured sinks concurrently.
func (m *Manager) Dispatch(ctx context.Context, ev store.Event) []error {
	if len(m.sinks) == 0 {
		return nil
	}
	if len(m.sinks) == 1 {
		if err := m.sinks[0].Send(ctx, ev); err != nil {
			return []error{fmt.Errorf("sink %s: %w", m.sinks[0].Name(), err)}
		}
		return nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for _, s := range m.sinks {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()
			if err := s.Send(ctx, ev); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("sink %s: %w", s.Name(), err))
				mu.Unlock()
			}
		}(s)
	}
	wg.Wait()
	return errs
}

// SinkInfo describes a configured sink for CLI output.
type SinkInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SinkInfoList wraps a slice of SinkInfo with Pretty rendering.
type SinkInfoList []SinkInfo

// Pretty renders sinks in a table.
func (sil SinkInfoList) Pretty(w io.Writer, color bool) {
	if len(sil) == 0 {
		fmt.Fprintln(w, "No sinks configured in config.toml under [sinks.<name>].")
		return
	}

	header := []string{"NAME", "TYPE"}
	rows := make([][]string, 0, len(sil))
	for _, si := range sil {
		rows = append(rows, []string{si.Name, si.Type})
	}
	output.Table(w, color, header, rows)
}
