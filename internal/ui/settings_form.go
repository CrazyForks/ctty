package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

type settingsField int

const (
	settingsFieldLang settingsField = iota
	settingsFieldUpdate
	settingsFieldEscQuit
	settingsFieldFTPLayout
	settingsFieldSFTPLayout
	numSettingsFields
)

// langKeys are config codes persisted to disk; langOptionKeys map the same
// index to the i18n key rendering the current language option.
var langKeys = []string{"auto", "zh_CN", "en"}
var langOptionKeys = []string{"settings.lang_auto", "settings.lang_zh", "settings.lang_en"}
var updateKeys = []string{"settings.update_on", "settings.update_off"}
var escKeys = []string{"settings.esc_on", "settings.esc_off"}
var ftpLayoutKeys = []string{"ftp.layout_dual", "ftp.layout_single"}
var sftpLayoutKeys = []string{"sftp.layout_dual", "sftp.layout_single"}

type settingsFormModel struct {
	styles     Styles
	width      int
	height     int
	appConfig  config.AppConfig
	focusIndex settingsField

	scrollOffset int

	langIndex       int // 0=auto, 1=zh_CN, 2=en
	updateIndex     int // 0=enabled, 1=disabled
	disableEscIndex int // 0=enabled (quit on esc), 1=disabled (vim mode)
	ftpLayoutIndex  int // 0=dual, 1=single
	sftpLayoutIndex int // 0=dual, 1=single

	saved     bool
	cancelled bool
}

type settingsCloseMsg struct {
	Saved     bool
	AppConfig *config.AppConfig
}

// NewSettingsForm creates a new settings form
func NewSettingsForm(styles Styles, width, height int, appConfig *config.AppConfig) *settingsFormModel {
	cfg := config.GetDefaultAppConfig()
	if appConfig != nil {
		cfg = *appConfig
	}

	// Match current language index
	langIdx := 0 // default "auto"
	norm := strings.ToLower(strings.TrimSpace(cfg.Language))
	switch {
	case strings.HasPrefix(norm, "zh"):
		langIdx = 1
	case strings.HasPrefix(norm, "en"):
		langIdx = 2
	default:
		langIdx = 0
	}

	// Match update check index
	updateIdx := 0
	if cfg.CheckForUpdates != nil && !*cfg.CheckForUpdates {
		updateIdx = 1
	}

	// Match disableEscQuit index
	escIdx := 0
	if cfg.KeyBindings.DisableEscQuit {
		escIdx = 1
	}

	ftpLayout := config.NormalizeFTPLayout(cfg.FTPLayout)
	ftpLayoutIdx := 0
	if ftpLayout == config.FTPLayoutSingle {
		ftpLayoutIdx = 1
	}

	sftpLayout := config.NormalizeSFTPLayout(cfg.SFTPLayout)
	sftpLayoutIdx := 0
	if sftpLayout == config.SFTPLayoutSingle {
		sftpLayoutIdx = 1
	}

	return &settingsFormModel{
		styles:          styles,
		width:           width,
		height:          height,
		appConfig:       cfg,
		focusIndex:      settingsFieldLang,
		langIndex:       langIdx,
		updateIndex:     updateIdx,
		disableEscIndex: escIdx,
		ftpLayoutIndex:  ftpLayoutIdx,
		sftpLayoutIndex: sftpLayoutIdx,
	}
}

func (m *settingsFormModel) Init() tea.Cmd {
	return nil
}

func (m *settingsFormModel) Update(msg tea.Msg) (*settingsFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			m.cancelled = true
			return m, func() tea.Msg { return settingsCloseMsg{Saved: false} }

		case "tab", "down", "j":
			m.focusIndex = (m.focusIndex + 1) % numSettingsFields
			return m, nil

		case "shift+tab", "up", "k":
			if m.focusIndex == 0 {
				m.focusIndex = numSettingsFields - 1
			} else {
				m.focusIndex--
			}
			return m, nil

		case "left", "h":
			m.adjustField(-1)
			return m, nil

		case "right", "l", " ":
			m.adjustField(1)
			return m, nil

		case "enter", "ctrl+s":
			return m, m.saveSettings()
		}
	}

	return m, nil
}

func (m *settingsFormModel) adjustField(dir int) {
	switch m.focusIndex {
	case settingsFieldLang:
		m.langIndex = (m.langIndex + dir + len(langKeys)) % len(langKeys)
	case settingsFieldUpdate:
		m.updateIndex = (m.updateIndex + dir + 2) % 2
	case settingsFieldEscQuit:
		m.disableEscIndex = (m.disableEscIndex + dir + 2) % 2
	case settingsFieldFTPLayout:
		m.ftpLayoutIndex = (m.ftpLayoutIndex + dir + 2) % 2
	case settingsFieldSFTPLayout:
		m.sftpLayoutIndex = (m.sftpLayoutIndex + dir + 2) % 2
	}
}

func (m *settingsFormModel) saveSettings() tea.Cmd {
	return func() tea.Msg {
		// Update AppConfig values
		m.appConfig.Language = langKeys[m.langIndex]
		enableUpdates := (m.updateIndex == 0)
		m.appConfig.CheckForUpdates = &enableUpdates
		m.appConfig.KeyBindings.DisableEscQuit = (m.disableEscIndex == 1)
		if m.ftpLayoutIndex == 0 {
			m.appConfig.FTPLayout = config.FTPLayoutDual
		} else {
			m.appConfig.FTPLayout = config.FTPLayoutSingle
		}
		if m.sftpLayoutIndex == 0 {
			m.appConfig.SFTPLayout = config.SFTPLayoutDual
		} else {
			m.appConfig.SFTPLayout = config.SFTPLayoutSingle
		}

		// Persist to disk
		_ = config.SaveAppConfig(&m.appConfig)

		// Apply language immediately
		i18n.Init(m.appConfig.Language)

		m.saved = true
		return settingsCloseMsg{
			Saved:     true,
			AppConfig: &m.appConfig,
		}
	}
}

