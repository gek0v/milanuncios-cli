package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"text/tabwriter"

	toon "github.com/toon-format/toon-go"
)

// Format defines stdout rendering format.
type Format string

const (
	JSON   Format = "json"
	JSONL  Format = "jsonl"
	Pretty Format = "pretty"
	Toon   Format = "toon"
)

// ParseFormat converts a raw string to a validated Format.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case JSON, JSONL, Pretty, Toon:
		return Format(s), nil
	}
	return "", Usagef("unknown format %q. Use json, jsonl, pretty or toon", s)
}

// Prettier can be implemented by domain results that provide human-friendly terminal formatting.
type Prettier interface {
	Pretty(w io.Writer, color bool)
}

// Printer handles output rendering to stdout.
type Printer struct {
	W      io.Writer
	Format Format
	Color  bool
}

// Print renders the given value according to the configured format.
func (p Printer) Print(v any) error {
	switch p.Format {
	case JSONL:
		if items, ok := asSlice(v); ok {
			enc := json.NewEncoder(p.W)
			for _, it := range items {
				if err := enc.Encode(it); err != nil {
					return err
				}
			}
			return nil
		}
		return json.NewEncoder(p.W).Encode(v)

	case Pretty:
		if pr, ok := v.(Prettier); ok {
			pr.Pretty(p.W, p.Color)
			return nil
		}
		enc := json.NewEncoder(p.W)
		enc.SetIndent("", "  ")
		return enc.Encode(v)

	case Toon:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var generic any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return err
		}
		out, err := toon.Marshal(generic)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(p.W, string(out))
		return err

	case JSON:
		fallthrough
	default:
		enc := json.NewEncoder(p.W)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
}

func asSlice(v any) ([]any, bool) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) == 0 || raw[0] != '[' {
		return nil, false
	}
	var items []any
	if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	return items, true
}

// Table outputs tab-aligned columns, bolding headers when color is enabled.
func Table(w io.Writer, color bool, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if len(header) > 0 {
		h := strings.Join(header, "\t")
		if color {
			h = "\x1b[1m" + h + "\x1b[0m"
		}
		fmt.Fprintln(tw, h)
	}
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// Truncate cuts string s to at most n runes, appending an ellipsis if truncated.
func Truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\t", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Dim wraps text in ANSI faint code if color is enabled.
func Dim(s string, color bool) string {
	if !color {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

var ansiRegex = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_]|)`)

// StripANSI removes ANSI escape sequences and raw escape control characters from string s.
func StripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	return ansiRegex.ReplaceAllString(s, "")
}

// ColorEnabled inspects NO_COLOR, TERM=dumb and TTY status.
func ColorEnabled(noColorFlag bool, f *os.File) bool {
	if noColorFlag || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(f)
}

// IsTerminal reports whether file f is an interactive character terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Error handling & exit codes

// ErrorFormat selects stderr rendering format.
type ErrorFormat string

const (
	ErrText ErrorFormat = "text"
	ErrJSON ErrorFormat = "json"
)

// Kind classifies domain errors to determine semantics and exit codes.
type Kind int

const (
	KindGeneric Kind = iota
	KindUsage
	KindAuth
	KindNotFound
	KindBlocked
	KindNetwork
	KindAPIChanged
	KindInterrupted
)

// ExitCode returns the POSIX exit code associated with the Kind.
func (k Kind) ExitCode() int {
	switch k {
	case KindGeneric:
		return 1
	case KindUsage:
		return 2
	case KindAuth:
		return 3
	case KindNotFound:
		return 4
	case KindBlocked:
		return 5
	case KindNetwork:
		return 6
	case KindAPIChanged:
		return 7
	case KindInterrupted:
		return 130
	default:
		return 1
	}
}

func (k Kind) String() string {
	switch k {
	case KindGeneric:
		return "generic"
	case KindUsage:
		return "usage"
	case KindAuth:
		return "auth"
	case KindNotFound:
		return "not_found"
	case KindBlocked:
		return "blocked"
	case KindNetwork:
		return "network"
	case KindAPIChanged:
		return "api_changed"
	case KindInterrupted:
		return "interrupted"
	default:
		return "generic"
	}
}

// DomainError represents a classified error in the application.
type DomainError struct {
	Kind      Kind
	Msg       string
	Endpoint  string
	Status    int
	Body      string
	Retryable bool
	APICode   int
}

func (e *DomainError) Error() string {
	return e.Msg
}

// IsKind reports whether err is or wraps a *DomainError of the specified Kind.
func IsKind(err error, kind Kind) bool {
	if err == nil {
		return false
	}
	var de *DomainError
	if errors.As(err, &de) {
		return de.Kind == kind
	}
	return false
}

// UsageError marks mistakes made in command flags or arguments.
type UsageError struct {
	Msg string
}

func (e *UsageError) Error() string {
	return e.Msg
}

// Usagef creates a formatted UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// NotFoundf creates a classified DomainError with KindNotFound.
func NotFoundf(format string, args ...any) error {
	return &DomainError{
		Kind: KindNotFound,
		Msg:  fmt.Sprintf(format, args...),
	}
}

// Authf creates a classified DomainError with KindAuth.
func Authf(format string, args ...any) error {
	return &DomainError{
		Kind: KindAuth,
		Msg:  fmt.Sprintf(format, args...),
	}
}

// Envelope is the machine-readable error JSON printed on stderr with --error-format json.
type Envelope struct {
	Code              int      `json:"code"`
	Category          string   `json:"category"`
	Retryable         bool     `json:"retryable"`
	Message           string   `json:"message"`
	HTTPStatus        int      `json:"http_status,omitempty"`
	Endpoint          string   `json:"endpoint,omitempty"`
	SuggestedCommands []string `json:"suggested_commands,omitempty"`
	IssueURL          string   `json:"issue_url,omitempty"`
}

const repoURL = "https://github.com/gek0v/milanuncios-cli"

// Classify maps any error into its structured Envelope.
func Classify(err error, version, command string) Envelope {
	env := Envelope{
		Code:      1,
		Category:  "generic",
		Retryable: false,
		Message:   err.Error(),
	}

	var de *DomainError
	if errors.As(err, &de) {
		env.Code = de.Kind.ExitCode()
		env.Category = de.Kind.String()
		env.Retryable = de.Retryable
		env.HTTPStatus = de.Status
		env.Endpoint = de.Endpoint
		switch de.Kind {
		case KindAuth:
			env.SuggestedCommands = []string{"milanuncios auth status", "milanuncios auth login"}
		case KindAPIChanged:
			env.IssueURL = IssueURL(version, command, de)
			env.SuggestedCommands = []string{"rerun with --debug"}
		case KindBlocked:
			env.SuggestedCommands = []string{"wait 15 minutes before retrying to clear cooldown"}
		}
	}

	var ue *UsageError
	if errors.As(err, &ue) {
		env.Code = 2
		env.Category = "usage"
	} else if de == nil {
		msg := err.Error()
		if strings.HasPrefix(msg, "unknown command") ||
			strings.HasPrefix(msg, "unknown flag") ||
			strings.HasPrefix(msg, "unknown shorthand flag") ||
			strings.HasPrefix(msg, "flag needs an argument") ||
			strings.HasPrefix(msg, "invalid argument") ||
			strings.Contains(msg, "required flag") ||
			strings.Contains(msg, "accepts ") ||
			strings.Contains(msg, "requires at least") ||
			strings.Contains(msg, "exact ") {
			env.Code = 2
			env.Category = "usage"
		}
	}

	return env
}

var (
	emailRegex        = regexp.MustCompile(`(?i)\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	jwtRegex          = regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.ey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+\b`)
	bearerRegex       = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-\.]+`)
	jsonSecretRegex   = regexp.MustCompile(`(?i)"(password|passwd|token|secret|session_token)"\s*:\s*"[^"]*"`)
	cookieRegex       = regexp.MustCompile(`(?i)\b(token|session|adevinta_session)=[^;\s&]+`)
	jsonPhoneRegex    = regexp.MustCompile(`(?i)"(phone|telephone|mobile|tlf|celular)"\s*:\s*"[^"]*"`)
	spanishPhoneRegex = regexp.MustCompile(`(?:\b|\+)(?:34[\s.-]*)?[6789]\d{2}[\s.-]?\d{3}[\s.-]?\d{3}\b`)
)

