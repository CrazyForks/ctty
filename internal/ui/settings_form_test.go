package ui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TestSettingsFormRowsAligned verifies the ◂ value ▸ widgets line up in a
// single column on both sides; variable-length labels and values previously
// left the closing arrow ragged.
func TestSettingsFormRowsAligned(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run(lang, func(t *testing.T) {
			i18n.Init(lang)
			f := NewSettingsForm(NewStyles(80), 100, 40, &config.AppConfig{})
			if got, want := i18n.CurrentLang(), lang; got != want {
				t.Fatalf("language = %q, want %q", got, want)
			}
			checkArrowColumn(t, ansiRe.ReplaceAllString(f.View(), ""))
		})
	}
}

func checkArrowColumn(t *testing.T, view string) {
	t.Helper()
	var openCol, closeCol = -1, -1
	for _, line := range strings.Split(view, "\n") {
		open := strings.Index(line, "◂")
		close := strings.Index(line, "▸")
		if open < 0 || close < 0 {
			continue
		}
		oc := ansi.StringWidth(line[:open])
		cc := ansi.StringWidth(line[:close])
		if openCol == -1 {
			openCol, closeCol = oc, cc
			continue
		}
		if oc != openCol {
			t.Fatalf("opening arrow at column %d, want %d (line: %s)", oc, openCol, line)
		}
		if cc != closeCol {
			t.Fatalf("closing arrow at column %d, want %d (line: %s)", cc, closeCol, line)
		}
	}
	if openCol == -1 {
		t.Fatal("no setting rows rendered")
	}
}

// TestSettingsFormFillsTerminalHeight verifies the form pads its output to
// the full terminal height on window resize. Bubble Tea's standard renderer
// leaves the previous frame's bottom border on screen when a frame shrinks;
// emitting exactly m.height lines every frame covers it up.
func TestSettingsFormFillsTerminalHeight(t *testing.T) {
	i18n.Init(i18n.LangEN)
	natural := settingsFormNaturalHeight(t)
	for _, h := range []int{natural - 4, natural, natural + 4, natural + 20} {
		t.Run(itoa(h), func(t *testing.T) {
			f := NewSettingsForm(NewStyles(80), 80, h, &config.AppConfig{})
			f.Update(tea.WindowSizeMsg{Width: 80, Height: h})
			rows := len(strings.Split(strings.TrimRight(f.View(), "\n"), "\n"))
			if h >= natural && rows != h {
				t.Fatalf("height=%d: rendered %d rows, want %d (full fill)", h, rows, h)
			}
			if h < natural && rows != natural {
				t.Fatalf("height=%d: rendered %d rows, want natural %d", h, rows, natural)
			}
		})
	}
}

// settingsFormNaturalHeight returns the dialog's unpadded height: the box
// rendered without lipgloss.Place vertical fill. Below this height the View
// degrades to the raw box (which overflows the terminal); at/above it the
// output is padded to exactly h rows.
func settingsFormNaturalHeight(t *testing.T) int {
	t.Helper()
	for h := 1; h <= 40; h++ {
		g := NewSettingsForm(NewStyles(80), 80, h, &config.AppConfig{})
		g.Update(tea.WindowSizeMsg{Width: 80, Height: h})
		rows := len(strings.Split(strings.TrimRight(g.View(), "\n"), "\n"))
		if rows == h {
			return h
		}
	}
	return 40
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestSettingsFormScrollFollowsFocus verifies that on a terminal too short
// for the whole dialog the focused setting row is always kept visible
// (focus-following auto-scroll, same behavior as the add/edit forms).
func TestSettingsFormScrollFollowsFocus(t *testing.T) {
	i18n.Init(i18n.LangEN)
	labels := []string{
		i18n.T("settings.lang_label"),
		i18n.T("settings.update_label"),
		i18n.T("settings.esc_quit_label"),
		i18n.T("settings.ftp_layout_label"),
		i18n.T("settings.sftp_layout_label"),
	}

	// Terminal too short for the full dialog: only a few rows are visible.
	f := NewSettingsForm(NewStyles(80), 80, 10, &config.AppConfig{})
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 10})

	for idx := range labels {
		f.focusIndex = settingsField(idx)
		view := ansiRe.ReplaceAllString(f.View(), "")
		found := false
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, labels[idx]) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("focus row %d (%q) not visible at height 10", idx, labels[idx])
		}
	}
}
