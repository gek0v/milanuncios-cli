## milanuncios ad delete

Delete or withdraw an active ad listing

### Synopsis

Withdraw or remove an active ad listing with a structured reason.

Valid reasons:
  - sold_milanuncios: Item sold through Milanuncios
  - sold_other: Item sold via another platform or offline
  - withdrawn: Listing withdrawn by seller (default)
  - expired: Listing is no longer available or valid

```
milanuncios ad delete <id> [flags]
```

### Options

```
      --feedback string   optional closing feedback or comments
  -h, --help              help for delete
  -r, --reason string     reason for deletion (sold_milanuncios, sold_other, withdrawn, expired) (default "withdrawn")
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

