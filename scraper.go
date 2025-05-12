package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/gammazero/deque"
	"github.com/legowerewolf/AO3fetch/ao3client"
	"github.com/legowerewolf/AO3fetch/buildinfo"
	"golang.org/x/net/html"
)

// global variables
var (
	isWorkMatcher, isSeriesMatcher, isSpecialMatcher *regexp.Regexp
	client                                           *ao3client.Ao3Client
)

func main() {
	// parse flags
	var (
		seedURLRaw, credentials, outputFile string
		pages, delay                        int
		includeSeries, showVersionAndQuit   bool
	)
	flag.BoolVar(&showVersionAndQuit, "version", false, "Show version information and quit.")
	flag.StringVar(&seedURLRaw, "url", "", "URL to start crawling from (including page number).")
	flag.IntVar(&pages, "pages", 1, "Number of pages to crawl. If set to -1, crawl to the end.")
	flag.BoolVar(&includeSeries, "series", true, "Crawl discovered series.")
	flag.IntVar(&delay, "delay", 10, "Delay between requests in seconds. Minimum 10s.")
	flag.StringVar(&credentials, "login", "", "Login credentials in the form of username:password")
	flag.StringVar(&outputFile, "outputFile", "", "Write collected works to file instead of standard output.")
	flag.Parse()

	// Check parameters

	if showVersionAndQuit {
		settings, err := buildinfo.GetBuildSettings()
		if err != nil {
			log.Fatal("Failed to read build info: ", err)
		}

		fmt.Printf("%s (%s:%s) built by %s %s-%s\n", (*settings)["vcs.revision.refName"], (*settings)["vcs"], (*settings)["vcs.revision.withModified"], (*settings)["GOVERSION"], (*settings)["GOOS"], (*settings)["GOARCH.withVersion"])

		return
	}

	var seedURL *url.URL
	var startPage int
	if seedURLRaw == "" {
		log.Fatal("No URL provided")
	} else {
		var err error
		seedURL, err = url.Parse(seedURLRaw)
		if err != nil {
			log.Fatal("Invalid URL provided: ", seedURLRaw)
		}

		query := seedURL.Query()
		startPage = 1
		if query.Has("page") {
			var err error
			startPage, err = strconv.Atoi(query.Get("page"))
			if err != nil {
				log.Fatal("Failed to parse start page: ", err)
			}
		}
	}

	if delay < 10 {
		log.Fatal("Delay must be greater than or equal to 10.")
	}

	var outputFileHandle *os.File
	if outputFile != "" {
		var err error
		outputFileHandle, err = os.OpenFile(outputFile, os.O_CREATE|os.O_RDWR, os.ModePerm)
		if err != nil {
			log.Fatal("Failed to open output file for writing: ", err)
		}
		defer outputFileHandle.Close()
	}

	// initialize client so we can check credentials if they're provided
	var err error
	client, err = ao3client.NewAo3Client(seedURLRaw)
	if err != nil {
		log.Fatal("AO3 client initialization failed: ", err)
	}

	if credentials != "" {
		username, pass, _ := strings.Cut(credentials, ":")

		log.Println("Logging in as " + username + "...")

		err := client.Authenticate(username, pass)
		if err != nil {
			log.Fatal("Authentication failure. Check your credentials and try again.")
		}

		log.Println("Login successful.")
	}

	if pages == -1 {
		// get number of pages from seed URL
		log.Println("Getting number of pages...")

		resp, err := client.Get(seedURL.String())
		if err != nil {
			log.Fatal("Page-counting request failed: ", err)
		}
		defer resp.Body.Close()

		highest := 0

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

			uhref, err := url.Parse(href)
			if err != nil {
				continue
			}

			query := uhref.Query()
			if query.Has("page") {
				page, err := strconv.Atoi(query.Get("page"))
				if err != nil {
					continue
				}

				if page > highest {
					highest = page
				}
			}
		}

		pages = highest - startPage + 1
		log.Printf("Discovered highest page number to be %d; number of pages given start page (%d) is %d\n", highest, startPage, pages)
	} else if pages < 1 {
		log.Fatal("Number of pages must be -1 (autodetect) or greater than 0.")
	}

	// parameters all check out, finish initializing

	// compile regexes
	isWorkMatcher = regexp.MustCompile(`/works/\d+`)
	isSeriesMatcher = regexp.MustCompile(`/series/\d+`)
	isSpecialMatcher = regexp.MustCompile(`bookmarks|comments|collections|search|tags|users|transformative|chapters|kudos|navigate|share|view_full_work`)

	// make the coordination channels, queue, and sets
	returnedWorks := make(chan string)   // relays detected work URLs back to coordinator
	returnedSeries := make(chan string)  // ditto for series
	finished := make(chan int)           // tells coordinator when a crawl is finished
	var queue deque.Deque[string]        // stores URLs to be crawled
	workSet := mapset.NewSet[string]()   // stores URLs of works that have been detected
	seriesSet := mapset.NewSet[string]() // ditto for series

	// populate queue with initial pages
	query := seedURL.Query()
	for addlPage := range pages {
		query.Set("page", strconv.Itoa(startPage+addlPage))
		seedURL.RawQuery = query.Encode()

		queue.PushBack(seedURL.String())
	}

	// initialization done, start scraping

	log.Println("Scrape parameters: ")
	fmt.Println("URL:    ", seedURL)
	fmt.Println("Pages:  ", pages)
	fmt.Println("Series?: ", includeSeries)
	fmt.Println("Delay:  ", delay)

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

			case waitTime := <-finished: // exit coordinator loop when crawl is finished
				if waitTime >= 0 { // waitTime >= 0 means we should try again later, so rotate the queue
					queue.Rotate(1)
					time.Sleep(time.Duration(waitTime) * time.Second)
				} else if waitTime == -1 { // we were successful or got a non-retryable error, so remove the URL from the queue
					queue.PopFront()

				} else if waitTime == -2 { // Fatal error while crawling: stop crawling, dump results
					queue.Clear()
				}

				crawlInProgress = false
			}
		}

		// exit immediately if queue is empty, do not wait for next rate limiter tick
		if queue.Len() == 0 {
			break
		}
	}

	log.Printf("Found %d works across %d pages. \n", workSet.Cardinality(), pages+seriesSet.Cardinality())
	fmt.Println()

	var workOutputTarget io.Writer

	if outputFileHandle != nil {
		workOutputTarget = outputFileHandle
	} else {
		workOutputTarget = log.Writer()
	}

	for url := range workSet.Iter() {
		fmt.Fprintln(workOutputTarget, url)
	}

}

