## milanuncios mcp

Model Context Protocol (MCP) JSON-RPC 2.0 server for AI Agents

### Synopsis

Starts the stdio MCP server exposing Milanuncios tools to AI Agents (Claude Code, Cursor, Codex).
Each line over stdin is a JSON-RPC 2.0 request; responses are emitted line-by-line over stdout.

```
milanuncios mcp [flags]
```

### Options

```
  -h, --help   help for mcp
```

### Options inherited from parent commands

```
      --debug                 log every http request to stderr (secrets redacted)
      --error-format string   error rendering on stderr: text or json (default "text")
      --format string         output format: json, jsonl, pretty, toon (default "json")
      --no-color              disable colour in pretty output (also NO_COLOR)
      --no-input              never prompt; fail when input is missing
      --profile string        profile to act as (env MILANUNCIOS_PROFILE)
```

### SEE ALSO

* [milanuncios](milanuncios.md)	 - Terminal client for the Milanuncios marketplace
* [milanuncios mcp install](milanuncios_mcp_install.md)	 - Configure an AI coding assistant environment with milanuncios MCP server

