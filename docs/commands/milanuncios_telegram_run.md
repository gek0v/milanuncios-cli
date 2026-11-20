## milanuncios telegram run

Run the inbound Telegram long-polling daemon in foreground

### Synopsis

Run the inbound Telegram daemon with long polling.
Handles incoming messages, enforces chat ID whitelisting, and forwards
swipe-to-reply responses back to Milanuncios conversations.

```
milanuncios telegram run [flags]
```

### Options

```
  -h, --help          help for run
      --timeout int   long polling timeout in seconds (default 30)
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

