package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/cli"
	"github.com/gek0v/milanuncios-cli/internal/eval"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func setupEvalMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/ads/492817294":
			ad := milanuncios.AdItem{
				ID:         "492817294",
				Title:      "Cannondale Topstone 1 Gravel (Talla M)",
				Price:      950,
				Currency:   "EUR",
				CategoryID: 1102,
				URL:        "https://www.milanuncios.com/bicicletas/cannondale-topstone-1-492817294.htm",
			}
			_ = json.NewEncoder(w).Encode(ad)

		case strings.HasPrefix(r.URL.Path, "/api/v1/search"):
			q := r.URL.Query().Get("s")
			if q == "" {
				q = r.URL.Query().Get("keywords")
			}
			if strings.Contains(strings.ToLower(q), "macbook") {
				res := milanuncios.SearchResult{
					Items: []milanuncios.AdItem{
						{ID: "m1", Title: "Macbook Air M1 256GB - Chollo", Price: 400},
						{ID: "m2", Title: "Macbook Air M1 Buen Estado", Price: 650},
						{ID: "m3", Title: "Macbook Air M1 Gris Espacial", Price: 700},
						{ID: "m4", Title: "Macbook Air M1 Impecable", Price: 750},
						{ID: "m5", Title: "Macbook Air M1 Caro", Price: 900},
					},
					Total: 5,
				}
				_ = json.NewEncoder(w).Encode(res)
				return
			}

			// Default comparable search for cannondale topstone
			res := milanuncios.SearchResult{
				Items: []milanuncios.AdItem{
					{ID: "comp-1", Title: "Cannondale Topstone 1 Gravel", Price: 1100, URL: "https://www.milanuncios.com/bicicletas/c1.htm"},
					{ID: "comp-2", Title: "Cannondale Topstone 2 Shimano", Price: 1150, URL: "https://www.milanuncios.com/bicicletas/c2.htm"},
					{ID: "comp-3", Title: "Cannondale Topstone 1 Talla L", Price: 1200, URL: "https://www.milanuncios.com/bicicletas/c3.htm"},
					{ID: "comp-4", Title: "Cannondale Topstone Carbon", Price: 1250, URL: "https://www.milanuncios.com/bicicletas/c4.htm"},
					{ID: "comp-5", Title: "Cannondale Topstone 3", Price: 1300, URL: "https://www.milanuncios.com/bicicletas/c5.htm"},
				},
				Total: 5,
			}
			_ = json.NewEncoder(w).Encode(res)

		case r.URL.Path == "/api/v1/ads/999999999":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not found"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not found"}`))
		}
	}))
}

func TestEvaluateCmd_Formats(t *testing.T) {
	ts := setupEvalMockServer(t)
	defer ts.Close()

	tmpDir := t.TempDir()
	t.Setenv("MILANUNCIOS_CONFIG", filepath.Join(tmpDir, "config.toml"))
	t.Setenv("MILANUNCIOS_STATE_DB", filepath.Join(tmpDir, "state.db"))

	// 1. JSON format
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"evaluate", "492817294", "--format", "json"})
		if code != 0 {
			t.Fatalf("milanuncios evaluate failed with code %d, stderr: %s", code, stderr.String())
		}

		var report eval.EvaluationReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("failed to parse evaluation JSON output: %v, raw: %s", err, stdout.String())
		}

		if report.AdID != "492817294" {
			t.Errorf("expected ad_id 492817294, got %s", report.AdID)
		}
		if report.DealScore <= 0 || report.DealScore > 100 {
			t.Errorf("expected deal_score between 1 and 100, got %d", report.DealScore)
		}
		if report.Rating == "" {
			t.Errorf("expected non-empty rating")
		}
		if report.MarketMedian != 1200 {
			t.Errorf("expected market median 1200, got %f", report.MarketMedian)
		}
		if report.DiscountPercent <= 0 {
			t.Errorf("expected positive discount percent for bargain, got %f", report.DiscountPercent)
		}
	}

	// 2. Pretty format
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"evaluate", "492817294", "--format", "pretty", "--no-color"})
		if code != 0 {
			t.Fatalf("milanuncios evaluate pretty failed with code %d, stderr: %s", code, stderr.String())
		}
		outStr := stdout.String()
		if !strings.Contains(outStr, "EVALUACIÓN DE PRECIO & DEAL SCORE") {
			t.Errorf("pretty output missing banner, got: %s", outStr)
		}
		if !strings.Contains(outStr, "Deal Score:") {
			t.Errorf("pretty output missing Deal Score, got: %s", outStr)
		}
		if !strings.Contains(outStr, "BENCHMARK DE MERCADO:") {
			t.Errorf("pretty output missing BENCHMARK DE MERCADO, got: %s", outStr)
		}
		if !strings.Contains(outStr, "Cannondale Topstone") {
			t.Errorf("pretty output missing title, got: %s", outStr)
		}
	}

	// 3. Toon format
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"evaluate", "492817294", "--format", "toon"})
		if code != 0 {
			t.Fatalf("milanuncios evaluate toon failed with code %d, stderr: %s", code, stderr.String())
		}
		outStr := stdout.String()
		if !strings.Contains(outStr, "ad_id") {
			t.Errorf("toon output missing ad_id, got: %s", outStr)
		}
	}

	// 4. Include comparables
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"evaluate", "492817294", "--include-comparables", "--format", "json"})
		if code != 0 {
			t.Fatalf("milanuncios evaluate --include-comparables failed with code %d, stderr: %s", code, stderr.String())
		}
		var report eval.EvaluationReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("failed to parse evaluation JSON output: %v", err)
		}
		if len(report.Comparables) == 0 {
			t.Errorf("expected comparables in report, got none")
		}
	}

	// 5. Root alias: eval
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"eval", "492817294", "--format", "json"})
		if code != 0 {
			t.Fatalf("milanuncios eval failed with code %d, stderr: %s", code, stderr.String())
		}
	}

	// 6. Subcommand: ad evaluate
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"ad", "evaluate", "492817294", "--format", "json"})
		if code != 0 {
			t.Fatalf("milanuncios ad evaluate failed with code %d, stderr: %s", code, stderr.String())
		}
	}

	// 7. Subcommand alias: ad eval
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"ad", "eval", "492817294", "--format", "json"})
		if code != 0 {
			t.Fatalf("milanuncios ad eval failed with code %d, stderr: %s", code, stderr.String())
		}
	}
}

func TestEvaluateCmd_MissingArg(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	// Missing arg on evaluate
	code := execApp(app, []string{"evaluate"})
	if code != 2 {
		t.Fatalf("expected exit code 2 on missing arg, got %d (stderr: %s)", code, stderr.String())
	}

	// Missing arg on ad evaluate
	stderr.Reset()
	code = execApp(app, []string{"ad", "evaluate"})
	if code != 2 {
		t.Fatalf("expected exit code 2 on missing arg for ad evaluate, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestEvaluateCmd_NotFound(t *testing.T) {
	ts := setupEvalMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"evaluate", "999999999", "--format", "json"})
	if code != 4 {
		t.Fatalf("expected exit code 4 on not found ad, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestSearch_MinDealScore(t *testing.T) {
	ts := setupEvalMockServer(t)
	defer ts.Close()

	tmpDir := t.TempDir()
	t.Setenv("MILANUNCIOS_CONFIG", filepath.Join(tmpDir, "config.toml"))

	// Valid filtering: min-deal-score 75
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"search", "macbook air m1", "--min-deal-score", "75", "--format", "json"})
		if code != 0 {
			t.Fatalf("search with --min-deal-score failed with code %d, stderr: %s", code, stderr.String())
		}

		var res milanuncios.SearchResult
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			t.Fatalf("failed to parse search result JSON: %v", err)
		}

		// Median of [400, 650, 700, 750, 900] is 700.
		// For price 400: delta = (700-400)/700 = 0.428 (>= 0.35) -> score = 65 + 35*0.5 = 83 (>= 75).
		// For price 650: delta = (700-650)/700 = 0.071 -> score = 65 + 35*(0.071/0.35)*0.5 = 69 (< 75).
		// For price 700: score = 65 (< 75).
		// So only the 400€ item should survive min-deal-score 75.
		if len(res.Items) != 1 {
			t.Fatalf("expected exactly 1 item to survive min-deal-score 75, got %d", len(res.Items))
		}
		if res.Items[0].Price != 400 {
			t.Errorf("expected surviving item to be 400 EUR, got %f", res.Items[0].Price)
		}
		if res.Total != 1 {
			t.Errorf("expected res.Total to be 1, got %d", res.Total)
		}
	}

	// Validation: min-deal-score > 100 -> Usage error (exit code 2)
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"search", "macbook air m1", "--min-deal-score", "105"})
		if code != 2 {
			t.Fatalf("expected exit code 2 for min-deal-score 105, got %d", code)
		}
	}

	// Validation: min-deal-score < 0 -> Usage error (exit code 2)
	{
		app := cli.NewApp("test")
		app.Client.BaseURL = ts.URL
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr

		code := execApp(app, []string{"search", "macbook air m1", "--min-deal-score", "-5"})
		if code != 2 {
			t.Fatalf("expected exit code 2 for min-deal-score -5, got %d", code)
		}
	}
}
