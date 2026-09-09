package ui

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/ftpclient"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ftpcred"
	"github.com/zsuroy/ctty/internal/i18n"
)

// FTP dual-pane browser modes (modeled on SFTP browse / upload-select).
type ftpMode int

const (
	ftpBrowse      ftpMode = iota // remote pane focused
	ftpLocalBrowse                // local pane focused
	ftpDownloadConfirm
	ftpPasswordInput
	ftpError
)

// Narrow terminals show one focused pane (SFTP-style) instead of cramped dual columns.
const ftpNarrowWidth = 80

type ftpFormModel struct {
	styles   Styles
	width    int
	height   int
	siteName string
	site     ftpconfig.FTPSite

	client     *ftpclient.Client
	remoteTbl  table.Model
	localTbl   table.Model
	entries    []ftpclient.RemoteEntry
	cwd        string
	localCwd   string
	mode       ftpMode
	loading    bool
	loadError  string
	password   string
	focusLocal bool

	progressDone   int64
	progressTotal  int64
	progressFile   string
	progressGen    int
	transferring   bool
	transferCancel context.CancelFunc
	queue          ftpTransferQueue

	inputBuffer  string
	inputPrompt  string
	selected     *ftpclient.RemoteEntry
	statusMsg    string
	statusExpiry time.Time

	localFiles  []string
	searchInput textinput.Model
	searchMode  bool
}

type ftpConnectedMsg struct {
	client *ftpclient.Client
	cwd    string
}
type ftpEntriesMsg struct {
	entries []ftpclient.RemoteEntry
	cwd     string
	err     error
}
type ftpErrorMsg struct{ err error }
type ftpPasswordPromptMsg struct{}
type ftpDownloadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}
type ftpUploadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}
type ftpProgressMsg struct {
	gen      int
	filename string
	done     int64
	total    int64
	isUpload bool
}
type ftpDoneMsg struct{}
type ftpBackToSitesMsg struct{}

// NewFTPForm creates the dual-pane FTP browser for a saved site.
func NewFTPForm(styles Styles, width, height int, siteName string) *ftpFormModel {
	site, _ := ftpconfig.Find(siteName)
	m := &ftpFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		siteName:   siteName,
		site:       site,
		mode:       ftpBrowse,
		loading:    true,
		localCwd:   defaultLocalUploadDir(),
		focusLocal: false,
	}
	h := m.paneTableHeight()
	cols := m.remoteColumns()
	m.remoteTbl = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
	m.localTbl = table.New(table.WithColumns(m.localColumns()), table.WithHeight(h), table.WithFocused(false))
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = "filter…"
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(m.width, "/")
	return m
}

func (m *ftpFormModel) Init() tea.Cmd {
	return m.connectCmd("")
}

func (m *ftpFormModel) connectCmd(password string) tea.Cmd {
	return func() tea.Msg {
		client, err := ftpclient.Connect(m.site, password)
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "login") || strings.Contains(msg, "530") ||
				strings.Contains(msg, "auth") || strings.Contains(msg, "Login") {
				return ftpPasswordPromptMsg{}
			}
			return ftpErrorMsg{err: err}
		}
		cwd, err := client.CurrentDir()
		if err != nil {
			cwd = "/"
		}
		return ftpConnectedMsg{client: client, cwd: cwd}
	}
}

func (m *ftpFormModel) loadDirCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := m.client.ListDir(dir)
		if err != nil {
			return ftpEntriesMsg{cwd: dir, err: err}
		}
		return ftpEntriesMsg{entries: entries, cwd: dir}
	}
}

