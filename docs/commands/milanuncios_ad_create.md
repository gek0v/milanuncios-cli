## milanuncios ad create

Validate or publish an ad listing from declarative specification

### Synopsis

Create or validate an ad listing using a declarative AdSpec JSON document.

Supports file input (--from-file <path>), stdin (--from-file -), or inline payload (--spec '<json>').
By default, --dry-run is true to simulate validation without creating the listing.

```
milanuncios ad create [flags]
```

### Options

```
      --dry-run            simulate validation without creating listing (default true)
      --from-file string   path to ad spec JSON file or '-' for stdin
  -h, --help               help for create
      --spec string        inline ad spec JSON payload
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

