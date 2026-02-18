package dialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type keymap struct {
	up     key.Binding
	down   key.Binding
	submit key.Binding
	cancel key.Binding
}

func (k keymap) ShortHelp() []key.Binding {
	return []key.Binding{k.up, k.down, k.submit, k.cancel}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.up, k.down},
		{k.submit, k.cancel},
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
	submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	cancel: key.NewBinding(
		key.WithKeys("esc", "ctrl+c"),
		key.WithHelp("esc", "cancel"),
	),
}

type Model struct {
	isVisible bool

	title               string
	submitInProgressMsg string

	focused int
	inputs  []textinput.Model

	status string

	spin spinner.Model
	help help.Model

	submit func(inputs []textinput.Model) error

	Aborted bool
}

func NewModel(title, submitInProgressMsg string, inputs []textinput.Model, submit func(inputs []textinput.Model) error) Model {

	m := Model{
		isVisible: false,

		title:               title,
		submitInProgressMsg: submitInProgressMsg,

		focused: 0,
		inputs:  inputs,

		spin: spinner.New(spinner.WithSpinner(spinner.Ellipsis)),
		help: help.New(),

		submit: submit,

		Aborted: false,
	}

	m.help.Styles.ShortKey = lipgloss.NewStyle().Faint(true).Bold(true)

	m.inputs[m.focused].Focus()

	return m
}

func (m *Model) SetVisible(visible bool) {
	m.isVisible = visible
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spin.Tick,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, realizedKeymap.cancel):
			m.Aborted = true
			m.isVisible = false
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

		case key.Matches(msg, realizedKeymap.submit):
			if m.focused == -1 {
				return m, nil
			}

			for idx, input := range m.inputs {
				if input.Err != nil {
					m.focused = idx
					m.status = "Validation error: " + input.Err.Error()
					return m, m.updateFocus()
				}
			}

			m.focused = -1
			m.status = m.submitInProgressMsg
			return m, tea.Batch(m.updateFocus(), m.attemptSubmit())
		}

	case submitSuccessMsg:
		m.status = ""
		m.isVisible = false
		return m, tea.Quit

	case submitErrMsg:
		m.focused = 0
		m.status = msg.err.Error()
		return m, m.updateFocus()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	return m, m.updateInputs(msg)
}

func (m *Model) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}

	return tea.Batch(cmds...)
}

func (m *Model) updateFocus() tea.Cmd {
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

type submitErrMsg struct {
	err error
}
type submitSuccessMsg struct{}

func (m *Model) attemptSubmit() tea.Cmd {
	return func() tea.Msg {

		err := m.submit(m.inputs)

		if err == nil {
			return submitSuccessMsg{}
		}

		return submitErrMsg{
			err: err,
		}
	}
}

func (m Model) View() string {
	if !m.isVisible {
		return ""
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintln(m.title))

	// display all inputs
	for _, input := range m.inputs {
		b.WriteString(fmt.Sprintln(input.View()))
	}

	// status line
	b.WriteString(m.status)
	if m.focused == -1 {
		b.WriteString(m.spin.View())
	}
	b.WriteString("\n")

	b.WriteString(m.help.View(realizedKeymap))

	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	return style.Render(b.String())
}
