# AO3Fetch

Tool for scraping the work URLs off of any AO3 page. Capable of navigating the
depths of index pages. Designed for use with the
[FanFicFare](https://github.com/JimmXinu/FanFicFare) extension for Calibre.

## Arguments

- `-url` (string) URL to start crawling from
- `-delay` (int) Delay between requests in seconds (default 10)
- `-login` (string) Login credentials in the form of username:password
- `-pages` (int) Number of pages to crawl (default 1)
- `-progress` (boolean) Show progress bar (default true, disable with
  `-progress=false`)
- `-series` (boolean) Include series in the crawl (default true, disable with
  `-series=false`)
