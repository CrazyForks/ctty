package peek

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
)

// HostStats holds parsed health & resource metrics for an SSH host.
type HostStats struct {
	Uptime      string  `json:"uptime"`
	Users       string  `json:"users"`
	Load1       string  `json:"load1"`
	Load5       string  `json:"load5"`
	Load15      string  `json:"load15"`
	MemTotalMB  int     `json:"mem_total_mb"`
	MemUsedMB   int     `json:"mem_used_mb"`
	MemPercent  float64 `json:"mem_percent"`
	DiskTotal   string  `json:"disk_total"`
	DiskUsed    string  `json:"disk_used"`
	DiskAvail   string  `json:"disk_avail"`
	DiskPercent float64 `json:"disk_percent"`
	RawOutput   string  `json:"raw_output,omitempty"`
}

var (
	reLoadAverage = regexp.MustCompile(`load average[s]?:\s*([0-9.]+)[,\s]+([0-9.]+)[,\s]+([0-9.]+)`)
	reUptime      = regexp.MustCompile(`up\s+(\d+\s+days?,\s*[^,]+|[^,]+)`)
	reUsers       = regexp.MustCompile(`(\d+)\s+users?`)
)

// ParseHostStats parses output of:
// uptime 2>/dev/null; echo "---"; free -m 2>/dev/null || vm_stat 2>/dev/null; echo "---"; df -h / 2>/dev/null
func ParseHostStats(output string) *HostStats {
	stats := &HostStats{RawOutput: strings.TrimSpace(output)}
	sections := strings.Split(output, "---")

	// Section 1: Uptime & Load
	if len(sections) > 0 {
		upLine := sections[0]
		if m := reLoadAverage.FindStringSubmatch(upLine); len(m) >= 4 {
			stats.Load1 = m[1]
			stats.Load5 = m[2]
			stats.Load15 = m[3]
		}
		if m := reUptime.FindStringSubmatch(upLine); len(m) >= 2 {
			stats.Uptime = strings.TrimSpace(m[1])
		}
		if m := reUsers.FindStringSubmatch(upLine); len(m) >= 2 {
			stats.Users = m[1]
		}
	}

	// Section 2: Memory (free -m)
	if len(sections) > 1 {
		memLines := strings.Split(sections[1], "\n")
		for _, line := range memLines {
			fields := strings.Fields(line)
			if len(fields) >= 3 && strings.HasPrefix(fields[0], "Mem:") {
				if total, err := strconv.Atoi(fields[1]); err == nil && total > 0 {
					stats.MemTotalMB = total
					if used, err := strconv.Atoi(fields[2]); err == nil {
						stats.MemUsedMB = used
						stats.MemPercent = float64(used) / float64(total) * 100.0
					}
				}
				break
			}
		}
	}

	// Section 3: Disk (df -h /)
	if len(sections) > 2 {
		diskLines := strings.Split(sections[2], "\n")
		for _, line := range diskLines {
			fields := strings.Fields(line)
			if len(fields) >= 5 && (fields[len(fields)-1] == "/" || strings.HasSuffix(fields[0], "/")) {
				stats.DiskTotal = fields[1]
				stats.DiskUsed = fields[2]
				stats.DiskAvail = fields[3]
				pctStr := strings.TrimSuffix(fields[4], "%")
				if pct, err := strconv.ParseFloat(pctStr, 64); err == nil {
					stats.DiskPercent = pct
				}
				break
			}
		}
	}

	return stats
}

// RenderProgressBar formats a visual progress bar e.g. [████████░░░░░░░░] 48.0%
func RenderProgressBar(percent float64, barWidth int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	if barWidth <= 0 {
		barWidth = 16
	}

	filledLen := int(percent / 100.0 * float64(barWidth))
	if filledLen > barWidth {
		filledLen = barWidth
	}
	emptyLen := barWidth - filledLen

	var color lipgloss.Color
	switch {
	case percent >= 85:
		color = lipgloss.Color("9") // Red
	case percent >= 70:
		color = lipgloss.Color("11") // Yellow
	default:
		color = lipgloss.Color("10") // Green
	}

	filledStyle := lipgloss.NewStyle().Foreground(color)
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	bar := filledStyle.Render(strings.Repeat("█", filledLen)) +
		emptyStyle.Render(strings.Repeat("░", emptyLen))

	return fmt.Sprintf("[%s] %5.1f%%", bar, percent)
}

// FormatError cleans terminal escape codes and control characters from error output.
func FormatError(err error, raw string) string {
	if err == nil {
		return ""
	}
	detail := strings.Join(strings.Fields(ansi.Strip(raw)), " ")
	detail = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, detail)
	runes := []rune(detail)
	if len(runes) > 512 {
		detail = "…" + string(runes[len(runes)-512:])
	}
	if detail == "" {
		return err.Error()
	}
	return err.Error() + ": " + detail
}

// BuildSSHEnv constructs the environment for SSH probe commands, injecting
// SSH_ASKPASS variables when a saved password exists for the host.
func BuildSSHEnv(hostName string) []string {
	env := os.Environ()
	if pass, ok := credential.GetPassword(hostName); ok && pass != "" {
		selfPath, err := os.Executable()
		if err == nil {
			env = append(env,
				"SSH_ASKPASS="+selfPath,
				"SSH_ASKPASS_REQUIRE=force",
				"CTTY_ASKPASS_TOKEN="+base64.StdEncoding.EncodeToString([]byte(pass)),
				"DISPLAY=ctty:0",
			)
		}
	}
	return env
}

// FetchHostStats executes a remote probe to collect health metrics from an SSH host.
func FetchHostStats(ctx context.Context, host config.SSHHost, configFile string, timeout time.Duration) (*HostStats, string, error) {
	if timeout <= 0 {
		timeout = 7 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var args []string
	if configFile != "" {
		args = append(args, "-F", configFile)
	}
	if host.Port != "" && host.Port != "22" {
		args = append(args, "-p", host.Port)
	}
	if host.Identity != "" {
		args = append(args, "-i", host.Identity)
	}
	if host.ProxyJump != "" {
		args = append(args, "-J", host.ProxyJump)
	}
	timeoutSec := int(timeout.Seconds())
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	args = append(args,
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeoutSec),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
	)

	target := host.Name
	if host.Hostname != "" && host.Hostname != host.Name {
		if host.User != "" {
			target = fmt.Sprintf("%s@%s", host.User, host.Hostname)
		} else {
			target = host.Hostname
		}
	} else if host.User != "" {
		target = fmt.Sprintf("%s@%s", host.User, host.Name)
	}
	args = append(args, target)

	probeScript := `uptime 2>/dev/null; echo "---"; free -m 2>/dev/null || vm_stat 2>/dev/null; echo "---"; df -h / 2>/dev/null`
	args = append(args, probeScript)

	cmd := exec.CommandContext(probeCtx, "ssh", args...)
	cmd.Env = BuildSSHEnv(host.Name)

	out, err := cmd.CombinedOutput()
	raw := string(out)
	if err != nil {
		return nil, raw, err
	}
	stats := ParseHostStats(raw)
	return stats, raw, nil
}
