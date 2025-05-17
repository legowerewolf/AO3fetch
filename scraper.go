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

	"github.com/andybalholm/cascadia"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/gammazero/deque"

	"github.com/legowerewolf/AO3fetch/ao3client"
	"github.com/legowerewolf/AO3fetch/buildinfo"
	"github.com/legowerewolf/AO3fetch/logbuffer"
)

// global variables
var client *ao3client.Ao3Client

var isWorkMatcher = regexp.MustCompile(`/works/\d+`)
var isSeriesMatcher = regexp.MustCompile(`/series/\d+`)
var isSpecialMatcher = regexp.MustCompile(`bookmarks|comments|collections|search|tags|users|transformative|chapters|kudos|navigate|share|view_full_work`)

var linkSelector = mustParse("a")

const delayBackoffFactor = 1.3
const delayDecayFactor = 0.9

func main() {
	// parse flags
	var (
		seedURLRaw, credentials, outputFile string
		pages, delay                        int
		includeSeries, showVersionAndQuit   bool
	)
	flag.BoolVar(&showVersionAndQuit, "version", false, "Show version information and quit.")
	flag.StringVar(&seedURLRaw, "url", "", "URL to start crawling from.")
	flag.IntVar(&pages, "pages", 1, "Number of pages to crawl.")
	flag.BoolVar(&includeSeries, "series", true, "Discover and crawl series.")
	flag.IntVar(&delay, "delay", 10, "Delay between requests in seconds.")
	flag.StringVar(&credentials, "login", "", "Login credentials in the form of username:password.")
	flag.StringVar(&outputFile, "outputFile", "", "Filename to write collected work URLs to instead of standard output.")
	flag.Parse()

	if flag.NFlag() == 0 {
		fmt.Println("AO3Fetch by @legowerewolf - https://github.com/legowerewolf/AO3fetch")
		fmt.Println()
		flag.PrintDefaults()
		return
	}

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

	if seedURLRaw == "" {
		log.Fatal("No URL provided.")
	} else {
		var err error
		seedURL, err = url.Parse(seedURLRaw)
		if err != nil {
			log.Fatal("Invalid URL provided: ", seedURLRaw)
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
		if seedURL.Scheme != "https" {
			log.Fatal("Credentials cannot be used with insecure URLs.")
		}

		username, pass, found := strings.Cut(credentials, ":")

		if !found {
			log.Fatal("Credentials provided but could not split username from password. Did you include a colon?")
		}

		log.Println("Logging in as " + username + "...")

		err := client.Authenticate(username, pass)
		if err != nil {
			log.Fatal("Authentication failure. Check your credentials and try again.")
		}

		log.Println("Login successful.")
	}

	if pages < 1 && pages != -1 {
		log.Fatal("Number of pages must be -1 (autodetect) or greater than 0.")
	}

	// parameters all check out, finish initializing

	// initialization done, start scraping

	log.Println("Scrape parameters: ")
	fmt.Println("URL:     ", seedURL)
	fmt.Println("Pages:   ", pages)
	fmt.Println("Series?: ", includeSeries)
	fmt.Println("Delay:   ", delay)

	p := tea.NewProgram(initRuntimeModel(includeSeries, delay, *seedURL, pages), tea.WithAltScreen())

	r, err := p.Run()
	fmt.Print(progressCode(0, 0))
	fmt.Print(title("AO3Fetch"))
	if err != nil {
		log.Fatal("Tea program quit: ", err)
	}

	rModel := r.(runtimeModel)

	fmt.Println()
	fmt.Println("Runtime logs:")
	rModel.logs.Dump(os.Stdout)

	fmt.Println()
	log.Printf("Found %d works across %d pages. \n", rModel.workSet.Cardinality(), rModel.pagesCrawled)
	fmt.Println()

	var workOutputTarget io.Writer

	if outputFileHandle != nil {
		workOutputTarget = outputFileHandle
	} else {
		workOutputTarget = log.Writer()
	}

	for url := range rModel.workSet.Iter() {
		fmt.Fprintln(workOutputTarget, url)
	}

}

type runtimeModel struct {
	// config properties
	includeSeries  bool
	autodetectStop bool
	delay          time.Duration

	// work and series data
	queue        deque.Deque[string] // stores URLs to be crawled
	queueSet     mapset.Set[string]  // stores URLs that have been queued to be crawled
	workSet      mapset.Set[string]  // stores URLs of works that have been detected
	seriesSet    mapset.Set[string]  // ditto for series
	pagesCrawled int

	// logging
	logs   logbuffer.LogBuffer
	logger *log.Logger

	// control
	nextCrawlTime   time.Time
	currentDelay    time.Duration
	crawlInProgress bool

	// view props
	width  int
	height int

	// sub-models
	prog progress.Model
	spin spinner.Model
}

func (m *runtimeModel) queueUrl(_url url.URL) bool {
	uurl := _url.String()

	isNew := m.queueSet.Add(uurl)

	if isNew {
		m.queue.PushBack(uurl)
	}

	return isNew
}

func getPageNum(u url.URL) int {
	str := u.Query().Get("page")

	if str == "" {
		return 1
	}

	i, err := strconv.Atoi(str)

	if err != nil {
		return 1
	}

	return i
}

func (m *runtimeModel) queueUrlRange(seedURL url.URL, endPage int) {
	startPage := getPageNum(seedURL)

	query := seedURL.Query()

	for pageNum := endPage; pageNum >= startPage; pageNum-- {
		query.Set("page", strconv.Itoa(pageNum))
		seedURL.RawQuery = query.Encode()

		if added := m.queueUrl(seedURL); !added {
			break
		}
	}

}

func initRuntimeModel(includeSeries bool, delay int, seedURL url.URL, pages int) (m runtimeModel) {
	m.includeSeries = includeSeries
	m.delay = time.Duration(delay) * time.Second
	m.currentDelay = m.delay

	m.workSet = mapset.NewSet[string]()
	m.seriesSet = mapset.NewSet[string]()
	m.queueSet = mapset.NewSet[string]()

	if pages > 0 {
		m.queueUrlRange(seedURL, pages)
	} else {
		m.autodetectStop = true
		m.queueUrl(seedURL)
	}

	m.prog = progress.New()
	m.spin = spinner.New(spinner.WithSpinner(spinner.Ellipsis))

	m.width = 80
	m.height = 40

	m.logs = logbuffer.NewLogBuffer()
	m.logger = log.New(m.logs, "", log.Ltime)

	return
}

func (m runtimeModel) View() string {

	doc := strings.Builder{}

	var percent float64 = 0
	if m.pagesCrawled > 0 {
		percent = float64(m.pagesCrawled) / float64(m.pagesCrawled+m.queue.Len())
	}

	// write progress bars
	doc.WriteString(progressCode(1, percent))
	doc.WriteString(title(fmt.Sprintf("AO3Fetch - %d%%", int(percent*100))))
	doc.WriteString(lipgloss.NewStyle().MarginBottom(1).Render(m.prog.ViewAs(percent)) + "\n")

	// current stats
	currentAction := fmt.Sprintf("Requesting%s", m.spin.View())
	if !m.crawlInProgress {
		currentAction = fmt.Sprintf("Sleeping %s", time.Until(m.nextCrawlTime).Round(time.Second).String())
	}

	eta := m.nextCrawlTime
	if m.crawlInProgress {
		eta = time.Now()
	}
	eta = eta.Add(m.currentDelay * time.Duration(m.queue.Len()-1))

	totalPages := m.pagesCrawled + m.queue.Len()
	if m.crawlInProgress {
		totalPages += 1
	}

	series := "Ignoring series"
	if m.includeSeries {
		series = fmt.Sprintf("Series discovered: %d", m.seriesSet.Cardinality())
	}

	stats := []string{
		currentAction,
		fmt.Sprintf("ETA: %s", eta.Local().Format("15:04:05")),
		fmt.Sprintf("Works discovered: %d", m.workSet.Cardinality()),
		series,
		fmt.Sprintf("To crawl: %d", m.queue.Len()),
		fmt.Sprintf("Crawled: %d", m.pagesCrawled),
		fmt.Sprintf("Total pages: %d", totalPages),
	}

	statBlock := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderTop(true).
		BorderRight(true).
		BorderLeft(true).
		BorderBottom(true).
		MarginRight(2).
		Padding(1, 2).
		Width(25).
		Render(strings.Join(stats, "\n"))

	// add help message
	helpMsg := lipgloss.NewStyle().
		Faint(true).
		Render("abort: esc / ctrl+c")

	// group stat block and help message into column
	leftCol := lipgloss.JoinVertical(lipgloss.Center, statBlock, helpMsg)

	// logs
	logLines := m.logs.GetAtMostFromEnd(max(remainingLines(&m, &doc), lipgloss.Height(leftCol)))

	logBlock := lipgloss.NewStyle().
		MaxWidth(m.width - lipgloss.Width(leftCol)).
		Render(strings.Join(logLines, "\n"))

	// group stats and logs
	doc.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftCol, logBlock) + "\n")

	// write everything to screen
	return doc.String()
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

	return tea.Batch(tick(), m.spin.Tick)
}

