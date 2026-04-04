package ao3client

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/andybalholm/cascadia"
	buildinfo "github.com/legowerewolf/AO3fetch/buildinfo"
	"golang.org/x/net/html"
)

type Ao3Client struct {
	client            *http.Client
	userAgentString   string
	baseUrl           *url.URL
	authenticatedUser string
}

func NewAo3Client(baseUrl string) (*Ao3Client, error) {
	uBaseUrl, err := url.Parse(baseUrl)
	if err != nil {
		return nil, err
	}

	uBaseUrl2 := &url.URL{Scheme: uBaseUrl.Scheme, Host: uBaseUrl.Host}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	buildInfo, err := buildinfo.GetBuildSettings()
	if err != nil {
		return nil, err
	}

	uaString := fmt.Sprintf("AO3Fetch/%s (+https://github.com/legowerewolf/AO3fetch)", (*buildInfo)["vcs.revision.withModified"])

	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// based on default CheckRedirect function
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}

			req.Header.Set("User-Agent", uaString)

			return nil
		},
	}

	return &Ao3Client{client: client, userAgentString: uaString, baseUrl: uBaseUrl2}, nil
}

func (c *Ao3Client) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgentString)
	return c.client.Do(req)
}

func (c *Ao3Client) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	return c.Do(req)
}

func (c *Ao3Client) PostForm(url string, data url.Values) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return c.Do(req)
}

func (c *Ao3Client) Authenticate(username, password string) error {
	// phase 1: get login form
	getFormResp, apiErr := c.Get(c.baseUrl.JoinPath("/users/login").String())
	if apiErr != nil {
		log.Fatal("Form request failed: ", apiErr)
	}

	dom, err := html.Parse(getFormResp.Body)
	if err != nil {
		log.Fatal("Form parse failed: ", err)
	}

	formInputs := cascadia.QueryAll(dom, cascadia.MustCompile("#loginform input"))

	formValues := url.Values{}

	for _, input := range formInputs {
		n, ne := getAttr(input, "name")
		if ne != nil {
			log.Fatal("Form input parse name failed: ", ne)
		}

		v, ve := getAttr(input, "value")
		if ve != nil {
			continue
		}

		formValues.Set(n, v)
	}

	// phase 2: fill data
	formValues.Set("[user]login", username)
	formValues.Set("[user]password", password)

	// phase 3: submit
	submitFormResp, err := c.PostForm(c.baseUrl.JoinPath("/users/login").String(), formValues)
	if err != nil {
		return err
	}

	for _, cookie := range c.client.Jar.Cookies(c.baseUrl) {
		if cookie.Name == "user_credentials" {
			c.authenticatedUser = username
			return nil
		}
	}

	body, err := io.ReadAll(submitFormResp.Body)
	defer submitFormResp.Body.Close()

	fmt.Println(string(body))
	return fmt.Errorf("login failed")
}

func (c *Ao3Client) ToFullURL(_url string) string {
	o, _ := url.Parse(_url)

	return c.baseUrl.JoinPath(o.Path).String()
}

func (c *Ao3Client) GetUser() string {
	if c.authenticatedUser == "" {
		return "Anonymous"
	}

	return c.authenticatedUser
}

func getAttr(t *html.Node, attr string) (string, error) {
	for _, a := range t.Attr {
		if a.Key == attr {
			return a.Val, nil
		}
	}
	return "", errors.New("no attribute found")
}
