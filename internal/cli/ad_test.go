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
		switch {
		case r.URL.Path == "/api/v1/categories/11/filters":
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

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/media/upload":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"media_id": "media-m-123", "url": "https://img.milanuncios.com/fotos/media-m-123.jpg"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/ads":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
				return
			}
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			resp := map[string]interface{}{
				"id":                "492817294",
				"client_request_id": body["client_request_id"],
				"status":            "published",
				"title":             body["title"],
				"price":             body["price"],
				"category_id":       body["category_id"],
				"url":               "https://www.milanuncios.com/coches-de-segunda-mano/volkswagen-golf-492817294.htm",
				"created_at":        "2026-11-18T08:50:00Z",
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/ads/492817294":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
				return
			}
			var patch map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&patch)
			var updatedFields []string
			for k := range patch {
				updatedFields = append(updatedFields, k)
			}
			sort := func(s []string) {
				for i := 0; i < len(s); i++ {
					for j := i + 1; j < len(s); j++ {
						if s[i] > s[j] {
							s[i], s[j] = s[j], s[i]
						}
					}
				}
			}
			sort(updatedFields)
			resp := map[string]interface{}{
				"id":             "492817294",
				"updated_fields": updatedFields,
				"previous_price": 13900.0,
				"new_price":      patch["price"],
				"updated_at":     "2026-11-18T09:15:00Z",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/ads/492817294":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
				return
			}
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			resp := map[string]interface{}{
				"id":      "492817294",
				"status":  "deleted",
				"reason":  payload["reason"],
				"message": "Listing deleted successfully",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

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

func TestAdCreateRealPublishSuccess(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	app.Client.SetSession("fake-token")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	specJSON := `{"category_id": 11, "title": "PlayStation 5 con mando dualsense", "description": "Consola en perfecto estado con caja original y cables incluidos sin apenas uso.", "price": 400.0, "location": {"province": "Sevilla"}, "photos": ["media://m-123"]}`

	code := execApp(app, []string{"ad", "create", "--spec", specJSON, "--dry-run=false", "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	var ad milanuncios.AdItem
	if err := json.Unmarshal(stdout.Bytes(), &ad); err != nil {
		t.Fatalf("failed to parse published AdItem: %v, raw: %s", err, stdout.String())
	}
	if ad.ID != "492817294" {
		t.Errorf("expected ad ID 492817294, got %s", ad.ID)
	}
	if ad.Status != "published" {
		t.Errorf("expected status published, got %s", ad.Status)
	}
}

func TestAdCreateRealPublishUnauthenticated(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	// Unauthenticated: no session token
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	specJSON := `{"category_id": 11, "title": "PlayStation 5 con mando dualsense", "description": "Consola en perfecto estado con caja original y cables incluidos sin apenas uso.", "price": 400.0, "location": {"province": "Sevilla"}}`

	code := execApp(app, []string{"ad", "create", "--spec", specJSON, "--dry-run=false", "--format", "json"})
	if code != 3 {
		t.Fatalf("expected exit code 3 for unauthenticated ad create, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestAdUpdateSuccess_Flags(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	app.Client.SetSession("fake-token")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "update", "492817294", "--price", "13500", "--title", "Nuevo Titulo Golf", "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	var res milanuncios.AdUpdateResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse AdUpdateResult: %v, raw: %s", err, stdout.String())
	}
	if res.ID != "492817294" {
		t.Errorf("expected ID 492817294, got %s", res.ID)
	}
	if res.NewPrice != 13500 {
		t.Errorf("expected new price 13500, got %f", res.NewPrice)
	}
}

func TestAdUpdateSuccess_SpecJSON(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	app.Client.SetSession("fake-token")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "update", "492817294", "--spec", `{"price": 12900}`, "--format", "pretty"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "✔ Ad updated successfully") {
		t.Errorf("expected success message in pretty output, got:\n%s", stdout.String())
	}
}

func TestAdUpdateSuccess_PatchFile(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	tmpDir := t.TempDir()
	patchPath := filepath.Join(tmpDir, "patch.json")
	if err := os.WriteFile(patchPath, []byte(`{"price": 12500}`), 0644); err != nil {
		t.Fatalf("failed to write patch file: %v", err)
	}

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	app.Client.SetSession("fake-token")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "update", "492817294", "--patch-file", patchPath, "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestAdUpdateMissingFlags_ExitCode2(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	// Missing flags
	code := execApp(app, []string{"ad", "update", "492817294"})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no update fields provided") {
		t.Errorf("expected no update fields error, got: %s", stderr.String())
	}
}

func TestAdUpdateUnauthenticated_ExitCode3(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "update", "492817294", "--price", "100"})
	if code != 3 {
		t.Fatalf("expected exit code 3 for unauthenticated ad update, got %d", code)
	}
}

