# milanuncios-cli (npm package)

Official npm wrapper for [`milanuncios-cli`](https://github.com/gek0v/milanuncios-cli).

This package downloads and executes the appropriate precompiled Go binary for your platform (Linux, macOS, Windows on AMD64 & ARM64).

## Quick Start

### Run with `npx` (No installation needed)
```bash
npx milanuncios-cli search "bicicleta" --format pretty
```

### Install globally
```bash
npm install -g milanuncios-cli
milanuncios search "macbook" --max-price 500
```

### Connect to AI Agents via MCP
```bash
milanuncios mcp install claude-code
milanuncios mcp install cursor
```

## Documentation

Full documentation, guides, and source code are available at:
👉 **[github.com/gek0v/milanuncios-cli](https://github.com/gek0v/milanuncios-cli)**

## License

MIT License. See [LICENSE](https://github.com/gek0v/milanuncios-cli/blob/main/LICENSE).
