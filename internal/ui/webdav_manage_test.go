package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/webdavclient"
	"github.com/zsuroy/ctty/internal/webdavconfig"
)

func newWebDAVManageTestForm(t *testing.T) *webdavFormModel {
	t.Helper()
	i18n.SetLang("en")
	m := NewWebDAVForm(NewStyles(100), 100, 30, "davsite")
	m.client = &webdavclient.Client{}
	m.loading = false
	m.mode = webdavBrowse
	m.cwd = "/remote"
	m.localCwd = t.TempDir()
	m.entries = []webdavclient.RemoteEntry{
		{Name: "data.txt", Size: 123, ModTime: time.Now()},
		{Name: "folder", IsDir: true, ModTime: time.Now()},
	}
	m.updateRemoteRows()
	m.remoteTbl.SetCursor(0)
	return m
}

func typeWebDAVRunes(t *testing.T, m *webdavFormModel, s string) *webdavFormModel {
	t.Helper()
	updated := tea.Model(m)
	for _, r := range s {
		updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	fm, ok := updated.(*webdavFormModel)
	if !ok {
		t.Fatal("Update did not return *webdavFormModel")
	}
	return fm
}

func TestWebDAVMkdirFlow(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*webdavFormModel)
	if fm.mode != webdavMkdirInput {
		t.Fatalf("mode = %v, want mkdir input", fm.mode)
	}

	fm = typeWebDAVRunes(t, fm, "newdir")
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*webdavFormModel)
	if fm.mode != webdavBrowse {
		t.Fatalf("mode = %v, want browse after enter", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected mkdir cmd after enter")
	}

	updated, cmd = fm.Update(webdavMkdirResultMsg{name: "newdir", success: true})
	fm = updated.(*webdavFormModel)
	if fm.loading {
		t.Fatal("loading must clear on mkdir result")
	}
	if cmd == nil {
		t.Fatal("expected refresh cmd after mkdir success")
	}
	if !strings.Contains(fm.statusMsg, "Directory created") && !strings.Contains(fm.statusMsg, "newdir") {
		t.Fatalf("status = %q, want created message", fm.statusMsg)
	}
}

func TestWebDAVMkdirEmptyNameCancels(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*webdavFormModel)
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*webdavFormModel)
	if fm.mode != webdavBrowse {
		t.Fatalf("mode = %v, want browse", fm.mode)
	}
	if cmd != nil {
		t.Fatal("empty name must not produce a cmd")
	}
}

func TestWebDAVDeleteConfirmFlow(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*webdavFormModel)
	if fm.mode != webdavDeleteConfirm {
		t.Fatalf("mode = %v, want delete confirm", fm.mode)
	}
	if fm.selected == nil || fm.selected.Name != "data.txt" {
		t.Fatalf("selected = %+v, want data.txt", fm.selected)
	}
	if !strings.Contains(fm.View(), "data.txt") {
		t.Fatal("delete confirm must show filename")
	}

	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*webdavFormModel)
	if fm.mode != webdavBrowse {
		t.Fatalf("mode = %v, want browse after confirm", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected delete cmd after confirm")
	}

	updated, cmd = fm.Update(webdavDeleteResultMsg{filename: "data.txt", success: true})
	fm = updated.(*webdavFormModel)
	if fm.selected != nil {
		t.Fatal("selected must clear on delete result")
	}
	if cmd == nil {
		t.Fatal("expected refresh cmd after delete success")
	}
	if !strings.Contains(fm.statusMsg, "data.txt") {
		t.Fatalf("status = %q, want data.txt", fm.statusMsg)
	}
}

func TestWebDAVDeleteCancel(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*webdavFormModel)
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*webdavFormModel)
	if fm.mode != webdavBrowse || fm.selected != nil {
		t.Fatal("esc must cancel delete confirm")
	}
}

func TestWebDAVRenameFlow(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*webdavFormModel)
	if fm.mode != webdavRenameInput {
		t.Fatalf("mode = %v, want rename input", fm.mode)
	}

	fm = typeWebDAVRunes(t, fm, "renamed.txt")
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*webdavFormModel)
	if fm.mode != webdavBrowse {
		t.Fatalf("mode = %v, want browse after enter", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected rename cmd after enter")
	}

	updated, cmd = fm.Update(webdavRenameResultMsg{oldName: "data.txt", newName: "renamed.txt", success: true})
	fm = updated.(*webdavFormModel)
	if cmd == nil {
		t.Fatal("expected refresh cmd after rename success")
	}
	if !strings.Contains(fm.statusMsg, "renamed.txt") {
		t.Fatalf("status = %q, want renamed.txt", fm.statusMsg)
	}
}