func TestAdDeleteSuccess(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	app.Client.SetSession("fake-token")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "delete", "492817294", "--reason", "sold_milanuncios", "--feedback", "Vendido rápido", "--format", "json"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}

	var res milanuncios.AdDeleteResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse AdDeleteResult: %v, raw: %s", err, stdout.String())
	}
	if res.ID != "492817294" {
		t.Errorf("expected ID 492817294, got %s", res.ID)
	}
	if res.Reason != "sold_milanuncios" {
		t.Errorf("expected reason sold_milanuncios, got %s", res.Reason)
	}
}

func TestAdDeleteInvalidReason_ExitCode2(t *testing.T) {
	app := cli.NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "delete", "492817294", "--reason", "invalid_reason"})
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "invalid reason") {
		t.Errorf("expected invalid reason error message, got: %s", stderr.String())
	}
}

func TestAdDeleteUnauthenticated_ExitCode3(t *testing.T) {
	ts := setupAdMockServer(t)
	defer ts.Close()

	app := cli.NewApp("test")
	app.Client.BaseURL = ts.URL
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	code := execApp(app, []string{"ad", "delete", "492817294"})
	if code != 3 {
		t.Fatalf("expected exit code 3 for unauthenticated ad delete, got %d", code)
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

func TestFavoriteAnnotationsCLI(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "annotations.db")
	t.Setenv("MILANUNCIOS_STATE_DB", dbPath)
	t.Setenv("MILANUNCIOS_CONFIG", filepath.Join(tmpDir, "config.toml"))

	// 1. Missing args on tag
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr
		code := execApp(app, []string{"ad", "favorite", "tag"})
		if code != 2 {
			t.Fatalf("expected exit code 2 for missing tag args, got %d", code)
		}
	}

	// 2. Missing args on notes
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr
		code := execApp(app, []string{"ad", "favorite", "notes"})
		if code != 2 {
			t.Fatalf("expected exit code 2 for missing notes args, got %d", code)
		}
	}

	// 3. Notes not found -> 4
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr
		code := execApp(app, []string{"ad", "favorite", "notes", "999999999"})
		if code != 4 {
			t.Fatalf("expected exit code 4 for non-existent annotation, got %d", code)
		}
	}

	// 4. Save annotation via 'ad favorite tag'
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr
		code := execApp(app, []string{"ad", "favorite", "tag", "492817294", "oferta_enviada", "--note", "Ofrecidos 12.500€ el 18/11.", "--target-price", "12500", "--format", "json"})
		if code != 0 {
			t.Fatalf("expected exit code 0 for ad favorite tag, got %d (stderr: %s)", code, stderr.String())
		}

		var res map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode tag confirmation: %v, stdout: %s", err, stdout.String())
		}
		if res["status"] != "annotated" || res["ad_id"] != "492817294" || res["tag"] != "oferta_enviada" || res["target_price"] != float64(12500) {
			t.Fatalf("unexpected tag confirmation: %+v", res)
		}
	}

	// 5. Read annotation via 'ad favorite notes'
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr
		code := execApp(app, []string{"ad", "favorite", "notes", "492817294", "--format", "json"})
		if code != 0 {
			t.Fatalf("expected exit code 0 for ad favorite notes, got %d (stderr: %s)", code, stderr.String())
		}

		var res map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode notes response: %v, stdout: %s", err, stdout.String())
		}
		if res["ad_id"] != "492817294" || res["tag"] != "oferta_enviada" || res["note"] != "Ofrecidos 12.500€ el 18/11." {
			t.Fatalf("unexpected notes response: %+v", res)
		}
	}

	// 6. Test top-level 'favorite tag' and 'favorite notes' aliases
	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr
		code := execApp(app, []string{"favorite", "tag", "492817294", "contraoferta", "--format", "json"})
		if code != 0 {
			t.Fatalf("expected exit code 0 for favorite tag, got %d (stderr: %s)", code, stderr.String())
		}
	}

	{
		app := cli.NewApp("test")
		var stdout, stderr bytes.Buffer
		app.Stdout = &stdout
		app.Stderr = &stderr
		code := execApp(app, []string{"favorite", "notes", "492817294", "--format", "json"})
		if code != 0 {
			t.Fatalf("expected exit code 0 for favorite notes, got %d", code)
		}
		var res map[string]any
		_ = json.Unmarshal(stdout.Bytes(), &res)
		if res["tag"] != "contraoferta" {
			t.Fatalf("expected tag 'contraoferta', got %v", res["tag"])
		}
	}
}
