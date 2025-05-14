# AO3Fetch

[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/legowerewolf/AO3fetch?label=latest%20release&sort=semver)](https://github.com/legowerewolf/AO3fetch/releases/latest)
![GitHub release (latest by SemVer)](https://img.shields.io/github/downloads/legowerewolf/ao3fetch/latest/total?label=latest%20release%20downloads)

![GitHub commits since latest release (by date)](https://img.shields.io/github/commits-since/legowerewolf/ao3fetch/latest?label=commits%20since%20latest%20release)

Tool for scraping the work URLs off of any AO3 page. Capable of navigating the
depths of index pages. Designed for use with the
[FanFicFare](https://github.com/JimmXinu/FanFicFare) extension for Calibre.

## Notes for AO3 Maintainers

- This crawler uses the user-agent string `legowerewolf-ao3scraper/[commit]`.
- The crawler enforces a maximum request rate of 1 request per 10 seconds.
- It will also obey `Retry-After` headers if they are set in the response.
- I am more than happy to make changes if requested.

## Arguments

Also available with the `-help` flag, or when run with no arguments.

- `-url` (string) URL to start crawling from
- `-pages` (int) Number of pages to crawl (default 1)
- `-series` (boolean) Include series in the crawl (default true, disable with
  `-series=false`)
- `-outputFile` (string) Write the list of collected works to a file instead of
  the terminal output.
- `-login` (string) Login credentials in the form of username:password
- `-delay` (int) Delay between requests in seconds (default 10)
- `-version` (boolean) Show version and exit (default false)
