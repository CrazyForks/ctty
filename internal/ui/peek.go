package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/peek"
)

// HostStats holds parsed health & resource metrics for an SSH host.
type HostStats = peek.HostStats

// hostStatsResultMsg is returned when host stats probing finishes.
type hostStatsResultMsg struct {
	hostName string
	stats    *HostStats
	err      error
	raw      string
}

// parseHostStats parses probe command output.
var parseHostStats = peek.ParseHostStats

// renderProgressBar formats a visual progress bar e.g. [████████░░░░░░░░] 48.0%
func renderProgressBar(percent float64, barWidth int) string {
	return peek.RenderProgressBar(percent, barWidth)
}

// fetchHostStatsCmd executes a fast remote probe to collect health metrics.
func fetchHostStatsCmd(host config.SSHHost, configFile string) tea.Cmd {
	return func() tea.Msg {
		stats, raw, err := peek.FetchHostStats(context.Background(), host, configFile, 7*time.Second)
		if err != nil {
			return hostStatsResultMsg{hostName: host.Name, err: err, raw: raw}
		}
		return hostStatsResultMsg{hostName: host.Name, stats: stats, raw: raw}
	}
}

func (msg hostStatsResultMsg) errorText() string {
	return peek.FormatError(msg.err, msg.raw)
}

// renderPeekModal renders the centered Quick Peek card box.
func (m Model) renderPeekModal() string {
	if m.peekHost == nil {
		return ""
	}

	hostName := m.peekHost.Name
	title := m.styles.FocusedLabel.Bold(true).Render(fmt.Sprintf("⚡ "+i18n.T("peek.title"), hostName))

	var rows []string
	rows = append(rows, title, "")

	if m.peekLoading {
		loadingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Italic(true)
		rows = append(rows, loadingStyle.Render("  ⏳ "+i18n.T("peek.loading")), "")
		rows = append(rows, m.styles.HelpText.Render("  Esc: "+i18n.T("pf.action_cancel")))
		return renderCardBox(m.styles.FormContainer, m.width, rows...)
	}

	if m.peekErr != "" {
		errStyle := m.styles.ErrorText
		rows = append(rows, errStyle.Render(fmt.Sprintf("  ✗ "+i18n.T("peek.error"), m.peekErr)), "")
		rows = append(rows, m.styles.HelpText.Render("  r: "+i18n.T("peek.refresh_hint")+" • Esc: "+i18n.T("pf.action_cancel")))
		return renderCardBox(m.styles.FormContainer, m.width, rows...)
	}

	if m.peekStats != nil {
		stats := m.peekStats
		labelStyle := m.styles.Label
		valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

		// Uptime & users
		if stats.Uptime != "" {
			usersPart := ""
			if stats.Users != "" {
				usersPart = fmt.Sprintf(" ("+i18n.T("peek.users")+")", stats.Users)
			}
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.uptime")+":", 12)), valStyle.Render("up "+stats.Uptime+usersPart)))
		}

		// Load average
		if stats.Load1 != "" {
			loadStr := fmt.Sprintf("%s, %s, %s", stats.Load1, stats.Load5, stats.Load15)
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.load")+":", 12)), valStyle.Render(loadStr)))
		}

		// Memory
		if stats.MemTotalMB > 0 {
			bar := renderProgressBar(stats.MemPercent, 14)
			memInfo := fmt.Sprintf("%s (%dMB / %dMB)", bar, stats.MemUsedMB, stats.MemTotalMB)
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.memory")+":", 12)), memInfo))
		}

		// Root disk
		if stats.DiskTotal != "" {
			bar := renderProgressBar(stats.DiskPercent, 14)
			diskInfo := fmt.Sprintf("%s (%s / %s)", bar, stats.DiskUsed, stats.DiskTotal)
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.disk")+":", 12)), diskInfo))
		}

		// Fallback if no structured metrics could be parsed
		if stats.Uptime == "" && stats.MemTotalMB == 0 && stats.DiskTotal == "" && stats.RawOutput != "" {
			rawLines := strings.Split(stats.RawOutput, "\n")
			if len(rawLines) > 6 {
				rawLines = rawLines[:6]
			}
			for _, rl := range rawLines {
				if strings.TrimSpace(rl) != "" && strings.TrimSpace(rl) != "---" {
					rows = append(rows, "  "+valStyle.Render(rl))
				}
			}
		}
	}

	rows = append(rows, "", m.styles.HelpText.Render("  "+i18n.T("peek.help")))
	return renderCardBox(m.styles.FormContainer, m.width, rows...)
}
