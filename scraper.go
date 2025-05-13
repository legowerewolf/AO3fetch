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

	"golang.org/x/net/html"

	tea "github.com/charmbracelet/bubbletea"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/gammazero/deque"

	"github.com/legowerewolf/AO3fetch/ao3client"
	"github.com/legowerewolf/AO3fetch/buildinfo"
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
	fmt.Println("URL:     ", seedURL)
	fmt.Println("Pages:   ", pages)
	fmt.Println("Series?: ", includeSeries)
	fmt.Println("Delay:   ", delay)

	p := tea.NewProgram(runtimeModel{
		includeSeries: includeSeries,
		delay:         delay,

		queue:     queue,
		workSet:   workSet,
		seriesSet: seriesSet,
	}, tea.WithAltScreen())

	r, err := p.Run()
	if err != nil {
		log.Fatal("Tea program quit: ", err)
	}

	log.Printf("Found %d works across %d pages. \n", workSet.Cardinality(), r.(runtimeModel).pagesCrawled)
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

type runtimeModel struct {
	includeSeries bool
	delay         int

	queue     deque.Deque[string]
	workSet   mapset.Set[string]
	seriesSet mapset.Set[string]

	secsToNextCrawl int
	pagesCrawled    int
}

func (m runtimeModel) View() string {
	return fmt.Sprintf("Countdown time: %d\nWorks: %d\nPages: %d\nQueue length: %d\nTotal: %d", m.secsToNextCrawl, m.workSet.Cardinality(), m.pagesCrawled, m.queue.Len(), m.pagesCrawled+m.queue.Len())
}

type tickMsg struct{}

type crawlResponseMsg struct {
	CrawlUrl string
	Success  bool

	// fail fields
	Retryable bool
	Fatal     bool
	ErrMsg    string
	WaitFor   int // seconds

	// success fields
	AddWorks  []string
	AddSeries []string
}

func (m runtimeModel) Init() tea.Cmd {

	return func() tea.Msg {
		return tickMsg{}
	}
}

func (m runtimeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}

	case tickMsg:
		m.secsToNextCrawl--
		if m.secsToNextCrawl <= 0 {
			// start a crawl
			toCrawl := m.queue.PopFront()

			return m, func() tea.Msg {
				return crawl(toCrawl)
			}

		}

		return m, tick()

	case crawlResponseMsg:
		if msg.Success {
			m.pagesCrawled++

			m.workSet.Append(msg.AddWorks...)

			if m.includeSeries {

				for _, crawlable := range msg.AddSeries {
					if m.seriesSet.Contains(crawlable) {
						continue
					}

					m.seriesSet.Add(crawlable)
					m.queue.PushBack(crawlable)

				}

			}

			m.secsToNextCrawl = m.delay

		} else {
			if msg.Retryable {
				m.queue.PushBack(msg.CrawlUrl)
			}

			if msg.Fatal {
				return m, tea.Quit
			}

			m.secsToNextCrawl = max(msg.WaitFor, m.delay)
		}

		// queue empty, quit
		if m.queue.Len() == 0 {
			return m, tea.Quit
		}

		return m, tick()

	}

	return m, nil
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func crawl(crawlUrl string) (cr crawlResponseMsg) {
	cr.CrawlUrl = crawlUrl

	// make request, handle errors
	resp, err := client.Get(crawlUrl)
	if err != nil {
		err := err.(*url.Error)

		if err.Timeout() {
			cr.Retryable = true
			cr.ErrMsg = "Request timed out."
			return
		}

		cr.ErrMsg = fmt.Sprint("Unknown error: ", err.Error())
		return
	}
	defer resp.Body.Close()

	// handle retry header
	if retryHeader := resp.Header.Get("Retry-After"); retryHeader != "" {
		if retryTime, err := strconv.Atoi(retryHeader); err == nil {
			cr.WaitFor = retryTime
		} else if retryDate, err := http.ParseTime(retryHeader); err == nil {
			cr.WaitFor = int(time.Until(retryDate).Seconds())
		} else {
			cr.ErrMsg = fmt.Sprintf("Server requested pause, but gave invalid time ('%s').", retryHeader)
			cr.Fatal = true
			return
		}

		cr.ErrMsg = fmt.Sprintf("Server requested pause. Suspending for %d seconds.", cr.WaitFor)
		cr.Retryable = true
		return
	}

	// handle non-2xx status codes
	if codeClass := resp.StatusCode / 100; codeClass != 2 {
		switch codeClass {
		case 4:
			cr.ErrMsg = fmt.Sprintf("Bad request (%d).", resp.StatusCode)
		case 5:

			cr.ErrMsg = fmt.Sprintf("Server error (%d).", resp.StatusCode)
			cr.Retryable = true
		default:
			cr.ErrMsg = fmt.Sprintf("Got unexpected status code %d.", resp.StatusCode)
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
			cr.AddWorks = append(cr.AddWorks, client.ToFullURL(href))
		} else if !crawledPageIsSeries && isSeriesMatcher.MatchString(href) {
			cr.AddSeries = append(cr.AddSeries, client.ToFullURL(href))
		}

		if crawledPageIsSeries {
			for _, attr := range token.Attr {
				if attr.Key != "rel" {
					continue
				}

				if attr.Val == "next" {
					cr.AddSeries = append(cr.AddSeries, client.ToFullURL(href))
					break
				}
			}
		}
	}

	cr.Success = true
	return
}

func getHref(t html.Token) (string, error) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			return a.Val, nil
		}
	}
	return "", errors.New("no href attribute found")
}
