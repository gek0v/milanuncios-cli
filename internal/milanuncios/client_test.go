package milanuncios_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		{"https://www.milanuncios.com/moviles-en-madrid/iphone-14-pro-max-512gb-492817294.htm", "492817294", false},
		{"https://www.milanuncios.com/motor/bmw-320d-pack-m-492817294.htm?fromSearch=1", "492817294", false},
		{"https://www.milanuncios.com/motor/bmw-320d-pack-m-492817294", "492817294", false},
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
			Photos: []milanuncios.UploadedPhoto{
				{Index: 0, URL: "https://img.milanuncios.com/photos/1.jpg"},
				{Index: 1, URL: "https://img.milanuncios.com/photos/2.jpg"},
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

func TestParseCookies(t *testing.T) {
	// JSON format (Cookie-Editor)
	jsonCookies := `[
		{"name": "other_cookie", "value": "123"},
		{"name": "token", "value": "jwt-session-token-abc"}
	]`
	tok, err := milanuncios.ParseCookies(jsonCookies)
	if err != nil || tok != "jwt-session-token-abc" {
		t.Fatalf("expected 'jwt-session-token-abc', got %q (err: %v)", tok, err)
	}

	// Single JSON object
	singleJSON := `{"name": "token", "value": "single-json-token"}`
	tok, err = milanuncios.ParseCookies(singleJSON)
	if err != nil || tok != "single-json-token" {
		t.Fatalf("expected 'single-json-token', got %q (err: %v)", tok, err)
	}

	// Netscape format with #HttpOnly_
	httpOnlyCookies := `# Netscape HTTP Cookie File
#HttpOnly_.milanuncios.com	TRUE	/	TRUE	1799999999	token	httponly-token-val
`
	tok, err = milanuncios.ParseCookies(httpOnlyCookies)
	if err != nil || tok != "httponly-token-val" {
		t.Fatalf("expected 'httponly-token-val', got %q (err: %v)", tok, err)
	}

	// Quoted cookie value
	quotedCookies := `token="quoted-jwt-token"; other=123`
	tok, err = milanuncios.ParseCookies(quotedCookies)
	if err != nil || tok != "quoted-jwt-token" {
		t.Fatalf("expected 'quoted-jwt-token', got %q (err: %v)", tok, err)
	}

	// Netscape format
	netscapeCookies := `# Netscape HTTP Cookie File
.milanuncios.com	TRUE	/	TRUE	1799999999	session	session-token-xyz
`
	tok, err = milanuncios.ParseCookies(netscapeCookies)
	if err != nil || tok != "session-token-xyz" {
		t.Fatalf("expected 'session-token-xyz', got %q (err: %v)", tok, err)
	}

	// Raw Cookie header format
	headerCookies := "other=abc; adevinta_session=adevinta-secret-999; foo=bar"
	tok, err = milanuncios.ParseCookies(headerCookies)
	if err != nil || tok != "adevinta-secret-999" {
		t.Fatalf("expected 'adevinta-secret-999', got %q (err: %v)", tok, err)
	}

	// Direct token
	tok, err = milanuncios.ParseCookies("directtoken123456789")
	if err != nil || tok != "directtoken123456789" {
		t.Fatalf("expected 'directtoken123456789', got %q (err: %v)", tok, err)
	}

	// Invalid empty
	_, err = milanuncios.ParseCookies("   ")
	if err == nil {
		t.Fatal("expected error on empty cookies, got nil")
	}
}

func TestClientAuthMeAndFavorites(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid-token-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/api/v1/me":
			me := milanuncios.UserProfile{
				ID:       "user-777",
				Name:     "Laura Gómez",
				Email:    "laura@example.com",
				Verified: true,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(me)

		case "/api/v1/me/favorites":
			favs := []milanuncios.AdItem{
				{ID: "888", Title: "Mesa comedor", Price: 70},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favs)

		case "/api/v1/ads/888/favorite":
			if r.Method == http.MethodPost || r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	// 1. Unauthenticated Me should fail
	_, err := client.Me(context.Background())
	if err == nil {
		t.Fatal("expected auth error when unauthenticated, got nil")
	}

	// 2. Set valid session
	client.SetSession("valid-token-123")

	me, err := client.Me(context.Background())
	if err != nil {
		t.Fatalf("unexpected error fetching Me: %v", err)
	}
	if me.Name != "Laura Gómez" || me.Email != "laura@example.com" {
		t.Fatalf("unexpected profile: %+v", me)
	}

	// 3. Test MyFavorites
	favs, err := client.MyFavorites(context.Background())
	if err != nil {
		t.Fatalf("unexpected error fetching favorites: %v", err)
	}
	if len(favs) != 1 || favs[0].ID != "888" {
		t.Fatalf("unexpected favorites: %+v", favs)
	}

	// 4. Test Favorite and Unfavorite
	if err := client.Favorite(context.Background(), "888"); err != nil {
		t.Fatalf("unexpected error in Favorite: %v", err)
	}
	if err := client.Unfavorite(context.Background(), "888"); err != nil {
		t.Fatalf("unexpected error in Unfavorite: %v", err)
	}
}

func TestSendMessagePayloadInBody(t *testing.T) {
	var receivedBody map[string]string
	var receivedContentType string
	var rawQuery string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/conversations/conv-123/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		rawQuery = r.URL.RawQuery
		receivedContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)

		msg := milanuncios.Message{
			ID:             "msg-1",
			ConversationID: "conv-123",
			SenderID:       "user-me",
			Text:           receivedBody["text"],
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msg)
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("valid-token")

	msg, err := client.SendMessage(context.Background(), "conv-123", "¿Sigue disponible?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Text != "¿Sigue disponible?" {
		t.Fatalf("expected message text '¿Sigue disponible?', got %q", msg.Text)
	}
	if !strings.HasPrefix(receivedContentType, "application/json") {
		t.Fatalf("expected application/json Content-Type, got %q", receivedContentType)
	}
	if rawQuery != "" {
		t.Fatalf("expected query string to be empty, got %q", rawQuery)
	}
	if receivedBody["text"] != "¿Sigue disponible?" {
		t.Fatalf("expected body text '¿Sigue disponible?', got %q", receivedBody["text"])
	}
}

func TestStartConversation(t *testing.T) {
	t.Run("happy path with mock server", func(t *testing.T) {
		var (
			receivedMethod      string
			receivedPath        string
			receivedContentType string
			receivedBody        map[string]string
		)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			receivedPath = r.URL.Path
			receivedContentType = r.Header.Get("Content-Type")
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)

			resp := milanuncios.ConversationStartResult{
				ConversationID: "conv-8849201",
				AdID:           "492817294",
				AdTitle:        "Volkswagen Golf VII 2.0 TDI",
				RecipientName:  "Javier R.",
				MessageID:      "msg-9901",
				Text:           "Hola, me interesa el coche.",
				SentAt:         "2026-11-18T09:20:00Z",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer ts.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = ts.URL
		client.SetSession("valid-session")

		res, err := client.StartConversation(context.Background(), "492817294", "Hola, me interesa el coche.")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedMethod != http.MethodPost {
			t.Errorf("expected POST, got %s", receivedMethod)
		}
		if receivedPath != "/api/v1/ads/492817294/contact" {
			t.Errorf("expected /api/v1/ads/492817294/contact, got %s", receivedPath)
		}
		if !strings.HasPrefix(receivedContentType, "application/json") {
			t.Errorf("expected Content-Type application/json, got %s", receivedContentType)
		}
		if receivedBody["ad_id"] != "492817294" || receivedBody["text"] != "Hola, me interesa el coche." {
			t.Errorf("unexpected body payload: %+v", receivedBody)
		}

		if res.ConversationID != "conv-8849201" {
			t.Errorf("expected conv-8849201, got %s", res.ConversationID)
		}
		if res.RecipientName != "Javier R." {
			t.Errorf("expected Javier R., got %s", res.RecipientName)
		}

		var buf bytes.Buffer
		res.Pretty(&buf, false)
		prettyOut := buf.String()
		if !strings.Contains(prettyOut, "conv-8849201") || !strings.Contains(prettyOut, "Volkswagen Golf") {
			t.Errorf("pretty output missing expected fields: %s", prettyOut)
		}

		buf.Reset()
		res.Pretty(&buf, true)
		if !strings.Contains(buf.String(), "conv-8849201") {
			t.Errorf("pretty color output missing conversation ID: %s", buf.String())
		}
	})

	t.Run("404 not found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not found"}`))
		}))
		defer ts.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = ts.URL
		client.SetSession("valid-session")

		_, err := client.StartConversation(context.Background(), "492817294", "Hola")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var de *output.DomainError
		if !errors.As(err, &de) {
			t.Fatalf("expected DomainError, got %T: %v", err, err)
		}
		if de.Kind != output.KindNotFound {
			t.Errorf("expected KindNotFound, got %v", de.Kind)
		}
		if !strings.Contains(err.Error(), "ad not found: 492817294") {
			t.Errorf("expected error message to contain 'ad not found: 492817294', got %q", err.Error())
		}
	})

	t.Run("own ad validation error 400", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message": "cannot start conversation on your own ad"}`))
		}))
		defer ts.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = ts.URL
		client.SetSession("valid-session")

		_, err := client.StartConversation(context.Background(), "492817294", "Hola")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var ue *output.UsageError
		if !errors.As(err, &ue) {
			t.Fatalf("expected UsageError, got %T: %v", err, err)
		}
		if ue.Msg != "cannot start conversation on your own ad" {
			t.Errorf("unexpected error message: %q", ue.Msg)
		}
	})

	t.Run("own ad validation error 422", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error": "own_ad"}`))
		}))
		defer ts.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = ts.URL
		client.SetSession("valid-session")

		_, err := client.StartConversation(context.Background(), "492817294", "Hola")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var ue *output.UsageError
		if !errors.As(err, &ue) {
			t.Fatalf("expected UsageError, got %T: %v", err, err)
		}
		if ue.Msg != "cannot start conversation on your own ad" {
			t.Errorf("unexpected error message: %q", ue.Msg)
		}
	})

	t.Run("empty text validation", func(t *testing.T) {
		client := milanuncios.NewClient("test")
		client.SetSession("valid-session")

		_, err := client.StartConversation(context.Background(), "492817294", "   \t\n ")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var ue *output.UsageError
		if !errors.As(err, &ue) {
			t.Fatalf("expected UsageError, got %T: %v", err, err)
		}
		if ue.Msg != "message text cannot be empty" {
			t.Errorf("unexpected error message: %q", ue.Msg)
		}
	})

	t.Run("client_request_id header and body payload", func(t *testing.T) {
		var (
			receivedHeaderID string
			receivedBody     map[string]string
		)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaderID = r.Header.Get("X-Client-Request-Id")
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)

			resp := milanuncios.ConversationStartResult{
				ConversationID: "conv-12345",
				AdID:           "492817294",
				MessageID:      "msg-1",
				Text:           receivedBody["text"],
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer ts.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = ts.URL
		client.SetSession("valid-session")

		reqID := "550e8400-e29b-41d4-a716-446655440001"
		res, err := client.StartConversation(context.Background(), "https://www.milanuncios.com/anuncios/492817294.htm", "Oferta inicial", reqID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedHeaderID != reqID {
			t.Errorf("expected X-Client-Request-Id %q, got %q", reqID, receivedHeaderID)
		}
		if receivedBody["client_request_id"] != reqID {
			t.Errorf("expected client_request_id in body %q, got %q", reqID, receivedBody["client_request_id"])
		}
		if res.ConversationID != "conv-12345" {
			t.Errorf("unexpected conversation id: %s", res.ConversationID)
		}
	})
}