func (m *ftpFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case ftpConnectedMsg:
		m.client = msg.client
		m.cwd = msg.cwd
		m.loading = false
		m.mode = ftpBrowse
		m.refreshLocal()
		return m, m.loadDirCmd(m.cwd)

	case ftpPasswordPromptMsg:
		m.loading = false
		m.mode = ftpPasswordInput
		m.inputBuffer = ""
		m.inputPrompt = fmt.Sprintf("FTP password for %s@%s:", m.site.User, m.site.Addr())
		return m, nil

	case ftpErrorMsg:
		m.loading = false
		m.mode = ftpError
		m.loadError = msg.err.Error()
		return m, nil

	case ftpEntriesMsg:
		m.loading = false
		if msg.err != nil {
			m.setStatus("Error: " + msg.err.Error())
			return m, nil
		}
		m.cwd = msg.cwd
		m.entries = msg.entries
		sort.Slice(m.entries, func(i, j int) bool {
			if m.entries[i].IsDir != m.entries[j].IsDir {
				return m.entries[i].IsDir
			}
			return strings.ToLower(m.entries[i].Name) < strings.ToLower(m.entries[j].Name)
		})
		m.updateRemoteRows()
		return m, nil

	case ftpProgressMsg:
		if staleFTPProgress(m.transferring, m.progressGen, msg.gen) {
			return m, nil
		}
		m.progressDone = msg.done
		m.progressTotal = msg.total
		m.progressFile = msg.filename
		job := m.queue.current()
		if job.filename == "" {
			job = ftpTransferJob{filename: msg.filename, isUpload: msg.isUpload}
		}
		cur, total := m.queue.position()
		m.statusMsg = formatFTPProgress(job, msg.done, msg.total, cur, total)
		m.statusExpiry = time.Now().Add(10 * time.Second)
		return m, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return ftpProgressMsg{
				gen: msg.gen, filename: job.filename,
				done: m.progressDone, total: m.progressTotal, isUpload: job.isUpload,
			}
		})

	case ftpDownloadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, false)
	case ftpUploadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, true)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.searchInput.Width = searchInputWidth(m.width, "/")
		h := m.paneTableHeight()
		m.remoteTbl.SetHeight(h)
		m.localTbl.SetHeight(h)
		m.updateRemoteRows()
		m.updateLocalRows()
		return m, nil

	case tea.KeyMsg:
		if m.mode == ftpError {
			if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, func() tea.Msg { return ftpDoneMsg{} }
			}
			return m, nil
		}
		if m.mode == ftpPasswordInput {
			return m.handlePasswordInput(msg)
		}
		if m.mode == ftpDownloadConfirm {
			return m.handleDownloadConfirm(msg)
		}
		if m.searchMode {
			return m.handleSearchKeys(msg)
		}
		return m.handleBrowseKeys(msg)
	}

	if m.focusLocal {
		m.localTbl, cmd = m.localTbl.Update(msg)
	} else {
		m.remoteTbl, cmd = m.remoteTbl.Update(msg)
	}
	return m, cmd
}

func (m *ftpFormModel) handlePasswordInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, func() tea.Msg { return ftpDoneMsg{} }
	case "enter":
		m.password = m.inputBuffer
		m.loading = true
		m.mode = ftpBrowse
		// Persist to dedicated FTP cred store (not SSH vault).
		_ = ftpcred.SetPassword(m.siteName, m.password)
		return m, m.connectCmd(m.password)
	case "backspace":
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
		return m, nil
	default:
		if len(msg.Runes) == 1 && msg.Type == tea.KeyRunes {
			m.inputBuffer += string(msg.Runes)
		}
		return m, nil
	}
}

func (m *ftpFormModel) handleDownloadConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = ftpBrowse
		if m.selected == nil {
			return m, nil
		}
		remote := path.Join(m.cwd, m.selected.Name)
		local := filepath.Join(m.localCwd, m.selected.Name)
		job := ftpTransferJob{
			filename: m.selected.Name, localPath: local, remotePath: remote, isUpload: false,
		}
		return m, m.requestTransfer(job)
	case "n", "N", "esc":
		m.mode = ftpBrowse
		return m, nil
	}
	return m, nil
}

func (m *ftpFormModel) setFocusLocal(local bool) {
	m.focusLocal = local
	if local {
		m.mode = ftpLocalBrowse
		m.localTbl.Focus()
		m.remoteTbl.Blur()
	} else {
		m.mode = ftpBrowse
		m.remoteTbl.Focus()
		m.localTbl.Blur()
	}
}

