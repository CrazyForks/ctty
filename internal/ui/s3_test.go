package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/s3client"
	"github.com/zsuroy/ctty/internal/s3config"
)

func TestMainListOOpensS3Sites(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{{Name: "h1", Hostname: "example.com"}}
	m := NewModel(hosts, "", false, "v0.6.3", true)
	m.ready = true
	m.width, m.height = 100, 30
	m.viewMode = ViewList

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	um := updated.(Model)
	if um.viewMode != ViewS3 {
		t.Fatalf("viewMode=%v want ViewS3", um.viewMode)
	}
	if um.s3SitesForm == nil {
		t.Fatal("s3SitesForm not created")
	}

	// Lowercase o must remain open/quick-connect, not S3 sites.
	m2 := NewModel(hosts, "", false, "v0.6.3", true)
	m2.ready = true
	m2.width, m2.height = 100, 30
	m2.viewMode = ViewList
	m2.updateTableRows()
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	um = updated.(Model)
	if um.viewMode == ViewS3 {
		t.Fatal("lowercase o must not open S3")
	}
}

func TestS3SitesTableFitsTerminalWidth(t *testing.T) {
	i18n.SetLang("en")
	for _, width := range []int{20, 24, 30, 40, 50, 60, 70, 73, 74, 79, 80, 81, 90, 100, 120, 160, 200} {
		t.Run("width", func(t *testing.T) {
			m := NewS3SitesForm(NewStyles(width), width, 24)
			view := m.View()
			for _, line := range strings.Split(view, "\n") {
				if w := lineDisplayWidth(line); w > width {
					t.Errorf("width=%d: rendered line %d cols exceeds terminal\n%q", width, w, line)
				}
			}
		})
	}
}

func TestS3BrowserDualAndSinglePaneFitsTerminalWidth(t *testing.T) {
	i18n.SetLang("en")
	for _, layout := range []config.S3Layout{config.S3LayoutDual, config.S3LayoutSingle} {
		for _, width := range []int{40, 50, 60, 70, 79, 80, 81, 90, 100, 120, 140, 160, 200} {
			t.Run(string(layout), func(t *testing.T) {
				m := NewS3FormWithLayout(NewStyles(width), width, 30, "test-site", layout)
				m.client = &s3client.Client{}
				m.loading = false
				m.mode = s3Browse
				m.cwd = "my-bucket/documents/subfolder"
				m.entries = []s3client.RemoteEntry{
					{Name: "very_long_file_name_for_testing_overflow_protection.pdf", Size: 1048576, ModTime: time.Now()},
					{Name: "another_nested_folder_name", IsDir: true, ModTime: time.Now()},
				}
				m.updateRemoteRows()
				m.updateLocalRows()

				view := m.View()
				for i, line := range strings.Split(view, "\n") {
					w := ansi.StringWidth(line)
					if w > width {
						t.Errorf("layout=%s width=%d line %d width=%d exceeds terminal width:\n%s", layout, width, i, w, line)
					}
				}
			})
		}
	}
}

func TestS3BrowserSearchBarAbovePanes(t *testing.T) {
	i18n.SetLang("en")
	m := NewS3Form(NewStyles(100), 100, 30, "site")
	m.client = &s3client.Client{}
	m.loading = false
	m.mode = s3Browse
	m.searchMode = true
	m.cwd = "mybucket"
	m.entries = []s3client.RemoteEntry{{Name: "a.txt", IsDir: false, Size: 4}}
	m.refreshLocal()
	m.updateRemoteRows()

	view := ansi.Strip(m.View())
	searchIdx := strings.Index(view, "╭") // rounded search bar chrome
	tableIdx := strings.Index(view, "┌")  // pane border chrome
	if searchIdx < 0 || tableIdx < 0 || searchIdx > tableIdx {
		t.Fatalf("search bar must render above the panes: search at %d, panes at %d\n%s", searchIdx, tableIdx, view)
	}
}

func TestMainHelpAndHelpFormIncludeS3Key(t *testing.T) {
	i18n.SetLang("en")
	if !strings.Contains(i18n.T("main.help"), "O: s3") && !strings.Contains(i18n.T("main.help"), "O: S3") {
		t.Fatalf("main.help missing O: s3: %q", i18n.T("main.help"))
	}
	i18n.SetLang("zh")
	if !strings.Contains(i18n.T("main.help"), "O: S3") {
		t.Fatalf("zh main.help missing O: S3: %q", i18n.T("main.help"))
	}
	i18n.SetLang("en")
	help := NewHelpForm(NewStyles(120), 120, 50, "v0.6.3")
	view := help.View()
	if !strings.Contains(view, "O") || !strings.Contains(view, "S3") {
		t.Fatalf("help form missing O/S3 entry: %q", view)
	}
}

