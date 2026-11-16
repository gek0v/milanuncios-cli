package milanuncios

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/output"
)

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Client interacts with the Milanuncios web endpoints and private API.
type Client struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client
	Proxy     string
	Debug     io.Writer
	Notice    func(string)
	OnBlock   func(ctx context.Context)

	redactStrings []string
	uaMu          sync.Mutex
	useBrowserUA  bool
}

// NewClient creates an initialized Client.
func NewClient(version string) *Client {
	if version == "" {
		version = "dev"
	}
	return &Client{
		BaseURL:   "https://www.milanuncios.com",
		UserAgent: fmt.Sprintf("milanuncios-cli/%s (+https://github.com/gek0v/milanuncios-cli)", version),
		HTTP: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Redact registers a string to be replaced with [REDACTED] in debug logs.
func (c *Client) Redact(secret string) {
	if secret != "" {
		c.redactStrings = append(c.redactStrings, secret)
	}
}

func (c *Client) redact(s string) string {
	for _, r := range c.redactStrings {
		s = strings.ReplaceAll(s, r, "[REDACTED]")
	}
	return s
}

// UseProxy configures an HTTP/SOCKS5 proxy for all requests without direct fallback.
func (c *Client) UseProxy(u *url.URL) {
	c.Proxy = u.String()
	transport := &http.Transport{
		Proxy: http.ProxyURL(u),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	c.HTTP.Transport = transport
}

func (c *Client) currentUA() string {
	c.uaMu.Lock()
	defer c.uaMu.Unlock()
	if c.useBrowserUA {
		return browserUA
	}
	return c.UserAgent
}

func (c *Client) setBrowserUA() {
	c.uaMu.Lock()
	c.useBrowserUA = true
	c.uaMu.Unlock()
}

func isWAFBlock(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "cloudflare") ||
		strings.Contains(lower, "akamai") ||
		strings.Contains(lower, "datadome") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "attention required")
}

func isBotStatus(status int, body []byte) bool {
	return status == http.StatusTooManyRequests ||
		(status == http.StatusForbidden && !json.Valid(body))
}

func (c *Client) debugf(format string, args ...any) {
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, format+"\n", args...)
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values, out any) error {
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	buildReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.currentUA())
		req.Header.Set("Accept", "application/json")
		return req, nil
	}

	endpoint := method + " " + path
	c.debugf("> %s %s", method, c.redact(u))

	req, err := buildReq()
	if err != nil {
		return err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return c.wrapNetError(endpoint, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return c.wrapNetError(endpoint, err)
	}
	c.debugf("< %d %s (%d bytes)", resp.StatusCode, endpoint, len(raw))

	// If WAF blocks honest UA with 403 HTML, retry once with realistic browser UA
	if resp.StatusCode == http.StatusForbidden && isWAFBlock(raw) && !c.useBrowserUA {
		c.setBrowserUA()
		if c.Notice != nil {
			c.Notice("milanuncios rejected cli user agent; retrying with browser user agent")
		}

		req, err = buildReq()
		if err != nil {
			return err
		}
		resp, err = c.HTTP.Do(req)
		if err != nil {
			return c.wrapNetError(endpoint, err)
		}
		defer resp.Body.Close()
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return c.wrapNetError(endpoint, err)
		}
		c.debugf("< %d %s (%d bytes, browser UA)", resp.StatusCode, endpoint, len(raw))
	}

	if isBotStatus(resp.StatusCode, raw) {
		if c.OnBlock != nil {
			c.OnBlock(ctx)
		}
		return &output.DomainError{
			Kind:      output.KindBlocked,
			Status:    resp.StatusCode,
			Endpoint:  endpoint,
			Body:      c.snippet(raw),
			Retryable: true,
			Msg:       "milanuncios blocked or rate-limited the request. Wait 15 minutes before retrying",
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		return &output.DomainError{
			Kind:     output.KindNotFound,
			Status:   resp.StatusCode,
			Endpoint: endpoint,
			Msg:      "resource not found on milanuncios",
		}
	}

	if resp.StatusCode >= 500 {
		return &output.DomainError{
			Kind:      output.KindGeneric,
			Status:    resp.StatusCode,
			Endpoint:  endpoint,
			Retryable: true,
			Msg:       fmt.Sprintf("milanuncios server returned HTTP %d", resp.StatusCode),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &output.DomainError{
			Kind:     output.KindGeneric,
			Status:   resp.StatusCode,
			Endpoint: endpoint,
			Body:     c.snippet(raw),
			Msg:      fmt.Sprintf("unexpected HTTP %d from milanuncios", resp.StatusCode),
		}
	}

	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return &output.DomainError{
				Kind:     output.KindAPIChanged,
				Status:   resp.StatusCode,
				Endpoint: endpoint,
				Body:     "json decode: " + err.Error() + "; body: " + c.snippet(raw),
				Msg:      "milanuncios returned an unexpected response structure",
			}
		}
	}

	return nil
}

func (c *Client) snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return c.redact(s)
}

func (c *Client) wrapNetError(endpoint string, err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return &output.DomainError{
			Kind:      output.KindNetwork,
			Endpoint:  endpoint,
			Retryable: true,
			Msg:       "request to milanuncios timed out. Check your network or proxy",
		}
	}

	via := ""
	if c.Proxy != "" {
		via = " through proxy " + c.Proxy + " (direct connection was refused)"
	}

	return &output.DomainError{
		Kind:      output.KindNetwork,
		Endpoint:  endpoint,
		Retryable: true,
		Msg:       "could not reach milanuncios" + via + ": " + c.redact(err.Error()),
	}
}

// Search executes a search query against Milanuncios listings.
func (c *Client) Search(ctx context.Context, q SearchQuery) (*SearchResult, error) {
	params := url.Values{}
	if q.Keywords != "" {
		params.Set("s", q.Keywords)
	}
	if q.MinPrice > 0 {
		params.Set("fromPrice", strconv.FormatFloat(q.MinPrice, 'f', -1, 64))
	}
	if q.MaxPrice > 0 {
		params.Set("toPrice", strconv.FormatFloat(q.MaxPrice, 'f', -1, 64))
	}
	if q.Province != "" {
		params.Set("province", q.Province)
	}
	if q.Category > 0 {
		params.Set("category", strconv.Itoa(q.Category))
	}
	if q.Sort != "" {
		params.Set("sort", q.Sort)
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Next != "" {
		params.Set("next", q.Next)
	}

	var res SearchResult
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/search", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
