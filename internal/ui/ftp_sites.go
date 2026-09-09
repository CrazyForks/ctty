package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ftpcred"
	"github.com/zsuroy/ctty/internal/i18n"
)

// Site manager for saved FTP sites (mirrors telnet/serial list pattern).
type ftpSitesModel struct {
	styles      Styles
	width       int
	height      int
	table       table.Model
	sites       []ftpconfig.FTPSite
	filtered    []ftpconfig.FTPSite
	searchInput textinput.Model
	searchMode  bool
	deleteIdx   int
	confirmDel  bool
	ready       bool
	addMode     bool
	editMode    bool
	editOldName string
	addFields   []textinput.Model
	addFocus    int
	addErr      string
}

type ftpOpenBrowserMsg struct {
	siteName string
}

// NewFTPSitesForm creates the FTP site list manager.
func NewFTPSitesForm(styles Styles, width, height int) *ftpSitesModel {
	m := &ftpSitesModel{
		styles: styles,
		width:  width,
		height: height,
	}
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = "search sites…"
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(width, "/")
	m.reload()
	m.ready = true
	return m
}

func (m *ftpSitesModel) Init() tea.Cmd { return nil }

func (m *ftpSitesModel) reload() {
	sites, err := ftpconfig.Load()
	if err != nil {
		sites = nil
	}
	m.sites = sites
	m.applyFilter()
	m.rebuildTable()
}

func (m *ftpSitesModel) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" {
		m.filtered = m.sites
		return
	}
	words := strings.Fields(q)
	var out []ftpconfig.FTPSite
	for _, s := range m.sites {
		hay := strings.ToLower(s.Name + " " + s.Host + " " + s.User + " " + strings.Join(s.Tags, " "))
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s)
		}
	}
	m.filtered = out
}

func (m *ftpSitesModel) tableHeight() int {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

// siteRows renders the filtered sites for the current column set.
func (m *ftpSitesModel) siteRows(narrow bool) []table.Row {
	rows := make([]table.Row, 0, len(m.filtered))
	for _, s := range m.filtered {
		user := s.User
		if user == "" {
			user = "anonymous"
		}
		if narrow {
			rows = append(rows, table.Row{
				s.Name,
				fmt.Sprintf("%s@%s", user, s.Host),
				fmt.Sprintf("%d", s.Port),
			})
			continue
		}
		rows = append(rows, table.Row{
			s.Name,
			fmt.Sprintf("%s@%s", user, s.Host),
			fmt.Sprintf("%d", s.Port),
			strings.Join(s.Tags, ", "),
		})
	}
	return rows
}

func (m *ftpSitesModel) rebuildTable() {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.tableHeight()
	// Bubbles table renders each cell with Padding(0,1) = 2 extra cols per
	// cell; TableFocused adds border(2); App adds padding(2). So
	// rendered = colWidths + numCols*2 + 4, same convention as the serial and
	// telnet getColumns. Budget the column widths to fill the terminal exactly.
	portW := 6
	if w < 40 {
		portW = 4
	}
	if w < 74 {
		// Narrow terminals: drop the Tags column, keep Name/User@Host/Port.
		rem := w - 4 - 3*2 - portW
		if rem < 6 {
			rem = 6
		}
		hostW := rem * 3 / 5
		if hostW < 3 {
			hostW = 3
		}
		nameW := rem - hostW
		if nameW < 3 {
			nameW = 3
		}
		cols := []table.Column{
			{Title: "Name", Width: nameW},
			{Title: "User@Host", Width: hostW},
			{Title: "Port", Width: portW},
		}
		if m.table.Columns() == nil || len(m.table.Columns()) == 0 {
			m.table = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
		} else {
			m.table.SetColumns(cols)
			m.table.SetHeight(h)
		}
		m.table.SetRows(m.siteRows(true))
		return
	}
	rem := w - 4 - 4*2 - portW
	if rem < 12 {
		rem = 12
	}
	nameW := rem * 2 / 5
	if nameW < 12 {
		nameW = 12
	}
	hostW := rem * 2 / 5
	if hostW < 12 {
		hostW = 12
	}
	tagsW := rem - nameW - hostW
	if tagsW < 8 {
		tagsW = 8
	}
	cols := []table.Column{
		{Title: "Name", Width: nameW},
		{Title: "User@Host", Width: hostW},
		{Title: "Port", Width: portW},
		{Title: "Tags", Width: tagsW},
	}
	if m.table.Columns() == nil || len(m.table.Columns()) == 0 {
		m.table = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
	} else {
		m.table.SetColumns(cols)
		m.table.SetHeight(h)
	}
	m.table.SetRows(m.siteRows(false))
}

func (m *ftpSitesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildTable()
		return m, nil
	case tea.KeyMsg:
		if m.addMode || m.editMode {
			return m.handleAddKeys(msg)
		}
		if m.confirmDel {
			return m.handleDeleteConfirm(msg)
		}
		if m.searchMode {
			return m.handleSearch(msg)
		}
		return m.handleListKeys(msg)
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *ftpSitesModel) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		return m, func() tea.Msg { return ftpDoneMsg{} }
	case "enter":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		name := m.filtered[idx].Name
		return m, func() tea.Msg { return ftpOpenBrowserMsg{siteName: name} }
	case "/":
		m.searchMode = true
		m.searchInput.Focus()
		return m, nil
	case "a":
		m.startAdd()
		return m, nil
	case "e":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		m.startEdit(m.filtered[idx])
		return m, nil
	case "d", "x":
		if len(m.filtered) == 0 {
			return m, nil
		}
		m.deleteIdx = m.table.Cursor()
		m.confirmDel = true
		return m, nil
	case "r":
		m.reload()
		return m, nil
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
}

