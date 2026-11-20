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
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/output"
	"golang.org/x/net/proxy"
)

func browserUA() string {
	return browserUAForOS(runtime.GOOS)
}

func browserUAForOS(goos string) string {
	switch goos {
	case "darwin":
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	case "linux":
		return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	default:
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	}
}

func browserPlatformForOS(goos string) string {
	switch goos {
	case "darwin":
		return `"macOS"`
	case "linux":
		return `"Linux"`
	default:
		return `"Windows"`
	}
}

func (c *Client) setBrowserHeaders(h http.Header) {
	h.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	h.Set("Sec-Ch-Ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	h.Set("Sec-Ch-Ua-Mobile", "?0")
	h.Set("Sec-Ch-Ua-Platform", browserPlatformForOS(runtime.GOOS))
	h.Set("Sec-Fetch-Site", "same-origin")
	h.Set("Sec-Fetch-Mode", "cors")
	h.Set("Sec-Fetch-Dest", "empty")
}

type contextKey string

const clientRequestIDKey contextKey = "client_request_id"

// WithClientRequestID attaches a client_request_id to the context for outgoing requests.
func WithClientRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, clientRequestIDKey, id)
}

// Client interacts with the Milanuncios web endpoints and private API.
type Client struct {
	BaseURL         string
	UserAgent       string
	SessionToken    string
	Cookies         map[string]string
	HTTP            *http.Client
	Proxy           string
	Debug           io.Writer
	Notice          func(string)
	OnBlock         func(until time.Time)
	AllowPrivateIPs bool

	redactStrings []string
	uaMu          sync.Mutex
	useBrowserUA  bool

	cookiesMu    sync.RWMutex
	blockMu      sync.RWMutex
	blockedUntil *time.Time
}

