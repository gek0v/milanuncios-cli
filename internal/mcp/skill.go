package mcp

const MilanunciosSkill = `# Milanuncios CLI Agent Guide

Autonomous terminal client and MCP integration for the Milanuncios marketplace.

## Available MCP Tools

When connected as an MCP server, ` + "`milanuncios-cli`" + ` exposes:
- ` + "`milanuncios_search`" + `: Search listings with keywords, min/max price, province, category, sort.
- ` + "`milanuncios_get_ad`" + `: Fetch comprehensive ad details by ID or URL.
- ` + "`milanuncios_categories`" + `: List main categories and subcategories taxonomy.
- ` + "`milanuncios_category_filters`" + `: List specific dynamic filters for a category.
- ` + "`milanuncios_me`" + `: Fetch authenticated user account info.
- ` + "`milanuncios_favorites`" + `: List user's saved favorite ads.
- ` + "`milanuncios_watch_list`" + `: Inspect persistent local monitors.
- ` + "`milanuncios_watch_check`" + `: Run a check cycle on active monitors.
- ` + "`milanuncios_watch_events`" + `: Query recorded changes (new ads, price drops).

## Direct CLI Usage (Zero Shell Hassle)

All commands produce clean JSON by default on ` + "`stdout`" + ` with logs and errors isolated to ` + "`stderr`" + `:

` + "```bash" + `
# Search listings
milanuncios search "bicicleta carretera" --min-price 300 --max-price 800 --province Valencia

# Detailed ad inspection
milanuncios ad show 492817294

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