func TestS3LayoutTogglePersists(t *testing.T) {
	i18n.SetLang("en")
	isolateAppConfigDir(t)

	m := NewS3FormWithLayout(NewStyles(100), 100, 30, "site", config.S3LayoutDual)
	m.client = &s3client.Client{}
	m.loading = false
	m.mode = s3Browse

	vKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}
	updated, _ := m.Update(vKey)
	fm := updated.(*s3FormModel)
	if fm.layout != config.S3LayoutSingle {
		t.Fatalf("layout after v = %q, want single", fm.layout)
	}
	saved, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if saved.S3Layout != config.S3LayoutSingle {
		t.Fatalf("persisted layout = %q, want single", saved.S3Layout)
	}

	updated, _ = fm.Update(vKey)
	fm = updated.(*s3FormModel)
	if fm.layout != config.S3LayoutDual {
		t.Fatalf("layout after second v = %q, want dual", fm.layout)
	}
	saved, err = config.LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if saved.S3Layout != config.S3LayoutDual {
		t.Fatalf("persisted layout = %q, want dual", saved.S3Layout)
	}
}

func TestS3OpenBrowserRespectsConfiguredLayout(t *testing.T) {
	i18n.SetLang("en")
	single := config.AppConfig{S3Layout: config.S3LayoutSingle}
	m := Model{
		appConfig: &single,
		styles:    NewStyles(100),
		width:     100,
		height:    30,
	}
	updated, _ := m.Update(s3OpenBrowserMsg{siteName: "site"})
	um := updated.(Model)
	if um.s3Form == nil {
		t.Fatal("s3Form not created")
	}
	if um.s3Form.layout != config.S3LayoutSingle {
		t.Fatalf("browser layout = %q, want single", um.s3Form.layout)
	}
}

func newS3ManageTestForm(t *testing.T) *s3FormModel {
	t.Helper()
	i18n.SetLang("en")
	m := NewS3Form(NewStyles(100), 100, 30, "s3site")
	m.client = &s3client.Client{}
	m.loading = false
	m.mode = s3Browse
	m.cwd = "mybucket/prefix"
	m.localCwd = t.TempDir()
	m.entries = []s3client.RemoteEntry{
		{Name: "data.txt", Size: 123, ModTime: time.Now()},
		{Name: "folder", IsDir: true, ModTime: time.Now()},
	}
	m.updateRemoteRows()
	m.table.SetCursor(0)
	return m
}

func typeS3Runes(t *testing.T, m *s3FormModel, s string) *s3FormModel {
	t.Helper()
	updated := tea.Model(m)
	for _, r := range s {
		updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	fm, ok := updated.(*s3FormModel)
	if !ok {
		t.Fatal("Update did not return *s3FormModel")
	}
	return fm
}

func TestS3MkdirFlow(t *testing.T) {
	m := newS3ManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*s3FormModel)
	if fm.mode != s3MkdirInput {
		t.Fatalf("mode = %v, want mkdir input", fm.mode)
	}

	fm = typeS3Runes(t, fm, "newdir")
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*s3FormModel)
	if fm.mode != s3Browse {
		t.Fatalf("mode = %v, want browse after enter", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected mkdir cmd after enter")
	}

	updated, _ = fm.Update(s3MkdirResultMsg{name: "newdir", success: true})
	fm = updated.(*s3FormModel)
	if fm.loading {
		t.Fatal("loading must clear on mkdir result")
	}
}

func TestS3DeleteConfirmFlow(t *testing.T) {
	m := newS3ManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*s3FormModel)
	if fm.mode != s3DeleteConfirm {
		t.Fatalf("mode = %v, want delete confirm", fm.mode)
	}
	if fm.selected == nil || fm.selected.Name != "data.txt" {
		t.Fatalf("selected = %+v, want data.txt", fm.selected)
	}
	if !strings.Contains(fm.View(), "data.txt") {
		t.Fatal("delete confirm must show filename")
	}

	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*s3FormModel)
	if fm.mode != s3Browse {
		t.Fatalf("mode = %v, want browse after confirm", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected delete cmd after confirm")
	}
}