// NewClient creates an initialized Client.
func NewClient(version string) *Client {
	if version == "" {
		version = "dev"
	}
	return &Client{
		BaseURL:   "https://www.milanuncios.com",
		UserAgent: fmt.Sprintf("milanuncios-cli/%s (+https://github.com/gek0v/milanuncios-cli)", version),
		Cookies:   make(map[string]string),
		HTTP: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// SetSession configures the active bearer/cookie session token and redacts it from logs.
func (c *Client) SetSession(token string) {
	c.SessionToken = token
	c.Redact(token)
}

// SetCookies configures additional cookies (such as WAF/anti-bot cookies) sent with requests.
func (c *Client) SetCookies(cookies map[string]string) {
	c.cookiesMu.Lock()
	defer c.cookiesMu.Unlock()
	if c.Cookies == nil {
		c.Cookies = make(map[string]string)
	}
	for k, v := range cookies {
		c.Cookies[k] = v
		c.Redact(v)
	}
}

// SetCookie adds or updates a single cookie and redacts its value.
func (c *Client) SetCookie(name, value string) {
	c.cookiesMu.Lock()
	defer c.cookiesMu.Unlock()
	if c.Cookies == nil {
		c.Cookies = make(map[string]string)
	}
	c.Cookies[name] = value
	c.Redact(value)
}

// GetCookies returns a snapshot of the configured cookies.
func (c *Client) GetCookies() map[string]string {
	c.cookiesMu.RLock()
	defer c.cookiesMu.RUnlock()
	copied := make(map[string]string, len(c.Cookies))
	for k, v := range c.Cookies {
		copied[k] = v
	}
	return copied
}

func (c *Client) cookieHeader(auth bool) string {
	var parts []string
	if c.SessionToken != "" {
		parts = append(parts, "token="+c.SessionToken)
	}
	c.cookiesMu.RLock()
	for k, v := range c.Cookies {
		lower := strings.ToLower(k)
		if c.SessionToken != "" && (lower == "token" || lower == "session") {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	c.cookiesMu.RUnlock()
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// SetBlockedUntil sets a cooldown expiration time for the client.
func (c *Client) SetBlockedUntil(until time.Time) {
	c.blockMu.Lock()
	defer c.blockMu.Unlock()
	c.blockedUntil = &until
}

// BlockedUntil returns the cooldown expiration time if active, or nil.
func (c *Client) BlockedUntil() *time.Time {
	c.blockMu.RLock()
	defer c.blockMu.RUnlock()
	if c.blockedUntil == nil {
		return nil
	}
	t := *c.blockedUntil
	return &t
}

// IsBlocked returns true if the client is currently in a cooldown period.
func (c *Client) IsBlocked() bool {
	c.blockMu.RLock()
	defer c.blockMu.RUnlock()
	if c.blockedUntil == nil {
		return false
	}
	return time.Now().Before(*c.blockedUntil)
}

// ClearBlock clears any active cooldown.
func (c *Client) ClearBlock() {
	c.blockMu.Lock()
	defer c.blockMu.Unlock()
	c.blockedUntil = nil
}

func parseCooldown(resp *http.Response) time.Duration {
	defaultCooldown := 15 * time.Minute
	if resp == nil {
		return defaultCooldown
	}
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return defaultCooldown
	}
	if sec, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(retryAfter); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return defaultCooldown
}

func isWAFCookie(name string) bool {
	lower := strings.ToLower(name)
	return lower == "datadome" ||
		lower == "cf_clearance" ||
		lower == "__cf_bm" ||
		lower == "_cfuvid" ||
		lower == "bm_sz" ||
		lower == "_abck" ||
		lower == "ak_bmsc" ||
		strings.HasPrefix(lower, "cf_") ||
		strings.HasPrefix(lower, "_cf") ||
		strings.HasPrefix(lower, "__cf") ||
		strings.Contains(lower, "datadome")
}

func isSessionCookieName(lower string) bool {
	return lower == "token" || lower == "session" || lower == "adevinta_session" || lower == "jwt" || lower == "session_token"
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

// RedactString sanitizes registered secrets from any string.
func (c *Client) RedactString(s string) string {
	return c.redact(s)
}

// UseProxy configures an HTTP/SOCKS5 proxy for all requests without direct fallback.
func (c *Client) UseProxy(u *url.URL) {
	if u.User != nil {
		if p, ok := u.User.Password(); ok && p != "" {
			c.Redact(p)
			queryEscaped := url.QueryEscape(p)
			c.Redact(queryEscaped)
			c.Redact(strings.ReplaceAll(queryEscaped, "+", "%20"))
			c.Redact(url.PathEscape(p))
			userInfo := u.User.String()
			if idx := strings.Index(userInfo, ":"); idx != -1 {
				rawPass := userInfo[idx+1:]
				if rawPass != "" {
					c.Redact(rawPass)
				}
			}
		}
	}
	c.Proxy = u.Redacted()

	switch u.Scheme {
	case "socks5", "socks5h":
		baseDialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		dialer, err := proxy.FromURL(u, baseDialer)
		if err != nil {
			c.HTTP.Transport = &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return nil, err
				},
			}
			return
		}
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			c.HTTP.Transport = &http.Transport{
				DialContext: cd.DialContext,
			}
		} else {
			c.HTTP.Transport = &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				},
			}
		}
	default:
		transport := &http.Transport{
			Proxy: http.ProxyURL(u),
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}
		c.HTTP.Transport = transport
	}
}

func (c *Client) currentUA() string {
	c.uaMu.Lock()
	defer c.uaMu.Unlock()
	if c.useBrowserUA {
		return browserUA()
	}
	return c.UserAgent
}

func (c *Client) setBrowserUA() {
	c.uaMu.Lock()
	c.useBrowserUA = true
	c.uaMu.Unlock()
}

func (c *Client) isBrowserUA() bool {
	c.uaMu.Lock()
	defer c.uaMu.Unlock()
	return c.useBrowserUA
}

func isWAFBlock(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "cloudflare") ||
		strings.Contains(lower, "akamai") ||
		strings.Contains(lower, "datadome") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "attention required")
}

