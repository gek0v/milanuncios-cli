package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/cli"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func setupAdMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/categories/11/filters":
			res := milanuncios.CategoryFiltersResult{
				CategoryID:   11,
				CategoryName: "Coches de segunda mano",
				Filters: []milanuncios.FilterDefinition{
					{
						Key:     "fuel",
						Label:   "Combustible",
						Type:    "select",
						Options: []string{"diesel", "gasolina", "electrico", "hibrido", "hibrido_enchufable", "gas"},
					},
					{
						Key:     "transmission",
						Label:   "Cambio",
						Type:    "select",
						Options: []string{"manual", "automatico"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(res)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not found"}`))
		}
	}))
}

func execApp(app *cli.App, args []string) int {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = append([]string{"milanuncios"}, args...)
	return app.Execute()
}

func TestAdSchemaSuccess(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "schema", "--category", "11", "--format", "json"})
	if code != 0 {
		t.Fatalf("ad schema failed with code %d, stderr: %s", code, stderr.String())
	}

	var schema milanuncios.CategoryJSONSchema
	if err := json.Unmarshal(stdout.Bytes(), &schema); err != nil {
		t.Fatalf("failed to parse schema JSON: %v, raw: %s", err, stdout.String())
	}

	if schema.CategoryID != 11 {
		t.Errorf("expected category 11, got %d", schema.CategoryID)
	}
	if schema.Schema != "http://json-schema.org/draft-07/schema#" {
		t.Errorf("expected draft-07 schema, got %s", schema.Schema)
	}
}

func TestAdSchemaPretty(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "schema", "-c", "11", "--format", "pretty"})
	if code != 0 {
		t.Fatalf("ad schema pretty failed with code %d: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Coches de segunda mano") {
		t.Errorf("expected category name in pretty output, got:\n%s", out)
	}
	if !strings.Contains(out, "fuel") {
		t.Errorf("expected fuel attribute in pretty output, got:\n%s", out)
	}
}

func TestAdSchemaMissingCategoryFlag(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "schema"})
	if code != 2 {
		t.Fatalf("expected exit code 2 for missing required flag, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestAdCreateFromFileDryRunValid(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	tmpDir := t.TempDir()
	specFile := filepath.Join(tmpDir, "valid_ad.json")
	validSpecJSON := `{
		"category_id": 11,
		"title": "Volkswagen Golf VII 2.0 TDI Advance",
		"description": "Vehículo en impecable estado de conservación con libro de mantenimiento oficial sellado.",
		"price": 13900.0,
		"location": {
			"province": "Madrid",
			"municipality": "Alcobendas"
		},
		"attributes": {
			"fuel": "diesel",
			"transmission": "manual"
		}
	}`
	if err := os.WriteFile(specFile, []byte(validSpecJSON), 0644); err != nil {
		t.Fatalf("failed to write spec file: %v", err)
	}

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "create", "--from-file", specFile, "--dry-run", "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	var report milanuncios.ValidationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse report: %v, raw: %s", err, stdout.String())
	}

	if !report.Valid {
		t.Errorf("expected report to be valid, got errors: %+v", report.Errors)
	}
	if report.CategoryID != 11 {
		t.Errorf("expected category 11, got %d", report.CategoryID)
	}
}

func TestAdCreateInlineSpecDryRunValid(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	specJSON := `{"category_id": 11, "title": "Bicicleta de carretera carbono", "description": "Bicicleta en perfecto estado con frenos de disco y cambios Shimano 105.", "price": 850.0, "location": {"province": "Barcelona"}}`

	code := execApp(app, []string{"ad", "create", "--spec", specJSON, "--dry-run", "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	var report milanuncios.ValidationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse report: %v, raw: %s", err, stdout.String())
	}

	if !report.Valid {
		t.Errorf("expected report to be valid, got errors: %+v", report.Errors)
	}
}

func TestAdCreateStdinDryRunValid(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	specJSON := `{"category_id": 11, "title": "Bicicleta de montaña 29 pulgadas", "description": "Bicicleta rígida cuadro de aluminio ligero y horquilla de aire RockShox.", "price": 450.0, "location": {"province": "Valencia"}}`

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	app.Stdin = strings.NewReader(specJSON)
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "create", "--from-file", "-", "--dry-run", "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	var report milanuncios.ValidationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse report: %v, raw: %s", err, stdout.String())
	}

	if !report.Valid {
		t.Errorf("expected report to be valid, got: %+v", report.Errors)
	}
}

func TestAdCreateValidationFailureExitCode2(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	// Invalid spec: title too short, price 0, invalid enum gasoil
	specJSON := `{
		"category_id": 11,
		"title": "Golf",
		"description": "Short",
		"price": 0,
		"location": {"province": ""},
		"attributes": {"fuel": "gasoil"}
	}`

	code := execApp(app, []string{"ad", "create", "--spec", specJSON, "--dry-run", "--format", "json"})
	if code != 2 {
		t.Fatalf("expected exit code 2 for validation failure, got %d (stderr: %s)", code, stderr.String())
	}

	var report milanuncios.ValidationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("failed to parse report JSON: %v, raw: %s", err, stdout.String())
	}

	if report.Valid {
		t.Errorf("expected report to be invalid")
	}

	// Verify suggestion for gasoil -> diesel
	foundGasoil := false
	for _, e := range report.Errors {
		if e.Field == "attributes.fuel" {
			foundGasoil = true
			if e.Suggestion != "Change 'gasoil' to 'diesel'" {
				t.Errorf("expected suggestion to change gasoil to diesel, got %q", e.Suggestion)
			}
		}
	}
	if !foundGasoil {
		t.Errorf("missing attributes.fuel error in report: %+v", report.Errors)
	}
}

func TestAdCreateNonDryRunNotice(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	specJSON := `{"category_id": 11, "title": "PlayStation 5 con mando dualsense", "description": "Consola en perfecto estado con caja original y cables incluidos sin apenas uso.", "price": 400.0, "location": {"province": "Sevilla"}}`

	code := execApp(app, []string{"ad", "create", "--spec", specJSON, "--dry-run=false", "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "Slice 13") {
		t.Errorf("expected notice about Slice 13 on stderr, got: %s", stderr.String())
	}
}

func TestAdCreateMissingSpecAndFromFileFlags(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "create"})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "must provide ad spec") {
		t.Errorf("expected error message to mention must provide ad spec, got: %s", stderr.String())
	}
}

func TestAdCreateConflictingFlags(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "create", "--from-file", "foo.json", "--spec", "{}"})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cannot specify both") {
		t.Errorf("expected error message to mention cannot specify both, got: %s", stderr.String())
	}
}
