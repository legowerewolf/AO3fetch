package interactivelogin

import (
	"errors"
	"fmt"
	"log"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/legowerewolf/AO3fetch/ao3client"
	"github.com/legowerewolf/AO3fetch/dialog"

	tea "github.com/charmbracelet/bubbletea"
)

func Login(client *ao3client.Ao3Client) bool {

	usernameInput := textinput.New()
	usernameInput.Prompt = "Username > "
	usernameInput.Validate = func(s string) error {
		if s == "" {
			return errors.New("username is required")
		}

		return nil
	}
	usernameInput.SetValue("")

	passwordInput := textinput.New()
	passwordInput.Prompt = "Password > "
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '*'
	passwordInput.Validate = func(s string) error {
		if s == "" {
			return errors.New("password is required")
		}

		return nil
	}
	passwordInput.SetValue("")

	m := dialog.NewModel(
		fmt.Sprintln("Credentials for", client.ToFullURL("/")),
		"Logging in",
		[]textinput.Model{
			usernameInput,
			passwordInput,
		},
		func(inputs []textinput.Model) error {
			return client.Authenticate(inputs[0].Value(), inputs[1].Value())
		},
	)
	m.SetVisible(true)

	p := tea.NewProgram(m)

	result, err := p.Run()
	if err != nil {
		log.Fatal("interactive login error: ", err)
	}

	dialogResult := result.(dialog.Model)

	return !dialogResult.Aborted
}