func (m *ftpSitesModel) handleSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.SetValue("")
		m.searchInput.Blur()
		m.applyFilter()
		m.rebuildTable()
		return m, nil
	case "enter":
		m.searchMode = false
		m.searchInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.applyFilter()
	m.rebuildTable()
	return m, cmd
}

func (m *ftpSitesModel) handleDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
			name := m.filtered[m.deleteIdx].Name
			_ = ftpconfig.Delete(name)
			_ = ftpcred.DeletePassword(name)
			m.reload()
		}
		m.confirmDel = false
		return m, nil
	case "n", "N", "esc":
		m.confirmDel = false
		return m, nil
	}
	return m, nil
}

func (m *ftpSitesModel) startAdd() {
	m.addMode = true
	m.editMode = false
	m.editOldName = ""
	m.addErr = ""
	m.addFocus = 0
	m.initFields(ftpconfig.DefaultSite(), "")
}

func (m *ftpSitesModel) startEdit(site ftpconfig.FTPSite) {
	m.addMode = false
	m.editMode = true
	m.editOldName = site.Name
	m.addErr = ""
	m.addFocus = 0
	pass, _ := ftpcred.GetPassword(site.Name)
	m.initFields(site, pass)
}

func (m *ftpSitesModel) initFields(site ftpconfig.FTPSite, password string) {
	labels := []string{"Name", "Host", "Port", "User", "Password (optional, cleartext)"}
	m.addFields = make([]textinput.Model, len(labels))
	for i, label := range labels {
		ti := textinput.New()
		ti.Placeholder = label
		ti.CharLimit = 128
		ti.Width = 40
		m.addFields[i] = ti
	}
	m.addFields[0].SetValue(site.Name)
	m.addFields[1].SetValue(site.Host)
	port := site.Port
	if port <= 0 {
		port = ftpconfig.DefaultPort
	}
	m.addFields[2].SetValue(fmt.Sprintf("%d", port))
	user := site.User
	if user == "" {
		user = "anonymous"
	}
	m.addFields[3].SetValue(user)
	m.addFields[4].EchoMode = textinput.EchoPassword
	m.addFields[4].EchoCharacter = '*'
	m.addFields[4].SetValue(password)
	m.addFields[0].Focus()
}

