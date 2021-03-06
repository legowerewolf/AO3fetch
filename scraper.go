package main

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gopkg.in/cheggaaa/pb.v1"
)

func main() {
	foundWorks := make(map[string]bool)
	foundSeries := make(map[string]bool)

	seedURL := os.Args[1]
	pages := 1
	includeSeries := true
	delay := 10
	showProgress := true
	token := ""

	for index, arg := range os.Args {
		if index == 0 {
			continue
		} else if arg == "-url" && len(os.Args) >= 1+index {
			seedURL = os.Args[index+1]
		} else if arg == "-pages" && len(os.Args) >= 1+index {
			var err error
			pages, err = strconv.Atoi(os.Args[1+index])
			if err != nil {
				pages = 1
			}
		} else if arg == "-delay" && len(os.Args) >= 1+index {
			var err error
			delay, err = strconv.Atoi(os.Args[1+index])
			if err != nil {
				delay = 10
			}
		} else if arg == "-noseries" {
			includeSeries = false
		} else if arg == "-noprogress" {
			showProgress = false
		} else if arg == "-login" {
			s := strings.Split(os.Args[1+index], ":")
			token = login(s[0], s[1])
		}
	}

	fmt.Println("Running at", seedURL, "across", strconv.Itoa(pages), "pages with a", strconv.Itoa(delay), "second delay and series set to", strconv.FormatBool(includeSeries))

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
		go crawl(seedURL+"?page="+strconv.Itoa(page), token, chworks, chseries, finished)
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
				go crawl(url, token, chworks, chseries, finished)
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

func crawl(url, token string, works chan string, series chan string, chFinished chan bool) {
	req, err := http.NewRequest("GET", toFullURL(url), nil)
	if token != "" && token != "error" {
		req.AddCookie(&http.Cookie{Name: "user_credentials", Value: "39468c3a580f3a6540b15e845146ed6ceb738fae4ac4acaf8aa92d781588e326fb406687c3ffaea52efde4bafca46c7c6b4e6d2a824ad58b43a3932c3db50a2f%3A%3A796379"})
	}
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
