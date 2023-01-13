package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
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

func getAo3Token() string {
	resp, apiErr := http.Get("https://archiveofourown.org/token_dispenser.json")
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
	unmarshallErr := json.Unmarshal(text, &r)
	if unmarshallErr != nil {
		log.Fatal(unmarshallErr)
	}

	return r["token"].(string)
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
