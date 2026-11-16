## milanuncios watch run

Start daemon runner checking active watches periodically

```
milanuncios watch run [flags]
```

### Options

```
  -h, --help              help for run
      --interval string   polling interval (e.g. 5m, 300s, 1h) (default "5m")
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

* [milanuncios watch](milanuncios_watch.md)	 - Monitor searches and listings with local SQLite engine

