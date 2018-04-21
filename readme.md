# AO3Fetch

Tool for scraping the work URLs off of any paginated view.

## Arguments

* **-url** [URL] - The base URL you want to crawl.
* **-pages** [integer] - The number of pages of that URL you want to crawl.
* -delay [integer] - the time to wait between starting page crawls, in seconds. Be nice to the servers and set this to a reasonable time, or leave it at the default 10 seconds.
* -noseries - Will prevent the crawler from crawling series that it encounters, which it does by default.
* -noprogress - Prevents the tool from showing a progress bar. Intended for instances where you're writing to a file.
* -login [username:password] - Logs the tool in to AO3 using your account. Used for crawling works and bookmarks hidden from non-users.