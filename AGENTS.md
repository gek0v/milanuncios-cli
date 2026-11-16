# Milanuncios CLI — Agent Guidelines (`AGENTS.md`)

This guide is intended for AI coding assistants (Claude Code, Cursor, Codex, Antigravity) working on or interacting with `milanuncios-cli`.

## 1. Project Philosophy & Core Constraints

- **Dual Audience**: Designed for human developers in the terminal and autonomous AI agents via MCP (*Model Context Protocol*).
- **Strict I/O Separation**:
  - `stdout`: Pure structured data payload only (`json`, `jsonl`, `pretty`, `toon`).
  - `stderr`: Human notices, debug logs, error envelopes. Never mix logs into `stdout`.
- **Zero-CGO**: Built with pure Go (`modernc.org/sqlite`, pure TOML parser) for cross-compilation without host toolchains.
- **Semantic Exit Codes**:
  - `0`: Success
  - `1`: Generic / internal error
  - `2`: Usage / flag validation error
  - `3`: Authentication error (not logged in, invalid session)
  - `4`: Not found (ad, category, watch, sink)
  - `5`: Blocked by anti-bot / WAF challenge (triggers cooldown)
  - `6`: Network / transport failure
  - `7`: API changed (includes auto-generated GitHub issue URL)
  - `130`: Interrupted (SIGINT/SIGTERM)

## 2. Directory Structure

```
cmd/
  milanuncios/        # CLI main binary entrypoint
  docsgen/            # Automated markdown documentation generator
internal/
  cli/                # Cobra commands, flag binding, and interactive prompts
  config/             # TOML configuration and credential management in XDG paths
  mcp/                # JSON-RPC 2.0 stdio MCP server & agent installers
  milanuncios/        # Milanuncios HTTP client, anti-WAF heuristics, data models
  output/             # Multi-format printer (JSON, Pretty, TOON) and error classifier
  sink/               # Notification channels (ntfy, webhooks, exec)
  store/              # SQLite persistent store (watches, seen_items, events)
  watch/              # Diffing engine, periodic checker, and event emitter
docs/
  commands/           # Auto-generated markdown CLI reference
  milanuncios-cli/    # Architectural specifications and roadmap
  reference-wallapop/ # Technical recon from reference project
```

## 3. Autonomous Development Rules

1. **Test-Driven Workflows**:
   - Always run `go test -v ./...` before submitting or committing code.
   - Code must pass `gofmt` and `go vet ./...`.
2. **Deterministic Documentation**:
   - Run `go run ./cmd/docsgen` whenever Cobra flags, commands, or descriptions change.
3. **Environment Isolation in Tests**:
   - Use `t.TempDir()`.
   - Override `MILANUNCIOS_CONFIG`, `MILANUNCIOS_CREDENTIALS_FILE`, and `MILANUNCIOS_STATE_DB` using `t.Setenv(...)`.
4. **Proxy Safety**:
   - Credentials and proxy passwords must never be logged or echoed in error envelopes.
