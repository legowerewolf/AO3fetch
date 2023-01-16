package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cheggaaa/pb/v3"
	mapset "github.com/deckarep/golang-set/v2"
	"golang.org/x/net/html"
)

var isWorkMatcher, isSeriesMatcher, isSpecialMatcher *regexp.Regexp

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
		username, pass, _ := strings.Cut(credentials, ":")

		log.Println("Logging in as " + username + "...")

		err := login(username, pass)
		if err != nil {
			log.Fatal("Authentication failure. Check your credentials and try again.")
		}
	}

	// Finish initialization
	isWorkMatcher = regexp.MustCompile(`/works/\d+`)
	isSeriesMatcher = regexp.MustCompile(`/series/\d+`)
	isSpecialMatcher = regexp.MustCompile(`bookmarks|comments|collections|search|tags|users|transformative|chapters|kudos`)

	// Start processing pages

	log.Println("Printing scrape parameters:")
	fmt.Println("URL:    ", seedURL)
	fmt.Println("Pages:  ", pages)
	fmt.Println("Series?:", includeSeries)
	fmt.Println("Delay:  ", delay)

	// make and populate queue
	queue := make(chan string, 10*pages)
	for page := 1; page <= pages; page++ {
		query := seedURL.Query()
		query.Set("page", strconv.Itoa(page))
		seedURL.RawQuery = query.Encode()

		queue <- seedURL.String()
	}
	log.Println("Loaded queue with", pages, "page(s)")

	returned_works := make(chan string)
	returned_series := make(chan string)
	finished := make(chan bool)

	go crawl_queue(queue, delay, returned_works, returned_series, finished)

	bar := pb.New(pages)
	bar.SetTemplateString(`{{counters .}} {{bar . " " ("█" | green) ("█" | green) ("█" | white) " "}} {{percent .}} {{rtime .}}`)
	if showProgress {
		bar.Start()
	}

	works_set := mapset.NewSet[string]()
	series_set := mapset.NewSet[string]()

	// Get works and series
	for crawled, addlPages := 0, 0; crawled < pages+addlPages; {
		select {
		case url := <-returned_works:
			works_set.Add(url)
		case url := <-returned_series:
			if !includeSeries {
				continue
			}

			if series_set.Contains(url) {
				continue
			}

			series_set.Add(url)
			queue <- url
			addlPages++
			bar.SetTotal(int64(pages + addlPages))
		case <-finished:
			crawled++
			bar.Increment()
		}
	}

	bar.Finish()

	fmt.Println("\nFound", works_set.Cardinality(), "works across", pages, "pages and", series_set.Cardinality(), "series:\n ")

	// iterate over works_set
	for url := range works_set.Iter() {
		fmt.Println(url)
	}
}

func crawl_queue(queue chan string, delay int, returned_works, returned_series chan string, finished chan bool) {
	for url := range queue {
		go crawl(url, returned_works, returned_series, finished)
		time.Sleep(time.Duration(delay) * time.Second)
	}
}

func crawl(url string, returned_works, returned_series chan string, finished chan bool) {
	defer sendt(finished)

	req, err := http.NewRequest("GET", toFullURL(url), nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		log.Println("ERROR: Failed to crawl \"" + url + "\"")
		return
	}

	z := html.NewTokenizer(resp.Body)

	for tt := z.Next(); tt != html.ErrorToken; tt = z.Next() {
		if tt != html.StartTagToken {
			continue
		}

		t := z.Token()

		if t.Data != "a" {
			continue
		}

		href, err := getHref(t)
		if err != nil {
			continue
		}

		isWork := isWorkMatcher.MatchString(href)
		isSeries := isSeriesMatcher.MatchString(href)
		isSpecial := isSpecialMatcher.MatchString(href)

		if isWork && !isSpecial {
			returned_works <- toFullURL(href)
		}
		if isSeries && !isSpecial && !isSeriesMatcher.MatchString(url) {
			returned_series <- toFullURL(href)
		}

	}

}

func getHref(t html.Token) (string, error) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			return a.Val, nil
		}
	}
	return "", errors.New("no href attribute found")
}

func toFullURL(url_ string) string {
	url, err := url.Parse(url_)
	if err != nil {
		log.Fatal(err)
	}

	url.Scheme = "https"
	url.Host = "archiveofourown.org"

	return url.String()
}

func sendt(c chan bool) {
	c <- true
}
