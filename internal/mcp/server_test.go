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
	eng := watch.NewEngine(st, client)

	// Prepare mock stdin and stdout
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"milanuncios_search","arguments":{"keywords":"mesa"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"milanuncios_get_ad","arguments":{"id_or_url":"555123"}}}`,
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
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 response lines, got %d (output: %s)", len(lines), out.String())
	}

	// Verify line 1: initialize
	var resp1 map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp1); err != nil {
		t.Fatalf("unmarshaling initialize: %v", err)
	}
	resObj := resp1["result"].(map[string]any)
	if resObj["protocolVersion"] != "2024-11-05" {
		t.Fatalf("unexpected protocol version: %v", resObj["protocolVersion"])
	}

	// Verify line 3: tools/list
	var resp3 map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &resp3); err != nil {
		t.Fatalf("unmarshaling tools/list: %v", err)
	}
	toolList := resp3["result"].(map[string]any)["tools"].([]any)
	if len(toolList) < 5 {
		t.Fatalf("expected >= 5 tools, got %d", len(toolList))
	}

	// Verify line 4: search tool execution
	var resp4 map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &resp4); err != nil {
		t.Fatalf("unmarshaling search tool: %v", err)
	}
	content := resp4["result"].(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Mesa de oficina") {
		t.Fatalf("expected search output to contain 'Mesa de oficina', got: %s", text)
	}
}

func TestMCPSkill(t *testing.T) {
	skill := mcp.GetSkill()
	if !strings.Contains(skill, "Milanuncios CLI Agent Guide") {
		t.Fatalf("unexpected skill content: %s", skill)
	}
}
