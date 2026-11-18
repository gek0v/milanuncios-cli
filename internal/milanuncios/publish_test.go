package milanuncios_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func setupCategoryMockServer(t *testing.T) *httptest.Server {
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
					{
						Key:   "year",
						Label: "Año",
						Type:  "number",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(res)
		case "/api/v1/categories/999/filters":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "category not found"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestGetCategorySchema(t *testing.T) {
	ts := setupCategoryMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	ctx := context.Background()

	// Negative category ID
	_, err := client.GetCategorySchema(ctx, 0)
	if err == nil {
		t.Fatal("expected error for category ID 0")
	}

	// Valid category schema
	schema, err := client.GetCategorySchema(ctx, 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if schema.Schema != "http://json-schema.org/draft-07/schema#" {
		t.Errorf("expected draft-07 schema, got %q", schema.Schema)
	}
	if schema.CategoryID != 11 {
		t.Errorf("expected category ID 11, got %d", schema.CategoryID)
	}
	if schema.CategoryName != "Coches de segunda mano" {
		t.Errorf("expected Coches de segunda mano, got %q", schema.CategoryName)
	}
	if len(schema.Required) < 4 {
		t.Errorf("expected at least 4 required properties, got %v", schema.Required)
	}

	props, ok := schema.Properties["attributes"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected attributes property in schema")
	}
	attrProps, ok := props["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties under attributes")
	}
	fuelProp, ok := attrProps["fuel"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fuel property under attributes")
	}
	enums, ok := fuelProp["enum"].([]string)
	if !ok || len(enums) != 6 {
		t.Errorf("expected 6 fuel enums, got %v", fuelProp["enum"])
	}

	// Test Pretty method
	var buf bytes.Buffer
	schema.Pretty(&buf, false)
	out := buf.String()
	if !strings.Contains(out, "Coches de segunda mano") {
		t.Errorf("expected pretty output to contain category name, got:\n%s", out)
	}
	if !strings.Contains(out, "fuel") {
		t.Errorf("expected pretty output to contain attribute fuel, got:\n%s", out)
	}
}

func TestValidateAdSpecValid(t *testing.T) {
	ts := setupCategoryMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	ctx := context.Background()

	tmpDir := t.TempDir()
	photoPath := filepath.Join(tmpDir, "car.jpg")
	if err := os.WriteFile(photoPath, []byte("fake-image-bytes-12345"), 0644); err != nil {
		t.Fatalf("failed to create temp photo: %v", err)
	}

	spec := &milanuncios.AdSpec{
		CategoryID:  11,
		Title:       "Volkswagen Golf 2.0 TDI Advance",
		Description: "Vehículo en impecable estado de conservación, siempre guardado en garaje privado con todas las revisiones al día.",
		Price:       13900.0,
		Location: milanuncios.AdLocationSpec{
			Province:     "Madrid",
			Municipality: "Alcobendas",
			PostalCode:   "28100",
		},
		Photos: []string{
			photoPath,
			"https://example.com/photo2.jpg",
			"data:image/jpeg;base64,/9j/4AAQSkZJRg==",
		},
		Attributes: map[string]interface{}{
			"fuel":         "diesel",
			"transmission": "manual",
			"year":         2018,
		},
	}

	report, err := client.ValidateAdSpec(ctx, spec)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if !report.Valid {
		t.Fatalf("expected valid spec, but got errors: %+v", report.Errors)
	}
	if report.PhotosDetected != 3 {
		t.Errorf("expected 3 photos detected, got %d", report.PhotosDetected)
	}
	if report.EstimatedUploadSizeBytes <= 0 {
		t.Errorf("expected positive estimated upload size, got %d", report.EstimatedUploadSizeBytes)
	}

	// Test Pretty output
	var buf bytes.Buffer
	report.Pretty(&buf, false)
	prettyOut := buf.String()
	if !strings.Contains(prettyOut, "✔ Validation successful!") {
		t.Errorf("expected success header in pretty output, got:\n%s", prettyOut)
	}
}

func TestValidateAdSpecInvalidEnumSuggestion(t *testing.T) {
	ts := setupCategoryMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	ctx := context.Background()

	spec := &milanuncios.AdSpec{
		CategoryID:  11,
		Title:       "Volkswagen Golf 2.0 TDI Advance",
		Description: "Vehículo en impecable estado de conservación, revisiones al día con libro sellado y sin accidentes.",
		Price:       12000.0,
		Location: milanuncios.AdLocationSpec{
			Province: "Madrid",
		},
		Attributes: map[string]interface{}{
			"fuel":         "gasoil", // alias for diesel
			"transmission": "automa", // close to automatico
		},
	}

	report, err := client.ValidateAdSpec(ctx, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Valid {
		t.Fatal("expected report to be invalid")
	}

	if len(report.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d: %+v", len(report.Errors), report.Errors)
	}

	// Verify fuel suggestion (alias)
	var fuelErr *milanuncios.ValidationError
	for _, e := range report.Errors {
		if e.Field == "attributes.fuel" {
			fuelErr = &e
			break
		}
	}
	if fuelErr == nil {
		t.Fatalf("missing attributes.fuel error")
	}
	if fuelErr.Code != "E_VAL_INVALID_ENUM" {
		t.Errorf("expected E_VAL_INVALID_ENUM, got %s", fuelErr.Code)
	}
	if fuelErr.Suggestion != "Change 'gasoil' to 'diesel'" {
		t.Errorf("expected Change 'gasoil' to 'diesel', got %q", fuelErr.Suggestion)
	}

	// Verify transmission suggestion (Levenshtein match)
	var transErr *milanuncios.ValidationError
	for _, e := range report.Errors {
		if e.Field == "attributes.transmission" {
			transErr = &e
			break
		}
	}
	if transErr == nil {
		t.Fatalf("missing attributes.transmission error")
	}
	if !strings.Contains(transErr.Suggestion, "automatico") {
		t.Errorf("expected suggestion to mention automatico, got %q", transErr.Suggestion)
	}

	// Test Pretty rendering for failure
	var buf bytes.Buffer
	report.Pretty(&buf, true) // with ANSI colors
	out := buf.String()
	if !strings.Contains(out, "Validation failed") {
		t.Errorf("expected failure header in pretty output, got:\n%s", out)
	}
	if !strings.Contains(out, "Change 'gasoil' to 'diesel'") {
		t.Errorf("expected suggestion in pretty output, got:\n%s", out)
	}
}

func TestValidateAdSpecCoreFieldViolations(t *testing.T) {
	ts := setupCategoryMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	ctx := context.Background()

	// Missing all core fields
	spec := &milanuncios.AdSpec{
		CategoryID:  0,
		Title:       "Short",
		Description: "Too short",
		Price:       0,
		Location: milanuncios.AdLocationSpec{
			Province: "",
		},
		Photos: []string{"/non/existent/path/to/img.png"},
	}

	report, err := client.ValidateAdSpec(ctx, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Valid {
		t.Fatal("expected report to be invalid")
	}

	codes := make(map[string]string)
	for _, e := range report.Errors {
		codes[e.Field] = e.Code
	}

	if codes["category_id"] != "E_VAL_MISSING_REQUIRED" {
		t.Errorf("expected category_id error E_VAL_MISSING_REQUIRED, got %s", codes["category_id"])
	}
	if codes["title"] != "E_VAL_STRING_TOO_SHORT" {
		t.Errorf("expected title error E_VAL_STRING_TOO_SHORT, got %s", codes["title"])
	}
	if codes["description"] != "E_VAL_STRING_TOO_SHORT" {
		t.Errorf("expected description error E_VAL_STRING_TOO_SHORT, got %s", codes["description"])
	}
	if codes["price"] != "E_VAL_NUMBER_TOO_LOW" {
		t.Errorf("expected price error E_VAL_NUMBER_TOO_LOW, got %s", codes["price"])
	}
	if codes["location.province"] != "E_VAL_MISSING_REQUIRED" {
		t.Errorf("expected location.province error E_VAL_MISSING_REQUIRED, got %s", codes["location.province"])
	}
	if codes["photos[0]"] != "E_VAL_FILE_NOT_FOUND" {
		t.Errorf("expected photos[0] error E_VAL_FILE_NOT_FOUND, got %s", codes["photos[0]"])
	}

	// Test overly long fields
	longSpec := &milanuncios.AdSpec{
		CategoryID:  11,
		Title:       strings.Repeat("A", 105),
		Description: strings.Repeat("B", 5005),
		Price:       100,
		Location: milanuncios.AdLocationSpec{
			Province: "Madrid",
		},
	}

	reportLong, err := client.ValidateAdSpec(ctx, longSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reportLong.Valid {
		t.Fatal("expected report to be invalid for overly long fields")
	}

	longCodes := make(map[string]string)
	for _, e := range reportLong.Errors {
		longCodes[e.Field] = e.Code
	}
	if longCodes["title"] != "E_VAL_STRING_TOO_LONG" {
		t.Errorf("expected E_VAL_STRING_TOO_LONG for title, got %s", longCodes["title"])
	}
	if longCodes["description"] != "E_VAL_STRING_TOO_LONG" {
		t.Errorf("expected E_VAL_STRING_TOO_LONG for description, got %s", longCodes["description"])
	}
}

func TestValidateAdSpecCategoryNotFound(t *testing.T) {
	ts := setupCategoryMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	ctx := context.Background()

	spec := &milanuncios.AdSpec{
		CategoryID:  999, // returns 404
		Title:       "Valid title with sufficient length",
		Description: "Valid description with sufficient length over twenty characters.",
		Price:       50,
		Location: milanuncios.AdLocationSpec{
			Province: "Madrid",
		},
	}

	report, err := client.ValidateAdSpec(ctx, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Valid {
		t.Fatal("expected invalid report for nonexistent category")
	}

	if len(report.Errors) != 1 || report.Errors[0].Code != "E_VAL_CATEGORY_NOT_FOUND" {
		t.Errorf("expected E_VAL_CATEGORY_NOT_FOUND error, got: %+v", report.Errors)
	}
}

func setupPublishMockServer(t *testing.T) *httptest.Server {
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
						Options: []string{"diesel", "gasolina"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(res)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/media/upload":
			// Validate multipart/form-data
			if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": "expected multipart/form-data"}`))
				return
			}
			err := r.ParseMultipartForm(16 << 20)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": "invalid multipart form"}`))
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": "missing file field"}`))
				return
			}
			file.Close()

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"media_id": "media-m-991203", "url": "https://img.milanuncios.com/fotos/media-m-991203.jpg"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/ads":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
				return
			}
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": "invalid json body"}`))
				return
			}
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
			sort.Strings(updatedFields)
			resp := map[string]interface{}{
				"id":             "492817294",
				"updated_fields": updatedFields,
				"previous_price": 13900.0,
				"new_price":      patch["price"],
				"updated_at":     "2026-11-18T09:15:00Z",
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/ads/404":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "ad not found"}`))

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

		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/ads/404":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "ad not found"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not found"}`))
		}
	}))
}

func TestUploadPhoto_LocalFile(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("fake-token")

	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "sample.jpg")
	// Write standard JPEG header \xFF\xD8\xFF\xE0
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01}
	if err := os.WriteFile(localFile, jpegBytes, 0644); err != nil {
		t.Fatalf("failed to write temp jpeg: %v", err)
	}

	photo, err := client.UploadPhoto(context.Background(), localFile, 0)
	if err != nil {
		t.Fatalf("UploadPhoto failed: %v", err)
	}

	if photo.Index != 0 {
		t.Errorf("expected index 0, got %d", photo.Index)
	}
	if photo.MediaID != "media-m-991203" {
		t.Errorf("expected media-m-991203, got %s", photo.MediaID)
	}
	if photo.URL != "https://img.milanuncios.com/fotos/media-m-991203.jpg" {
		t.Errorf("expected cdn url, got %s", photo.URL)
	}
	if photo.Source != localFile {
		t.Errorf("expected source to match localFile, got %s", photo.Source)
	}
}

func TestUploadPhoto_RemoteURL(t *testing.T) {
	// Remote image server serving a PNG
	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n', 0, 0, 0, 0}
		_, _ = w.Write(pngBytes)
	}))
	defer imgServer.Close()

	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("fake-token")

	remoteURL := imgServer.URL + "/car.png"
	photo, err := client.UploadPhoto(context.Background(), remoteURL, 1)
	if err != nil {
		t.Fatalf("UploadPhoto with remote URL failed: %v", err)
	}

	if photo.Index != 1 {
		t.Errorf("expected index 1, got %d", photo.Index)
	}
	if photo.MediaID != "media-m-991203" {
		t.Errorf("expected media-m-991203, got %s", photo.MediaID)
	}
}

func TestUploadPhoto_Base64DataURI(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("fake-token")

	// Base64 for JPEG bytes /9j/4AAQSkZJRg==
	dataURI := "data:image/jpeg;base64,/9j/4AAQSkZJRg=="
	photo, err := client.UploadPhoto(context.Background(), dataURI, 2)
	if err != nil {
		t.Fatalf("UploadPhoto with base64 data URI failed: %v", err)
	}

	if photo.Index != 2 {
		t.Errorf("expected index 2, got %d", photo.Index)
	}
	if photo.MediaID != "media-m-991203" {
		t.Errorf("expected media-m-991203, got %s", photo.MediaID)
	}
}

func TestUploadPhoto_MediaID(t *testing.T) {
	client := milanuncios.NewClient("test")

	photo, err := client.UploadPhoto(context.Background(), "media://m-88231", 3)
	if err != nil {
		t.Fatalf("UploadPhoto with media:// failed: %v", err)
	}

	if photo.Index != 3 {
		t.Errorf("expected index 3, got %d", photo.Index)
	}
	if photo.MediaID != "m-88231" {
		t.Errorf("expected m-88231, got %s", photo.MediaID)
	}
}

func TestCreateAd_Success(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("valid-session-token")

	spec := &milanuncios.AdSpec{
		ClientRequestID: "uuid-test-123",
		CategoryID:      11,
		Title:           "Volkswagen Golf VII 2.0 TDI Advance",
		Description:     "Vehículo en impecable estado de conservación con libro de mantenimiento oficial.",
		Price:           13900.0,
		Location: milanuncios.AdLocationSpec{
			Province:     "Madrid",
			Municipality: "Alcobendas",
		},
		Photos: []string{
			"media://existing-photo-1",
			"data:image/jpeg;base64,/9j/4AAQSkZJRg==",
		},
		Attributes: map[string]interface{}{
			"fuel": "diesel",
		},
	}

	ad, err := client.CreateAd(context.Background(), spec)
	if err != nil {
		t.Fatalf("CreateAd failed: %v", err)
	}

	if ad.ID != "492817294" {
		t.Errorf("expected ad ID 492817294, got %s", ad.ID)
	}
	if ad.Status != "published" {
		t.Errorf("expected status published, got %s", ad.Status)
	}
	if ad.ClientRequestID != "uuid-test-123" {
		t.Errorf("expected client_request_id uuid-test-123, got %s", ad.ClientRequestID)
	}
	if len(ad.Photos) != 2 {
		t.Fatalf("expected 2 photos, got %d", len(ad.Photos))
	}

	// Test Pretty output
	var buf bytes.Buffer
	ad.Pretty(&buf, false)
	prettyOut := buf.String()
	if !strings.Contains(prettyOut, "✔ Ad published successfully!") {
		t.Errorf("expected pretty header in confirmation, got:\n%s", prettyOut)
	}
	if !strings.Contains(prettyOut, "492817294") {
		t.Errorf("expected ID in pretty output, got:\n%s", prettyOut)
	}
}

func TestCreateAd_Unauthenticated(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	// No session token set

	spec := &milanuncios.AdSpec{
		CategoryID:  11,
		Title:       "Volkswagen Golf VII 2.0 TDI Advance",
		Description: "Vehículo en impecable estado de conservación con libro de mantenimiento oficial.",
		Price:       13900.0,
		Location: milanuncios.AdLocationSpec{
			Province: "Madrid",
		},
	}

	_, err := client.CreateAd(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error for unauthenticated CreateAd, got nil")
	}
}

func TestCreateAd_ValidationError(t *testing.T) {
	client := milanuncios.NewClient("test")
	client.SetSession("fake-token")

	// Invalid spec: title too short
	spec := &milanuncios.AdSpec{
		CategoryID:  11,
		Title:       "Golf",
		Description: "Short",
		Price:       0,
	}

	_, err := client.CreateAd(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error for invalid spec, got nil")
	}
}

func TestUpdateAd_Success(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("valid-session-token")

	patch := map[string]interface{}{
		"price":       13500.0,
		"description": "Actualizado: ITV recién pasada en taller oficial.",
	}

	result, err := client.UpdateAd(context.Background(), "492817294", patch)
	if err != nil {
		t.Fatalf("UpdateAd failed: %v", err)
	}

	if result.ID != "492817294" {
		t.Errorf("expected ID 492817294, got %s", result.ID)
	}
	if len(result.UpdatedFields) != 2 {
		t.Errorf("expected 2 updated fields, got %v", result.UpdatedFields)
	}

	var buf bytes.Buffer
	result.Pretty(&buf, false)
	prettyOut := buf.String()
	if !strings.Contains(prettyOut, "✔ Ad updated successfully") {
		t.Errorf("expected success header in pretty output, got:\n%s", prettyOut)
	}
}

func TestUpdateAd_NotFound(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("valid-session-token")

	_, err := client.UpdateAd(context.Background(), "404", map[string]interface{}{"price": 100})
	if err == nil {
		t.Fatal("expected error for 404 ad, got nil")
	}
}

func TestDeleteAd_Success(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("valid-session-token")

	result, err := client.DeleteAd(context.Background(), "492817294", "sold_milanuncios", "Vendido en 48 horas")
	if err != nil {
		t.Fatalf("DeleteAd failed: %v", err)
	}

	if result.ID != "492817294" {
		t.Errorf("expected ID 492817294, got %s", result.ID)
	}
	if result.Reason != "sold_milanuncios" {
		t.Errorf("expected reason sold_milanuncios, got %s", result.Reason)
	}

	var buf bytes.Buffer
	result.Pretty(&buf, false)
	prettyOut := buf.String()
	if !strings.Contains(prettyOut, "✔ Ad deleted/withdrawn successfully") {
		t.Errorf("expected success header in pretty output, got:\n%s", prettyOut)
	}
}

func TestDeleteAd_InvalidReason(t *testing.T) {
	client := milanuncios.NewClient("test")
	client.SetSession("valid-session-token")

	_, err := client.DeleteAd(context.Background(), "492817294", "unsupported_reason", "")
	if err == nil {
		t.Fatal("expected error for invalid reason, got nil")
	}
}

func TestDeleteAd_NotFound(t *testing.T) {
	ts := setupPublishMockServer(t)
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("valid-session-token")

	_, err := client.DeleteAd(context.Background(), "404", "withdrawn", "")
	if err == nil {
		t.Fatal("expected error for 404 ad, got nil")
	}
}
