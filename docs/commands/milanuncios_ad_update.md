## milanuncios ad update

Update an existing ad listing using RFC 7396 JSON merge patch

### Synopsis

Update an existing ad listing with partial patch object.

Supports inline patch (--spec '{"price": 1200}'), patch file (--patch-file <path> or - for stdin),
or shortcut flags (--price, --title, --description).

```
milanuncios ad update <id> [flags]
```

### Options

```
      --description string   new ad description
  -h, --help                 help for update
      --patch-file string    path to JSON patch file or '-' for stdin
      --price float          new price in Euros
      --spec string          inline JSON patch object
      --title string         new ad title
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

* [milanuncios ad](milanuncios_ad.md)	 - Inspect and interact with Milanuncios ads