func isWAFChallenge(contentType string, body []byte) bool {
	lowerCT := strings.ToLower(contentType)
	lowerBody := strings.ToLower(string(body))

	hasChallengeSig := strings.Contains(lowerBody, "captcha") ||
		strings.Contains(lowerBody, "challenge") ||
		strings.Contains(lowerBody, "datadome") ||
		strings.Contains(lowerBody, "turnstile") ||
		strings.Contains(lowerBody, "cloudflare") ||
		strings.Contains(lowerBody, "akamai") ||
		strings.Contains(lowerBody, "access denied") ||
		strings.Contains(lowerBody, "attention required")

	isHTML := strings.Contains(lowerCT, "text/html") ||
		strings.Contains(lowerCT, "application/xhtml+xml") ||
		strings.Contains(lowerBody, "<!doctype html") ||
		strings.Contains(lowerBody, "<html")

	if isHTML {
		if hasChallengeSig || strings.Contains(lowerCT, "text/html") {
			return true
		}
	}

	return hasChallengeSig && strings.HasPrefix(strings.TrimSpace(lowerBody), "<")
}

func isBotStatus(status int, body []byte) bool {
	if status == http.StatusTooManyRequests || status == http.StatusForbidden {
		return true
	}
	return isWAFChallenge("", body)
}

func (c *Client) debugf(format string, args ...any) {
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, format+"\n", args...)
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values, body any, auth bool, out any) error {
	endpoint := method + " " + path

	if c.IsBlocked() {
		until := c.BlockedUntil()
		msg := "request paused: milanuncios circuit breaker is active"
		if until != nil {
			msg = fmt.Sprintf("request paused: milanuncios circuit breaker is active until %s", until.Local().Format("15:04:05"))
		}
		return &output.DomainError{
			Kind:      output.KindBlocked,
			Status:    http.StatusTooManyRequests,
			Endpoint:  endpoint,
			Retryable: false,
			Msg:       msg,
		}
	}

	if auth && c.SessionToken == "" {
		return &output.DomainError{
			Kind:     output.KindAuth,
			Endpoint: endpoint,
			Msg:      "this command requires an authenticated profile. Run `milanuncios auth login`",
		}
	}

	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
	}

	buildReq := func() (*http.Request, error) {
		var bodyReader io.Reader
		if len(bodyBytes) > 0 {
			bodyReader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.currentUA())
		req.Header.Set("Accept", "application/json")
		c.setBrowserHeaders(req.Header)
		if len(bodyBytes) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		if auth && c.SessionToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.SessionToken)
		}
		if ck := c.cookieHeader(auth); ck != "" {
			req.Header.Set("Cookie", ck)
		}
		if reqID, ok := ctx.Value(clientRequestIDKey).(string); ok && reqID != "" {
			req.Header.Set("X-Client-Request-Id", reqID)
		}
		return req, nil
	}

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

	for _, ck := range resp.Cookies() {
		if isWAFCookie(ck.Name) {
			c.SetCookie(ck.Name, ck.Value)
		}
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return c.wrapNetError(endpoint, err)
	}
	c.debugf("< %d %s (%d bytes)", resp.StatusCode, endpoint, len(raw))

	// If WAF blocks honest UA with 403 HTML or challenge page, retry once with realistic browser UA
	if (resp.StatusCode == http.StatusForbidden && isWAFBlock(raw) || isWAFChallenge(resp.Header.Get("Content-Type"), raw)) && !c.isBrowserUA() {
		c.setBrowserUA()
		if c.Notice != nil {
			c.Notice("milanuncios rejected cli user agent; retrying with browser user agent")
		}

		req, err = buildReq()
		if err != nil {
			return err
		}
		resp.Body.Close()
		resp, err = c.HTTP.Do(req)
		if err != nil {
			return c.wrapNetError(endpoint, err)
		}
		defer resp.Body.Close()
		for _, ck := range resp.Cookies() {
			if isWAFCookie(ck.Name) {
				c.SetCookie(ck.Name, ck.Value)
			}
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return c.wrapNetError(endpoint, err)
		}
		c.debugf("< %d %s (%d bytes, browser UA)", resp.StatusCode, endpoint, len(raw))
	}

	if isBotStatus(resp.StatusCode, raw) || isWAFChallenge(resp.Header.Get("Content-Type"), raw) {
		cooldown := parseCooldown(resp)
		until := time.Now().Add(cooldown)
		c.SetBlockedUntil(until)
		if c.OnBlock != nil {
			c.OnBlock(until)
		}
		return &output.DomainError{
			Kind:      output.KindBlocked,
			Status:    resp.StatusCode,
			Endpoint:  endpoint,
			Body:      c.snippet(endpoint, raw),
			Retryable: false,
			Msg:       fmt.Sprintf("milanuncios blocked or rate-limited the request. Wait until %s before retrying", until.Local().Format("15:04:05")),
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return &output.DomainError{
			Kind:     output.KindAuth,
			Status:   resp.StatusCode,
			Endpoint: endpoint,
			Msg:      "milanuncios rejected session token. Run `milanuncios auth login` again",
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
			Body:     c.snippet(endpoint, raw),
			Msg:      fmt.Sprintf("unexpected HTTP %d from milanuncios", resp.StatusCode),
		}
	}

	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			if isWAFChallenge(resp.Header.Get("Content-Type"), raw) {
				cooldown := parseCooldown(resp)
				until := time.Now().Add(cooldown)
				c.SetBlockedUntil(until)
				if c.OnBlock != nil {
					c.OnBlock(until)
				}
				return &output.DomainError{
					Kind:      output.KindBlocked,
					Status:    resp.StatusCode,
					Endpoint:  endpoint,
					Body:      c.snippet(endpoint, raw),
					Retryable: false,
					Msg:       fmt.Sprintf("milanuncios blocked or rate-limited the request. Wait until %s before retrying", until.Local().Format("15:04:05")),
				}
			}
			return &output.DomainError{
				Kind:     output.KindAPIChanged,
				Status:   resp.StatusCode,
				Endpoint: endpoint,
				Body:     "json decode: " + err.Error() + "; body: " + c.snippet(endpoint, raw),
				Msg:      "milanuncios returned an unexpected response structure",
			}
		}
	}

	return nil
}

