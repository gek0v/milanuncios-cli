## milanuncios search

Search listings on Milanuncios

### Synopsis

Search through listings on Milanuncios with filters.

Results are printed as JSON by default on stdout (ideal for jq),
or formatted in a clean table with --format pretty.

```
milanuncios search [keywords] [flags]
```

### Options

```
      --category int         filter by category numeric ID
      --filter stringArray   category-specific filter in key=value format (repeatable)
  -h, --help                 help for search
      --limit int            maximum number of results per page (default 20)
      --max-price float      maximum price in EUR
      --min-price float      minimum price in EUR
      --next string          pagination cursor for the next page
      --province string      filter by Spanish province (e.g. Madrid, Barcelona)
      --sort string          sort order: relevance, newest, price_asc, price_desc (default "relevance")
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

* [milanuncios](milanuncios.md)	 - Terminal client for the Milanuncios marketplace
* [milanuncios search filters](milanuncios_search_filters.md)	 - List available attribute filters for a given category

