package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
	"github.com/gek0v/milanuncios-cli/internal/store"
	"github.com/gek0v/milanuncios-cli/internal/watch"
)

// Server implements Model Context Protocol (MCP) JSON-RPC 2.0 stdio server.
type Server struct {
	Version string
	Client  *milanuncios.Client
	Store   *store.Store
	Engine  *watch.Engine

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// NewServer creates a new MCP server.
func NewServer(version string, client *milanuncios.Client, st *store.Store, eng *watch.Engine, in io.Reader, out, err io.Writer) *Server {
	return &Server{
		Version: version,
		Client:  client,
		Store:   st,
		Engine:  eng,
		stdin:   in,
		stdout:  out,
		stderr:  err,
	}
}

// JSON-RPC 2.0 types
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcErr `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type toolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Run starts the stdio JSON-RPC processing loop.
func (s *Server) Run(ctx context.Context) error {
	reader := bufio.NewReader(s.stdin)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			continue
		}

		var req request
		if err := json.Unmarshal([]byte(trimmed), &req); err != nil {
			s.sendError(nil, -32700, "Parse error")
			continue
		}

		s.handleRequest(ctx, &req)
	}
}

func (s *Server) handleRequest(ctx context.Context, req *request) {
	// JSON-RPC 2.0 §4.1: Notifications MUST NOT be replied to
	if req.ID == nil {
		return
	}
	if req.Method == "" || req.JSONRPC != "2.0" {
		s.sendError(req.ID, -32600, "Invalid Request")
		return
	}

	switch req.Method {
	case "initialize":
		res := map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "milanuncios-cli",
				"version": s.Version,
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		}
		s.sendResult(req.ID, res)

	case "notifications/initialized":
		// No response required for notification

	case "ping":
		s.sendResult(req.ID, map[string]any{})

	case "tools/list":
		s.sendResult(req.ID, map[string]any{
			"tools": s.getToolDefinitions(),
		})

	case "tools/call":
		var callParams struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			s.sendError(req.ID, -32602, "Invalid params: "+err.Error())
			return
		}

		callRes, err := s.executeTool(ctx, callParams.Name, callParams.Arguments)
		if err != nil {
			s.sendResult(req.ID, toolCallResult{
				Content: []contentItem{
					{Type: "text", Text: "Error: " + err.Error()},
				},
				IsError: true,
			})
			return
		}

		s.sendResult(req.ID, callRes)

	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *Server) sendResult(id any, result any) {
	resp := response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintf(s.stdout, "%s\n", string(b))
}

func (s *Server) sendError(id any, code int, msg string) {
	resp := response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcErr{
			Code:    code,
			Message: msg,
		},
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintf(s.stdout, "%s\n", string(b))
}

func (s *Server) getToolDefinitions() []toolInfo {
	return []toolInfo{
		{
			Name:        "milanuncios_search",
			Description: "Search for listings on Milanuncios with optional filters for price, location, category, and sorting.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keywords": map[string]any{
						"type":        "string",
						"description": "Keywords to query on Milanuncios (e.g. 'bicicleta carretera', 'playstation 5')",
					},
					"min_price": map[string]any{
						"type":        "number",
						"description": "Minimum price filter in EUR",
					},
					"max_price": map[string]any{
						"type":        "number",
						"description": "Maximum price filter in EUR",
					},
					"province": map[string]any{
						"type":        "string",
						"description": "Province filter (e.g. 'Madrid', 'Barcelona', 'Valencia')",
					},
					"category": map[string]any{
						"type":        "integer",
						"description": "Category ID filter",
					},
					"sort": map[string]any{
						"type":        "string",
						"description": "Sorting order: 'newest', 'relevance', 'price_asc', 'price_desc'",
					},
				},
				"required": []string{"keywords"},
			},
		},
		{
			Name:        "milanuncios_get_ad",
			Description: "Fetch detailed information of a listing by its ad ID or Milanuncios URL.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id_or_url": map[string]any{
						"type":        "string",
						"description": "Listing numeric ID or full web URL",
					},
				},
				"required": []string{"id_or_url"},
			},
		},
		{
			Name:        "milanuncios_categories",
			Description: "List top-level categories and subcategories on Milanuncios.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "milanuncios_category_filters",
			Description: "Get dynamic filter definitions available for a given category ID.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category_id": map[string]any{
						"type":        "integer",
						"description": "Category ID to inspect",
					},
				},
				"required": []string{"category_id"},
			},
		},
		{
			Name:        "milanuncios_category_resolve",
			Description: "Find numeric category IDs and canonical category paths by natural language query (e.g. 'coches', 'bicicleta gravel', 'portátiles').",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language category query (e.g. 'coches', 'bicicleta gravel', 'portátiles')",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "milanuncios_ad_schema",
			Description: "Retrieve the formal JSON Schema Draft-07 for a category, detailing required fields, attribute enums, numeric ranges, and photo constraints.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category_id": map[string]any{
						"type":        "integer",
						"description": "Numeric Milanuncios category ID",
					},
				},
				"required": []string{"category_id"},
			},
		},
		{
			Name:        "milanuncios_ad_create",
			Description: "Create a classified ad on Milanuncios or simulate creation using dry_run=true. Always run with dry_run=true first to inspect any attribute or schema validation errors before publishing.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"spec": map[string]any{
						"type":        "object",
						"description": "Full declarative AdSpec object matching milanuncios_ad_schema",
					},
					"dry_run": map[string]any{
						"type":        "boolean",
						"default":     true,
						"description": "If true, simulates validation without creating the listing (default true)",
					},
				},
				"required": []string{"spec"},
			},
		},
		{
			Name:        "milanuncios_ad_update",
			Description: "Update an existing listing using a partial patch object (RFC 7396). Supports changing price, description, title, or attributes.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ad_id": map[string]any{
						"type":        "string",
						"description": "Listing ID to update",
					},
					"patch": map[string]any{
						"type":        "object",
						"description": "Partial patch object containing fields to update (RFC 7396)",
					},
				},
				"required": []string{"ad_id", "patch"},
			},
		},
		{
			Name:        "milanuncios_ad_delete",
			Description: "Delete or withdraw an active listing with a structured reason.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ad_id": map[string]any{
						"type":        "string",
						"description": "Listing ID to delete",
					},
					"reason": map[string]any{
						"type":        "string",
						"enum":        []string{"sold_milanuncios", "sold_other", "withdrawn", "expired"},
						"description": "Reason for deletion (sold_milanuncios, sold_other, withdrawn, expired)",
					},
					"feedback": map[string]any{
						"type":        "string",
						"description": "Optional closing remarks or feedback",
					},
				},
				"required": []string{"ad_id", "reason"},
			},
		},
		{
			Name:        "milanuncios_ad_favorite",
			Description: "Add or remove an ad listing from account favorites.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ad_id": map[string]any{
						"type":        "string",
						"description": "Listing ID or URL to favorite or unfavorite",
					},
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"favorite", "unfavorite"},
						"default":     "favorite",
						"description": "Action: 'favorite' to add, 'unfavorite' to remove (default: 'favorite')",
					},
				},
				"required": []string{"ad_id"},
			},
		},
		{
			Name:        "milanuncios_favorite_tag",
			Description: "Save private local notes, tags, or target purchase price on a listing for autonomous agent decision-making.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ad_id": map[string]any{
						"type":        "string",
						"description": "Listing numeric ID or URL to annotate",
					},
					"tag": map[string]any{
						"type":        "string",
						"description": "Tag label (e.g. 'oferta_enviada', 'evaluar', 'interesante')",
					},
					"note": map[string]any{
						"type":        "string",
						"description": "Optional private agent notes or remarks",
					},
					"target_price": map[string]any{
						"type":        "number",
						"description": "Optional target purchase price in EUR",
					},
				},
				"required": []string{"ad_id", "tag"},
			},
		},
		{
			Name:        "milanuncios_me",
			Description: "Retrieve account profile of the authenticated user.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "milanuncios_favorites",
			Description: "List saved favorite ads of the authenticated user.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "milanuncios_watch_list",
			Description: "List all local persistent watches monitoring searches or listings.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "milanuncios_watch_check",
			Description: "Trigger an immediate check on a specific watch or all active watches.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"watch": map[string]any{
						"type":        "string",
						"description": "Optional watch ID or name. Omit to check all active watches.",
					},
				},
			},
		},
		{
			Name:        "milanuncios_watch_events",
			Description: "Query recent events recorded by the watch engine (new ads, price drops, removals).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"watch_id": map[string]any{
						"type":        "integer",
						"description": "Optional filter by watch ID",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of events to return (default 20, max 100)",
					},
				},
			},
		},
		{
			Name:        "milanuncios_chat_start",
			Description: "Initiate a chat conversation with the seller of a Milanuncios listing. Returns conversation ID.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ad_id": map[string]any{
						"type":        "string",
						"description": "Listing ID or URL to contact",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Initial text message to send to the seller",
					},
				},
				"required": []string{"ad_id", "message"},
			},
		},
		{
			Name:        "milanuncios_chat_list",
			Description: "List active chat conversations for the authenticated user.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"unread_only": map[string]any{
						"type":        "boolean",
						"default":     false,
						"description": "Filter conversations to only include unread threads (default false)",
					},
				},
			},
		},
		{
			Name:        "milanuncios_chat_show",
			Description: "View full message history for a specific conversation ID.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"conversation_id": map[string]any{
						"type":        "string",
						"description": "Conversation thread ID",
					},
				},
				"required": []string{"conversation_id"},
			},
		},
		{
			Name:        "milanuncios_chat_send",
			Description: "Send a text message in an existing Milanuncios conversation.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"conversation_id": map[string]any{
						"type":        "string",
						"description": "Conversation thread ID",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Text message content to send",
					},
				},
				"required": []string{"conversation_id", "text"},
			},
		},
	}
}

func (s *Server) executeTool(ctx context.Context, name string, args map[string]any) (*toolCallResult, error) {
	switch name {
	case "milanuncios_search":
		keywords, _ := args["keywords"].(string)
		if keywords == "" {
			return nil, fmt.Errorf("keywords is required")
		}

		q := milanuncios.SearchQuery{Keywords: keywords}
		if minP, ok := args["min_price"].(float64); ok {
			q.MinPrice = minP
		}
		if maxP, ok := args["max_price"].(float64); ok {
			q.MaxPrice = maxP
		}
		if prov, ok := args["province"].(string); ok {
			q.Province = prov
		}
		if cat, ok := args["category"].(float64); ok {
			q.Category = int(cat)
		}
		if sort, ok := args["sort"].(string); ok {
			q.Sort = sort
		}

		res, err := s.Client.Search(ctx, q)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(res, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_get_ad":
		target := normalizeString(args["id_or_url"])
		if target == "" {
			return nil, fmt.Errorf("id_or_url is required")
		}

		ad, err := s.Client.GetAd(ctx, target)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(ad, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_categories":
		cats, err := s.Client.ListCategories(ctx)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(cats, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_category_filters":
		catID := parseCategoryInt(args["category_id"])
		if catID <= 0 {
			return nil, fmt.Errorf("category_id must be a positive integer")
		}

		filters, err := s.Client.GetCategoryFilters(ctx, catID)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(filters, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_category_resolve":
		query, _ := args["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}

		cats, err := s.Client.ListCategories(ctx)
		if err != nil {
			return nil, err
		}

		matches := resolveCategories(cats, query)
		b, _ := json.MarshalIndent(matches, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_ad_schema":
		catID := parseCategoryInt(args["category_id"])
		if catID <= 0 {
			return nil, fmt.Errorf("category_id must be a positive integer")
		}

		schema, err := s.Client.GetCategorySchema(ctx, catID)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(schema, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_ad_create":
		specRaw, ok := args["spec"]
		if !ok || specRaw == nil {
			return nil, fmt.Errorf("spec is required")
		}
		specBytes, err := json.Marshal(specRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid spec object: %w", err)
		}
		var spec milanuncios.AdSpec
		if err := json.Unmarshal(specBytes, &spec); err != nil {
			return nil, fmt.Errorf("parsing spec: %w", err)
		}

		dryRun := true
		if val, ok := args["dry_run"]; ok {
			switch v := val.(type) {
			case bool:
				dryRun = v
			case string:
				if strings.ToLower(v) == "false" || v == "0" {
					dryRun = false
				}
			}
		}

		if dryRun {
			report, err := s.Client.ValidateAdSpec(ctx, &spec)
			if err != nil {
				return nil, err
			}
			b, _ := json.MarshalIndent(report, "", "  ")
			return &toolCallResult{
				Content: []contentItem{{Type: "text", Text: string(b)}},
				IsError: !report.Valid,
			}, nil
		}

		// Real creation: validate first so any validation failure returns machine-readable ValidationReport
		report, err := s.Client.ValidateAdSpec(ctx, &spec)
		if err != nil {
			return nil, err
		}
		if !report.Valid {
			b, _ := json.MarshalIndent(report, "", "  ")
			return &toolCallResult{
				Content: []contentItem{{Type: "text", Text: string(b)}},
				IsError: true,
			}, nil
		}

		item, err := s.Client.CreateAd(ctx, &spec)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(item, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_ad_update":
		adID := normalizeString(args["ad_id"])
		if adID == "" {
			return nil, fmt.Errorf("ad_id is required")
		}
		patch, ok := args["patch"].(map[string]any)
		if !ok || patch == nil {
			return nil, fmt.Errorf("patch is required")
		}
		res, err := s.Client.UpdateAd(ctx, adID, patch)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_ad_delete":
		adID := normalizeString(args["ad_id"])
		if adID == "" {
			return nil, fmt.Errorf("ad_id is required")
		}
		reason, _ := args["reason"].(string)
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return nil, fmt.Errorf("reason is required")
		}
		feedback, _ := args["feedback"].(string)
		res, err := s.Client.DeleteAd(ctx, adID, reason, feedback)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_ad_favorite":
		adID := normalizeString(args["ad_id"])
		if adID == "" {
			return nil, fmt.Errorf("ad_id is required")
		}
		action := "favorite"
		if act, ok := args["action"].(string); ok && strings.TrimSpace(act) != "" {
			action = strings.ToLower(strings.TrimSpace(act))
		}
		var err error
		switch action {
		case "favorite":
			err = s.Client.Favorite(ctx, adID)
		case "unfavorite":
			err = s.Client.Unfavorite(ctx, adID)
		default:
			return nil, fmt.Errorf("invalid action %q (allowed: 'favorite', 'unfavorite')", action)
		}
		if err != nil {
			return nil, err
		}
		res := map[string]string{
			"status": action + "d",
			"ad_id":  adID,
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_favorite_tag":
		if s.Store == nil {
			return nil, fmt.Errorf("store not configured")
		}
		rawAdID := normalizeString(args["ad_id"])
		if rawAdID == "" {
			return nil, fmt.Errorf("ad_id is required")
		}
		adID, err := milanuncios.ExtractAdID(rawAdID)
		if err != nil {
			return nil, fmt.Errorf("invalid ad ID or URL: %w", err)
		}
		tag, _ := args["tag"].(string)
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return nil, fmt.Errorf("tag is required")
		}
		note, _ := args["note"].(string)
		var targetPrice float64
		if tp, ok := args["target_price"].(float64); ok {
			targetPrice = tp
		} else if sVal, ok := args["target_price"].(string); ok {
			targetPrice, _ = strconv.ParseFloat(sVal, 64)
		}
		if err := s.Store.SaveAnnotation(ctx, adID, tag, note, targetPrice); err != nil {
			return nil, err
		}
		ann, err := s.Store.GetAnnotation(ctx, adID)
		if err != nil {
			res := map[string]any{
				"ad_id":        adID,
				"tag":          tag,
				"note":         note,
				"target_price": targetPrice,
			}
			b, _ := json.MarshalIndent(res, "", "  ")
			return &toolCallResult{
				Content: []contentItem{{Type: "text", Text: string(b)}},
			}, nil
		}
		b, _ := json.MarshalIndent(ann, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_me":
		me, err := s.Client.Me(ctx)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(me, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_favorites":
		favs, err := s.Client.MyFavorites(ctx)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(favs, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_watch_list":
		if s.Store == nil {
			return nil, fmt.Errorf("watch store not configured")
		}

		list, err := s.Store.ListWatches(ctx)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(list, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_watch_check":
		if s.Engine == nil {
			return nil, fmt.Errorf("watch engine not configured")
		}

		target := normalizeString(args["watch"])
		if target != "" {
			w, err := s.Store.GetWatch(ctx, target)
			if err != nil {
				return nil, err
			}
			res, err := s.Engine.CheckWatch(ctx, w)
			if err != nil {
				return nil, err
			}
			b, _ := json.MarshalIndent(res, "", "  ")
			return &toolCallResult{
				Content: []contentItem{{Type: "text", Text: string(b)}},
			}, nil
		}

		allRes, err := s.Engine.CheckAll(ctx)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(allRes, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_watch_events":
		if s.Store == nil {
			return nil, fmt.Errorf("watch store not configured")
		}

		var watchID int64
		if v, ok := args["watch_id"].(float64); ok {
			watchID = int64(v)
		}
		limit := 20
		if v, ok := args["limit"].(float64); ok {
			limit = int(v)
		}
		if limit <= 0 || limit > 100 {
			limit = 50
		}

		events, err := s.Store.ListEvents(ctx, watchID, limit)
		if err != nil {
			return nil, err
		}

		b, _ := json.MarshalIndent(events, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_chat_start":
		adID := normalizeString(args["ad_id"])
		if adID == "" {
			return nil, fmt.Errorf("ad_id is required")
		}
		msg, _ := args["message"].(string)
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return nil, fmt.Errorf("message is required")
		}
		res, err := s.Client.StartConversation(ctx, adID, msg)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(res, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_chat_list":
		unreadOnly := false
		if v, ok := args["unread_only"].(bool); ok {
			unreadOnly = v
		} else if sVal, ok := args["unread_only"].(string); ok {
			if strings.ToLower(sVal) == "true" || sVal == "1" {
				unreadOnly = true
			}
		}

		list, err := s.Client.ListConversations(ctx)
		if err != nil {
			return nil, err
		}

		if unreadOnly {
			filtered := make(milanuncios.ConversationList, 0)
			for _, c := range list {
				if c.Unread {
					filtered = append(filtered, c)
				}
			}
			list = filtered
		}

		b, _ := json.MarshalIndent(list, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_chat_show":
		convID, _ := args["conversation_id"].(string)
		if convID == "" {
			return nil, fmt.Errorf("conversation_id is required")
		}
		details, err := s.Client.GetConversation(ctx, convID)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(details, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	case "milanuncios_chat_send":
		convID, _ := args["conversation_id"].(string)
		text, _ := args["text"].(string)
		if convID == "" || text == "" {
			return nil, fmt.Errorf("conversation_id and text are required")
		}
		msg, err := s.Client.SendMessage(ctx, convID, text)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(msg, "", "  ")
		return &toolCallResult{
			Content: []contentItem{{Type: "text", Text: string(b)}},
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func normalizeString(v any) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		return strconv.FormatInt(int64(val), 10)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case json.Number:
		return val.String()
	default:
		return ""
	}
}

func parseCategoryInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(val))
		return n
	case json.Number:
		n, _ := strconv.Atoi(val.String())
		return n
	default:
		return 0
	}
}

// CategoryResolvedMatch represents a category search result with canonical path.
type CategoryResolvedMatch struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Slug string `json:"slug,omitempty"`
}

func resolveCategories(cats []milanuncios.Category, query string) []CategoryResolvedMatch {
	qLower := strings.ToLower(strings.TrimSpace(query))
	words := strings.Fields(qLower)
	matches := make([]CategoryResolvedMatch, 0)

	var traverse func(cat milanuncios.Category, parentPath string)
	traverse = func(cat milanuncios.Category, parentPath string) {
		currentPath := cat.Name
		if parentPath != "" {
			currentPath = parentPath + " > " + cat.Name
		}

		nameLower := strings.ToLower(cat.Name)
		pathLower := strings.ToLower(currentPath)
		slugLower := strings.ToLower(cat.Slug)

		matched := false
		if strings.Contains(nameLower, qLower) || strings.Contains(pathLower, qLower) || strings.Contains(slugLower, qLower) {
			matched = true
		} else if strconv.Itoa(cat.ID) == qLower {
			matched = true
		} else if len(words) > 1 {
			allWords := true
			for _, w := range words {
				if !strings.Contains(pathLower, w) && !strings.Contains(slugLower, w) {
					allWords = false
					break
				}
			}
			if allWords {
				matched = true
			}
		}

		if matched {
			matches = append(matches, CategoryResolvedMatch{
				ID:   cat.ID,
				Name: cat.Name,
				Path: currentPath,
				Slug: cat.Slug,
			})
		}

		for _, child := range cat.Children {
			traverse(child, currentPath)
		}
	}

	for _, c := range cats {
		traverse(c, "")
	}

	return matches
}