func crawl(crawlUrl string, returnedWorks, returnedSeries chan string, finished chan int) {
	waitTime := -1 // default to no-retry finish
	defer func() { // always send a message when we exit
		finished <- waitTime
	}()

	// make request, handle errors
	resp, err := client.Get(crawlUrl)
	if err != nil {
		err := err.(*url.Error)

		if err.Timeout() {
			log.Println("Request timed out. Will retry later.", crawlUrl)
			waitTime = 0
			return
		}

		log.Println("Unknown error. Skipping.", err.Error(), crawlUrl)
		return
	}
	defer resp.Body.Close()
	if retryHeader := resp.Header.Get("Retry-After"); retryHeader != "" {
		if retryTime, err := strconv.Atoi(retryHeader); err == nil {
			waitTime = retryTime
		} else if retryDate, err := http.ParseTime(retryHeader); err == nil {
			waitTime = int(time.Until(retryDate).Seconds())
		} else {
			log.Printf("Server requested pause, but gave invalid time ('%s'). Aborting. Please file an issue. \n", retryHeader)
			waitTime = -2
			return
		}

		log.Printf("Server requested pause. Suspending for %d seconds. \n", waitTime)
		return
	}
	if codeClass := resp.StatusCode / 100; codeClass != 2 {
		switch codeClass {
		case 4:
			log.Println("Bad request. Skipping.", resp.StatusCode, crawlUrl)
		case 5:
			log.Println("Server error. Will retry later.", resp.StatusCode, crawlUrl)
			waitTime = 0
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
			returnedWorks <- client.ToFullURL(href)
		} else if !crawledPageIsSeries && isSeriesMatcher.MatchString(href) {
			returnedSeries <- client.ToFullURL(href)
		}

		if crawledPageIsSeries {
			for _, attr := range token.Attr {
				if attr.Key != "rel" {
					continue
				}

				if attr.Val == "next" {
					returnedSeries <- client.ToFullURL(href)
					break
				}
			}
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
