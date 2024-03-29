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
	Client          *http.Client
	UserAgentString string
}

func NewAo3Client() (*Ao3Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	buildInfo, err := buildinfo.GetBuildSettings()
	if err != nil {
		return nil, err
	}

	uaString := fmt.Sprintf("legowerewolf-ao3scraper/%s", (*buildInfo)["vcs.revision.withModified"])

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

	return &Ao3Client{Client: client, UserAgentString: uaString}, nil
}

func (c *Ao3Client) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.UserAgentString)
	return c.Client.Do(req)
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
	ao3url, _ := url.Parse("https://archiveofourown.org/users/login")

	_, err := c.PostForm(ao3url.String(), c.generateLoginForm(username, password))
	if err != nil {
		return err
	}

	for _, cookie := range c.Client.Jar.Cookies(ao3url) {
		if cookie.Name == "user_credentials" {
			return nil
		}
	}

	return fmt.Errorf("login failed")
}

func (c *Ao3Client) getAo3Token() string {
	resp, apiErr := c.Get("https://archiveofourown.org/token_dispenser.json")
	if apiErr != nil {
		log.Fatal(apiErr)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	text, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Fatal(readErr)
	}

	var r map[string]interface{}
	unmarshalErr := json.Unmarshal(text, &r)
	if unmarshalErr != nil {
		log.Fatal(unmarshalErr)
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
