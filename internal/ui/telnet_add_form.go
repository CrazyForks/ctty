package ui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/telnetconfig"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

// telnetAddFormModel is the form for adding or editing a telnet device,
// modernized using charmbracelet/huh with dynamic theme support and responsive viewport.
type telnetAddFormModel struct {
	form     *huh.Form
	viewport viewport.Model
	styles   Styles
	width    int
	height   int
	editing  *telnetconfig.TelnetHost
	err      string

	nameVal    string
	hostVal    string
	portVal    string
	tagsVal    string
	confirmVal bool

	done      bool
	cancelled bool
}

func newTelnetAddForm(styles Styles, width, height int, initial *telnetconfig.TelnetHost) *telnetAddFormModel {
	m := &telnetAddFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		editing:    initial,
		portVal:    strconv.Itoa(telnetconfig.DefaultPort),
		confirmVal: true,
	}

	if initial != nil {
		m.nameVal = initial.Name
		m.hostVal = initial.Host
		if initial.Port > 0 {
			m.portVal = strconv.Itoa(initial.Port)
		}
		m.tagsVal = strings.Join(initial.Tags, ", ")
	}

	m.buildForm()
	return m
}

func (m *telnetAddFormModel) buildForm() {
	innerW := formPageInnerWidth(m.width)
	if innerW < 20 {
		innerW = 20
	}

	validatePort := func(s string) error {
		val := strings.TrimSpace(s)
		if val == "" {
			return nil
		}
		p, err := strconv.Atoi(val)
		if err != nil || p < 1 || p > 65535 {
			return errors.New(i18n.T("pf.err_invalid_port"))
		}
		return nil
	}

	validateName := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(i18n.T("form.err_host_name_req"))
		}
		return nil
	}
	validateHost := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(i18n.T("form.err_hostname_req"))
		}
		return nil
	}

	currentHuhTheme := theme.GetTheme(m.styles.Theme.ID).HuhTheme()

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(i18n.T("telnet.col_name")).
				Prompt("> ").
				Placeholder("e.g. core-sw console").
				Validate(validateName).
				Value(&m.nameVal),

			huh.NewInput().
				Key("host").
				Title(i18n.T("telnet.col_host")).
				Prompt("> ").
				Placeholder("e.g. 192.168.1.1 or ::1").
				Validate(validateHost).
				Value(&m.hostVal),

			huh.NewInput().
				Key("port").
				Title(i18n.T("telnet.col_port")).
				Prompt("> ").
				Placeholder("23").
				Validate(validatePort).
				Value(&m.portVal),

			huh.NewInput().
				Key("tags").
				Title(i18n.T("table.col.tags")).
				Prompt("> ").
				Placeholder("lab,network").
				Value(&m.tagsVal),

			huh.NewConfirm().
				Key("confirm").
				Title("").
				Affirmative(i18n.T("form.btn_save")).
				Negative(i18n.T("form.btn_cancel")).
				Value(&m.confirmVal),
		),
	).WithLayout(huh.LayoutStack).
		WithTheme(currentHuhTheme).
		WithWidth(innerW).
		WithShowHelp(false)
}

func (m *telnetAddFormModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *telnetAddFormModel) nextField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "confirm" {
		for m.form.GetFocusedField().GetKey() != "name" {
			m.form.PrevField()
		}
		return nil
	}
	return m.form.NextField()
}

func (m *telnetAddFormModel) prevField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "name" {
		for m.form.GetFocusedField().GetKey() != "confirm" {
			m.form.NextField()
		}
		return nil
	}
	return m.form.PrevField()
}

