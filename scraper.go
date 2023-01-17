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
	"github.com/gammazero/deque"
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

		log.Println("Login successful.")
	}

	// parameters all check out, finish initializing

	// compile regexes
	isWorkMatcher = regexp.MustCompile(`/works/\d+`)
	isSeriesMatcher = regexp.MustCompile(`/series/\d+`)
	isSpecialMatcher = regexp.MustCompile(`bookmarks|comments|collections|search|tags|users|transformative|chapters|kudos`)

	// make the coordination channels, queue, and sets
	returned_works := make(chan string)   // relays detected work URLs back to coordinator
	returned_series := make(chan string)  // ditto for series
	finished := make(chan bool)           // tell coordinator when a crawl is finished
	queue := deque.New[string](pages)     // stores URLs to be crawled
	works_set := mapset.NewSet[string]()  // stores URLs of works that have been detected
	series_set := mapset.NewSet[string]() // ditto for series

	// initialization done, start scraping

	log.Println("Scrape parameters:")
	fmt.Println("URL:    ", seedURL)
	fmt.Println("Pages:  ", pages)
	fmt.Println("Series?:", includeSeries)
	fmt.Println("Delay:  ", delay)

	// populate queue
	query := seedURL.Query()
	for page := 1; page <= pages; page++ {
		query.Set("page", strconv.Itoa(page))
		seedURL.RawQuery = query.Encode()

		queue.PushBack(seedURL.String())
	}

	// set up and start progress bar
	bar := pb.New(pages)
	bar.SetTemplateString(`{{counters .}} {{bar . " " ("█" | green) ("█" | green) ("█" | white) " "}} {{percent .}} {{rtime .}}`)
	if showProgress {
		bar.Start()
	}

	for rlimiter := time.Tick(time.Duration(delay) * time.Second); queue.Len() > 0; <-rlimiter {
		go crawl(queue.PopFront(), returned_works, returned_series, finished)

		// "the coordinator"
		for crawl_in_progress := true; crawl_in_progress; {
			select {
			case work := <-returned_works: // save detected works
				works_set.Add(work)
			case series := <-returned_series: // save detected series, add unique series to queue
				if !includeSeries {
					continue
				}

				if series_set.Contains(series) {
					continue
				}

				series_set.Add(series)
				queue.PushBack(series)
				bar.SetTotal(int64(pages + series_set.Cardinality()))
			case <-finished: // exit coordinator loop when crawl is finished
				crawl_in_progress = false
			}
		}

		bar.Increment()

		// exit immediately if queue is empty
		if queue.Len() == 0 {
			break
		}
	}

	bar.Finish()

	fmt.Println("\nFound", works_set.Cardinality(), "works across", pages, "pages and", series_set.Cardinality(), "series:\n ")

	// iterate over works_set
	for url := range works_set.Iter() {
		fmt.Println(url)
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