func (m *ftpFormModel) handleBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q":
		if m.transferring {
			m.cancelTransfer()
			return m, nil
		}
		if m.client != nil {
			_ = m.client.Close()
			m.client = nil
		}
		return m, func() tea.Msg { return ftpDoneMsg{} }
	case "tab":
		m.setFocusLocal(!m.focusLocal)
		return m, nil
	case "u":
		// Focus local pane (SFTP-style upload switch). If already local, Enter semantics.
		if !m.focusLocal {
			m.setFocusLocal(true)
			return m, nil
		}
		return m.handleLocalEnter()
	case "/":
		m.searchMode = true
		m.searchInput.Focus()
		return m, nil
	case "left", "h", "backspace":
		if m.focusLocal {
			return m, m.localUp()
		}
		return m, m.remoteUp()
	case "right", "l":
		// Open directory only (Enter handles file transfer).
		if m.focusLocal {
			return m.handleLocalOpenDir()
		}
		return m.handleRemoteOpenDir()
	case "enter":
		// Enter = enter dir / transfer file
		if m.focusLocal {
			return m.handleLocalEnter()
		}
		return m.handleRemoteEnter()
	case "d":
		// Direct download (no confirm) — reduces d/Enter/confirm stacking.
		if m.focusLocal {
			return m, nil
		}
		return m.startDownloadDirect()
	case "r":
		if m.transferring {
			return m, nil
		}
		if m.focusLocal {
			m.refreshLocal()
			return m, nil
		}
		m.loading = true
		return m, m.loadDirCmd(m.cwd)
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		if m.focusLocal {
			m.localTbl, cmd = m.localTbl.Update(msg)
		} else {
			m.remoteTbl, cmd = m.remoteTbl.Update(msg)
		}
		return m, cmd
	}
	return m, nil
}

func (m *ftpFormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.SetValue("")
		m.searchInput.Blur()
		m.updateRemoteRows()
		m.updateLocalRows()
		return m, nil
	case "enter":
		m.searchMode = false
		m.searchInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.updateRemoteRows()
	m.updateLocalRows()
	return m, cmd
}

func (m *ftpFormModel) handleRemoteOpenDir() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil || !e.IsDir {
		return m, nil
	}
	next := path.Join(m.cwd, e.Name)
	m.loading = true
	return m, m.loadDirCmd(next)
}

func (m *ftpFormModel) handleRemoteEnter() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	if e.IsDir {
		next := path.Join(m.cwd, e.Name)
		m.loading = true
		return m, m.loadDirCmd(next)
	}
	// File: one confirm, then transfer.
	if m.transferring {
		remote := path.Join(m.cwd, e.Name)
		local := filepath.Join(m.localCwd, e.Name)
		return m, m.requestTransfer(ftpTransferJob{
			filename: e.Name, localPath: local, remotePath: remote, isUpload: false,
		})
	}
	m.selected = e
	m.mode = ftpDownloadConfirm
	return m, nil
}

func (m *ftpFormModel) handleLocalOpenDir() (tea.Model, tea.Cmd) {
	idx := m.localTbl.Cursor()
	files := m.filteredLocal()
	if idx < 0 || idx >= len(files) {
		return m, nil
	}
	full := files[idx]
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		return m, nil
	}
	m.localCwd = full
	m.refreshLocal()
	return m, nil
}

func (m *ftpFormModel) handleLocalEnter() (tea.Model, tea.Cmd) {
	idx := m.localTbl.Cursor()
	files := m.filteredLocal()
	if idx < 0 || idx >= len(files) {
		return m, nil
	}
	full := files[idx]
	info, err := os.Stat(full)
	if err != nil {
		return m, nil
	}
	if info.IsDir() {
		m.localCwd = full
		m.refreshLocal()
		return m, nil
	}
	remote := path.Join(m.cwd, filepath.Base(full))
	job := ftpTransferJob{
		filename: filepath.Base(full), localPath: full, remotePath: remote, isUpload: true,
	}
	return m, m.requestTransfer(job)
}

func (m *ftpFormModel) startDownloadDirect() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil || e.IsDir {
		return m, nil
	}
	remote := path.Join(m.cwd, e.Name)
	local := filepath.Join(m.localCwd, e.Name)
	job := ftpTransferJob{
		filename: e.Name, localPath: local, remotePath: remote, isUpload: false,
	}
	return m, m.requestTransfer(job)
}

func (m *ftpFormModel) remoteUp() tea.Cmd {
	if m.cwd == "/" || m.cwd == "" {
		return nil
	}
	parent := path.Dir(m.cwd)
	if parent == "." {
		parent = "/"
	}
	m.loading = true
	return m.loadDirCmd(parent)
}

func (m *ftpFormModel) localUp() tea.Cmd {
	if isLocalFilesystemRoot(m.localCwd) {
		return nil
	}
	parent := filepath.Dir(m.localCwd)
	if parent == m.localCwd {
		return nil
	}
	m.localCwd = parent
	m.refreshLocal()
	return nil
}

