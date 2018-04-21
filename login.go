package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
)

func login(username, password string) string {
	c := http.Client{}
	c.Jar, _ = cookiejar.New(nil)

	ao3url, _ := url.Parse("https://archiveofourown.org/user_sessions")

	_, err := c.PostForm(ao3url.String(), generateLoginForm(username, password, c))
	if err != nil {
		fmt.Println(err)
	}

	for _, cookie := range c.Jar.Cookies(ao3url) {
		if cookie.Name == "user_credentials" {
			return cookie.Value
		}
	}

	return "error"
}

//TokenResponse exported for JSON requests
type TokenResponse struct {
	Token string `json:"token"`
}

func getAo3Token(c http.Client) string {
	resp, _ := c.Get("https://archiveofourown.org/token_dispenser.json")
	text, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	var r TokenResponse
	json.Unmarshal(text, &r)
	return r.Token
}

func generateLoginForm(username, password string, c http.Client) url.Values {
	val := url.Values{}
	val.Set("utf8", "✓")
	val.Set("authenticity_token", getAo3Token(c))
	val.Set("[user_session]login", username)
	val.Set("[user_session]password", password)
	val.Set("user_session[remember_me]", "0")
	val.Set("commit", "Log In")

	return val
}