func (c *Client) snippet(endpoint string, raw []byte) string {
	if strings.Contains(endpoint, "/me") || strings.Contains(endpoint, "/conversations") || strings.Contains(endpoint, "/messages") {
		return "[redacted authenticated payload]"
	}
	s := strings.TrimSpace(string(raw))
	s = output.Truncate(s, 300)
	s = output.SanitizePII(s)
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
	for k, v := range q.Filters {
		if k != "" && v != "" {
			params.Set("filter["+k+"]", v)
		}
	}

	var res SearchResult
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/search", params, nil, false, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListCategories retrieves the category taxonomy tree.
func (c *Client) ListCategories(ctx context.Context) (CategoryListResult, error) {
	var cats CategoryListResult
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/categories", nil, nil, false, &cats); err != nil {
		return nil, err
	}
	return cats, nil
}

// GetCategoryFilters returns dynamic filter definitions available for a given category.
func (c *Client) GetCategoryFilters(ctx context.Context, categoryID int) (*CategoryFiltersResult, error) {
	if categoryID <= 0 {
		return nil, output.Usagef("category ID must be greater than 0")
	}

	path := fmt.Sprintf("/api/v1/categories/%d/filters", categoryID)
	var res CategoryFiltersResult
	if err := c.doRequest(ctx, http.MethodGet, path, nil, nil, false, &res); err != nil {
		return nil, err
	}
	if res.CategoryID == 0 {
		res.CategoryID = categoryID
	}
	return &res, nil
}

var (
	adURLHtmRegex  = regexp.MustCompile(`[-/rR]([0-9]{1,15})\.htm(?:[?#]|$)`)
	adURLRegex     = regexp.MustCompile(`[-/rR]([0-9]{1,15})/?(?:[?#]|$)`)
	adNumericRegex = regexp.MustCompile(`^[rR]?([0-9]{1,15})$`)
)

// ExtractAdID parses an ad ID from either a numeric string or full Milanuncios URL.
func ExtractAdID(idOrURL string) (string, error) {
	trimmed := strings.TrimSpace(idOrURL)
	if trimmed == "" {
		return "", output.Usagef("ad ID or URL cannot be empty")
	}

	if m := adNumericRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		return m[1], nil
	}

	if m := adURLHtmRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		return m[1], nil
	}

	matches := adURLRegex.FindAllStringSubmatch(trimmed, -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1]
		if len(last) > 1 {
			return last[1], nil
		}
	}

	return "", output.Usagef("could not extract ad ID from %q (expected numeric ID or Milanuncios URL)", idOrURL)
}

