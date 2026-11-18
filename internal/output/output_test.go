package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/output"
)

type sampleItem struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Price float64 `json:"price"`
}

func (s sampleItem) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "[PRETTY] %s: %s (%.2f €)\n", s.ID, s.Title, s.Price)
}

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	p := output.Printer{W: &buf, Format: output.JSON}

	item := sampleItem{ID: "123", Title: "Bicicleta", Price: 85.5}
	err := p.Print(item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid json produced: %v", err)
	}

	if parsed["id"] != "123" || parsed["title"] != "Bicicleta" || parsed["price"] != 85.5 {
		t.Fatalf("unexpected json content: %s", buf.String())
	}
}

func TestPrintJSONL(t *testing.T) {
	var buf bytes.Buffer
	p := output.Printer{W: &buf, Format: output.JSONL}

	items := []sampleItem{
		{ID: "1", Title: "Item 1", Price: 10},
		{ID: "2", Title: "Item 2", Price: 20},
	}
	err := p.Print(items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in jsonl, got %d", len(lines))
	}
}

func TestPrintPretty(t *testing.T) {
	var buf bytes.Buffer
	p := output.Printer{W: &buf, Format: output.Pretty}

	item := sampleItem{ID: "123", Title: "Bicicleta", Price: 85.5}
	err := p.Print(item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "[PRETTY] 123: Bicicleta (85.50 €)") {
		t.Fatalf("expected pretty output, got: %s", buf.String())
	}
}

func TestPrintToon(t *testing.T) {
	var buf bytes.Buffer
	p := output.Printer{W: &buf, Format: output.Toon}

	item := sampleItem{ID: "123", Title: "Bicicleta", Price: 85.5}
	err := p.Print(item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(buf.String()) == 0 {
		t.Fatal("expected non-empty toon output")
	}
}

func TestClassifyAndPrintError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		format       output.ErrorFormat
		expectedCode int
		expectedCat  string
	}{
		{
			name:         "generic error",
			err:          errors.New("something went wrong"),
			format:       output.ErrText,
			expectedCode: 1,
			expectedCat:  "generic",
		},
		{
			name:         "usage error",
			err:          output.Usagef("bad flag --foo"),
			format:       output.ErrJSON,
			expectedCode: 2,
			expectedCat:  "usage",
		},
		{
			name: "auth error",
			err: &output.DomainError{
				Kind: output.KindAuth,
				Msg:  "session expired",
			},
			format:       output.ErrText,
			expectedCode: 3,
			expectedCat:  "auth",
		},
		{
			name: "not found error",
			err: &output.DomainError{
				Kind: output.KindNotFound,
				Msg:  "ad not found",
			},
			format:       output.ErrJSON,
			expectedCode: 4,
			expectedCat:  "not_found",
		},
		{
			name: "blocked error",
			err: &output.DomainError{
				Kind: output.KindBlocked,
				Msg:  "rate limit reached",
			},
			format:       output.ErrText,
			expectedCode: 5,
			expectedCat:  "blocked",
		},
		{
			name: "api changed error",
			err: &output.DomainError{
				Kind:     output.KindAPIChanged,
				Msg:      "unexpected json shape",
				Endpoint: "GET /api/search",
			},
			format:       output.ErrJSON,
			expectedCode: 7,
			expectedCat:  "api_changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			code := output.PrintError(&buf, tt.err, tt.format, "1.0.0", "search")
			if code != tt.expectedCode {
				t.Fatalf("expected code %d, got %d", tt.expectedCode, code)
			}

			if tt.format == output.ErrJSON {
				var env output.Envelope
				if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
					t.Fatalf("failed to parse json envelope: %v", err)
				}
				if env.Category != tt.expectedCat {
					t.Fatalf("expected category %q, got %q", tt.expectedCat, env.Category)
				}
			}
		})
	}
}

func TestTableAndTruncate(t *testing.T) {
	var buf bytes.Buffer
	output.Table(&buf, false, []string{"COL1", "COL2"}, [][]string{
		{"val1", "val2"},
	})
	if !strings.Contains(buf.String(), "COL1") || !strings.Contains(buf.String(), "val1") {
		t.Fatalf("table missing contents: %s", buf.String())
	}

	truncated := output.Truncate("Hello World Long Text", 8)
	if len([]rune(truncated)) > 8 {
		t.Fatalf("expected truncated string length <= 8, got %q", truncated)
	}
}

func TestSanitizePII(t *testing.T) {
	raw := `Error at user juan.perez@example.es with phone +34 612 345 678 and mobile 712345678, token Bearer secret-token-123, jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NSJ9.sig, "password": "supersecretpassword", "phone": "912345678"`
	sanitized := output.SanitizePII(raw)

	if strings.Contains(sanitized, "juan.perez@example.es") {
		t.Errorf("email not sanitized: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[REDACTED_EMAIL]") {
		t.Errorf("missing [REDACTED_EMAIL] placeholder: %s", sanitized)
	}
	if strings.Contains(sanitized, "612 345 678") || strings.Contains(sanitized, "712345678") {
		t.Errorf("phone not sanitized: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[REDACTED_PHONE]") {
		t.Errorf("missing [REDACTED_PHONE] placeholder: %s", sanitized)
	}
	if strings.Contains(sanitized, "secret-token-123") {
		t.Errorf("bearer token not sanitized: %s", sanitized)
	}
	if strings.Contains(sanitized, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("jwt token not sanitized: %s", sanitized)
	}
	if strings.Contains(sanitized, "supersecretpassword") {
		t.Errorf("password not sanitized: %s", sanitized)
	}
}

func TestIssueURLPIISanitized(t *testing.T) {
	veryLongBody := strings.Repeat("A", 700) + " contact: test@corp.es phone: 612345678"
	err := &output.DomainError{
		Kind:     output.KindAPIChanged,
		Endpoint: "POST /api/v1/ads",
		Status:   200,
		Body:     veryLongBody,
	}

	rawURL := output.IssueURL("1.0.0", "ad create", err)
	if strings.Contains(rawURL, "test@corp.es") {
		t.Errorf("IssueURL contains raw email: %s", rawURL)
	}
	if strings.Contains(rawURL, "612345678") {
		t.Errorf("IssueURL contains raw phone: %s", rawURL)
	}

	// Verify length of details query param <= 500
	parsed, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		t.Fatalf("failed to parse IssueURL: %v", parseErr)
	}
	details := parsed.Query().Get("details")
	if len([]rune(details)) > 500 {
		t.Fatalf("expected details length <= 500, got %d", len([]rune(details)))
	}
}
