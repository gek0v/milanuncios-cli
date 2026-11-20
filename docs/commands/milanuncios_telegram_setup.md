## milanuncios telegram setup

Configure Telegram bot token and allowed chat ID

### Synopsis

Configure Telegram bot credentials and chat whitelist.
Validates the token with Telegram Bot API, saves the bot token in credentials.toml,
saves the chat ID in config.toml, and sends a confirmation test message.

```
milanuncios telegram setup [flags]
```

### Options

```
      --chat-id int    Telegram chat ID to whitelist and notify
  -h, --help           help for setup
      --token string   Telegram bot API token
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

* [milanuncios telegram](milanuncios_telegram.md)	 - Manage Telegram bot and inbound message daemon

