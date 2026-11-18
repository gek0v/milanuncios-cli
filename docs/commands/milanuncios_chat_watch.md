## milanuncios chat watch

Monitor chat conversations for incoming unread messages

```
milanuncios chat watch [flags]
```

### Options

```
  -h, --help              help for watch
      --interval string   polling interval (e.g. 30s, 1m; min: 10s) (default "30s")
      --once              run single check cycle and exit immediately
      --unread-only       only check conversations marked as unread (default true)
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

* [milanuncios chat](milanuncios_chat.md)	 - View and send messages on Milanuncios