func (m *ftpSitesModel) handleAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.addMode = false
		m.editMode = false
		m.editOldName = ""
		m.addErr = ""
		return m, nil
	case "tab", "down":
		m.addFields[m.addFocus].Blur()
		m.addFocus = (m.addFocus + 1) % len(m.addFields)
		m.addFields[m.addFocus].Focus()
		return m, nil
	case "shift+tab", "up":
		m.addFields[m.addFocus].Blur()
		m.addFocus = (m.addFocus - 1 + len(m.addFields)) % len(m.addFields)
		m.addFields[m.addFocus].Focus()
		return m, nil
	case "enter", "ctrl+s":
		name := strings.TrimSpace(m.addFields[0].Value())
		host := strings.TrimSpace(m.addFields[1].Value())
		portStr := strings.TrimSpace(m.addFields[2].Value())
		user := strings.TrimSpace(m.addFields[3].Value())
		pass := m.addFields[4].Value()
		port := ftpconfig.DefaultPort
		if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port <= 0 {
			port = ftpconfig.DefaultPort
		}
		site := ftpconfig.FTPSite{Name: name, Host: host, Port: port, User: user}
		var err error
		if m.editMode {
			err = ftpconfig.Update(m.editOldName, site)
			if err == nil && m.editOldName != name {
				_ = ftpcred.DeletePassword(m.editOldName)
			}
		} else {
			err = ftpconfig.Add(site)
		}
		if err != nil {
			m.addErr = err.Error()
			return m, nil
		}
		if pass != "" {
			_ = ftpcred.SetPassword(name, pass)
		} else if m.editMode {
			_ = ftpcred.DeletePassword(name)
		}
		m.addMode = false
		m.editMode = false
		m.editOldName = ""
		m.reload()
		return m, nil
	}
	var cmd tea.Cmd
	m.addFields[m.addFocus], cmd = m.addFields[m.addFocus].Update(msg)
	return m, cmd
}

func (m *ftpSitesModel) View() string {
	if m.addMode || m.editMode {
		var b strings.Builder
		title := i18n.T("ftp.sites_add_title")
		if m.editMode {
			title = i18n.T("ftp.sites_edit_title")
		}
		b.WriteString(m.styles.FormTitle.Render(" "+title+" ") + "\n\n")
		labels := []string{"Name", "Host", "Port", "User", "Password"}
		for i, ti := range m.addFields {
			b.WriteString(fmt.Sprintf("  %-10s %s\n", labels[i]+":", ti.View()))
		}
		if m.addErr != "" {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.addErr) + "\n")
			b.WriteString("  " + m.styles.HelpText.Render(i18n.T("ftp.sites_form_err_hint")) + "\n")
		}
		b.WriteString("\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.sites_form_help")))
		b.WriteString("\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.cred_hint")))
		return renderFormPage(m.styles, m.width, b.String())
	}

	var components []string
	components = append(components, m.styles.Header.Render(" "+i18n.T("ftp.sites_title")+" "))
	// Truncate the path hint so long paths can't push past the right edge on narrow terminals.
	components = append(components, renderHelpText(m.styles, i18n.T("ftp.sites_paths"), m.width))
	if m.searchMode {
		components = append(components, renderSearchBar(m.styles, true, "/", m.searchInput.View(), m.width))
	}
	components = append(components, m.styles.TableFocused.Render(m.table.View()))
	if m.confirmDel && m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
		name := m.filtered[m.deleteIdx].Name
		components = append(components, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(
			i18n.T("ftp.sites_delete_confirm", name)))
	}
	components = append(components, renderHelpText(m.styles, i18n.T("ftp.sites_help"), m.width))
	return m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, components...))
}