func TestWAFChallengeHTMLClassification(t *testing.T) {
	// 1. Cloudflare Turnstile interstitial returning HTTP 200 with HTML
	{
		tsTurnstile := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Just a moment...</title></head><body><div id="cf-turnstile">Checking your browser before accessing milanuncios.com</div></body></html>`))
		}))
		defer tsTurnstile.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = tsTurnstile.URL
		var onBlockCalled bool
		client.OnBlock = func(ctx context.Context) {
			onBlockCalled = true
		}

		_, err := client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error for WAF Turnstile challenge on HTTP 200, got nil")
		}
		classified := output.Classify(err, "1.0", "search")
		if classified.Code != 5 {
			t.Fatalf("expected Exit Code 5 (blocked) for WAF Turnstile challenge, got Exit Code %d (category: %s)", classified.Code, classified.Category)
		}
		if !onBlockCalled {
			t.Error("expected OnBlock callback to be triggered for WAF challenge")
		}
	}

	// 2. Datadome returning HTTP 200 with HTML challenge
	{
		tsDatadome := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><head><title>Datadome Challenge</title></head><body><script src="https://ct.datadome.co/tags.js"></script><p>Please complete captcha challenge</p></body></html>`))
		}))
		defer tsDatadome.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = tsDatadome.URL

		_, err := client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error for Datadome challenge on HTTP 200, got nil")
		}
		classified := output.Classify(err, "1.0", "search")
		if classified.Code != 5 {
			t.Fatalf("expected Exit Code 5 (blocked) for Datadome challenge, got Exit Code %d (category: %s)", classified.Code, classified.Category)
		}
	}

	// 3. API endpoint returning text/html interstitial on HTTP 200 without explicit challenge keywords
	{
		tsHTML := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>An unexpected HTML interstitial page from WAF</body></html>`))
		}))
		defer tsHTML.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = tsHTML.URL

		_, err := client.Search(context.Background(), milanuncios.SearchQuery{})
		if err == nil {
			t.Fatal("expected error for HTML interstitial on HTTP 200, got nil")
		}
		classified := output.Classify(err, "1.0", "search")
		if classified.Code != 5 {
			t.Fatalf("expected Exit Code 5 (blocked) for HTML interstitial, got Exit Code %d", classified.Code)
		}
	}

	// 4. Legitimate JSON response containing words like "challenge" must NOT be blocked
	{
		tsValid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[{"id":"123","title":"Dodge Challenger SRT","price":35000}],"total":1}`))
		}))
		defer tsValid.Close()

		client := milanuncios.NewClient("test")
		client.BaseURL = tsValid.URL

		res, err := client.Search(context.Background(), milanuncios.SearchQuery{})
		if err != nil {
			t.Fatalf("unexpected error for legitimate Dodge Challenger listing: %v", err)
		}
		if len(res.Items) != 1 || res.Items[0].Title != "Dodge Challenger SRT" {
			t.Fatalf("unexpected search result: %+v", res)
		}
	}
}