func (m *ftpFormModel) selectedRemote() *ftpclient.RemoteEntry {
	rows := m.filteredRemote()
	idx := m.remoteTbl.Cursor()
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	return &rows[idx]
}

func (m *ftpFormModel) filteredRemote() []ftpclient.RemoteEntry {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || m.focusLocal {
		return m.entries
	}
	var out []ftpclient.RemoteEntry
	for _, e := range m.entries {
		if strings.Contains(strings.ToLower(e.Name), q) {
			out = append(out, e)
		}
	}
	return out
}

func (m *ftpFormModel) filteredLocal() []string {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || !m.focusLocal {
		return m.localFiles
	}
	var out []string
	for _, f := range m.localFiles {
		if strings.Contains(strings.ToLower(filepath.Base(f)), q) {
			out = append(out, f)
		}
	}
	return out
}

func (m *ftpFormModel) refreshLocal() {
	m.localFiles = nil
	entries, err := os.ReadDir(m.localCwd)
	if err != nil {
		return
	}
	for _, e := range entries {
		m.localFiles = append(m.localFiles, filepath.Join(m.localCwd, e.Name()))
	}
	sort.Slice(m.localFiles, func(i, j int) bool {
		ai, _ := os.Stat(m.localFiles[i])
		aj, _ := os.Stat(m.localFiles[j])
		if ai != nil && aj != nil && ai.IsDir() != aj.IsDir() {
			return ai.IsDir()
		}
		return strings.ToLower(filepath.Base(m.localFiles[i])) < strings.ToLower(filepath.Base(m.localFiles[j]))
	})
	m.updateLocalRows()
}

func (m *ftpFormModel) updateRemoteRows() {
	cols := m.remoteColumns()
	m.remoteTbl.SetColumns(cols)
	var rows []table.Row
	for _, e := range m.filteredRemote() {
		kind := "file"
		if e.IsDir {
			kind = "dir"
		}
		rows = append(rows, table.Row{e.Name, kind, formatSize(e.Size)})
	}
	m.remoteTbl.SetRows(rows)
}

func (m *ftpFormModel) updateLocalRows() {
	cols := m.localColumns()
	m.localTbl.SetColumns(cols)
	var rows []table.Row
	for _, f := range m.filteredLocal() {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		kind := "file"
		sz := formatSize(info.Size())
		if info.IsDir() {
			kind = "dir"
			sz = ""
		}
		rows = append(rows, table.Row{filepath.Base(f), kind, sz})
	}
	m.localTbl.SetRows(rows)
}

func (m *ftpFormModel) paneTableHeight() int {
	h := m.height - 10
	if h < 5 {
		h = 5
	}
	return h
}

func (m *ftpFormModel) narrow() bool {
	return m.width > 0 && m.width < ftpNarrowWidth
}

func (m *ftpFormModel) paneWidth() int {
	if m.narrow() {
		// Single pane: lipgloss Width is content-only; border (2) sits outside.
		// App pad(2) + border(2) = 4 → content budget = term - 4.
		w := m.width - 4
		if w < 20 {
			w = 20
		}
		return w
	}
	// Dual pane: App pad(2) + gap("  ") + two pane borders(2*2) = 8 outside content.
	// Width(pw) is content width per pane; visual = 2*(pw+2) + 2 + 2 = 2*pw + 8.
	w := (m.width - 8) / 2
	if w < 20 {
		w = 20
	}
	return w
}

// paneContentWidth is the bubbles-table column budget inside Width(paneWidth) content.
func (m *ftpFormModel) paneContentWidth() int {
	// 3 cells * Padding(0,1)=2 → 6 chrome inside the bordered content box.
	w := m.paneWidth() - 6
	if w < 12 {
		return 12
	}
	return w
}

func (m *ftpFormModel) remoteColumns() []table.Column {
	inner := m.paneContentWidth()
	nameW := inner - 5 - 8
	if nameW < 8 {
		nameW = 8
	}
	return []table.Column{
		{Title: "Remote", Width: nameW},
		{Title: "Type", Width: 5},
		{Title: "Size", Width: 8},
	}
}

func (m *ftpFormModel) localColumns() []table.Column {
	inner := m.paneContentWidth()
	nameW := inner - 5 - 8
	if nameW < 8 {
		nameW = 8
	}
	return []table.Column{
		{Title: "Local", Width: nameW},
		{Title: "Type", Width: 5},
		{Title: "Size", Width: 8},
	}
}

