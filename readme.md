# AO3Fetch

[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/legowerewolf/AO3fetch?label=latest%20release&sort=semver)](https://github.com/legowerewolf/AO3fetch/releases/latest)
![GitHub release (latest by SemVer)](https://img.shields.io/github/downloads/legowerewolf/ao3fetch/latest/total?label=latest%20release%20downloads)

![GitHub commits since latest release (by date)](https://img.shields.io/github/commits-since/legowerewolf/ao3fetch/latest?label=commits%20since%20latest%20release)

Tool for scraping the work URLs off of any AO3 page. Capable of navigating the
depths of index pages. Designed for use with the
[FanFicFare](https://github.com/JimmXinu/FanFicFare) extension for Calibre.

## Notes for AO3 Maintainers

- This crawler uses the user-agent string `legowerewolf-ao3scraper/\[commit\]`.
- It rate-limits itself to 10 seconds between requests by default, although this
  is configurable. If a user requests fewer than 10 seconds between requests, it
  will throw a warning but proceed.
- It will also obey `Retry-After` headers if they are set in the response.
- I am more than happy to make changes if requested.

## Arguments

- `-url` (string) URL to start crawling from
- `-delay` (int) Delay between requests in seconds (default 10)
- `-login` (string) Login credentials in the form of username:password
- `-pages` (int) Number of pages to crawl (default 1)
- `-progress` (boolean) Show progress bar (default true, disable with
  `-progress=false`)
- `-series` (boolean) Include series in the crawl (default true, disable with
  `-series=false`)
- `-version` (boolean) Show version and exit (default false)
