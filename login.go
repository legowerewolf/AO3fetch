package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
)

func login(username, password string) error {

	http.DefaultClient.Jar, _ = cookiejar.New(nil)

	ao3url, _ := url.Parse("https://archiveofourown.org/users/login")

	_, err := http.PostForm(ao3url.String(), generateLoginForm(username, password))
	if err != nil {
		fmt.Println(err)
	}

	for _, cookie := range http.DefaultClient.Jar.Cookies(ao3url) {
		if cookie.Name == "user_credentials" {
			return nil
		}
	}

	return fmt.Errorf("login failed")
}

// TokenResponse exported for JSON requests
type TokenResponse struct {
	Token string `json:"token"`
}

func getAo3Token() string {
	resp, _ := http.Get("https://archiveofourown.org/token_dispenser.json")
	text, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var r TokenResponse
	json.Unmarshal(text, &r)
	return r.Token
}

func generateLoginForm(username, password string) url.Values {
	val := url.Values{}
	val.Set("utf8", "✓")
	val.Set("authenticity_token", getAo3Token())
	val.Set("[user]login", username)
	val.Set("[user]password", password)
	val.Set("commit", "Log In")

	return val
}
