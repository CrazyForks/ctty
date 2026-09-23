package hostimport

import (
	"testing"
)

func TestGenericJSONParseArray(t *testing.T) {
	data := []byte(`[
  {
    "name": "web-cluster-01",
    "ip": "10.10.1.10",
    "user": "ubuntu",
    "port": 2222,
    "identity_file": "~/.ssh/id_rsa",
    "proxy_jump": "bastion",
    "tags": ["web", "prod"]
  },
  {
    "title": "Database Master",
    "hostname": "10.10.1.20",
    "user_name": "postgres",
    "tags": "db, pg"
  }
]`)

	src, ok := Lookup("json")
	if !ok {
		t.Fatal("json importer not found")
	}

	hosts, err := src.Parse(data)
	if err != nil {
		t.Fatalf("parse generic json: %v", err)
	}

	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	h1 := hosts[0]
	if h1.Name != "web-cluster-01" || h1.Hostname != "10.10.1.10" || h1.User != "ubuntu" || h1.Port != "2222" {
		t.Errorf("unexpected h1: %+v", h1)
	}
	if h1.Identity != "~/.ssh/id_rsa" || h1.ProxyJump != "bastion" {
		t.Errorf("unexpected h1 keys/jump: %+v", h1)
	}
	if len(h1.Tags) != 2 || h1.Tags[0] != "web" || h1.Tags[1] != "prod" {
		t.Errorf("unexpected h1 tags: %v", h1.Tags)
	}

	h2 := hosts[1]
	if h2.Name != "Database-Master" || h2.Hostname != "10.10.1.20" || h2.User != "postgres" || h2.Port != "22" {
		t.Errorf("unexpected h2: %+v", h2)
	}
	if len(h2.Tags) != 2 || h2.Tags[0] != "db" || h2.Tags[1] != "pg" {
		t.Errorf("unexpected h2 tags: %v", h2.Tags)
	}
}

func TestGenericJSONParseWrapped(t *testing.T) {
	data := []byte(`{
  "servers": [
    {
      "alias": "api-gateway",
      "address": "api.internal",
      "login": "deploy",
      "port": 8022
    }
  ]
}`)

	src, _ := Lookup("json")
	hosts, err := src.Parse(data)
	if err != nil {
		t.Fatalf("parse wrapped json: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "api-gateway" || hosts[0].Hostname != "api.internal" || hosts[0].User != "deploy" || hosts[0].Port != "8022" {
		t.Errorf("unexpected host: %+v", hosts[0])
	}
}

func TestGenericJSONParseSingle(t *testing.T) {
	data := []byte(`{
  "name": "standalone-box",
  "host": "192.168.100.1",
  "username": "root"
}`)

	src, _ := Lookup("json")
	hosts, err := src.Parse(data)
	if err != nil {
		t.Fatalf("parse single host json: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "standalone-box" || hosts[0].Hostname != "192.168.100.1" || hosts[0].User != "root" || hosts[0].Port != "22" {
		t.Errorf("unexpected host: %+v", hosts[0])
	}
}
