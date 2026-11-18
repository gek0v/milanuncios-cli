package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Clean string", "Clean string"},
		{"\x1b[31mRed text\x1b[0m", "Red text"},
		{"\x1b[1;32mBold Green\x1b[0m normal", "Bold Green normal"},
		{"\x1b]0;Evil Title\x07Malicious", "Malicious"},
		{"Trailing escape \x1b", "Trailing escape "},
		{"Title with \x1b[2Jclear screen code", "Title with clear screen code"},
	}

	for _, tt := range tests {
		got := output.StripANSI(tt.input)
		if got != tt.expected {
			t.Errorf("StripANSI(%q) = %q; expected %q", tt.input, got, tt.expected)
		}
	}
}