// GetAd fetches full details of a listing by its ID or web URL.
func (c *Client) GetAd(ctx context.Context, idOrURL string) (*AdItem, error) {
	id, err := ExtractAdID(idOrURL)
	if err != nil {
		return nil, err
	}

	var item AdItem
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/ads/"+id, nil, nil, false, &item); err != nil {
		return nil, err
	}

	if item.ID == "" {
		item.ID = id
	}
	if item.URL == "" {
		item.URL = fmt.Sprintf("https://www.milanuncios.com/anuncios/%s.htm", id)
	}

	return &item, nil
}

// Me retrieves the authenticated user's account profile.
func (c *Client) Me(ctx context.Context) (*UserProfile, error) {
	var profile UserProfile
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/me", nil, nil, true, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

// MyAds returns listings published by the authenticated user.
func (c *Client) MyAds(ctx context.Context, sold bool) ([]AdItem, error) {
	params := url.Values{}
	if sold {
		params.Set("status", "sold")
	}
	var res []AdItem
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/me/ads", params, nil, true, &res); err != nil {
		return nil, err
	}
	return res, nil
}

// MyFavorites returns listings marked as favorites by the user.
func (c *Client) MyFavorites(ctx context.Context) ([]AdItem, error) {
	var res []AdItem
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/me/favorites", nil, nil, true, &res); err != nil {
		return nil, err
	}
	return res, nil
}

// Favorite marks an ad as favorite.
func (c *Client) Favorite(ctx context.Context, adID string) error {
	id, err := ExtractAdID(adID)
	if err != nil {
		return err
	}
	return c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/ads/%s/favorite", id), nil, nil, true, nil)
}

// Unfavorite removes an ad from favorites.
func (c *Client) Unfavorite(ctx context.Context, adID string) error {
	id, err := ExtractAdID(adID)
	if err != nil {
		return err
	}
	return c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/ads/%s/favorite", id), nil, nil, true, nil)
}

