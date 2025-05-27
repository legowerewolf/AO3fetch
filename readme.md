# AO3Fetch

[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/legowerewolf/AO3fetch?sort=semver&style=flat-square&label=latest%20release)![download count badge](https://img.shields.io/github/downloads/legowerewolf/ao3fetch/latest/total?sort=semver&style=flat-square&label=downloads)](https://github.com/legowerewolf/AO3fetch/releases/latest)

Tool for collecting work URLs from AO3 list views.

## What's this for?

This is the last step in a chain of tools for keeping your own personal backups
of your favorite works. Because we've all seen "This work has been deleted!" in
our bookmarks and cried.

- [Calibre](https://calibre-ebook.com/) is software for managing a personal
  ebook library. It doesn't care where they're from; any ebook files are fair
  game. Imported ebooks can be searched by title, author, user-assigned tags,
  and the full text of the book.

- [FanFicFare](https://github.com/JimmXinu/FanFicFare) is a plugin for Calibre
  that can download a work and populate its metadata when given a work URL.
  (This includes tags, which Calibre can't read out of a manually-downloaded
  work.) It can also check for updates for works that its imported.

- **This tool** takes the URL of an index page on AO3 and retrieves all the work
  URLs from it so that you can copy-paste them _en masse_ into FanFicFare.

## Installation

Windows users can install and update through WinGet, the Windows package
manager:

```shell
> winget install legowerewolf.AO3Fetch
```

Users on other platforms can download the latest release from the releases page
and use it in-place or add it to their system path.

If it's added to other package managers, and you want new releases to be
published there automatically, file a ticket and we'll see what I can put into
the release action.

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

- Arguments can be given in the form `-flag=value` (all flags) or `-flag value`
  (non-boolean flags).
- The minimum value for `-delay` is `10` seconds.
- If you set `-pages` to `-1`, it'll automatically determine the page count.
- If your `-url` includes a `page=n` query parameter, it'll start from that
  page.
- If you _don't_ want to include series in your crawl, use `-series=false`.
- It supports the official alternate URLs for the Archive:
  https://archiveofourown.gay and https://archive.transformativeworks.org.
- You cannot `-login` to an insecure `-url`.

See the
[flags package documentation](https://pkg.go.dev/flag#hdr-Command_line_flag_syntax)
for syntax details.

## Notes for AO3 Maintainers

- This tool uses the user-agent string
  `AO3Fetch/[commit] (+https://github.com/legowerewolf/AO3fetch)`.
- There is an enforced maximum request rate of 1 request per 10 seconds.
- `Retry-After` headers are obeyed.