func TestWebDAVDownloadConfirmFlow(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	// Press Enter on remote file to prompt download
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*webdavFormModel)
	if fm.mode != webdavDownloadConfirm {
		t.Fatalf("mode = %v, want download confirm", fm.mode)
	}
	if !strings.Contains(fm.View(), "data.txt") {
		t.Fatal("download confirm view must show filename")
	}

	// Confirm download with Enter
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*webdavFormModel)
	if fm.mode != webdavBrowse {
		t.Fatalf("mode = %v, want browse after confirm", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected download start cmd")
	}
}

func TestWebDAVSearchMode(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	// Enter search
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	fm := updated.(*webdavFormModel)
	if !fm.searchMode {
		t.Fatal("expected searchMode true")
	}

	// Type filter query
	fm = typeWebDAVRunes(t, fm, "txt")
	if len(fm.remoteTbl.Rows()) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(fm.remoteTbl.Rows()))
	}

	// Esc to exit search
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*webdavFormModel)
	if fm.searchMode {
		t.Fatal("expected searchMode false after Esc")
	}
	if len(fm.remoteTbl.Rows()) != 2 {
		t.Fatalf("unfiltered rows = %d, want 2", len(fm.remoteTbl.Rows()))
	}
}

func TestWebDAVFocusToggle(t *testing.T) {
	m := newWebDAVManageTestForm(t)
	if m.focusLocal {
		t.Fatal("initially remote should be focused")
	}

	// Tab to switch to local
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	fm := updated.(*webdavFormModel)
	if !fm.focusLocal {
		t.Fatal("expected focusLocal to be true after Tab")
	}

	// Tab again to switch back to remote
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyTab})
	fm = updated.(*webdavFormModel)
	if fm.focusLocal {
		t.Fatal("expected focusLocal to be false after second Tab")
	}
}

func TestWebDAVInfoView(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	// 'i' to show info
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*webdavFormModel)
	if !fm.showInfo || fm.entryInfo == nil {
		t.Fatal("expected showInfo true and entryInfo non-nil")
	}
	if fm.entryInfo.name != "data.txt" {
		t.Fatalf("entry name = %s, want data.txt", fm.entryInfo.name)
	}

	view := fm.View()
	if !strings.Contains(view, "data.txt") {
		t.Fatal("info view should contain entry name")
	}

	// Esc closes info
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*webdavFormModel)
	if fm.showInfo {
		t.Fatal("expected showInfo false after Esc")
	}
}

func TestWebDAVLocalMkdirAndDelete(t *testing.T) {
	m := newWebDAVManageTestForm(t)

	// Switch focus to local
	m.setFocusLocal(true)

	// Create test directory on disk in m.localCwd
	testDir := filepath.Join(m.localCwd, "localtemp")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatal(err)
	}
	m.refreshLocal()
	m.localTbl.SetCursor(0)

	// Press 'd' to delete local entry
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*webdavFormModel)
	if fm.mode != webdavDeleteConfirm {
		t.Fatalf("mode = %v, want delete confirm", fm.mode)
	}
	if !fm.localOp {
		t.Fatal("expected localOp to be true")
	}

	// Confirm delete with 'y'
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*webdavFormModel)
	if fm.mode != webdavBrowse {
		t.Fatalf("mode = %v, want browse after delete", fm.mode)
	}
	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Fatal("local directory should have been removed")
	}
}

func TestWebDAVAddFormI18n(t *testing.T) {
	orig := i18n.CurrentLang()
	defer i18n.SetLang(orig)

	// In English
	i18n.SetLang("en")
	styles := NewStyles(100)
	formEN := newWebDAVAddForm(styles, 100, 30, nil)
	viewEN := formEN.View()
	if strings.Contains(viewEN, "common.yes") || strings.Contains(viewEN, "common.no") {
		t.Fatalf("English WebDAV add form contains raw i18n keys:\n%s", viewEN)
	}
	if !strings.Contains(viewEN, "Yes") || !strings.Contains(viewEN, "No") {
		t.Fatalf("English WebDAV add form missing Yes/No:\n%s", viewEN)
	}
	if !strings.Contains(viewEN, "Skip TLS Verify") {
		t.Fatalf("English WebDAV add form missing 'Skip TLS Verify':\n%s", viewEN)
	}

	// In Chinese
	i18n.SetLang("zh_CN")
	formZH := newWebDAVAddForm(styles, 100, 30, nil)
	viewZH := formZH.View()
	if strings.Contains(viewZH, "common.yes") || strings.Contains(viewZH, "common.no") {
		t.Fatalf("Chinese WebDAV add form contains raw i18n keys:\n%s", viewZH)
	}
	if !strings.Contains(viewZH, "是") || !strings.Contains(viewZH, "否") {
		t.Fatalf("Chinese WebDAV add form missing 是/否:\n%s", viewZH)
	}
	if !strings.Contains(viewZH, "忽略 TLS 证书校验") {
		t.Fatalf("Chinese WebDAV add form missing '忽略 TLS 证书校验':\n%s", viewZH)
	}
}

