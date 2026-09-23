package hostimport

import (
	"testing"
)

func TestFinalShellParseArray(t *testing.T) {
	data := []byte(`[
  {
    "name": "Production API",
    "host": "192.168.10.100",
    "user_name": "root",
    "port": 22,
    "group": "Cloud",
    "tags": "prod,k8s"
  },
  {
    "name": "Redis Cache",
    "ip": "192.168.10.101",
    "userName": "admin",
    "port": "2222"
  }
]`)

	src, ok := Lookup("finalshell")
	if !ok {
		t.Fatal("finalshell source not found")
	}

	hosts, err := src.Parse(data)
	if err != nil {
		t.Fatalf("parse finalshell: %v", err)
	}

	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	h1 := hosts[0]
	if h1.Name != "Production-API" {
		t.Errorf("expected alias 'Production-API', got %q", h1.Name)
	}
	if h1.Hostname != "192.168.10.100" || h1.User != "root" || h1.Port != "22" {
		t.Errorf("unexpected h1: %+v", h1)
	}
	if len(h1.Tags) != 3 { // Cloud, prod, k8s
		t.Errorf("expected 3 tags on h1, got %v", h1.Tags)
	}

	h2 := hosts[1]
	if h2.Name != "Redis-Cache" {
		t.Errorf("expected alias 'Redis-Cache', got %q", h2.Name)
	}
	if h2.Hostname != "192.168.10.101" || h2.User != "admin" || h2.Port != "2222" {
		t.Errorf("unexpected h2: %+v", h2)
	}
}

func TestFinalShellParseSingleObject(t *testing.T) {
	data := []byte(`{
  "name": "Single Server",
  "host": "10.1.1.1",
  "user_name": "ubuntu",
  "port": 22
}`)

	src, _ := Lookup("finalshell")
	hosts, err := src.Parse(data)
	if err != nil {
		t.Fatalf("parse finalshell single: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "Single-Server" || hosts[0].Hostname != "10.1.1.1" {
		t.Errorf("unexpected host: %+v", hosts[0])
	}
}
