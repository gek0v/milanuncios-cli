package milanuncios_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/output"
)

func TestClientSearchSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("s") != "bicicleta" {
			t.Errorf("unexpected query 's': %s", r.URL.Query().Get("s"))
		}

		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{
					ID:       "1001",
					Title:    "Bicicleta de montaña",
					Price:    120,
					Currency: "€",
					Location: milanuncios.AdLocation{
						Province:     "Madrid",
						Municipality: "Alcalá",
					},
					Seller: milanuncios.AdSeller{
						Name: "Carlos",
						Type: "particular",
					},
					URL: "https://www.milanuncios.com/bicicletas/1001.htm",
				},
			},
			Total:    1,
			NextPage: "cursor-page-2",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	ctx := context.Background()
	res, err := client.Search(ctx, milanuncios.SearchQuery{Keywords: "bicicleta"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Items))
	}
	if res.Items[0].Title != "Bicicleta de montaña" {
		t.Errorf("unexpected item title: %s", res.Items[0].Title)
	}
	if res.NextPage != "cursor-page-2" {
		t.Errorf("unexpected next page: %s", res.NextPage)
	}

	var buf bytes.Buffer
	res.Pretty(&buf, false)
	out := buf.String()
	if !strings.Contains(out, "Bicicleta de montaña") || !strings.Contains(out, "120 €") {
		t.Errorf("expected pretty table to contain item info, got: %s", out)
	}
}

func TestClientWAFRetry(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// Simulate Cloudflare / Akamai blocking honest CLI UA
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><head><title>Attention Required! | Cloudflare</title></head><body>Access Denied</body></html>"))
			return
		}

		// Second attempt with browser UA succeeds
		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{ID: "2002", Title: "Mesa comedor", Price: 50},
			},
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	var noticeMsg string
	client.Notice = func(msg string) {
		noticeMsg = msg
	}

	res, err := client.Search(context.Background(), milanuncios.SearchQuery{})
	if err != nil {
		t.Fatalf("unexpected error after WAF retry: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Items))
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if !strings.Contains(noticeMsg, "retrying with browser user agent") {
		t.Errorf("expected notice message, got: %s", noticeMsg)
	}
}

func TestClientErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("case") {
		case "404":
			w.WriteHeader(http.StatusNotFound)
		case "429":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limit exceeded"}`))
		case "invalid_json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not a valid json response"))
		}
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	// 404 test
	_, err := client.Search(context.Background(), milanuncios.SearchQuery{Keywords: "test", Next: "404"})
	// Using custom param by changing query or testing path
	// Let's pass keywords as the case selector if we set case in query
	u, _ := url.Parse(ts.URL)
	_ = u

	// Test 404
	{
		ts404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		client.BaseURL = ts404.URL
		_, err = client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error on 404, got nil")
		}
		if ok := output.Classify(err, "1.0", "search"); ok.Code != 4 {
			t.Fatalf("expected exit code 4 for not found, got %d", ok.Code)
		}
		ts404.Close()
	}

	// Test 429
	{
		ts429 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		client.BaseURL = ts429.URL
		_, err = client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error on 429, got nil")
		}
		if ok := output.Classify(err, "1.0", "search"); ok.Code != 5 {
			t.Fatalf("expected exit code 5 for rate limit, got %d", ok.Code)
		}
		ts429.Close()
	}

	// Test invalid JSON (Exit 7 - API Changed)
	{
		tsBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("malformed json payload"))
		}))
		client.BaseURL = tsBad.URL
		_, err = client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error on bad json, got nil")
		}
		if ok := output.Classify(err, "1.0", "search"); ok.Code != 7 {
			t.Fatalf("expected exit code 7 for api changed, got %d", ok.Code)
		}
		tsBad.Close()
	}
}

func TestExtractAdID(t *testing.T) {
	tests := []struct {
		input       string
		expectedID  string
		expectError bool
	}{
		{"492817294", "492817294", false},
		{"r492817294", "492817294", false},
		{"R492817294", "492817294", false},
		{"https://www.milanuncios.com/bicicletas/bicicleta-montana-492817294.htm", "492817294", false},
		{"https://www.milanuncios.com/anuncios/492817294.htm", "492817294", false},
		{"https://www.milanuncios.com/anuncios/492817294", "492817294", false},
		{"invalid-no-id", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			id, err := milanuncios.ExtractAdID(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for input %q: %v", tt.input, err)
				}
				if id != tt.expectedID {
					t.Fatalf("expected ID %q, got %q", tt.expectedID, id)
				}
			}
		})
	}
}