// ListConversations retrieves the user's active chat conversations.
func (c *Client) ListConversations(ctx context.Context) (ConversationList, error) {
	var list ConversationList
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/conversations", nil, nil, true, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ListChats is an alias for ListConversations for interactive chat management.
func (c *Client) ListChats(ctx context.Context) (ConversationList, error) {
	return c.ListConversations(ctx)
}

// GetConversation retrieves the message history for a specific conversation ID.
func (c *Client) GetConversation(ctx context.Context, id string) (*ConversationDetails, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return nil, output.Usagef("conversation ID cannot be empty")
	}
	var details ConversationDetails
	if err := c.doRequest(ctx, http.MethodGet, "/api/v1/conversations/"+trimmed, nil, nil, true, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

// GetChat is an alias for GetConversation for interactive chat management.
func (c *Client) GetChat(ctx context.Context, id string) (*ConversationDetails, error) {
	return c.GetConversation(ctx, id)
}

// SendMessage sends a text message to a conversation thread.
func (c *Client) SendMessage(ctx context.Context, convID, text string) (*Message, error) {
	trimmedID := strings.TrimSpace(convID)
	trimmedText := strings.TrimSpace(text)
	if trimmedID == "" {
		return nil, output.Usagef("conversation ID cannot be empty")
	}
	if trimmedText == "" {
		return nil, output.Usagef("message text cannot be empty")
	}

	payload := map[string]string{"text": trimmedText}
	var msg Message
	if err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/conversations/%s/messages", trimmedID), nil, payload, true, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// StartConversation initiates a new contact message for an ad listing.
func (c *Client) StartConversation(ctx context.Context, adID, text string, clientRequestID ...string) (*ConversationStartResult, error) {
	id, err := ExtractAdID(adID)
	if err != nil {
		return nil, err
	}

	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, output.Usagef("message text cannot be empty")
	}

	payload := map[string]string{
		"ad_id": id,
		"text":  trimmedText,
	}

	if len(clientRequestID) > 0 && strings.TrimSpace(clientRequestID[0]) != "" {
		reqID := strings.TrimSpace(clientRequestID[0])
		payload["client_request_id"] = reqID
		ctx = WithClientRequestID(ctx, reqID)
	}

	var res ConversationStartResult
	endpoint := fmt.Sprintf("/api/v1/ads/%s/contact", id)
	if err := c.doRequest(ctx, http.MethodPost, endpoint, nil, payload, true, &res); err != nil {
		var de *output.DomainError
		if errors.As(err, &de) {
			if de.Status == http.StatusNotFound || de.Kind == output.KindNotFound {
				return nil, output.NotFoundf("ad not found: %s", id)
			}
			if de.Status == http.StatusBadRequest || de.Status == http.StatusUnprocessableEntity {
				lower := strings.ToLower(de.Body + " " + de.Msg)
				if strings.Contains(lower, "own") || strings.Contains(lower, "propio") ||
					strings.Contains(lower, "same user") || strings.Contains(lower, "same_user") {
					return nil, output.Usagef("cannot start conversation on your own ad")
				}
			}
		}
		return nil, err
	}

	if res.AdID == "" {
		res.AdID = id
	}
	if res.Text == "" {
		res.Text = trimmedText
	}

	return &res, nil
}

// ParseCookies extracts the session token and WAF mitigation cookies (datadome, cf_clearance, etc.)
// from browser export formats: JSON (Cookie-Editor, EditThisCookie), Netscape/curl cookies.txt, or raw Cookie header.
func ParseCookies(raw string) (string, map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, output.Usagef("cookies input is empty")
	}

	wafCookies := make(map[string]string)

	// 1. Try parsing JSON format (Cookie-Editor / EditThisCookie)
	if strings.HasPrefix(trimmed, "{") {
		var single struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(trimmed), &single); err == nil && single.Value != "" {
			val := strings.Trim(strings.TrimSpace(single.Value), "\"")
			if isSessionCookieName(strings.ToLower(single.Name)) || single.Name == "" {
				return val, wafCookies, nil
			}
		}
	}

	if strings.HasPrefix(trimmed, "[") {
		var cookies []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(trimmed), &cookies); err == nil && len(cookies) > 0 {
			var token string
			for _, ck := range cookies {
				val := strings.Trim(strings.TrimSpace(ck.Value), "\"")
				if val == "" {
					continue
				}
				lower := strings.ToLower(ck.Name)
				if isSessionCookieName(lower) {
					if token == "" {
						token = val
					}
				} else if isWAFCookie(ck.Name) {
					wafCookies[ck.Name] = val
				}
			}
			if token != "" {
				return token, wafCookies, nil
			}
		}
	}

	// 2. Try parsing Netscape cookies.txt lines
	lines := strings.Split(trimmed, "\n")
	var tokenFromNetscape string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 7 {
			name := parts[5]
			val := strings.Trim(strings.TrimSpace(parts[6]), "\"")
			if val == "" {
				continue
			}
			lower := strings.ToLower(name)
			if isSessionCookieName(lower) {
				if tokenFromNetscape == "" {
					tokenFromNetscape = val
				}
			} else if isWAFCookie(name) {
				wafCookies[name] = val
			}
		}
	}
	if tokenFromNetscape != "" {
		return tokenFromNetscape, wafCookies, nil
	}

	// 3. Try parsing standard HTTP Cookie: name=val; name2=val2
	var tokenFromHeader string
	for _, part := range strings.Split(trimmed, ";") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			name := strings.TrimSpace(kv[0])
			val := strings.Trim(strings.TrimSpace(kv[1]), "\"")
			if val == "" {
				continue
			}
			lower := strings.ToLower(name)
			if isSessionCookieName(lower) {
				if tokenFromHeader == "" {
					tokenFromHeader = val
				}
			} else if isWAFCookie(name) {
				wafCookies[name] = val
			}
		}
	}
	if tokenFromHeader != "" {
		return tokenFromHeader, wafCookies, nil
	}

	// 4. Fallback: if raw is a pure token string without spaces/semicolons
	if !strings.ContainsAny(trimmed, " \t\r\n;") && len(trimmed) > 10 {
		return strings.Trim(trimmed, "\""), wafCookies, nil
	}

	return "", nil, output.Usagef("could not find session cookie (token/session/adevinta_session) in provided data")
}
