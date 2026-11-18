package mcp

const MilanunciosSkill = `# Milanuncios CLI Agent Guide

Autonomous terminal client and MCP integration for the Milanuncios marketplace.

## Available MCP Tools

When connected as an MCP server, ` + "`milanuncios-cli`" + ` exposes:
- ` + "`milanuncios_search`" + `: Search listings with keywords, min/max price, province, category, sort.
- ` + "`milanuncios_get_ad`" + `: Fetch comprehensive ad details by ID or URL.
- ` + "`milanuncios_categories`" + `: List main categories and subcategories taxonomy.
- ` + "`milanuncios_category_filters`" + `: List specific dynamic filters for a category.
- ` + "`milanuncios_category_resolve`" + `: Find numeric category IDs and canonical category paths by natural language query (e.g. 'coches', 'bicicleta gravel', 'portátiles').
- ` + "`milanuncios_ad_schema`" + `: Retrieve formal JSON Schema Draft-07 for a category detailing required fields, attribute enums, numeric ranges, and photo constraints.
- ` + "`milanuncios_ad_create`" + `: Create a classified ad or simulate creation using dry_run=true. Always run with dry_run=true first to inspect any attribute or schema validation errors before publishing.
- ` + "`milanuncios_ad_update`" + `: Update an existing listing using a partial patch object (RFC 7396). Supports changing price, description, title, or attributes.
- ` + "`milanuncios_ad_delete`" + `: Delete or withdraw an active listing with a structured reason (sold_milanuncios, sold_other, withdrawn, expired).
- ` + "`milanuncios_chat_start`" + `: Initiate a chat conversation with the seller of a Milanuncios listing. Returns conversation ID.
- ` + "`milanuncios_chat_list`" + `: List active chat conversations (supports unread_only filtering).
- ` + "`milanuncios_chat_show`" + `: View full message history for a specific conversation ID.
- ` + "`milanuncios_chat_send`" + `: Send a text message reply in an existing conversation.
- ` + "`milanuncios_ad_favorite`" + `: Add or remove an ad listing from account favorites.
- ` + "`milanuncios_favorite_tag`" + `: Save private local notes, tags, or target purchase price on a listing for autonomous agent decision-making.
- ` + "`milanuncios_me`" + `: Fetch authenticated user account info.
- ` + "`milanuncios_favorites`" + `: List user's saved favorite ads.
- ` + "`milanuncios_watch_list`" + `: Inspect persistent local monitors.
- ` + "`milanuncios_watch_check`" + `: Run a check cycle on active monitors.
- ` + "`milanuncios_watch_events`" + `: Query recorded changes (new ads, price drops).

## Agent Best Practices: Declarative & Self-Healing Pattern

Autonomous agents interacting with Milanuncios should follow this standard declarative pattern:

1. **Resolve Category ID & Path**:
   Use ` + "`milanuncios_category_resolve`" + ` with a natural language query (e.g. "bicicleta gravel") to locate the category ID and canonical hierarchy.

2. **Introspect Formal JSON Schema**:
   Call ` + "`milanuncios_ad_schema`" + ` with the resolved ` + "`category_id`" + `. Inspect required fields, allowed enum values, numerical ranges, and photo constraints. Never guess attribute names.

3. **Validate in Dry-Run Mode First (` + "`dry_run=true`" + `)**:
   Always test your ` + "`AdSpec`" + ` object with ` + "`milanuncios_ad_create`" + ` keeping ` + "`dry_run: true`" + `.
   - If validation fails (` + "`isError: true`" + `), parse the returned ` + "`ValidationReport`" + `.
   - Each error provides ` + "`field`" + `, ` + "`code`" + `, ` + "`expected`" + `, ` + "`allowed_values`" + `, and an actionable ` + "`suggestion`" + `.
   - Apply suggestions directly to fix schema violations in a single self-healing turn.
   - Re-run ` + "`milanuncios_ad_create`" + ` with ` + "`dry_run: true`" + ` until ` + "`valid: true`" + `.

4. **Publish & Lifecycle**:
   - Publish live by calling ` + "`milanuncios_ad_create`" + ` with ` + "`dry_run: false`" + `.
   - Modify price, title, or attributes using RFC 7396 partial patches via ` + "`milanuncios_ad_update`" + `.
   - Close or withdraw listings cleanly via ` + "`milanuncios_ad_delete`" + ` with a structured reason (e.g. ` + "`sold_milanuncios`" + `).

5. **Conversations & Local Annotations**:
   - Start seller chats using ` + "`milanuncios_chat_start`" + ` and monitor unread threads with ` + "`milanuncios_chat_list(unread_only=true)`" + `.
   - Maintain private local records of negotiations, target purchase prices, and evaluation tags using ` + "`milanuncios_favorite_tag`" + ` without modifying remote state.

## Direct CLI Usage (Zero Shell Hassle)

All commands produce clean JSON by default on ` + "`stdout`" + ` with logs and errors isolated to ` + "`stderr`" + `:

` + "```bash" + `
# Search listings
milanuncios search "bicicleta carretera" --min-price 300 --max-price 800 --province Valencia

# Category resolution & schema inspection
milanuncios category list --format json
milanuncios ad schema --category 11

# Declarative ad publishing with dry-run
milanuncios ad create --from-file ./ad_spec.json --dry-run
milanuncios ad create --spec '{"category_id": 11, "title": "...", "price": 1200}'

# Chat and negotiation
milanuncios chat start 492817294 --message "Hola, ¿sigue disponible?"
milanuncios chat list --unread-only

# Local agent annotations
milanuncios favorite tag 492817294 "oferta_enviada" --note "Ofrecido 120€" --target-price 120

# Add local watch
milanuncios watch add search "macbook air m1" --name macbooks --interval 5m
milanuncios watch check macbooks
milanuncios watch events --watch 1
` + "```" + `
`

// GetSkill returns the agent guide for Milanuncios.
func GetSkill() string {
	return MilanunciosSkill
}