func (m *ftpFormModel) requestTransfer(job ftpTransferJob) tea.Cmd {
	started, now := m.queue.startOrEnqueue(job)
	if !now {
		m.setStatus(fmt.Sprintf("Queued: %s", job.filename))
		return nil
	}
	return m.beginJob(started)
}

func (m *ftpFormModel) beginJob(job ftpTransferJob) tea.Cmd {
	m.transferring = true
	m.loading = true
	m.progressFile = job.filename
	m.progressDone = 0
	m.progressTotal = 0
	m.progressGen++
	gen := m.progressGen
	cur, total := m.queue.position()
	m.statusMsg = formatFTPProgress(job, 0, 0, cur, total)
	m.statusExpiry = time.Now().Add(10 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	m.transferCancel = cancel

	if job.isUpload {
		return m.uploadCmd(ctx, gen, job)
	}
	return m.downloadCmd(ctx, gen, job)
}

func (m *ftpFormModel) downloadCmd(ctx context.Context, gen int, job ftpTransferJob) tea.Cmd {
	download := func() tea.Msg {
		err := m.client.DownloadWithProgressCtx(ctx, job.remotePath, job.localPath, func(done, total int64) {
			m.progressDone = done
			m.progressTotal = total
		})
		if ctx.Err() != nil {
			return ftpDownloadResultMsg{gen: gen, filename: job.filename, success: false, err: ctx.Err()}
		}
		return ftpDownloadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
	}
	return tea.Batch(
		download,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return ftpProgressMsg{
				gen: gen, filename: job.filename,
				done: m.progressDone, total: m.progressTotal, isUpload: false,
			}
		}),
	)
}

func (m *ftpFormModel) uploadCmd(ctx context.Context, gen int, job ftpTransferJob) tea.Cmd {
	upload := func() tea.Msg {
		err := m.client.UploadWithProgressCtx(ctx, job.localPath, job.remotePath, func(done, total int64) {
			m.progressDone = done
			m.progressTotal = total
		})
		if ctx.Err() != nil {
			return ftpUploadResultMsg{gen: gen, filename: job.filename, success: false, err: ctx.Err()}
		}
		return ftpUploadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
	}
	return tea.Batch(
		upload,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return ftpProgressMsg{
				gen: gen, filename: job.filename,
				done: m.progressDone, total: m.progressTotal, isUpload: true,
			}
		}),
	)
}

func (m *ftpFormModel) handleTransferResult(gen int, filename string, success bool, err error, isUpload bool) tea.Cmd {
	if !m.transferring || gen != m.progressGen {
		return nil
	}
	next, ok := m.queue.finishCurrent()
	if ok {
		return m.beginJob(next)
	}
	m.transferring = false
	m.loading = false
	m.transferCancel = nil
	if success {
		action := "Downloaded"
		if isUpload {
			action = "Uploaded"
		}
		m.setStatus(fmt.Sprintf("%s %s", action, filename))
		if isUpload {
			return m.loadDirCmd(m.cwd)
		}
		m.refreshLocal()
		return nil
	}
	if err != nil {
		if err == context.Canceled {
			m.setStatus(fmt.Sprintf("Cancelled: %s", filename))
		} else {
			m.setStatus("Error: " + err.Error())
		}
	}
	return nil
}

func (m *ftpFormModel) cancelTransfer() {
	if m.transferCancel != nil {
		m.transferCancel()
		m.transferCancel = nil
	}
	if m.client != nil {
		m.client.AbortTransfer()
	}
	// Keep transferring=true until the transfer result arrives so refresh/List
	// cannot race a still-finishing Read (SFTP-style wait-for-result cancel).
	m.queue.clear()
	m.setStatus(fmt.Sprintf("Cancelling: %s", m.progressFile))
}

func (m *ftpFormModel) setStatus(s string) {
	m.statusMsg = s
	m.statusExpiry = time.Now().Add(4 * time.Second)
}

func (m *ftpFormModel) statusActive() bool {
	return m.statusMsg != "" && time.Now().Before(m.statusExpiry)
}

func (m *ftpFormModel) focusLabel(local bool) string {
	active := local == m.focusLocal
	name := "[REMOTE]"
	if local {
		name = "[LOCAL]"
	}
	if active {
		return "● " + name
	}
	return "  " + name
}