func TestWebDAVAddFormConfirmLeftAlignment(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(100)
	form := newWebDAVAddForm(styles, 100, 30, nil)

	// Verify left alignment: find the line with Yes and No and check that it is left-aligned (small leading indent <= 10)
	view := ansi.Strip(form.View())
	lines := strings.Split(view, "\n")
	foundButtons := false
	for _, l := range lines {
		if strings.Contains(l, "Yes") && strings.Contains(l, "No") {
			foundButtons = true
			idx := strings.Index(l, "Yes")
			if idx > 10 {
				t.Fatalf("expected Yes button to be left-aligned with index <= 10 (got %d) in line: %q", idx, l)
			}
			break
		}
	}
	if !foundButtons {
		t.Fatalf("did not find line containing Yes and No in:\n%s", view)
	}
}

func mockWebDAVServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "OPTIONS":
			w.Header().Set("DAV", "1, 2")
			w.WriteHeader(http.StatusOK)
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(207)
			p := r.URL.Path
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>` + p + `</D:href>
    <D:propstat>
      <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>` + p + `/WebDAV_README.txt</D:href>
    <D:propstat>
      <D:prop><D:resourcetype/><D:getcontentlength>1842</D:getcontentlength></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

func TestMockWebDAVInTUI(t *testing.T) {
	ts := mockWebDAVServer(t)
	defer ts.Close()

	m := NewWebDAVFormWithLayout(NewStyles(100), 100, 30, ts.URL, config.WebDAVLayoutDual)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd")
	}
	msg := cmd()
	updated, cmd2 := m.Update(msg)
	fm := updated.(*webdavFormModel)
	if cmd2 != nil {
		msg2 := cmd2()
		updated2, _ := fm.Update(msg2)
		fm2 := updated2.(*webdavFormModel)
		if len(fm2.entries) == 0 {
			t.Fatalf("expected loaded entries from mock server, got 0")
		}
	}
}

func TestFullTUIWebDAVFlow(t *testing.T) {
	ts := mockWebDAVServer(t)
	defer ts.Close()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if err := webdavconfig.Save([]webdavconfig.WebDAVSite{
		{Name: "mock-site", URL: ts.URL, User: "mockuser"},
	}); err != nil {
		t.Fatal(err)
	}

	m := NewModel(nil, "", false, "1.1.0", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	// Press 'W' to open WebDAV sites
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}})
	m = updated.(Model)
	if m.viewMode != ViewWebDAV {
		t.Fatalf("viewMode=%v want ViewWebDAV", m.viewMode)
	}

	// Press Enter
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected cmd after Enter on site")
	}

	msg := cmd()
	updated, cmd = m.Update(msg)
	m = updated.(Model)
	if cmd != nil {
		msg2 := cmd()
		updated, cmd = m.Update(msg2)
		m = updated.(Model)

		if cmd != nil {
			msg3 := cmd()
			updated, _ = m.Update(msg3)
			m = updated.(Model)
		}
	}

	if m.webdavForm == nil {
		t.Fatal("expected webdavForm to be initialized")
	}
	if len(m.webdavForm.entries) == 0 {
		t.Fatalf("expected entries to be loaded from mock server, got 0")
	}

	// Press 'v' to toggle layout to dual pane
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)
	if m.webdavForm.layout != config.WebDAVLayoutSingle && m.webdavForm.layout != config.WebDAVLayoutDual {
		t.Fatalf("unexpected layout: %v", m.webdavForm.layout)
	}
}

func TestDirectWebDAVBrowserModeLaunch(t *testing.T) {
	ts := mockWebDAVServer(t)
	defer ts.Close()

	m := NewModel(nil, "", false, "", false)
	m.webdavForm = NewWebDAVFormWithLayout(m.styles, m.width, m.height, ts.URL, config.WebDAVLayoutDual)
	m.viewMode = ViewWebDAVBrowse
	m.webdavOnly = true

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init cmd")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("expected BatchMsg with at least 2 cmds, got %#v", msg)
	}
	connectCmd := batch[len(batch)-1]
	connectMsg := connectCmd()
	updated, cmd2 := m.Update(connectMsg)
	m = updated.(Model)
	if cmd2 == nil {
		t.Fatalf("expected cmd2 after connect; form mode=%v, err=%v", m.webdavForm.mode, m.webdavForm.loadError)
	}
	msg2 := cmd2()
	updated, _ = m.Update(msg2)
	m = updated.(Model)
	if len(m.webdavForm.entries) == 0 {
		t.Fatalf("expected entries, got 0")
	}
}
