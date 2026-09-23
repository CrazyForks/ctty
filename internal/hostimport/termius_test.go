package hostimport

import (
	"testing"
)

func TestTermiusParseTopLevelArray(t *testing.T) {
	data := []byte(`[
  {
    "label": "Prod Web 01",
    "address": "10.0.0.1",
    "username": "deploy",
    "port": 2222,
    "group": "production",
    "tags": ["web", "api"]
  },
  {
    "name": "DB Primary",
    "hostname": "10.0.0.2",
    "ssh_config": {
      "user": "postgres",
      "port": 5432
    },
    "group_label": "database"
  }
]`)

	src, ok := Lookup("termius")
	if !ok {
		t.Fatal("termius source not found")
	}

	hosts, err := src.Parse(data)
	if err != nil {
		t.Fatalf("parse termius: %v", err)
	}

	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	h1 := hosts[0]
	if h1.Name != "Prod-Web-01" {
		t.Errorf("expected alias 'Prod-Web-01', got %q", h1.Name)
	}
	if h1.Hostname != "10.0.0.1" || h1.User != "deploy" || h1.Port != "2222" {
		t.Errorf("unexpected host 1 values: %+v", h1)
	}
	if len(h1.Tags) != 3 { // group "production", tags "web", "api"
		t.Errorf("expected 3 tags on host 1, got %v", h1.Tags)
	}

	h2 := hosts[1]
	if h2.Name != "DB-Primary" {
		t.Errorf("expected alias 'DB-Primary', got %q", h2.Name)
	}
	if h2.Hostname != "10.0.0.2" || h2.User != "postgres" || h2.Port != "5432" {
		t.Errorf("unexpected host 2 values: %+v", h2)
	}
}

func TestTermiusParseWrappedObject(t *testing.T) {
	data := []byte(`{
  "hosts": [
    {
      "label": "Bastion",
      "address": "bastion.example.com",
      "port": 22,
      "username": "jumpuser"
    }
  ]
}`)

	src, _ := Lookup("termius")
	hosts, err := src.Parse(data)
	if err != nil {
		t.Fatalf("parse termius: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "Bastion" || hosts[0].Hostname != "bastion.example.com" {
		t.Errorf("unexpected host: %+v", hosts[0])
	}
}
