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
	returnedWorks := make(chan string)   // relays detected work URLs back to coordinator
	returnedSeries := make(chan string)  // ditto for series
	finished := make(chan bool)          // tell coordinator when a crawl is finished
	queue := deque.New[string](pages)    // stores URLs to be crawled
	workSet := mapset.NewSet[string]()   // stores URLs of works that have been detected
	seriesSet := mapset.NewSet[string]() // ditto for series

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

	for rateLimiter := time.Tick(time.Duration(delay) * time.Second); queue.Len() > 0; <-rateLimiter {
		go crawl(queue.Front(), returnedWorks, returnedSeries, finished)

		// "the coordinator"
		for crawlInProgress := true; crawlInProgress; {
			select {
			case work := <-returnedWorks: // save detected works
				workSet.Add(work)
			case series := <-returnedSeries: // save detected series, add unique series to queue
				if !includeSeries {
					continue
				}

				if seriesSet.Contains(series) {
					continue
				}

				seriesSet.Add(series)
				queue.PushBack(series)
				bar.SetTotal(int64(pages + seriesSet.Cardinality()))
			case shouldRetry := <-finished: // exit coordinator loop when crawl is finished
				if shouldRetry {
					queue.Rotate(1)
				} else {
					queue.PopFront()
					bar.Increment()
				}

				crawlInProgress = false
			}
		}

		// exit immediately if queue is empty
		if queue.Len() == 0 {
			break
		}
	}

	bar.Finish()

	log.Println("Found", workSet.Cardinality(), "works across", pages, "pages and", seriesSet.Cardinality(), "series.")
	fmt.Println()

	// iterate over works_set
	for url := range workSet.Iter() {
		fmt.Println(url)
	}
}

func crawl(crawlUrl string, returnedWorks, returnedSeries chan string, finished chan bool) {
	shouldRetry := false
	defer func() {
		finished <- shouldRetry
	}()

	// make request, handle errors
	resp, err := http.DefaultClient.Get(toFullURL(crawlUrl))
	if err != nil {
		err := err.(*url.Error)

		if err.Timeout() {
			log.Println("Request timed out. Will retry later.", crawlUrl)
			shouldRetry = true
			return
		}

		log.Println("Unknown error. Skipping.", err.Error(), crawlUrl)
		return
	}
	defer resp.Body.Close()
	if codeClass := resp.StatusCode / 100; codeClass != 2 {
		switch codeClass {
		case 4:
			log.Println("Bad request. Skipping.", resp.StatusCode, crawlUrl)
		case 5:
			log.Println("Server error. Will retry later.", resp.StatusCode, crawlUrl)
			shouldRetry = true
		default:
			log.Println("Unknown error. Skipping.", resp.StatusCode, crawlUrl)
		}
		return
	}

	crawledPageIsSeries := isSeriesMatcher.MatchString(crawlUrl)

	tokenizer := html.NewTokenizer(resp.Body)
	for tt := tokenizer.Next(); tt != html.ErrorToken; tt = tokenizer.Next() {
		token := tokenizer.Token()

		if !(token.Type == html.StartTagToken && token.Data == "a") {
			continue
		}

		href, err := getHref(token)
		if err != nil {
			continue
		}

		if isSpecialMatcher.MatchString(href) {
			continue
		}

		if isWorkMatcher.MatchString(href) {
			returnedWorks <- toFullURL(href)
		} else if !crawledPageIsSeries && isSeriesMatcher.MatchString(href) {
			returnedSeries <- toFullURL(href)
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
