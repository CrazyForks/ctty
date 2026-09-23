package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zsuroy/ctty/internal/peek"
)

func TestPeekCommandRegistration(t *testing.T) {
	found := false
	for _, cmd := range RootCmd.Commands() {
		if cmd.Name() == "peek" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("peek command not found in RootCmd")
	}

	flags := peekCmd.Flags()
	for _, name := range []string{"all", "tags", "format", "json", "timeout", "concurrency"} {
		if flags.Lookup(name) == nil {
			t.Errorf("expected flag --%s to be defined on peekCmd", name)
		}
	}

	if peekCmd.ValidArgsFunction == nil {
		t.Error("expected ValidArgsFunction on peekCmd")
	}
}

func TestPeekResultJSON(t *testing.T) {
	res := []peekHostResult{
		{
			Host: "prod-web",
			OK:   true,
			Stats: &peek.HostStats{
				Uptime:      "10 days",
				Users:       "1",
				Load1:       "0.10",
				Load5:       "0.20",
				Load15:      "0.30",
				MemTotalMB:  4096,
				MemUsedMB:   2048,
				MemPercent:  50.0,
				DiskTotal:   "100G",
				DiskUsed:    "40G",
				DiskAvail:   "60G",
				DiskPercent: 40.0,
			},
		},
		{
			Host:  "dev-fail",
			OK:    false,
			Error: "connection refused",
		},
	}

	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal peek results: %v", err)
	}

	var parsed []peekHostResult
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal peek results: %v", err)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 items, got %d", len(parsed))
	}
	if !parsed[0].OK || parsed[0].Host != "prod-web" || parsed[0].Stats.MemTotalMB != 4096 {
		t.Errorf("unexpected parsed[0]: %+v", parsed[0])
	}
	if parsed[1].OK || parsed[1].Host != "dev-fail" || parsed[1].Error != "connection refused" {
		t.Errorf("unexpected parsed[1]: %+v", parsed[1])
	}
}

func TestPeekTimeoutFlagDefault(t *testing.T) {
	val, err := peekCmd.Flags().GetDuration("timeout")
	if err != nil {
		t.Fatalf("get timeout flag: %v", err)
	}
	if val != 7*time.Second {
		t.Errorf("expected default timeout 7s, got %v", val)
	}
}