func TestS3DeleteCancel(t *testing.T) {
	m := newS3ManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*s3FormModel)
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*s3FormModel)
	if fm.mode != s3Browse {
		t.Fatal("esc must cancel delete confirm")
	}
}

func TestS3DownloadConfirmFlow(t *testing.T) {
	m := newS3ManageTestForm(t)
	m.table.SetCursor(0) // data.txt (file)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*s3FormModel)
	if fm.mode != s3DownloadConfirm {
		t.Fatalf("mode = %v, want download confirm", fm.mode)
	}
	if !strings.Contains(fm.View(), "data.txt") {
		t.Fatal("download confirm modal must include filename")
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*s3FormModel)
	if fm.mode != s3Browse {
		t.Fatal("esc must cancel download confirm")
	}
}

func TestS3SearchMode(t *testing.T) {
	m := newS3ManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	fm := updated.(*s3FormModel)
	if !fm.searchMode {
		t.Fatal("searchMode must be true after /")
	}

	fm = typeS3Runes(t, fm, "fold")
	filtered := fm.filteredRemote()
	if len(filtered) != 1 || filtered[0].Name != "folder" {
		t.Fatalf("filtered = %+v, want folder", filtered)
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*s3FormModel)
	if fm.searchMode {
		t.Fatal("searchMode must be false after esc")
	}
	if fm.searchInput.Value() != "" {
		t.Fatal("searchInput must clear on esc")
	}
}

func TestS3FocusToggle(t *testing.T) {
	m := newS3ManageTestForm(t)
	if m.focusLocal {
		t.Fatal("initial focus should be remote")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	fm := updated.(*s3FormModel)
	if !fm.focusLocal {
		t.Fatal("focus should switch to local on tab")
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyTab})
	fm = updated.(*s3FormModel)
	if fm.focusLocal {
		t.Fatal("focus should switch back to remote on second tab")
	}
}

func TestS3InfoView(t *testing.T) {
	m := newS3ManageTestForm(t)
	m.table.SetCursor(0)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*s3FormModel)
	if !fm.showInfo {
		t.Fatal("showInfo must be true after i")
	}
	view := fm.View()
	if !strings.Contains(view, "data.txt") {
		t.Fatal("info view must display filename")
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*s3FormModel)
	if fm.showInfo {
		t.Fatal("showInfo must be false after esc")
	}
}

func TestS3AddFormI18n(t *testing.T) {
	i18n.SetLang("en")
	form := newS3AddForm(NewStyles(80), 80, 24, nil)
	view := form.View()
	if !strings.Contains(view, "Add S3 Site") {
		t.Fatalf("English add form title missing: %q", view)
	}

	i18n.SetLang("zh")
	formZh := newS3AddForm(NewStyles(80), 80, 24, nil)
	viewZh := formZh.View()
	if !strings.Contains(viewZh, "添加 S3 站点") {
		t.Fatalf("Chinese add form title missing: %q", viewZh)
	}
}

func TestS3AddFormValidation(t *testing.T) {
	i18n.SetLang("en")
	form := newS3AddForm(NewStyles(80), 80, 24, nil)
	// Try saving without entering name or endpoint
	updated, _ := form.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	fm := updated.(*s3AddFormModel)
	if fm.done {
		t.Fatal("form must not complete when required fields are missing")
	}
	if fm.err == "" {
		t.Fatal("form should display error when required fields are empty")
	}
}

func TestS3SitesProbingAndBatchDelete(t *testing.T) {
	i18n.SetLang("en")
	isolateAppConfigDir(t)

	s1 := s3config.S3Site{Name: "site1", Endpoint: "s3.example.com", Bucket: "b1"}
	s2 := s3config.S3Site{Name: "site2", Endpoint: "s3.example.com", Bucket: "b2"}
	_ = s3config.Add(s1)
	_ = s3config.Add(s2)

	m := NewS3SitesForm(NewStyles(100), 100, 30)
	if len(m.sites) != 2 {
		t.Fatalf("expected 2 sites, got %d", len(m.sites))
	}

	// Space toggles selection
	m.table.SetCursor(0)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	sm := updated.(*s3SitesModel)
	if !sm.isSiteSelected("site1") {
		t.Fatal("site1 should be selected")
	}

	// Select all with Ctrl+A
	updated, _ = sm.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	sm = updated.(*s3SitesModel)
	if len(sm.selectedSites) != 2 {
		t.Fatalf("expected 2 selected sites, got %d", len(sm.selectedSites))
	}

	// Delete batch confirm
	updated, _ = sm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	sm = updated.(*s3SitesModel)
	if !sm.confirmDel {
		t.Fatal("confirmDel should be true")
	}

	// Confirm delete with 'y'
	updated, _ = sm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	sm = updated.(*s3SitesModel)
	if len(sm.sites) != 0 {
		t.Fatalf("expected 0 sites after batch delete, got %d", len(sm.sites))
	}
}