func (m runtimeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		m.prog.Width = msg.Width

		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tickMsg:
		if m.crawlInProgress {
			return m, tick()
		}

		// queue empty, quit
		if m.queue.Len() == 0 {
			return m, tea.Quit
		}

		// sleep time over, crawl
		if m.nextCrawlTime.Compare(time.Now()) == -1 {
			toCrawl := m.queue.PopFront()
			m.crawlInProgress = true

			return m, tea.Batch(
				tick(),
				startCrawl(toCrawl),
			)
		}

		return m, tick()
	case crawlResponseMsg:
		if msg.Fatal {
			return m, tea.Quit
		}

		m.crawlInProgress = false

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

			m.currentDelay = time.Duration(delayDecayFactor * float32(max(m.delay, m.currentDelay)))
		} else {
			logmsg := msg.ErrMsg

			if msg.WaitFor > 0 {
				logmsg += fmt.Sprintf(" [server-requested delay: %d]", msg.WaitFor)
			}

			if msg.Retryable {
				m.queue.PushBack(msg.CrawlUrl)
				logmsg += " [will retry]"
			} else {
				logmsg += " [unretryable]"
			}

			logmsg += "\n  for " + msg.CrawlUrl

			m.logger.Println(logmsg)

			m.currentDelay = time.Duration(float32(max(m.delay, m.currentDelay)) * delayBackoffFactor)
		}

		m.nextCrawlTime = time.Now().Add(max(m.currentDelay, time.Second*time.Duration(msg.WaitFor)))

		return m, nil
	}

	return m, nil
}

func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func startCrawl(crawlUrl string) tea.Cmd {
	return func() tea.Msg {
		return crawl(crawlUrl)
	}
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

	dom, err := html.Parse(resp.Body)
	if err != nil {
		cr.ErrMsg = "Failed to parse response body."
		return
	}

	crawledPageIsSeries := isSeriesMatcher.MatchString(crawlUrl)

	for _, node := range cascadia.QueryAll(dom, linkSelector) {
		href, err := getHref(node)
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
			for _, attr := range node.Attr {
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

func getHref(t *html.Node) (string, error) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			return a.Val, nil
		}
	}
	return "", errors.New("no href attribute found")
}

func progressCode(state int, progress float64) string {
	return "\x1b]9;4;" + strconv.Itoa(state) + ";" + strconv.Itoa(int(progress*100)) + "\x07"
}

func title(title string) string {
	return "\x1b]0;" + title + "\x07"
}

func remainingLines(m *runtimeModel, doc *strings.Builder) int {
	return m.height - strings.Count(doc.String(), "\n") - 1
}
func mustParse(selector string) cascadia.Sel {
	sel, err := cascadia.Parse(selector)

	if err != nil {
		panic(err)
	}

	return sel
}
