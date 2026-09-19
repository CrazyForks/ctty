package ui

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestQuickPeekFailureDetails(t *testing.T) {
	lang := i18n.CurrentLang()
	t.Cleanup(func() { i18n.SetLang(lang) })
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"kex", "kex_exchange_identification: write: Connection refused\r\n", "exit status 255: kex_exchange_identification: write: Connection refused"},
		{"auth", "Permission denied (publickey).\n", "exit status 255: Permission denied (publickey)."},
		{"empty", " \r\n", "exit status 255"},
		{"controls", "\x1b[31mConnection\x1b[0m\trefused\x07\r\n", "exit status 255: Connection refused"},
		{"long", strings.Repeat("x", 600) + "Connection refused", "exit status 255: …" + strings.Repeat("x", 494) + "Connection refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := config.SSHHost{Name: "test-box"}
			m := NewModel([]config.SSHHost{host}, "", false, "dev", true)
			m.peekOpen, m.peekLoading = true, true
			m.peekHost = &host
			m.peekStats = &HostStats{Uptime: "stale"}

			next, _ := m.Update(hostStatsResultMsg{
				hostName: host.Name,
				err:      errors.New("exit status 255"),
				raw:      tc.raw,
			})

			got := next.(Model)
			if got.peekErr != tc.want {
				t.Fatalf("error = %q, want %q", got.peekErr, tc.want)
			}
			if got.peekLoading || got.peekStats != nil {
				t.Fatal("failure must end loading and clear stale successful stats")
			}
			if strings.ContainsFunc(got.peekErr, unicode.IsControl) {
				t.Fatal("error retains terminal control characters")
			}
		})
	}
}
