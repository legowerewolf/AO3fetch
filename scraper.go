package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gopkg.in/cheggaaa/pb.v1"
)

func main() {
	var seedURLRaw string
	var pages int
	var includeSeries bool
	var delay int
	var showProgress bool
	var credentials string

	// parse flags
	flag.StringVar(&seedURLRaw, "url", "", "URL to start crawling from")
	flag.IntVar(&pages, "pages", 1, "Number of pages to crawl")
	flag.BoolVar(&includeSeries, "series", true, "Include series in the crawl")
	flag.IntVar(&delay, "delay", 10, "Delay between requests")
	flag.BoolVar(&showProgress, "progress", true, "Show progress bar")
	flag.StringVar(&credentials, "login", "", "Login credentials in the form of username:password")

	flag.Parse()

	// Check parameters

	var seedURL *url.URL
	if seedURLRaw == "" {
		log.Fatal("No URL provided")
	} else {
		var err error
		seedURL, err = url.Parse(seedURLRaw)
		if err != nil {
			log.Fatal("Invalid URL provided")
		}
	}

	if pages < 1 {
		log.Fatal("Number of pages must be greater than 0")
	}

	if delay < 0 {
		log.Fatal("Delay must be greater than or equal to 0")
	} else if delay < 10 {
		log.Println("Warning: Delay is less than 10 seconds. This may cause your IP to be blocked by the server.")
	}

	if credentials != "" {
		s := strings.Split(credentials, ":")
		err := login(s[0], s[1])
		if err != nil {
			log.Fatal("Authentication failure. Check your credentials and try again.")
		}
	}

	log.Println("Running at", seedURL.String(), "across", strconv.Itoa(pages), "pages with a", strconv.Itoa(delay), "second delay and series set to", strconv.FormatBool(includeSeries))

	// Channels
	chworks := make(chan string)
	chseries := make(chan string)
	finished := make(chan bool)

	// Crawl the listing - with pagination
	var bar *pb.ProgressBar
	if showProgress {
		bar = pb.StartNew(pages)
	}
	for page := 1; page <= pages; page++ {

		query := seedURL.Query()
		query.Set("page", strconv.Itoa(page))
		seedURL.RawQuery = query.Encode()

		go crawl(seedURL.String(), chworks, chseries, finished)
		if showProgress {
			bar.Increment()
		}
		if page < pages {
			time.Sleep(time.Duration(delay) * time.Second)
		}
	}
	if showProgress {
		bar.FinishPrint("Waiting for requests to return...")
	}

	foundWorks := make(map[string]bool)
	foundSeries := make(map[string]bool)

	// Get works and series
	for c, d := 0, 0; c < pages+d; {
		select {
		case url := <-chworks:
			foundWorks[url] = true
		case url := <-chseries:
			if foundSeries[url] || !includeSeries {
				continue
			} else {
				foundSeries[url] = true
				go crawl(url, chworks, chseries, finished)
				d++
			}
		case <-finished:
			c++
		}
	}

	fmt.Println("\nFound", len(foundWorks), "works across", pages, "pages and", len(foundSeries), "series:\n ")
	for url := range foundWorks {
		fmt.Println(toFullURL(url))
	}
}

func getHref(t html.Token) (ok bool, href string) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			href = a.Val
			ok = true
		}
	}
	return
}

func crawl(url string, works chan string, series chan string, chFinished chan bool) {
	req, err := http.NewRequest("GET", toFullURL(url), nil)

	resp, err := http.DefaultClient.Do(req)

	defer func() {
		chFinished <- true
	}()

	if err != nil {
		fmt.Println("ERROR: Failed to crawl \"" + url + "\"")
		return
	}

	z := html.NewTokenizer(resp.Body)

	for {
		tt := z.Next()

		switch {
		case tt == html.ErrorToken:
			// End of the document, we're done
			return
		case tt == html.StartTagToken:
			t := z.Token()

			if t.Data != "a" {
				continue
			}

			ok, url := getHref(t)
			if !ok {
				continue
			}

			isWork, _ := regexp.MatchString("/works/\\d+", url)
			isSeries, _ := regexp.MatchString("/series/\\d+", url)
			isSpecial, _ := regexp.MatchString("bookmarks|comments|collections|search|tags|users|transformative|chapters", url)
			if isWork && !isSpecial {
				works <- url
			}
			if isSeries && !isSpecial {
				series <- url
			}

		}
	}
}

func toFullURL(url string) string {
	isFullURL, _ := regexp.MatchString("http", url)
	if !isFullURL {
		url = "https://archiveofourown.org" + url
	}
	return url
}
