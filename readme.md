# AO3Fetch

[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/legowerewolf/AO3fetch?sort=semver&style=flat-square&label=latest%20release)![download count badge](https://img.shields.io/github/downloads/legowerewolf/ao3fetch/latest/total?sort=semver&style=flat-square&label=downloads)](https://github.com/legowerewolf/AO3fetch/releases/latest)

Tool for collecting work URLs from AO3 list views. Capable of navigating the
depths of index pages. Designed for use with the
[FanFicFare](https://github.com/JimmXinu/FanFicFare) extension for
[Calibre](https://calibre-ebook.com/).

## Arguments

Also available with the `-help` flag, or when run with no arguments.

```
  -delay int
        Delay between requests in seconds. (default 10)
  -login string
        Login credentials in the form of username:password.
  -outputFile string
        Filename to write collected work URLs to instead of standard output.
  -pages int
        Number of pages to crawl. (default 1)
  -series
        Discover and crawl series. (default true)
  -url string
        URL to start crawling from.
  -version
        Show version information and quit.
```

### Errata

- Values can be given in the form `-flag=value` (all flags) or `-flag value`
  (non-boolean flags).
- The minimum value for `-delay` is `10` seconds. Thou shalt not crash the
  Archive.
- If you set `-pages` to `-1`, it'll automatically determine the page count.
- If your `-url` includes a `page=n` parameter, it'll start from that page.
- If you _don't_ want to include series in your crawl, use `-series=false`.

See the
[flags package documentation](https://pkg.go.dev/flag#hdr-Command_line_flag_syntax)
for syntax details.

## Notes for AO3 Maintainers

- This crawler uses the user-agent string
  `AO3Fetch/[commit] (+https://github.com/legowerewolf/AO3fetch)`.
- The crawler enforces a maximum request rate of 1 request per 10 seconds.
- It will obey `Retry-After` headers if they are set in the response.
