package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/zsuroy/ctty/internal/config"
)

func TestSelectHosts(t *testing.T) {
	hosts := []config.SSHHost{
		{Name: "a", Tags: []string{"prod", "web"}},
		{Name: "b", Tags: []string{"dev"}},
		{Name: "c", Tags: []string{"prod", "api"}},
		{Name: "d", Tags: nil},
	}

	got := selectHosts(hosts, []string{"prod"}, nil)
	if len(got) != 2 {
		t.Fatalf("tags prod: want 2, got %d", len(got))
	}

	got = selectHosts(hosts, nil, []string{"b", "d"})
	if len(got) != 2 {
		t.Fatalf("hosts b,d: want 2, got %d", len(got))
	}

	got = selectHosts(hosts, []string{"prod"}, []string{"b"})
	if len(got) != 3 {
		t.Fatalf("tags OR hosts: want 3, got %d (%v)", len(got), namesOf(got))
	}

	got = selectHosts(hosts, []string{"missing"}, nil)
	if len(got) != 0 {
		t.Fatalf("expected empty")
	}
}

func namesOf(hosts []config.SSHHost) []string {
	var n []string
	for _, h := range hosts {
		n = append(n, h.Name)
	}
	return n
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a, b ,c")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	err = fn()
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return buf.String()
}

func TestPrintExecHostList(t *testing.T) {
	hosts := []config.SSHHost{{Name: "web1"}, {Name: "db1"}}

	out := captureStdout(t, func() error {
		return printExecHostList(hosts, "")
	})
	if out != "web1\ndb1\n" {
		t.Fatalf("human: %q", out)
	}

	out = captureStdout(t, func() error {
		return printExecHostList(nil, "")
	})
	if out != "" {
		t.Fatalf("empty human want blank, got %q", out)
	}

	out = captureStdout(t, func() error {
		return printExecHostList(hosts, "json")
	})
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		t.Fatalf("json parse: %v (%q)", err, out)
	}
	if len(names) != 2 || names[0] != "web1" || names[1] != "db1" {
		t.Fatalf("json names: %v", names)
	}

	out = captureStdout(t, func() error {
		return printExecHostList(nil, "json")
	})
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		t.Fatalf("empty json parse: %v (%q)", err, out)
	}
	if len(names) != 0 {
		t.Fatalf("empty json want [], got %v", names)
	}
}
