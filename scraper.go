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

	"github.com/andybalholm/cascadia"
	"github.com/cheggaaa/pb/v3"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/gammazero/deque"
	"github.com/legowerewolf/AO3fetch/ao3client"
	"github.com/legowerewolf/AO3fetch/buildinfo"
	"golang.org/x/net/html"
)

// global variables
var (
	isWorkMatcher, isSeriesMatcher, isSpecialMatcher *regexp.Regexp
	linkSelector, paginationSelector                 cascadia.Sel
	client                                           *ao3client.Ao3Client
)

func main() {
	var err error

	// compile regexes
	isWorkMatcher = regexp.MustCompile(`/works/\d+`)
	isSeriesMatcher = regexp.MustCompile(`/series/\d+`)
	isSpecialMatcher = regexp.MustCompile(`bookmarks|comments|collections|search|tags|users|transformative|chapters|kudos|navigate|share|view_full_work`)

	linkSelector, err = cascadia.Parse("a")
	if err != nil {
		panic(err)
	}

	paginationSelector, err = cascadia.Parse(".pagination a")
	if err != nil {
		panic(err)
	}

	// parse flags
	var (
		seedURLRaw, credentials, outputFile             string
		pages, delay                                    int
		includeSeries, showProgress, showVersionAndQuit bool
	)
	flag.BoolVar(&showVersionAndQuit, "version", false, "Show version information and quit.")
	flag.StringVar(&seedURLRaw, "url", "", "URL to start crawling from (including page number).")
	flag.IntVar(&pages, "pages", 1, "Number of pages to crawl. If set to -1, crawl to the end.")
	flag.BoolVar(&includeSeries, "series", true, "Crawl discovered series.")
	flag.IntVar(&delay, "delay", 10, "Delay between requests in seconds. Minimum 10s.")
	flag.BoolVar(&showProgress, "progress", true, "Show progress bar.")
	flag.StringVar(&credentials, "login", "", "Login credentials in the form of username:password")
	flag.StringVar(&outputFile, "outputFile", "", "Write collected works to file instead of standard output.")
	flag.Parse()

	// Check parameters

	if showVersionAndQuit {
		settings, err := buildinfo.GetBuildSettings()
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("%s:%s built by %s %s-%s\n", (*settings)["vcs"], (*settings)["vcs.revision.withModified"], (*settings)["GOVERSION"], (*settings)["GOOS"], (*settings)["GOARCH.withVersion"])

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
			log.Fatal("Invalid URL provided")
		}

		query := seedURL.Query()
		startPage = 1
		if query.Has("page") {
			var err error
			startPage, err = strconv.Atoi(query.Get("page"))
			if err != nil {
				log.Fatal(err)
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
			log.Fatal(err)
		}
		defer outputFileHandle.Close()
	}

	// initialize client so we can check credentials if they're provided
	client, err = ao3client.NewAo3Client()
	if err != nil {
		log.Fatal(err)
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
			log.Fatal(err)
		}
		defer resp.Body.Close()

		document, err := html.Parse(resp.Body)
		if err != nil {
			log.Fatal(err)
		}

		highest, err := getHighestPage(document)
		if err != nil {
			log.Fatal(err)
		}

		pages = highest - startPage + 1
		log.Printf("Discovered highest page number to be %d; number of pages given start page (%d) is %d\n", highest, startPage, pages)
	} else if pages < 1 {
		log.Fatal("Number of pages must be -1, autodetect, or greater than 0.")
	}

	// parameters all check out, finish initializing

	// make the coordination channels, queue, and sets
	returnedWorks := make(chan string)        // relays detected work URLs back to coordinator
	returnedSeries := make(chan string)       // ditto for series
	finished := make(chan int)                // tells coordinator when a crawl is finished
	queue := deque.New[string](pages)         // stores URLs to be crawled
	queuedPagesSet := mapset.NewSet[string]() // stores URLs that have been added to the queue
	workSet := mapset.NewSet[string]()        // stores URLs of works that have been detected

	// initialization done, start scraping

	log.Println("Scrape parameters:")
	fmt.Println("URL:    ", seedURL)
	fmt.Println("Pages:  ", pages)
	fmt.Println("Series?:", includeSeries)
	fmt.Println("Delay:  ", delay)

	// populate queue
	for _, page := range generatePageList(seedURL, startPage, startPage+pages-1) {
		dedupedEnque(queue, queuedPagesSet, page)
	}

	// set up and start progress bar
	bar := pb.New(pages)
	bar.SetTemplateString(`{{counters .}} {{bar . " " ("█" | green) ("█" | green) ("█" | white) " "}} {{percent .}}`)
	bar.SetTotal(int64(queuedPagesSet.Cardinality()))
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

				dedupedEnque(queue, queuedPagesSet, series)
				bar.SetTotal(int64(queuedPagesSet.Cardinality()))
			case waitTime := <-finished: // exit coordinator loop when crawl is finished
				if waitTime >= 0 { // waitTime >= 0 means we should try again later, so rotate the queue
					queue.Rotate(1)
					time.Sleep(time.Duration(waitTime) * time.Second)
				} else if waitTime == -1 { // we were successful or got a non-retryable error, so remove the URL from the queue
					queue.PopFront()
					bar.Increment()
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

	bar.Finish()

	log.Printf("Found %d works across %d pages. \n", workSet.Cardinality(), pages+queuedPagesSet.Cardinality())
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
	resp, err := client.Get(toFullURL(crawlUrl))
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

	document, err := html.Parse(resp.Body)
	if err != nil {
		panic("failed to parse")
	}

	nodeList := cascadia.QueryAll(document, linkSelector)

	for _, node := range nodeList {

		href, err := getAttr(node.Attr, "href")
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

		if crawledPageIsSeries {
			highestPage, err := getHighestPage(document)
			if err != nil {
				continue
			}

			u_crawledUrl, err := url.Parse(crawlUrl)
			if err != nil {
				continue
			}

			for _, page := range generatePageList(u_crawledUrl, 1, highestPage) {
				returnedSeries <- page
			}
		}
	}
}

func getAttr(attrList []html.Attribute, targetAttr string) (string, error) {
	for _, a := range attrList {
		if a.Key == targetAttr {
			return a.Val, nil
		}
	}
	return "", errors.New("target attribute not found")
}

func getHighestPage(document *html.Node) (int, error) {

	paginationLinks := cascadia.QueryAll(document, paginationSelector)

	highest := -1

	for _, link := range paginationLinks {

		href, err := getAttr(link.Attr, "href")
		if err != nil {
			continue
		}

		url_, err := url.Parse(href)
		if err != nil {
			continue
		}

		if pgnum := url_.Query().Get("page"); pgnum != "" {
			pgnum_p, err := strconv.Atoi(pgnum)
			if err != nil {
				continue
			}

			highest = max(highest, pgnum_p)
		}
	}

	return highest, nil
}

func dedupedEnque[T comparable](queue *deque.Deque[T], checkSet mapset.Set[T], item T) {
	if checkSet.Add(item) {
		queue.PushBack(item)
	}
}

func generatePageList(seedURL *url.URL, lowest, highest int) (result []string) {
	query := seedURL.Query()
	for addlPage := lowest; addlPage <= highest; addlPage++ {
		query.Set("page", strconv.Itoa(addlPage))
		seedURL.RawQuery = query.Encode()

		result = append(result, seedURL.String())
	}
	return
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
