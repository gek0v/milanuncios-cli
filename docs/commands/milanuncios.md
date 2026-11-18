## milanuncios

Terminal client for the Milanuncios marketplace

### Synopsis

milanuncios is an unofficial terminal client for Milanuncios.

Built for both human terminal usage and autonomous AI Agents (MCP integration).
Output is JSON on stdout by default so every command pipes cleanly into jq.
Use --format pretty for human-friendly tabular output.
Errors go to stderr; exit codes follow the semantic contract documented in docs/.

```
milanuncios [flags]
```

### Options

```
      --debug                 log every http request to stderr (secrets redacted)
      --error-format string   error rendering on stderr: text or json (default "text")
      --format string         output format: json, jsonl, pretty, toon (default "json")
  -h, --help                  help for milanuncios
      --no-color              disable colour in pretty output (also NO_COLOR)
      --no-input              never prompt; fail when input is missing
      --profile string        profile to act as (env MILANUNCIOS_PROFILE)
```

### SEE ALSO

* [milanuncios ad](milanuncios_ad.md)	 - Inspect and interact with Milanuncios ads
* [milanuncios auth](milanuncios_auth.md)	 - Manage authentication and sessions
* [milanuncios category](milanuncios_category.md)	 - Browse Milanuncios category taxonomy
* [milanuncios chat](milanuncios_chat.md)	 - View and send messages on Milanuncios
* [milanuncios config](milanuncios_config.md)	 - Inspect and modify configuration settings
* [milanuncios favorite](milanuncios_favorite.md)	 - Manage local agent favorite annotations and notes
* [milanuncios mcp](milanuncios_mcp.md)	 - Model Context Protocol (MCP) JSON-RPC 2.0 server for AI Agents
* [milanuncios me](milanuncios_me.md)	 - Manage own account, listings and favorites
* [milanuncios profile](milanuncios_profile.md)	 - Manage multiple Milanuncios accounts side by side
* [milanuncios search](milanuncios_search.md)	 - Search listings on Milanuncios
* [milanuncios sink](milanuncios_sink.md)	 - Manage and test notification sinks (ntfy, webhooks, exec)
* [milanuncios skills](milanuncios_skills.md)	 - Export documentation and prompt guidelines for AI Agents
* [milanuncios watch](milanuncios_watch.md)	 - Monitor searches and listings with local SQLite engine

