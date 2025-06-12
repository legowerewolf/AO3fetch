package interactivelogin

import (
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/legowerewolf/AO3fetch/ao3client"

	tea "github.com/charmbracelet/bubbletea"
)

func Login(client *ao3client.Ao3Client) bool {

	m := newModel(client)

	p := tea.NewProgram(m)

	result, err := p.Run()
	if err != nil {
		log.Fatal("interactive login error: ", err)
	}

	modelResult := result.(model)

	return modelResult.success

}

type keymap struct {
	up    key.Binding
	down  key.Binding
	login key.Binding
	exit  key.Binding
}

func (k keymap) ShortHelp() []key.Binding {
	return []key.Binding{k.up, k.down, k.login, k.exit}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.up, k.down},
		{k.login, k.exit},
	}
}

var realizedKeymap = keymap{
	up: key.NewBinding(
		key.WithKeys("up", "shift+tab"),
		key.WithHelp("↑", "move up"),
	),
	down: key.NewBinding(
		key.WithKeys("down", "tab"),
		key.WithHelp("↓", "move down"),
	),
	login: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "login"),
	),
	exit: key.NewBinding(
		key.WithKeys("esc", "ctrl+c"),
		key.WithHelp("esc", "quit"),
	),
}

type model struct {
	client *ao3client.Ao3Client

	inputs  []textinput.Model
	focused int
	status  string

	success bool

	spin spinner.Model
	help help.Model
}

func newModel(client *ao3client.Ao3Client) model {

	usernameInput := textinput.New()
	usernameInput.Prompt = "Username > "

	passwordInput := textinput.New()
	passwordInput.Prompt = "Password > "
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '*'

	inputs := []textinput.Model{
		usernameInput,
		passwordInput,
	}

	m := model{
		client:  client,
		inputs:  inputs,
		focused: 0,
		status:  "",

		spin: spinner.New(spinner.WithSpinner(spinner.Ellipsis)),
		help: help.New(),
	}

	m.help.Styles.ShortKey = lipgloss.NewStyle().Faint(true).Bold(true)

	m.inputs[m.focused].Focus()

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spin.Tick,
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, realizedKeymap.exit):
			return m, tea.Quit
		}

		if m.focused == -1 {
			return m, nil
		}

		switch {
		case key.Matches(msg, realizedKeymap.up):
			m.focused--
			if m.focused < 0 {
				m.focused = 0
			}

			m.status = ""

			return m, m.updateFocus()

		case key.Matches(msg, realizedKeymap.down):
			m.focused++
			if m.focused >= len(m.inputs) {
				m.focused = len(m.inputs) - 1
			}

			m.status = ""

			return m, m.updateFocus()

		case key.Matches(msg, realizedKeymap.login):
			if m.focused == -1 {
				return m, nil
			}

			m.focused = -1
			m.status = "Logging in"
			return m, tea.Batch(m.updateFocus(), m.attemptLogin())
		}

	case loginSuccessMsg:
		m.success = true
		m.status = "Login successful!"
		return m, tea.Quit

	case loginFailedMsg:
		m.focused = 0
		m.status = "Login failed. Check your credentials and try again."
		return m, m.updateFocus()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	return m, m.updateInputs(msg)
}

func (m *model) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}

	return tea.Batch(cmds...)
}

func (m *model) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	for i := range m.inputs {
		if i == m.focused {
			cmds[i] = m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}

	return tea.Batch(cmds...)
}

type loginFailedMsg struct{}
type loginSuccessMsg struct{}

func (m *model) attemptLogin() tea.Cmd {
	return func() tea.Msg {

		err := m.client.Authenticate(
			m.inputs[0].Value(), m.inputs[1].Value(),
		)

		if err == nil {
			return loginSuccessMsg{}
		}

		return loginFailedMsg{}
	}
}

func (m model) View() string {
	var b strings.Builder

	// display all inputs
	for _, input := range m.inputs {
		b.WriteString(fmt.Sprintln(input.View()))
	}

	// status line
	b.WriteString(m.status)
	if m.focused == -1 && !m.success {
		b.WriteString(m.spin.View())
	}
	b.WriteString("\n")

	b.WriteString(m.help.View(realizedKeymap))

	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	return style.Render(b.String()) + "\n"
}