func TestProxySOCKS5AndSOCKS5h(t *testing.T) {
	client := milanuncios.NewClient("test")

	// Test socks5h URL
	uSocks5h, err := url.Parse("socks5h://user:secret123@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parsing socks5h url: %v", err)
	}
	client.UseProxy(uSocks5h)

	if client.Proxy != "socks5h://user:xxxxx@127.0.0.1:1080" {
		t.Fatalf("unexpected redacted proxy: %s", client.Proxy)
	}

	tr, ok := client.HTTP.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatal("expected http.Transport with DialContext configured for socks5h")
	}

	// Test standard socks5 URL
	uSocks5, err := url.Parse("socks5://user:secret456@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parsing socks5 url: %v", err)
	}
	client.UseProxy(uSocks5)

	tr2, ok := client.HTTP.Transport.(*http.Transport)
	if !ok || tr2.DialContext == nil {
		t.Fatal("expected http.Transport with DialContext configured for socks5")
	}
}

func TestProxyURLEncodedPasswordRedaction(t *testing.T) {
	client := milanuncios.NewClient("test")

	// Proxy with URL-encoded characters in password (e.g. P@ss word -> P%40ss%20word)
	rawProxy := "http://user:P%40ss%20word@127.0.0.1:8080"
	u, err := url.Parse(rawProxy)
	if err != nil {
		t.Fatalf("parsing proxy url: %v", err)
	}

	client.UseProxy(u)

	// Verify both decoded "P@ss word" and encoded "P%40ss%20word" are redacted
	simulatedNetErr := errors.New("connection failed to 127.0.0.1:8080 with auth user:P%40ss%20word or P@ss word or P%40ss+word")
	errMsg := fmt.Sprintf("failed connecting through proxy %s with error %s", client.Proxy, simulatedNetErr.Error())
	redactedMsg := client.RedactString(errMsg)
	if strings.Contains(redactedMsg, "P@ss word") || strings.Contains(redactedMsg, "P%40ss%20word") || strings.Contains(redactedMsg, "P%40ss+word") {
		t.Fatalf("expected password forms to be redacted, got: %s", redactedMsg)
	}
}

