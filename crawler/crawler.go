package crawler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/gammazero/deque"
	"github.com/legowerewolf/AO3fetch/ao3client"
	"github.com/legowerewolf/AO3fetch/logbuffer"
	"github.com/legowerewolf/AO3fetch/osc"
	"golang.org/x/net/html"
)

// region consts

const delayBackoffFactor = 1.3
const delayDecayFactor = 0.9

var isSeriesMatcher = regexp.MustCompile(`/series/\d+`)

var workSelector = mustParseSelector(`.index .blurb .header .heading a[href^="/works/"]`)
var seriesSelector = mustParseSelector(`.index .blurb .header .heading a[href^="/series/"], .index .blurb .series a[href^="/series/"]`)
var paginationSelector = mustParseSelector(`.pagination li:nth-last-child(2) a`)

// region runtime model

type RuntimeModel struct {
	client *ao3client.Ao3Client

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
	Logs   logbuffer.LogBuffer
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

func InitRuntimeModel(includeSeries bool, delay int, seedURL url.URL, pages int, client *ao3client.Ao3Client) (m RuntimeModel) {
	m.client = client

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
		m.queueUrl(seedURL.String())
	}

	m.prog = progress.New()
	m.spin = spinner.New(spinner.WithSpinner(spinner.Ellipsis))

	m.width = 80
	m.height = 40

	m.Logs = logbuffer.NewLogBuffer()
	m.logger = log.New(m.Logs, "", log.Ltime)

	return
}

// region messages

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
	AddWorks         []string
	AddSeries        []string
	LastDetectedPage int
}

// region program view/init/update

func (m RuntimeModel) View() string {
	doc := strings.Builder{}

	// compute progress
	var percent float64 = 0
	if m.pagesCrawled > 0 {
		percent = float64(m.pagesCrawled) / float64(m.pagesCrawled+m.queue.Len())
	}

	// write progress bars
	doc.WriteString(osc.SetProgress(1, percent))
	doc.WriteString(osc.SetTitle(fmt.Sprintf("AO3Fetch - %.0f%%", percent*100)))
	doc.WriteString(lipgloss.NewStyle().MarginBottom(1).Render(m.prog.ViewAs(percent)) + "\n")

	// current stats

	// current action
	currentAction := fmt.Sprintf("Requesting%s", m.spin.View())
	if !m.crawlInProgress {
		currentAction = fmt.Sprintf("Sleeping %s", time.Until(m.nextCrawlTime).Round(time.Second).String())
	}

	// estimated completion time
	eta := m.nextCrawlTime
	if m.crawlInProgress {
		eta = time.Now()
	}
	eta = eta.Add(m.currentDelay * time.Duration(m.queue.Len()-1))

	// total number of pages
	totalPages := m.pagesCrawled + m.queue.Len()
	if m.crawlInProgress {
		totalPages += 1
	}

	// what to show for series
	series := "Ignoring series"
	if m.includeSeries {
		series = fmt.Sprintf("Series discovered: %d", m.seriesSet.Cardinality())
	}

	// batch all of the stats above into one list
	stats := []string{
		currentAction,
		m.client.GetUser(),
		fmt.Sprintf("ETA: %s", eta.Local().Format("15:04:05")),
		fmt.Sprintf("Works discovered: %d", m.workSet.Cardinality()),
		series,
		fmt.Sprintf("To crawl: %d", m.queue.Len()),
		fmt.Sprintf("Crawled: %d", m.pagesCrawled),
		fmt.Sprintf("Total pages: %d", totalPages),
	}

	// render all the stats to a block of text
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

	// get the right number of log lines
	logLines := m.Logs.GetAtMostFromEnd(max(remainingLines(&m, &doc), lipgloss.Height(leftCol)))

	// produce a text block
	logBlock := lipgloss.NewStyle().
		MaxWidth(m.width - lipgloss.Width(leftCol)).
		Render(strings.Join(logLines, "\n"))

	// group stats and logs
	doc.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftCol, logBlock) + "\n")

	// write everything to screen
	return doc.String()
}

