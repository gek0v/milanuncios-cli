## milanuncios watch add search

Watch a search query for new listings and price changes

```
milanuncios watch add search <keywords> [flags]
```

### Options

```
      --category int      filter by category ID
  -h, --help              help for search
      --interval string   check interval (e.g. 5m, 300s, 1h) (default "5m")
      --max-price float   maximum price in EUR
      --min-price float   minimum price in EUR
      --name string       friendly name for this watch
      --province string   filter by province
      --sort string       sorting: newest, relevance, price_asc, price_desc (default "newest")
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

* [milanuncios watch add](milanuncios_watch_add.md)	 - Add a search query or ad listing to monitoring

