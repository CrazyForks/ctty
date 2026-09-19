package ui

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Status emoji used in the host list ping column and modals (quick peek /
// batch / self-update). Source stays ASCII-only via \U escapes (editors /
// checkouts that strip emoji).
var statusEmoji = []string{
	"\U000026AB", // black circle
	"\U0001F7E2", // green circle
	"\U0001F534", // red circle
	"\U0001F7E1", // yellow circle
	"\U000026A1", // lightning (⚡)
	"\U000023F3", // hourglass (⏳)
	"\U00002705", // white heavy check mark (✅)
}

// jetBrainsAdvanceDeficit: how many columns ansi.StringWidth over-counts
// relative to JediTerm cursor advance for that glyph.
var jetBrainsAdvanceDeficit = map[string]int{
	"\U000026AB": 1, // medium black circle — often advances 1 in JediTerm
	"\U0001F7E2": 0, // green — typically full width 2
	"\U0001F534": 0, // red
	"\U0001F7E1": 0, // yellow
	"\U000026A1": 1, // lightning — advances 1 in JediTerm
	"\U000023F3": 1, // hourglass — advances 1 in JediTerm
	"\U00002705": 1, // check mark — advances 1 in JediTerm
}

// isJetBrainsTerminal reports whether we are running inside a JetBrains IDE
// embedded terminal (JediTerm). Status circle (U+26AB), lightning (U+26A1),
// and hourglass (U+23F3) advance one column there while ansi.StringWidth counts 2;
// green/red/yellow circles keep full-width advance 2.
func isJetBrainsTerminal() bool {
	if strings.Contains(os.Getenv("TERMINAL_EMULATOR"), "JetBrains") {
		return true
	}
	if os.Getenv("JETBRAINS_INTELLIJ_COMMAND_X") != "" {
		return true
	}
	if os.Getenv("IDEA_INITIAL_DIRECTORY") != "" {
		return true
	}
	return false
}

func jetBrainsWidthDeficit(s string) int {
	plain := ansi.Strip(s)
	deficit := 0
	for _, emoji := range statusEmoji {
		if d := jetBrainsAdvanceDeficit[emoji]; d != 0 {
			deficit += d * strings.Count(plain, emoji)
		}
	}
	return deficit
}

// terminalDisplayWidth returns the column advance of s in the current terminal.
// On JetBrains/JediTerm, U+26AB, U+26A1, and U+23F3 are adjusted (ansi over-counts by 1);
// ping result circles (green/red/yellow) keep ansi.StringWidth.
func terminalDisplayWidth(s string) int {
	w := ansi.StringWidth(s)
	if isJetBrainsTerminal() {
		w -= jetBrainsWidthDeficit(s)
		if w < 0 {
			w = 0
		}
	}
	return w
}

// padToTerminalWidth forces s to exactly width columns of terminalDisplayWidth.
// Truncation removes trailing runes (cells are already per-column truncated);
// padding appends spaces. Does not use ansi.Truncate on JetBrains because that
// API measures with StringWidth and would clip one column too early for U+26AB.
func padToTerminalWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if !isJetBrainsTerminal() {
		s = ansi.Truncate(s, width, "")
		sw := ansi.StringWidth(s)
		if sw < width {
			s += strings.Repeat(" ", width-sw)
		}
		return s
	}
	for terminalDisplayWidth(s) > width {
		r, size := utf8.DecodeLastRuneInString(s)
		if size <= 0 || r == utf8.RuneError && size == 1 {
			break
		}
		s = s[:len(s)-size]
	}
	for terminalDisplayWidth(s) < width {
		s += " "
	}
	return s
}
