# Contributing to milanuncios-cli

Thank you for your interest in contributing to `milanuncios-cli`! This project is designed for both human terminal usage and autonomous AI coding agents via MCP (*Model Context Protocol*).

## Development Philosophy & Invariants

1. **Dual Audience**: Ergonomic human CLI and strict machine-readable contracts for AI agents.
2. **Strict I/O Separation**:
   - `stdout`: Pure structured data payload only (`json`, `jsonl`, `pretty`, `toon`).
   - `stderr`: Human notices, warnings, debug info, error envelopes. Never mix logs into `stdout`.
3. **Zero-CGO**: Built with pure Go (`modernc.org/sqlite`, pure TOML parser) for seamless cross-compilation without host toolchains.
4. **Deterministic Documentation**: Whenever flags, commands, or descriptions change, run `make docs` (or `go run ./cmd/docsgen`) to regenerate markdown references in `docs/commands/`.

## Prerequisites

- Go 1.25+ installed (`go version`).
- Git.

## Building and Testing

```bash
# Clone repository
git clone https://github.com/gek0v/milanuncios-cli.git
cd milanuncios-cli

# Run test suite
make test
# or: go test -v ./... -count=1

# Check formatting and static analysis
make fmt
make vet
make check

# Build binary
make build
# outputs to bin/milanuncios
```

## Pull Request Guidelines

1. **Small Vertical Slices**: We prefer small, cohesive, well-tested pull requests that deliver a clear vertical slice of functionality.
2. **Test Coverage**: Every new feature or bug fix must be accompanied by automated tests.
3. **Environment Isolation in Tests**:
   - Use `t.TempDir()`.
   - Override environment variables (`MILANUNCIOS_CONFIG`, `MILANUNCIOS_CREDENTIALS_FILE`, `MILANUNCIOS_STATE_DB`) using `t.Setenv(...)`.
4. **Semantic Exit Codes**:
   - `0`: Success
   - `1`: Generic / internal error
   - `2`: Usage / flag validation error
   - `3`: Authentication error
   - `4`: Not found (ad, category, watch, sink)
   - `5`: Blocked by anti-bot / WAF challenge
   - `6`: Network / transport failure
   - `7`: API changed
   - `130`: Interrupted (SIGINT/SIGTERM)

## AI Agents & Automation

If you are an AI assistant (Claude Code, Cursor, Codex, Antigravity) working on this codebase, refer to [`AGENTS.md`](./AGENTS.md) for domain model definitions and operational constraints.
