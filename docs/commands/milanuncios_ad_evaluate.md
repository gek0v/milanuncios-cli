## milanuncios ad evaluate

Evaluate the fair market price and calculate Deal Score for an ad

### Synopsis

Evaluate the fair market price and calculate the Deal Score (0-100) of an ad listing.

Uses robust non-parametric statistical benchmarks (IQR outlier cleaning and median comparison)
to detect real bargains without AI hallucinations.

```
milanuncios ad evaluate <id|url> [flags]
```

### Options

```
  -h, --help                  help for evaluate
      --include-comparables   include top comparable listings in the output
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