func (m *ftpFormModel) renderErrorView() string {
	inner := formPageInnerWidth(m.width)
	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true).
		Padding(1, 2).
		Width(inner)
	detail := m.loadError
	if detail == "" {
		detail = "(no details)"
	}
	content := i18n.T("ftp.err_session", detail)
	return renderFormPage(m.styles, m.width, errStyle.Render(content))
}

func (m *ftpFormModel) View() string {
	if m.loading && m.client == nil && m.mode != ftpPasswordInput {
		inner := formPageInnerWidth(m.width)
		body := lipgloss.NewStyle().Width(inner).Render(
			m.styles.FormTitle.Render(" FTP — "+m.siteName+" ") + "\n\n" +
				"  Connecting to " + m.site.Addr() + "…")
		return renderFormPage(m.styles, m.width, body)
	}
	if m.mode == ftpPasswordInput {
		inner := formPageInnerWidth(m.width)
		body := lipgloss.NewStyle().Width(inner).Render(
			m.styles.FormTitle.Render(" FTP — Password ") + "\n\n" +
				fmt.Sprintf("  %s\n", m.inputPrompt) +
				fmt.Sprintf("  %s_\n", strings.Repeat("*", len(m.inputBuffer))) +
				"\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.help_password")) +
				"\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.cred_hint")))
		return renderFormPage(m.styles, m.width, body)
	}
	if m.mode == ftpError {
		return m.renderErrorView()
	}

	localStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	remoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
	if m.focusLocal {
		localStyle = localStyle.Bold(true)
	} else {
		remoteStyle = remoteStyle.Bold(true)
	}

	header := m.styles.Header.Render(fmt.Sprintf(" FTP — %s (%s@%s) ", m.siteName, m.site.User, m.site.Addr()))

	var paths string
	var panes string
	pw := m.paneWidth()
	if m.narrow() {
		// SFTP-style single focused pane on narrow terminals.
		if m.focusLocal {
			paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pw-4)))
			panes = m.styles.TableFocused.Width(pw).Render(m.localTbl.View())
		} else {
			paths = remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(m.cwd, pw-4)))
			panes = m.styles.TableFocused.Width(pw).Render(m.remoteTbl.View())
		}
	} else {
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pw-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(m.cwd, pw-4)))
		localBoxStyle := m.styles.TableUnfocused
		remoteBoxStyle := m.styles.TableUnfocused
		if m.focusLocal {
			localBoxStyle = m.styles.TableFocused
		} else {
			remoteBoxStyle = m.styles.TableFocused
		}
		localBox := localBoxStyle.Width(pw).Render(m.localTbl.View())
		remoteBox := remoteBoxStyle.Width(pw).Render(m.remoteTbl.View())
		panes = lipgloss.JoinHorizontal(lipgloss.Top, localBox, "  ", remoteBox)
	}

	var extras []string
	if m.statusActive() || m.transferring {
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		prefix := " "
		if m.transferring {
			prefix = " ⏳ "
		}
		extras = append(extras, progressStyle.Render(prefix+m.statusMsg))
	}
	if m.mode == ftpDownloadConfirm && m.selected != nil {
		extras = append(extras, lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Render(
			i18n.T("ftp.download_confirm", m.selected.Name, filepath.Join(m.localCwd, m.selected.Name))))
	}

	var helpParts []string
	if m.height < 20 {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("ftp.help_search_1"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("ftp.help_local_1"))
		} else {
			helpParts = append(helpParts, i18n.T("ftp.help_remote_1"))
		}
	} else {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("ftp.help_search_1"), i18n.T("ftp.help_search_2"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("ftp.help_local_1"), i18n.T("ftp.help_local_2"))
		} else {
			helpParts = append(helpParts, i18n.T("ftp.help_remote_1"), i18n.T("ftp.help_remote_2"))
		}
	}
	helpParts = dedupeStrings(helpParts)

	// Search bar sits above the panes like every other view (host list/serial/
	// telnet/FTP sites) so focus never jumps to the bottom of the screen.
	parts := []string{header, paths}
	if m.searchMode {
		parts = append(parts, renderSearchBar(m.styles, true, "/", m.searchInput.View(), m.width))
	}
	parts = append(parts, panes)
	parts = append(parts, extras...)
	if help := strings.Join(helpParts, "\n"); help != "" {
		// Single renderHelpText so the footer block is not joined twice.
		parts = append(parts, renderHelpText(m.styles, help, m.width))
	}
	return m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
