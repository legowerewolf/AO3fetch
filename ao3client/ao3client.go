package ao3client

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	buildinfo "github.com/legowerewolf/AO3fetch/buildinfo"
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
	_, err := c.PostForm(c.baseUrl.JoinPath("/users/login").String(), c.generateLoginForm(username, password))
	if err != nil {
		return err
	}

	for _, cookie := range c.client.Jar.Cookies(c.baseUrl) {
		if cookie.Name == "user_credentials" {
			c.authenticatedUser = username
			return nil
		}
	}

	return fmt.Errorf("login failed")
}

func (c *Ao3Client) getAo3Token() string {
	resp, apiErr := c.Get(c.baseUrl.JoinPath("/token_dispenser.json").String())
	if apiErr != nil {
		log.Fatal("Token request failed: ", apiErr)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	text, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Fatal("Token read failed: ", readErr)
	}

	var r map[string]interface{}
	unmarshalErr := json.Unmarshal(text, &r)
	if unmarshalErr != nil {
		log.Fatal("Token parse failed: ", unmarshalErr)
	}

	return r["token"].(string)
}

func (c *Ao3Client) generateLoginForm(username, password string) url.Values {
	val := url.Values{}
	val.Set("utf8", "✓")
	val.Set("authenticity_token", c.getAo3Token())
	val.Set("[user]login", username)
	val.Set("[user]password", password)
	val.Set("commit", "Log In")

	return val
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