func TestStandardBrowserHeaders(t *testing.T) {
	var capturedHeaders http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}))
	defer ts.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL

	_, err := client.Search(context.Background(), milanuncios.SearchQuery{})
	if err != nil {
		t.Fatalf("unexpected search error: %v", err)
	}

	expectedHeaders := []string{
		"Accept-Language",
		"Sec-Ch-Ua",
		"Sec-Ch-Ua-Mobile",
		"Sec-Ch-Ua-Platform",
		"Sec-Fetch-Site",
		"Sec-Fetch-Mode",
		"Sec-Fetch-Dest",
	}

	for _, h := range expectedHeaders {
		val := capturedHeaders.Get(h)
		if val == "" {
			t.Errorf("expected header %q to be present, but was empty", h)
		}
	}

	if capturedHeaders.Get("Accept-Language") != "es-ES,es;q=0.9,en;q=0.8" {
		t.Errorf("unexpected Accept-Language: %s", capturedHeaders.Get("Accept-Language"))
	}
	if capturedHeaders.Get("Sec-Fetch-Site") != "same-origin" {
		t.Errorf("unexpected Sec-Fetch-Site: %s", capturedHeaders.Get("Sec-Fetch-Site"))
	}
	if capturedHeaders.Get("Sec-Fetch-Mode") != "cors" {
		t.Errorf("unexpected Sec-Fetch-Mode: %s", capturedHeaders.Get("Sec-Fetch-Mode"))
	}
	if capturedHeaders.Get("Sec-Fetch-Dest") != "empty" {
		t.Errorf("unexpected Sec-Fetch-Dest: %s", capturedHeaders.Get("Sec-Fetch-Dest"))
	}
}