func (m *settingsFormModel) View() string {
	// Collect rows first so the label/value columns can be sized and aligned
	// to uniform widths, keeping the ◂ value ▸ frames aligned on both sides.
	// Option values are pure UI labels from the active locale — no
	// bilingual annotations inside them.
	rows := []struct {
		idx   settingsField
		label string
		value string
	}{
		{settingsFieldLang, i18n.T("settings.lang_label"), i18n.T(langOptionKeys[m.langIndex])},
		{settingsFieldUpdate, i18n.T("settings.update_label"), i18n.T(updateKeys[m.updateIndex])},
		{settingsFieldEscQuit, i18n.T("settings.esc_quit_label"), i18n.T(escKeys[m.disableEscIndex])},
		{settingsFieldFTPLayout, i18n.T("settings.ftp_layout_label"), i18n.T(ftpLayoutKeys[m.ftpLayoutIndex])},
		{settingsFieldSFTPLayout, i18n.T("settings.sftp_layout_label"), i18n.T(sftpLayoutKeys[m.sftpLayoutIndex])},
	}
	innerWidth := formPageInnerWidth(m.width)
	maxLabelWidth := 24
	for _, r := range rows {
		if w := ansi.StringWidth("  " + r.label); w > maxLabelWidth {
			maxLabelWidth = w
		}
	}
	// Leave at least one column between the label text and the arrow so the
	// widest label never sits flush against ◂.
	maxLabelWidth++

	// Panel layout: label left, value right-aligned to the box edge so rows
	// span the full inner width instead of leaving trailing whitespace on
	// wide terminals. Row = labelWidth + ◂ +(space)+ value(⇐ valueWidth) + (space)+ ▸.
	// valueWidth is at least the widest current value so selecting a long
	// option (e.g. Vim mode) never overflows the cell and wraps the row.
	widestValue := 0
	for _, r := range rows {
		if w := ansi.StringWidth(r.value); w > widestValue {
			widestValue = w
		}
	}
	maxValueWidth := innerWidth - maxLabelWidth - 4
	if maxValueWidth < widestValue {
		maxValueWidth = widestValue
	}
	if maxValueWidth < 10 {
		maxValueWidth = 10
	}

	// 1. Build the body lines (each setting row is one line).
	bodyLines := make([]string, 0, len(rows))
	for _, r := range rows {
		bodyLines = append(bodyLines, m.renderRow(r.idx, r.label, r.value, maxLabelWidth, maxValueWidth))
	}

	// 2. Pinned header and footer, scrolled body between them.
	header := m.styles.Header.Render(i18n.T("settings.title"))
	helpText := m.styles.HelpText.Render(ansi.Truncate(i18n.T("settings.help"), innerWidth, ""))

	// 3. Viewport calculation & focus-following auto-scroll (same pattern as
	//    the add/edit forms): the focused row is always visible, so the
	//    dialog stays usable down to a few rows of terminal height.
	// Content height = header(2) + 2 per row (row + blank) + help(1) = 3+2N,
	// plus FormContainer chrome (border 2 + vertical padding 2) = 4, so the
	// box fits h rows when N ≤ (h-7)/2.
	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}
	viewportHeight := (totalHeight - 7) / 2
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	rowPos := int(m.focusIndex)
	if len(bodyLines) > viewportHeight {
		if rowPos < m.scrollOffset {
			m.scrollOffset = rowPos
		}
		if rowPos >= m.scrollOffset+viewportHeight {
			m.scrollOffset = rowPos - viewportHeight + 1
		}
		if m.scrollOffset > len(bodyLines)-viewportHeight {
			m.scrollOffset = len(bodyLines) - viewportHeight
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	} else {
		m.scrollOffset = 0
	}

	endIdx := m.scrollOffset + viewportHeight
	if endIdx > len(bodyLines) {
		endIdx = len(bodyLines)
	}
	visibleBody := bodyLines[m.scrollOffset:endIdx]

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\n")
	for _, line := range visibleBody {
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	b.WriteString(helpText)

	// Fill the full terminal height via lipgloss.Place so every frame
	// outputs exactly m.height lines. If View returned fewer lines after a
	// resize, the standard renderer leaves the old frame's bottom border
	// behind on screen (no erase on WindowSizeMsg beyond repaint).
	box := m.styles.FormContainer.Width(m.width - 2).Render(b.String())
	if lipgloss.Height(box) > m.height {
		// Content taller than the terminal: no way to fill height without
		// clipping the dialog; return it as-is (terminal scrolls).
		return box
	}
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center, box)
}

func (m *settingsFormModel) renderRow(idx settingsField, label, value string, labelWidth, valueWidth int) string {
	labelStyle := m.styles.FormField
	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(SecondaryColor))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)

	if m.focusIndex == idx {
		labelStyle = m.styles.FocusedLabel
		arrowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor)).Bold(true)
		valStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor)).Bold(true)
	}

	displayLabel := "  " + label
	if sw := ansi.StringWidth(displayLabel); sw < labelWidth {
		displayLabel += strings.Repeat(" ", labelWidth-sw)
	}

	// Right-align the value so every row's closing arrow lines up.
	if w := ansi.StringWidth(value); w < valueWidth {
		value = strings.Repeat(" ", valueWidth-w) + value
	}

	var b strings.Builder
	b.WriteString(labelStyle.Render(displayLabel))
	b.WriteString(arrowStyle.Render("◂ "))
	b.WriteString(valStyle.Render(value))
	b.WriteString(arrowStyle.Render(" ▸"))
	return b.String()
}