func TestS3SitesHelpAlwaysVisible(t *testing.T) {
	i18n.SetLang("en")
	isolateAppConfigDir(t)
	s := s3config.S3Site{Name: "site1", Endpoint: "s3.example.com", Bucket: "b1"}
	_ = s3config.Add(s)

	for _, size := range []struct{ w, h int }{
		{80, 20},
		{80, 24},
		{100, 30},
		{120, 40},
		{60, 18},
	} {
		m := NewS3SitesForm(NewStyles(size.w), size.w, size.h)
		rendered := m.View()
		canvas := RenderCanvas(size.w, size.h, rendered)
		if !strings.Contains(canvas, "browse") {
			t.Errorf("w=%d h=%d: bottom help text missing from RenderCanvas output:\n%s", size.w, size.h, canvas)
		}
	}
}

func TestS3SitesHelpKey(t *testing.T) {
	i18n.SetLang("en")
	m := NewS3SitesForm(NewStyles(80), 80, 24)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if cmd == nil {
		t.Fatal("expected command for '?' key")
	}
	msg := cmd()
	if _, ok := msg.(s3ShowHelpMsg); !ok {
		t.Fatalf("expected s3ShowHelpMsg for '?', got %T", msg)
	}

	updated, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if cmd == nil {
		t.Fatal("expected command for 'h' key")
	}
	msg = cmd()
	if _, ok := msg.(s3ShowHelpMsg); !ok {
		t.Fatalf("expected s3ShowHelpMsg for 'h', got %T", msg)
	}
}

func TestS3AddFormNavigationAndResize(t *testing.T) {
	i18n.SetLang("en")
	isolateAppConfigDir(t)
	site := s3config.S3Site{
		Name:      "test-site",
		Endpoint:  "s3.us-west-1.amazonaws.com",
		Bucket:    "my-bucket",
		Region:    "us-west-1",
		AccessKey: "access123",
	}

	form := newS3AddForm(NewStyles(80), 80, 24, &site)
	if form.nameVal != "test-site" || form.endpointVal != "s3.us-west-1.amazonaws.com" {
		t.Fatalf("initial values not populated correctly: %+v", form)
	}

	// Down arrow navigates to next field without error
	updated, _ := form.Update(tea.KeyMsg{Type: tea.KeyDown})
	fm := updated.(*s3AddFormModel)

	// Up arrow navigates to previous field
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyUp})
	fm = updated.(*s3AddFormModel)

	// Tab navigates to next field
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyTab})
	fm = updated.(*s3AddFormModel)

	// Window resize updates dimensions without destroying form values
	updated, _ = fm.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	fm = updated.(*s3AddFormModel)
	if fm.nameVal != "test-site" || fm.endpointVal != "s3.us-west-1.amazonaws.com" {
		t.Fatalf("values lost after WindowSizeMsg: %+v", fm)
	}

	// Esc cancels form
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*s3AddFormModel)
	if !fm.cancelled {
		t.Fatal("form should be cancelled after Esc")
	}
}

func TestHelpFormIncludesS3Category(t *testing.T) {
	i18n.SetLang("en")
	help := NewHelpForm(NewStyles(120), 120, 60, "v0.6.3")
	view := help.View()
	if !strings.Contains(view, "S3 View") {
		t.Fatalf("help form missing 'S3 View' category: %q", view)
	}
}

