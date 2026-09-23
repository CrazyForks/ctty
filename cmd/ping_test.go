package cmd

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPingCommandRegistration(t *testing.T) {
	found := false
	for _, cmd := range RootCmd.Commands() {
		if cmd.Name() == "ping" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ping command not found in RootCmd")
	}

	flags := pingCmd.Flags()
	for _, name := range []string{"all", "tags", "format", "json", "timeout", "concurrency"} {
		if flags.Lookup(name) == nil {
			t.Errorf("expected flag --%s to be defined on pingCmd", name)
		}
	}

	if pingCmd.ValidArgsFunction == nil {
		t.Error("expected ValidArgsFunction on pingCmd")
	}
}

func TestPingResultJSON(t *testing.T) {
	res := []pingHostResult{
		{
			Host:      "prod-web",
			OK:        true,
			Status:    "online",
			LatencyMS: 15.4,
		},
		{
			Host:      "dev-offline",
			OK:        false,
			Status:    "offline",
			LatencyMS: 0,
			Error:     "connection refused",
		},
	}

	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal ping results: %v", err)
	}

	var parsed []pingHostResult
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal ping results: %v", err)
	}

	if len(parsed) != 2 {
		t.Fatalf("expected 2 items, got %d", len(parsed))
	}
	if !parsed[0].OK || parsed[0].Host != "prod-web" || parsed[0].LatencyMS != 15.4 {
		t.Errorf("unexpected parsed[0]: %+v", parsed[0])
	}
	if parsed[1].OK || parsed[1].Host != "dev-offline" || parsed[1].Error != "connection refused" {
		t.Errorf("unexpected parsed[1]: %+v", parsed[1])
	}
}

func TestPingTimeoutFlagDefault(t *testing.T) {
	val, err := pingCmd.Flags().GetDuration("timeout")
	if err != nil {
		t.Fatalf("get timeout flag: %v", err)
	}
	if val != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", val)
	}
}
