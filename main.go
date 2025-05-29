package main

import (
	"flag"
	"fmt"
	"io"
	"log"

	"net/url"
	"os"
	"regexp"

	"strings"

	"github.com/andybalholm/cascadia"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/legowerewolf/AO3fetch/ao3client"
	"github.com/legowerewolf/AO3fetch/buildinfo"

	"github.com/legowerewolf/AO3fetch/osc"
)

// global variables
var client *ao3client.Ao3Client

var isSeriesMatcher = regexp.MustCompile(`/series/\d+`)

var workSelector = mustParseSelector(`.index .blurb .header .heading a[href^="/works/"]`)
var seriesSelector = mustParseSelector(`.index .blurb .header .heading a[href^="/series/"], .index .blurb .series a[href^="/series/"]`)
var paginationSelector = mustParseSelector(`.pagination li:nth-last-child(2) a`)

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

		fmt.Printf("%s (%s:%s) for %s %s-%s\n", (*settings)["vcs.revision.refName"], (*settings)["vcs"], (*settings)["vcs.revision.withModified"], (*settings)["GOVERSION"], (*settings)["GOOS"], (*settings)["GOARCH.withVersion"])

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

		if len(username) == 0 || len(pass) == 0 {
			log.Fatal("Username or password was empty.")
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
	fmt.Print(osc.SetProgress(0, 0))
	fmt.Print(osc.SetTitle("AO3Fetch"))
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

		log.Printf("Writing to file %s...", outputFile)
	} else {
		workOutputTarget = log.Writer()
	}

	for url := range rModel.workSet.Iter() {
		fmt.Fprintln(workOutputTarget, url)
	}

}

func mustParseSelector(selector string) cascadia.Matcher {
	sel, err := cascadia.ParseGroup(selector)

	if err != nil {
		panic(err)
	}

	return sel
}