func (m *telnetAddFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		innerW := formPageInnerWidth(m.width)
		if innerW < 20 {
			innerW = 20
		}
		if m.form != nil {
			m.form.WithWidth(innerW)
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case msg.String() == "esc" || msg.String() == "ctrl+c":
			m.cancelled = true
			return m, nil
		case msg.String() == "ctrl+s":
			m.submit()
			return m, nil
		case msg.Type == tea.KeyTab || msg.String() == "tab":
			return m, m.nextField()
		case msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab":
			return m, m.prevField()
		case msg.Type == tea.KeyDown || msg.String() == "down":
			return m, m.nextField()
		case msg.Type == tea.KeyUp || msg.String() == "up":
			return m, m.prevField()
		case msg.Type == tea.KeyEnter || msg.String() == "enter":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil && focused.GetKey() == "confirm" {
					if m.confirmVal {
						m.submit()
					} else {
						m.cancelled = true
					}
					return m, nil
				}
			}
			return m, m.nextField()
		}
	}

	if m.form == nil {
		return m, nil
	}

	formModel, cmd := m.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateCompleted {
		if m.confirmVal {
			m.submit()
			return m, nil
		}
		m.cancelled = true
		return m, nil
	}

	if m.form.State == huh.StateAborted {
		m.cancelled = true
		return m, nil
	}

	return m, cmd
}

func (m *telnetAddFormModel) submit() {
	name := strings.TrimSpace(m.nameVal)
	host := strings.TrimSpace(m.hostVal)
	if name == "" || host == "" {
		m.err = i18n.T("telnet.err_name_host_req")
		return
	}

	port := telnetconfig.DefaultPort
	if strings.TrimSpace(m.portVal) != "" {
		p, err := strconv.Atoi(strings.TrimSpace(m.portVal))
		if err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}

	var tags []string
	for _, t := range strings.Split(m.tagsVal, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}

	newHost := telnetconfig.TelnetHost{Name: name, Host: host, Port: port, Tags: tags}

	var err error
	if m.editing != nil {
		err = telnetconfig.Update(m.editing.Name, newHost)
	} else {
		err = telnetconfig.Add(newHost)
	}
	if err != nil {
		m.err = err.Error()
		return
	}
	m.done = true
}

func (m *telnetAddFormModel) getFieldTitle(key string) string {
	switch key {
	case "name":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("telnet.col_name"), ":"))
	case "host":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("telnet.col_host"), ":"))
	case "port":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("telnet.col_port"), ":"))
	case "tags":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("table.col.tags"), ":"))
	case "confirm":
		return i18n.T("form.btn_save")
	default:
		return ""
	}
}

func (m *telnetAddFormModel) View() string {
	title := i18n.T("telnet.add_title")
	if m.editing != nil {
		title = i18n.T("telnet.edit_title")
	}

	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	container := m.styles.FormContainer
	if m.height < 24 {
		container = container.Padding(0, 1)
	}

	innerW := boxWidth - container.GetHorizontalFrameSize()
	if innerW < 10 {
		innerW = 10
	}
	titleText := m.styles.Header.Width(innerW).Render(title)

	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("telnet.help_add"))

	formView := ""
	if m.form != nil {
		formView = m.form.View()
	}

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(titleText)
	helpH := lipgloss.Height(helpText)

	targetBoxH := m.height
	if m.height >= 14 {
		targetBoxH = m.height - 1
	}

	overhead := frameH + headerH + helpH + 2
	availableH := targetBoxH - overhead
	if availableH < 2 {
		availableH = 2
	}

	formH := lipgloss.Height(formView)
	var bodyView string
	if formH <= availableH {
		bodyView = formView
	} else {
		m.viewport.Width = innerW
		m.viewport.Height = availableH
		m.viewport.SetContent(formView)

		// Auto-scroll viewport to keep focused field in view
		if m.form != nil {
			focused := m.form.GetFocusedField()
			if focused != nil {
				key := focused.GetKey()
				title := m.getFieldTitle(key)
				if title != "" {
					lines := strings.Split(formView, "\n")
					for idx, line := range lines {
						if strings.Contains(line, title) {
							if idx < m.viewport.YOffset {
								m.viewport.SetYOffset(idx)
							} else if idx+2 >= m.viewport.YOffset+availableH {
								m.viewport.SetYOffset(idx + 3 - availableH)
							}
							break
						}
					}
				}
			}
		}
		bodyView = m.viewport.View()
	}

	contentParts := []string{titleText, ""}
	if m.err != "" {
		contentParts = append(contentParts, m.styles.ErrorText.Width(innerW).MaxHeight(2).Render("❌ "+m.err), "")
	}
	contentParts = append(contentParts, bodyView, "", helpText)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}
