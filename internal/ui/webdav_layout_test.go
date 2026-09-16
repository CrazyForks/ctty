package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/webdavclient"
)

func TestWebDAVLayoutTogglePersists(t *testing.T) {
	i18n.SetLang("en")
	isolateAppConfigDir(t)

	m := NewWebDAVFormWithLayout(NewStyles(100), 100, 30, "site", config.WebDAVLayoutDual)
	m.client = &webdavclient.Client{}
	m.loading = false
	m.mode = webdavBrowse

	vKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}
	updated, _ := m.Update(vKey)
	fm := updated.(*webdavFormModel)
	if fm.layout != config.WebDAVLayoutSingle {
		t.Fatalf("layout after v = %q, want single", fm.layout)
	}
	saved, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if saved.WebDAVLayout != config.WebDAVLayoutSingle {
		t.Fatalf("persisted layout = %q, want single", saved.WebDAVLayout)
	}

	updated, _ = fm.Update(vKey)
	fm = updated.(*webdavFormModel)
	if fm.layout != config.WebDAVLayoutDual {
		t.Fatalf("layout after second v = %q, want dual", fm.layout)
	}
	saved, err = config.LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if saved.WebDAVLayout != config.WebDAVLayoutDual {
		t.Fatalf("persisted layout = %q, want dual", saved.WebDAVLayout)
	}
}

func TestWebDAVOpenBrowserRespectsConfiguredLayout(t *testing.T) {
	i18n.SetLang("en")
	single := config.AppConfig{WebDAVLayout: config.WebDAVLayoutSingle}
	m := Model{
		appConfig: &single,
		styles:    NewStyles(100),
		width:     100,
		height:    30,
	}
	updated, _ := m.Update(webdavOpenBrowserMsg{siteName: "site"})
	um := updated.(Model)
	if um.webdavForm == nil {
		t.Fatal("webdavForm not created")
	}
	if um.webdavForm.layout != config.WebDAVLayoutSingle {
		t.Fatalf("browser layout = %q, want single", um.webdavForm.layout)
	}
}