// SanitizePII removes emails, phone numbers, tokens, and credentials from error strings.
func SanitizePII(s string) string {
	s = jsonSecretRegex.ReplaceAllString(s, `"$1": "[REDACTED]"`)
	s = jsonPhoneRegex.ReplaceAllString(s, `"$1": "[REDACTED_PHONE]"`)
	s = emailRegex.ReplaceAllString(s, "[REDACTED_EMAIL]")
	s = jwtRegex.ReplaceAllString(s, "[REDACTED_TOKEN]")
	s = bearerRegex.ReplaceAllString(s, "Bearer [REDACTED_TOKEN]")
	s = cookieRegex.ReplaceAllString(s, "$1=[REDACTED]")
	s = spanishPhoneRegex.ReplaceAllString(s, "[REDACTED_PHONE]")
	return s
}

// IssueURL generates a prefilled GitHub issue URL when an API change (Exit 7) is detected.
func IssueURL(version, command string, e *DomainError) string {
	q := url.Values{}
	q.Set("template", "api-change.yml")
	q.Set("title", "[api change] "+strings.TrimSpace(e.Endpoint+" "+command))
	q.Set("version", version)
	q.Set("os", runtime.GOOS+"/"+runtime.GOARCH)
	q.Set("command", command)
	q.Set("endpoint", e.Endpoint)
	if e.Status != 0 {
		q.Set("status", fmt.Sprint(e.Status))
	}
	details := SanitizePII(e.Body)
	details = Truncate(details, 500)
	q.Set("details", details)
	return repoURL + "/issues/new?" + q.Encode()
}

// PrintError renders the error to stderr in text or JSON and returns the associated exit code.
func PrintError(w io.Writer, err error, format ErrorFormat, version, command string) int {
	env := Classify(err, version, command)
	if format == ErrJSON {
		_ = json.NewEncoder(w).Encode(env)
		return env.Code
	}

	fmt.Fprintf(w, "milanuncios: %s\n", env.Message)
	if env.Category == "api_changed" {
		fmt.Fprintf(w, "\nmilanuncios may have changed its web/api structure. Please report it:\n  %s\n", env.IssueURL)
		fmt.Fprintln(w, "rerun with --debug for full request details.")
	} else if len(env.SuggestedCommands) > 0 {
		fmt.Fprintf(w, "  try: %s\n", strings.Join(env.SuggestedCommands, "  |  "))
	}

	return env.Code
}