func TestS3UploadFlow(t *testing.T) {
	m := newS3ManageTestForm(t)
	// Create a dummy local file
	localFile := filepath.Join(m.localCwd, "upload.txt")
	_ = os.WriteFile(localFile, []byte("hello world"), 0644)
	m.refreshLocal()

	// Switch focus to local pane
	m.setFocusLocal(true)
	if !m.focusLocal {
		t.Fatal("expected focus on local pane")
	}

	// Cursor is at local upload.txt
	m.localTbl.SetCursor(0)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*s3FormModel)
	if !fm.transferring {
		t.Fatal("expected transferring=true when local file is selected for upload")
	}
	if cmd == nil {
		t.Fatal("expected transfer cmd to begin")
	}
	if fm.progressFile != "upload.txt" {
		t.Fatalf("expected progressFile upload.txt, got %q", fm.progressFile)
	}
}

func TestS3UploadAtRootBlocked(t *testing.T) {
	i18n.SetLang("en")
	m := NewS3Form(NewStyles(100), 100, 30, "s3site")
	m.client = &s3client.Client{}
	m.loading = false
	m.mode = s3Browse
	m.cwd = "" // at root (Buckets)
	m.localCwd = t.TempDir()

	localFile := filepath.Join(m.localCwd, "test.txt")
	_ = os.WriteFile(localFile, []byte("content"), 0644)
	m.refreshLocal()
	m.setFocusLocal(true)
	m.localTbl.SetCursor(0)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*s3FormModel)
	if fm.transferring {
		t.Fatal("uploading at root must be blocked")
	}
	if !strings.Contains(fm.statusMsg, "Cannot upload to root") {
		t.Fatalf("expected root upload status error, got %q", fm.statusMsg)
	}
	_ = cmd
}

func TestS3DirectoryNavigation(t *testing.T) {
	m := newS3ManageTestForm(t)
	// entries: [data.txt (index 0, file), folder (index 1, dir)]
	m.table.SetCursor(1) // select folder

	// Enter opens directory
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*s3FormModel)
	if !fm.loading {
		t.Fatal("entering directory should set loading=true")
	}
	if cmd == nil {
		t.Fatal("expected fetch command when entering directory")
	}

	// Simulate successful entries response
	newEntries := []s3client.RemoteEntry{{Name: "subfile.txt", Size: 456}}
	updated, _ = fm.Update(s3EntriesMsg{entries: newEntries, cwd: "mybucket/prefix/folder"})
	fm = updated.(*s3FormModel)
	if fm.cwd != "mybucket/prefix/folder" {
		t.Fatalf("cwd = %q, want mybucket/prefix/folder", fm.cwd)
	}
	if len(fm.entries) != 1 || fm.entries[0].Name != "subfile.txt" {
		t.Fatalf("entries = %+v, want subfile.txt", fm.entries)
	}

	// Go up ('h' or 'left')
	updated, cmd = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	fm = updated.(*s3FormModel)
	if !fm.loading {
		t.Fatal("going up should set loading=true")
	}
	if cmd == nil {
		t.Fatal("expected fetch command when going up")
	}
}

func TestS3BrowserRenameFlow(t *testing.T) {
	i18n.SetLang("en")
	m := NewS3Form(NewStyles(100), 100, 30, "test-site")
	m.client = &s3client.Client{}
	m.loading = false
	m.mode = s3Browse

	// 1. Root cwd ("") - buckets cannot be renamed
	m.cwd = ""
	m.entries = []s3client.RemoteEntry{{Name: "mybucket", IsDir: true}}
	m.updateRemoteRows()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*s3FormModel)
	if fm.mode != s3Browse {
		t.Fatalf("expected s3Browse mode when pressing R on root bucket, got %v", fm.mode)
	}
	if !strings.Contains(fm.statusMsg, "Bucket renaming is not supported") {
		t.Fatalf("expected bucket rename unsupported toast, got %q", fm.statusMsg)
	}

	// 2. Remote pane inside a bucket
	fm.cwd = "mybucket"
	fm.entries = []s3client.RemoteEntry{
		{Name: "report.pdf", Size: 1024, ModTime: time.Now()},
	}
	fm.updateRemoteRows()
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm = updated.(*s3FormModel)
	if fm.mode != s3RenameInput {
		t.Fatalf("expected s3RenameInput mode, got %v", fm.mode)
	}
	if fm.inputBuffer != "report.pdf" {
		t.Fatalf("expected inputBuffer 'report.pdf', got %q", fm.inputBuffer)
	}

	// Type new name and press enter
	fm.inputBuffer = "report_v2.pdf"
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*s3FormModel)
	if !fm.loading {
		t.Fatal("submitting rename should set loading=true")
	}
	if cmd == nil {
		t.Fatal("submitting rename should return a tea.Cmd")
	}

	// Simulate rename result
	updated, _ = fm.Update(s3RenameResultMsg{
		oldName: "report.pdf",
		newName: "report_v2.pdf",
		success: true,
	})
	fm = updated.(*s3FormModel)
	if fm.mode != s3Browse {
		t.Fatalf("mode after rename result = %v, want s3Browse", fm.mode)
	}
	if !strings.Contains(fm.statusMsg, "Renamed report.pdf → report_v2.pdf") {
		t.Fatalf("statusMsg = %q, want 'Renamed report.pdf → report_v2.pdf'", fm.statusMsg)
	}

	// 3. Local pane rename
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "local_doc.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}
	fm.localCwd = tmpDir
	fm.focusLocal = true
	fm.refreshLocal()

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm = updated.(*s3FormModel)
	if fm.mode != s3RenameInput || !fm.localOp {
		t.Fatalf("expected s3RenameInput with localOp=true, got mode=%v localOp=%v", fm.mode, fm.localOp)
	}
	if fm.inputBuffer != "local_doc.txt" {
		t.Fatalf("inputBuffer = %q, want local_doc.txt", fm.inputBuffer)
	}

	fm.inputBuffer = "local_doc_renamed.txt"
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*s3FormModel)
	if fm.mode != s3Browse {
		t.Fatalf("mode = %v, want s3Browse", fm.mode)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "local_doc_renamed.txt")); err != nil {
		t.Fatalf("expected renamed local file to exist: %v", err)
	}
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatalf("expected old local file to be removed: %v", err)
	}
}

