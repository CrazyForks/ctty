package peek

import (
	"errors"
	"strings"
	"testing"
	"unicode"
)

func TestParseHostStatsLinux(t *testing.T) {
	output := ` 14:23:45 up 12 days,  3:14,  2 users,  load average: 0.15, 0.25, 0.30
---
               total        used        free      shared  buff/cache   available
Mem:            8192        4096        2048         128        2048        3800
Swap:           2048           0        2048
---
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        50G   20G   28G  42% /
`
	stats := ParseHostStats(output)
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}

	if stats.Uptime != "12 days,  3:14" {
		t.Errorf("expected uptime '12 days,  3:14', got %q", stats.Uptime)
	}
	if stats.Users != "2" {
		t.Errorf("expected users '2', got %q", stats.Users)
	}
	if stats.Load1 != "0.15" || stats.Load5 != "0.25" || stats.Load15 != "0.30" {
		t.Errorf("unexpected loads: %q, %q, %q", stats.Load1, stats.Load5, stats.Load15)
	}
	if stats.MemTotalMB != 8192 || stats.MemUsedMB != 4096 {
		t.Errorf("unexpected memory: %d total, %d used", stats.MemTotalMB, stats.MemUsedMB)
	}
	if stats.MemPercent != 50.0 {
		t.Errorf("expected mem percent 50.0, got %f", stats.MemPercent)
	}
	if stats.DiskTotal != "50G" || stats.DiskUsed != "20G" || stats.DiskAvail != "28G" {
		t.Errorf("unexpected disk stats: %s, %s, %s", stats.DiskTotal, stats.DiskUsed, stats.DiskAvail)
	}
	if stats.DiskPercent != 42.0 {
		t.Errorf("expected disk percent 42.0, got %f", stats.DiskPercent)
	}
}

func TestRenderProgressBar(t *testing.T) {
	bar := RenderProgressBar(50.0, 10)
	if !strings.Contains(bar, "50.0%") {
		t.Errorf("expected bar to contain '50.0%%', got %q", bar)
	}
	if !strings.Contains(bar, "█████") {
		t.Errorf("expected 5 filled blocks for 50%% with width 10, got %q", bar)
	}

	// Boundary conditions
	bar0 := RenderProgressBar(-5.0, 10)
	if !strings.Contains(bar0, "0.0%") {
		t.Errorf("expected 0.0%%, got %q", bar0)
	}
	bar100 := RenderProgressBar(150.0, 10)
	if !strings.Contains(bar100, "100.0%") {
		t.Errorf("expected 100.0%%, got %q", bar100)
	}
}

func TestFormatError(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"kex", "kex_exchange_identification: write: Connection refused\r\n", "exit status 255: kex_exchange_identification: write: Connection refused"},
		{"auth", "Permission denied (publickey).\n", "exit status 255: Permission denied (publickey)."},
		{"empty", " \r\n", "exit status 255"},
		{"controls", "\x1b[31mConnection\x1b[0m\trefused\x07\r\n", "exit status 255: Connection refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatError(errors.New("exit status 255"), tc.raw)
			if got != tc.want {
				t.Fatalf("error = %q, want %q", got, tc.want)
			}
			if strings.ContainsFunc(got, unicode.IsControl) {
				t.Fatal("error retains terminal control characters")
			}
		})
	}
}
