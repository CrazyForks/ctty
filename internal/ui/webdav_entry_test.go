package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/webdavclient"
)

func TestMainListWOpensWebDAVSites(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{{Name: "h1", Hostname: "example.com"}}
	m := NewModel(hosts, "", false, "v0.6.3", true)
	m.ready = true
	m.width, m.height = 100, 30
	m.viewMode = ViewList

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	um := updated.(Model)
	if um.viewMode != ViewWebDAV {
		t.Fatalf("viewMode=%v want ViewWebDAV", um.viewMode)
	}
	if um.webdavSitesForm == nil {
		t.Fatal("webdavSitesForm not created")
	}

	// Lowercase w must remain tags filter, not WebDAV sites.
	m2 := NewModel(hosts, "", false, "v0.6.3", true)
	m2.ready = true
	m2.width, m2.height = 100, 30
	m2.viewMode = ViewList
	m2.updateTableRows()
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	um = updated.(Model)
	if um.viewMode == ViewWebDAV {
		t.Fatal("lowercase w must not open WebDAV")
	}
}

func TestWebDAVSitesTableFitsTerminalWidth(t *testing.T) {
	i18n.SetLang("en")
	for _, width := range []int{20, 24, 30, 40, 50, 60, 70, 73, 74, 79, 80, 81, 90, 100, 120, 160, 200} {
		t.Run("width", func(t *testing.T) {
			m := NewWebDAVSitesForm(NewStyles(width), width, 24)
			view := m.View()
			for _, line := range strings.Split(view, "\n") {
				if w := lineDisplayWidth(line); w > width {
					t.Errorf("width=%d: rendered line %d cols exceeds terminal\n%q", width, w, line)
				}
			}
		})
	}
}

func TestWebDAVBrowserSearchBarAbovePanes(t *testing.T) {
	i18n.SetLang("en")
	m := NewWebDAVForm(NewStyles(100), 100, 30, "site")
	m.client = &webdavclient.Client{}
	m.loading = false
	m.mode = webdavBrowse
	m.searchMode = true
	m.cwd = "/"
	m.entries = []webdavclient.RemoteEntry{{Name: "a.txt", IsDir: false, Size: 4}}
	m.refreshLocal()
	m.updateRemoteRows()

	view := ansi.Strip(m.View())
	searchIdx := strings.Index(view, "╭") // rounded search bar chrome
	tableIdx := strings.Index(view, "┌")  // pane border chrome
	if searchIdx < 0 || tableIdx < 0 || searchIdx > tableIdx {
		t.Fatalf("search bar must render above the panes: search at %d, panes at %d\n%s", searchIdx, tableIdx, view)
	}
}

func TestMainHelpAndHelpFormIncludeWebDAVKey(t *testing.T) {
	i18n.SetLang("en")
	if !strings.Contains(i18n.T("main.help"), "W: webdav") {
		t.Fatalf("main.help missing W: webdav: %q", i18n.T("main.help"))
	}
	i18n.SetLang("zh")
	if !strings.Contains(i18n.T("main.help"), "W: WebDAV") {
		t.Fatalf("zh main.help missing W: WebDAV: %q", i18n.T("main.help"))
	}
	i18n.SetLang("en")
	help := NewHelpForm(NewStyles(120), 120, 50, "v0.6.3")
	view := help.View()
	if !strings.Contains(view, "W") || !strings.Contains(view, "WebDAV") {
		t.Fatalf("help form missing W/WebDAV entry: %q", view)
	}
}