func TestS3ErrorEscExitsCleanlyWithoutLooping(t *testing.T) {
	i18n.SetLang("en")
	m := NewS3Form(NewStyles(100), 100, 30, "test-disconnected-site")
	m.mode = s3Error
	m.loadError = "dial tcp 192.168.1.100:9000: connect: connection refused"

	// Verify error view is rendered
	view := m.View()
	if !strings.Contains(view, "connection refused") {
		t.Fatalf("expected error view to contain 'connection refused', got:\n%s", view)
	}

	// Pressing 'esc' in s3Error mode MUST return s3DoneMsg to exit immediately, without retrying
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command from Esc in s3Error mode, got nil")
	}
	msg := cmd()
	if _, ok := msg.(s3DoneMsg); !ok {
		t.Fatalf("expected s3DoneMsg, got %T: %+v", msg, msg)
	}
	fm := updated.(*s3FormModel)
	if fm.mode != s3Error {
		t.Fatalf("model mode should not have transitioned to %v before message processing", fm.mode)
	}

	// Also test 'q' and 'enter' and 'ctrl+c' in s3Error mode
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyEnter},
		{Type: tea.KeyCtrlC},
	} {
		_, cmd = m.Update(key)
		if cmd == nil {
			t.Fatalf("expected command for key %v in s3Error mode, got nil", key)
		}
		msg = cmd()
		if _, ok := msg.(s3DoneMsg); !ok {
			t.Fatalf("key %v: expected s3DoneMsg, got %T: %+v", key, msg, msg)
		}
	}
}

func TestS3ErrorEscTransitionsBackToS3Sites(t *testing.T) {
	i18n.SetLang("en")
	m := NewModel(nil, "", false, "v1.3.0", true)
	m.ready = true
	m.width, m.height = 100, 30
	m.viewMode = ViewS3Browse
	m.s3FromSites = true
	m.s3Form = NewS3Form(m.styles, m.width, m.height, "broken-site")
	m.s3Form.mode = s3Error
	m.s3Form.loadError = "connect timeout"

	// Esc in ViewS3Browse when in s3Error
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected cmd from Esc")
	}
	doneMsg := cmd()
	if _, ok := doneMsg.(s3DoneMsg); !ok {
		t.Fatalf("expected s3DoneMsg, got %T", doneMsg)
	}

	// Model handles s3DoneMsg and returns to ViewS3
	updated, _ = m.Update(doneMsg)
	m = updated.(Model)
	if m.viewMode != ViewS3 {
		t.Fatalf("viewMode = %v, want ViewS3", m.viewMode)
	}
	if m.s3Form != nil {
		t.Fatal("expected s3Form to be nil after s3DoneMsg")
	}
	if m.s3SitesForm == nil {
		t.Fatal("expected s3SitesForm to be restored")
	}
}
