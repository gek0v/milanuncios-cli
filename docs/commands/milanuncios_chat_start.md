## milanuncios chat start

Start a conversation with the seller of an ad

```
milanuncios chat start <ad-id|url> [message] [flags]
```

### Options

```
      --client-request-id string   UUIDv4 idempotency key
      --from-stdin                 Read message from stdin
  -h, --help                       help for start
  -m, --message string             Text message to send
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