func (m RuntimeModel) Init() tea.Cmd {
	return tea.Batch(tick(), m.spin.Tick)
}

func (m RuntimeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
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
				startCrawl(m.client, toCrawl, m.includeSeries),
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

			for _, crawlable := range msg.AddSeries {
				m.seriesSet.Add(crawlable)
				m.queueUrl(crawlable)
			}

			if msg.LastDetectedPage != 0 && (m.autodetectStop || isSeriesMatcher.MatchString(msg.CrawlUrl)) {
				crawlUrl, _ := url.Parse(msg.CrawlUrl)
				m.queueUrlRange(*crawlUrl, msg.LastDetectedPage)
			}

			m.currentDelay = time.Duration(delayDecayFactor * float32(max(m.delay, m.currentDelay)))
		} else {
			logmsg := msg.ErrMsg

			if msg.WaitFor > 0 {
				wait := time.Second * time.Duration(msg.WaitFor)

				logmsg += fmt.Sprintf(" [server-requested delay: %s]", wait.String())
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

// region commands

func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func startCrawl(client *ao3client.Ao3Client, crawlUrl string, includeSeries bool) tea.Cmd {
	crawlUrlIsSeries := isSeriesMatcher.MatchString(crawlUrl)

	return func() tea.Msg {
		return crawl(client, crawlUrl, includeSeries && !crawlUrlIsSeries)
	}
}

// region other functions

func crawl(client *ao3client.Ao3Client, crawlUrl string, includeSeries bool) (cr crawlResponseMsg) {
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

		cr.ErrMsg = "Server requested pause."
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

	for _, node := range cascadia.QueryAll(dom, workSelector) {
		href, _ := getHref(node)

		cr.AddWorks = append(cr.AddWorks, client.ToFullURL(href))
	}

	if includeSeries {
		for _, series := range cascadia.QueryAll(dom, seriesSelector) {
			href, _ := getHref(series)

			cr.AddSeries = append(cr.AddSeries, client.ToFullURL(href))
		}
	}

	lastPage := cascadia.Query(dom, paginationSelector)
	if lastPage != nil {
		href, _ := getHref(lastPage)

		u, _ := url.Parse(href)

		cr.LastDetectedPage = getPageNum(*u)
	}

	cr.Success = true
	return
}

func mustParseSelector(selector string) cascadia.Matcher {
	sel, err := cascadia.ParseGroup(selector)

	if err != nil {
		panic(err)
	}

	return sel
}

func getHref(t *html.Node) (string, error) {
	for _, a := range t.Attr {
		if a.Key == "href" {
			return a.Val, nil
		}
	}
	return "", errors.New("no href attribute found")
}

func remainingLines(m *RuntimeModel, doc *strings.Builder) int {
	return m.height - strings.Count(doc.String(), "\n") - 1
}

func (m *RuntimeModel) queueUrl(url string) bool {
	isNew := m.queueSet.Add(url)

	if isNew {
		m.queue.PushBack(url)
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

func (m *RuntimeModel) queueUrlRange(seedURL url.URL, endPage int) {
	startPage := getPageNum(seedURL)

	query := seedURL.Query()

	for pageNum := endPage; pageNum >= startPage; pageNum-- {
		query.Set("page", strconv.Itoa(pageNum))
		seedURL.RawQuery = query.Encode()

		if added := m.queueUrl(seedURL.String()); !added {
			break
		}
	}
}

func (m *RuntimeModel) GetWorkCount() int {
	return m.workSet.Cardinality()
}

func (m *RuntimeModel) GetPagesCrawled() int {
	return m.pagesCrawled
}

func (m *RuntimeModel) GetWorks() <-chan string {
	return m.workSet.Iter()
}
