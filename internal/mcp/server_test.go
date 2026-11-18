package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gek0v/milanuncios-cli/internal/mcp"
	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/gek0v/milanuncios-cli/internal/watch"
)

func TestMCPServerLifecycle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/api/v1/search") {
			res := milanuncios.SearchResult{
				Items: []milanuncios.AdItem{
					{ID: "555123", Title: "Mesa de oficina", Price: 75},
				},
				Total: 1,
			}
			_ = json.NewEncoder(w).Encode(res)
			return
		}
		if strings.Contains(r.URL.Path, "/api/v1/categories/11/filters") {
			res := milanuncios.CategoryFiltersResult{
				CategoryID:   11,
				CategoryName: "Coches de segunda mano",
				Filters: []milanuncios.FilterDefinition{
					{
						Key:     "fuel",
						Label:   "Combustible",
						Type:    "select",
						Options: []string{"diesel", "gasolina", "electrico", "hibrido"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(res)
			return
		}
		if r.URL.Path == "/api/v1/categories" {
			cats := milanuncios.CategoryListResult{
				{
					ID:   10,
					Name: "Motor",
					Slug: "motor",
					Children: []milanuncios.Category{
						{ID: 11, Name: "Coches de segunda mano", Slug: "coches-segunda-mano"},
					},
				},
				{
					ID:   20,
					Name: "Bicicletas",
					Slug: "bicicletas",
					Children: []milanuncios.Category{
						{ID: 21, Name: "Gravel", Slug: "gravel"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(cats)
			return
		}
		if strings.Contains(r.URL.Path, "/api/v1/ads/555123/contact") {
			res := milanuncios.ConversationStartResult{
				ConversationID: "conv-101",
				AdID:           "555123",
				MessageID:      "msg-1",
				Text:           "Hola, ¿sigue disponible?",
				SentAt:         "2026-11-18T10:00:00Z",
			}
			_ = json.NewEncoder(w).Encode(res)
			return
		}
		if r.URL.Path == "/api/v1/conversations" {
			list := milanuncios.ConversationList{
				{
					ID:       "conv-1",
					AdID:     "555123",
					AdTitle:  "Mesa de oficina",
					UserName: "Carlos",
					Unread:   true,
				},
				{
					ID:       "conv-2",
					AdID:     "777888",
					AdTitle:  "Silla de oficina",
					UserName: "Ana",
					Unread:   false,
				},
			}
			_ = json.NewEncoder(w).Encode(list)
			return
		}
		if strings.Contains(r.URL.Path, "/api/v1/ads/555123/favorite") {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		if r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/api/v1/ads/555123") {
			res := milanuncios.AdUpdateResult{
				ID:            "555123",
				UpdatedFields: []string{"price"},
				PreviousPrice: 75,
				NewPrice:      60,
				UpdatedAt:     "2026-11-18T10:00:00Z",
			}
			_ = json.NewEncoder(w).Encode(res)
			return
		}
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/api/v1/ads/555123") {
			res := milanuncios.AdDeleteResult{
				ID:      "555123",
				Status:  "deleted",
				Reason:  "sold_milanuncios",
				Message: "Deleted successfully",
			}
			_ = json.NewEncoder(w).Encode(res)
			return
		}
		if strings.Contains(r.URL.Path, "/api/v1/ads/555123") {
			ad := milanuncios.AdItem{ID: "555123", Title: "Mesa de oficina", Price: 75}
			_ = json.NewEncoder(w).Encode(ad)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "state.db"))
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	client.BaseURL = ts.URL
	client.SetSession("mock_token_123")
	eng := watch.NewEngine(st, client)

	// Prepare mock stdin and stdout
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"milanuncios_search","arguments":{"keywords":"mesa"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"milanuncios_get_ad","arguments":{"id_or_url":"555123"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"milanuncios_category_resolve","arguments":{"query":"coches"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"milanuncios_category_resolve","arguments":{"query":"bicicleta gravel"}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"milanuncios_ad_schema","arguments":{"category_id":11}}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"milanuncios_ad_create","arguments":{"spec":{"category_id":11,"title":"Volkswagen Golf VII Advance","description":"Coche en perfecto estado con revisiones al dia y garantia.","price":12500,"location":{"province":"Madrid"},"attributes":{"fuel":"diesel"}},"dry_run":true}}}`,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"milanuncios_ad_create","arguments":{"spec":{"category_id":11,"title":"Coche","description":"Corto","price":0,"location":{"province":"Madrid"}},"dry_run":true}}}`,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"milanuncios_chat_start","arguments":{"ad_id":"555123","message":"Hola, ¿sigue disponible?"}}}`,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"milanuncios_favorite_tag","arguments":{"ad_id":"555123","tag":"oferta_enviada","note":"Ofrecidos 60€","target_price":60}}}`,
		`{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"milanuncios_chat_list","arguments":{"unread_only":true}}}`,
		`{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"milanuncios_ad_favorite","arguments":{"ad_id":"555123","action":"favorite"}}}`,
		`{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"milanuncios_ad_update","arguments":{"ad_id":"555123","patch":{"price":60}}}}`,
		`{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"milanuncios_ad_delete","arguments":{"ad_id":"555123","reason":"sold_milanuncios","feedback":"Vendido rapido"}}}`,
	}

	input := strings.Join(requests, "\n") + "\n"
	var in bytes.Buffer
	in.WriteString(input)
	var out, errOut bytes.Buffer

	srv := mcp.NewServer("0.1.0-test", client, st, eng, &in, &out, &errOut)

	if err := srv.Run(context.Background()); err != nil {
		t.Fatalf("mcp server failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 16 {
		t.Fatalf("expected at least 16 response lines, got %d (output: %s)", len(lines), out.String())
	}

	// Helper to find response by ID
	findResp := func(id float64) map[string]any {
		for _, line := range lines {
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err == nil {
				if m["id"] == id {
					return m
				}
			}
		}
		t.Fatalf("response with id %v not found", id)
		return nil
	}

	// Verify initialize (id: 1)
	resp1 := findResp(1)
	resObj := resp1["result"].(map[string]any)
	if resObj["protocolVersion"] != "2024-11-05" {
		t.Fatalf("unexpected protocol version: %v", resObj["protocolVersion"])
	}

	// Verify tools/list (id: 3)
	resp3 := findResp(3)
	toolList := resp3["result"].(map[string]any)["tools"].([]any)
	expectedTools := []string{
		"milanuncios_search",
		"milanuncios_get_ad",
		"milanuncios_categories",
		"milanuncios_category_filters",
		"milanuncios_category_resolve",
		"milanuncios_ad_schema",
		"milanuncios_ad_create",
		"milanuncios_ad_update",
		"milanuncios_ad_delete",
		"milanuncios_chat_start",
		"milanuncios_chat_list",
		"milanuncios_chat_show",
		"milanuncios_chat_send",
		"milanuncios_ad_favorite",
		"milanuncios_favorite_tag",
		"milanuncios_me",
		"milanuncios_favorites",
		"milanuncios_watch_list",
		"milanuncios_watch_check",
		"milanuncios_watch_events",
	}

	toolMap := make(map[string]map[string]any)
	for _, it := range toolList {
		tm := it.(map[string]any)
		toolMap[tm["name"].(string)] = tm
	}

	for _, expected := range expectedTools {
		tool, ok := toolMap[expected]
		if !ok {
			t.Errorf("expected tool %q not found in tools/list", expected)
			continue
		}
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema == nil {
			t.Errorf("tool %q missing inputSchema", expected)
		}
	}

	// Verify milanuncios_chat_list schema has unread_only property
	chatListDef := toolMap["milanuncios_chat_list"]
	chatSchema := chatListDef["inputSchema"].(map[string]any)
	chatProps := chatSchema["properties"].(map[string]any)
	if _, ok := chatProps["unread_only"]; !ok {
		t.Errorf("milanuncios_chat_list schema missing 'unread_only' property")
	}

	// Verify id: 6 - milanuncios_category_resolve (query: "coches")
	resp6 := findResp(6)
	content6 := resp6["result"].(map[string]any)["content"].([]any)
	text6 := content6[0].(map[string]any)["text"].(string)
	if !strings.Contains(text6, "Coches de segunda mano") || !strings.Contains(text6, "Motor") {
		t.Fatalf("expected category resolve for coches to match, got: %s", text6)
	}

	// Verify id: 7 - milanuncios_category_resolve (query: "bicicleta gravel")
	resp7 := findResp(7)
	content7 := resp7["result"].(map[string]any)["content"].([]any)
	text7 := content7[0].(map[string]any)["text"].(string)
	if !strings.Contains(text7, "Gravel") || !strings.Contains(text7, "Bicicletas") {
		t.Fatalf("expected category resolve for bicicleta gravel to match, got: %s", text7)
	}

	// Verify id: 8 - milanuncios_ad_schema (category_id: 11)
	resp8 := findResp(8)
	content8 := resp8["result"].(map[string]any)["content"].([]any)
	text8 := content8[0].(map[string]any)["text"].(string)
	if !strings.Contains(text8, "Category_11") && !strings.Contains(text8, `"category_id": 11`) {
		t.Fatalf("expected ad schema to return category 11 schema, got: %s", text8)
	}
	if !strings.Contains(text8, "diesel") {
		t.Fatalf("expected schema to include diesel enum, got: %s", text8)
	}

	// Verify id: 9 - milanuncios_ad_create dry-run valid
	resp9 := findResp(9)
	resObj9 := resp9["result"].(map[string]any)
	if isErr, ok := resObj9["isError"].(bool); ok && isErr {
		t.Fatalf("expected dry-run ad create valid to have isError=false, got true")
	}
	text9 := resObj9["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text9, `"valid": true`) {
		t.Fatalf("expected valid: true in dry-run report, got: %s", text9)
	}

	// Verify id: 10 - milanuncios_ad_create dry-run validation error
	resp10 := findResp(10)
	resObj10 := resp10["result"].(map[string]any)
	if isErr, ok := resObj10["isError"].(bool); !ok || !isErr {
		t.Fatalf("expected dry-run ad create invalid to have isError=true")
	}
	text10 := resObj10["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text10, `"valid": false`) || !strings.Contains(text10, "E_VAL_") {
		t.Fatalf("expected validation report with errors, got: %s", text10)
	}

	// Verify id: 11 - milanuncios_chat_start
	resp11 := findResp(11)
	text11 := resp11["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text11, "conv-101") {
		t.Fatalf("expected chat start to return conv-101, got: %s", text11)
	}

	// Verify id: 12 - milanuncios_favorite_tag
	resp12 := findResp(12)
	text12 := resp12["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text12, "oferta_enviada") || !strings.Contains(text12, "Ofrecidos 60€") {
		t.Fatalf("expected favorite tag to return saved annotation, got: %s", text12)
	}

	// Verify id: 13 - milanuncios_chat_list (unread_only: true)
	resp13 := findResp(13)
	text13 := resp13["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text13, "conv-1") {
		t.Fatalf("expected chat list unread_only to include conv-1, got: %s", text13)
	}
	if strings.Contains(text13, "conv-2") {
		t.Fatalf("expected chat list unread_only to exclude read conv-2, got: %s", text13)
	}

	// Verify id: 14 - milanuncios_ad_favorite
	resp14 := findResp(14)
	text14 := resp14["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text14, "favorited") {
		t.Fatalf("expected ad_favorite to return favorited status, got: %s", text14)
	}

	// Verify id: 15 - milanuncios_ad_update
	resp15 := findResp(15)
	text15 := resp15["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text15, "555123") || !strings.Contains(text15, "60") {
		t.Fatalf("expected ad_update to return updated ad, got: %s", text15)
	}

	// Verify id: 16 - milanuncios_ad_delete
	resp16 := findResp(16)
	text16 := resp16["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text16, "sold_milanuncios") {
		t.Fatalf("expected ad_delete to return deletion result, got: %s", text16)
	}
}

func TestMCPSkill(t *testing.T) {
	skill := mcp.GetSkill()
	if !strings.Contains(skill, "Milanuncios CLI Agent Guide") {
		t.Fatalf("unexpected skill content: %s", skill)
	}
	expectedTools := []string{
		"milanuncios_category_resolve",
		"milanuncios_ad_schema",
		"milanuncios_ad_create",
		"milanuncios_ad_update",
		"milanuncios_ad_delete",
		"milanuncios_chat_start",
		"milanuncios_ad_favorite",
		"milanuncios_favorite_tag",
	}
	for _, tool := range expectedTools {
		if !strings.Contains(skill, tool) {
			t.Errorf("skill missing documentation for tool: %s", tool)
		}
	}
}

func TestMCPFavoriteTagValidation(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "state.db"))
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer st.Close()

	client := milanuncios.NewClient("test")
	eng := watch.NewEngine(st, client)

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		// Invalid ad_id string
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"milanuncios_favorite_tag","arguments":{"ad_id":"invalid!@#","tag":"oferta"}}}`,
		// Valid Milanuncios full URL
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"milanuncios_favorite_tag","arguments":{"ad_id":"https://www.milanuncios.com/motor/coche-492817294.htm","tag":"seguimiento","target_price":9500}}}`,
	}

	input := strings.Join(requests, "\n") + "\n"
	var in, out, errOut bytes.Buffer
	in.WriteString(input)

	srv := mcp.NewServer("0.1.0-test", client, st, eng, &in, &out, &errOut)
	if err := srv.Run(context.Background()); err != nil {
		t.Fatalf("mcp server failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	findResp := func(id int) map[string]any {
		for _, l := range lines {
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err == nil {
				if idVal, ok := m["id"].(float64); ok && int(idVal) == id {
					return m
				}
			}
		}
		return nil
	}

	// Verify id: 2 - should return isError for invalid ad_id
	resp2 := findResp(2)
	if resp2 == nil || resp2["result"] == nil {
		t.Fatalf("expected result envelope for invalid ad_id, got: %+v", resp2)
	}
	res2Map, _ := resp2["result"].(map[string]any)
	if isErr, _ := res2Map["isError"].(bool); !isErr {
		t.Fatalf("expected isError: true for invalid ad_id, got: %+v", res2Map)
	}
	content2 := res2Map["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(content2, "invalid ad ID") {
		t.Fatalf("expected error mentioning invalid ad ID, got: %s", content2)
	}

	// Verify id: 3 - should extract 492817294 and succeed
	resp3 := findResp(3)
	if resp3 == nil || resp3["result"] == nil {
		t.Fatalf("expected result for valid URL ad_id, got: %+v", resp3)
	}
	text3 := resp3["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text3, "492817294") || !strings.Contains(text3, "seguimiento") {
		t.Fatalf("expected annotation with extracted ID 492817294, got: %s", text3)
	}
}