func TestClientGetAd(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ads/492817294" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		item := milanuncios.AdItem{
			ID:          "492817294",
			Title:       "Sofá Chaiselongue 3 plazas",
			Description: "Sofá en muy buen estado, tela antimanchas.",
			Price:       250,
			Currency:    "€",
			Location: milanuncios.AdLocation{
				Province:     "Valencia",
				Municipality: "Gandia",
			},
			Seller: milanuncios.AdSeller{
				Name: "Elena",
				Type: "particular",
			},
			Photos: []string{
				"https://img.milanuncios.com/photos/1.jpg",
				"https://img.milanuncios.com/photos/2.jpg",
			},
			URL: "https://www.milanuncios.com/anuncios/492817294.htm",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	item, err := client.GetAd(context.Background(), "492817294")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.Title != "Sofá Chaiselongue 3 plazas" {
		t.Errorf("unexpected title: %s", item.Title)
	}
	if len(item.Photos) != 2 {
		t.Errorf("expected 2 photos, got %d", len(item.Photos))
	}

	var buf bytes.Buffer
	item.Pretty(&buf, false)
	out := buf.String()
	if !strings.Contains(out, "Sofá Chaiselongue") || !strings.Contains(out, "Gandia (Valencia)") {
		t.Errorf("expected pretty output to contain item details, got: %s", out)
	}
}

func TestListCategories(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/categories" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		cats := milanuncios.CategoryListResult{
			{
				ID:   10,
				Name: "Motor",
				Slug: "motor",
				Children: []milanuncios.Category{
					{ID: 11, Name: "Coches", Slug: "coches-segunda-mano"},
					{ID: 12, Name: "Motos", Slug: "motos-segunda-mano"},
				},
			},
			{
				ID:   20,
				Name: "Inmobiliaria",
				Slug: "inmobiliaria",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cats)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	cats, err := client.ListCategories(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cats) != 2 {
		t.Fatalf("expected 2 root categories, got %d", len(cats))
	}
	if len(cats[0].Children) != 2 {
		t.Fatalf("expected 2 subcategories in Motor, got %d", len(cats[0].Children))
	}

	var buf bytes.Buffer
	cats.Pretty(&buf, false)
	out := buf.String()
	if !strings.Contains(out, "Motor") || !strings.Contains(out, "Coches") {
		t.Errorf("expected pretty output to contain category names, got: %s", out)
	}
}

func TestGetCategoryFilters(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/categories/10/filters" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		res := milanuncios.CategoryFiltersResult{
			CategoryID:   10,
			CategoryName: "Motor",
			Filters: []milanuncios.FilterDefinition{
				{
					Key:     "brand",
					Label:   "Marca",
					Type:    "select",
					Options: []string{"BMW", "Audi", "Toyota"},
				},
				{
					Key:   "fuel",
					Label: "Combustible",
					Type:  "select",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	res, err := client.GetCategoryFilters(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.CategoryID != 10 || len(res.Filters) != 2 {
		t.Fatalf("unexpected filters result: %+v", res)
	}

	var buf bytes.Buffer
	res.Pretty(&buf, false)
	out := buf.String()
	if !strings.Contains(out, "brand") || !strings.Contains(out, "BMW") {
		t.Errorf("expected pretty output to contain filter keys, got: %s", out)
	}
}

func TestSearchWithFilters(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter[brand]") != "Toyota" {
			t.Errorf("expected filter[brand]=Toyota, got: %s", r.URL.Query().Get("filter[brand]"))
		}
		if r.URL.Query().Get("filter[fuel]") != "diesel" {
			t.Errorf("expected filter[fuel]=diesel, got: %s", r.URL.Query().Get("filter[fuel]"))
		}

		res := milanuncios.SearchResult{
			Items: []milanuncios.AdItem{
				{ID: "5005", Title: "Toyota Auris Diesel", Price: 7500},
			},
			Total: 1,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	res, err := client.Search(context.Background(), milanuncios.SearchQuery{
		Keywords: "auris",
		Category: 10,
		Filters: map[string]string{
			"brand": "Toyota",
			"fuel":  "diesel",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(res.Items))
	}
}
