package webdavconfig

import (
	"os"
	"testing"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestWebDAVConfigRoundTrip(t *testing.T) {
	setupTestDir(t)

	sites, err := Load()
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("expected empty sites, got %d", len(sites))
	}

	site1 := WebDAVSite{
		Name: "nextcloud",
		URL:  "https://dav.example.com/remote.php/dav/files/user/",
		User: "alice",
		Tags: []string{"work", "cloud"},
	}
	if err := Add(site1); err != nil {
		t.Fatalf("add site1: %v", err)
	}

	// Duplicate add must fail
	if err := Add(site1); err == nil {
		t.Fatalf("expected duplicate error, got nil")
	}

	site2 := WebDAVSite{
		Name:        "nas",
		URL:         "192.168.1.100:5005/dav", // should normalize to http://192.168.1.100:5005/dav
		User:        "admin",
		InsecureTLS: true,
	}
	if err := Add(site2); err != nil {
		t.Fatalf("add site2: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load after adds: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 sites, got %d", len(loaded))
	}

	// Verify normalization
	nas, ok := Find("nas")
	if !ok {
		t.Fatalf("find nas failed")
	}
	if nas.URL != "http://192.168.1.100:5005/dav" {
		t.Errorf("nas.URL = %q, want http://192.168.1.100:5005/dav", nas.URL)
	}
	if !nas.InsecureTLS {
		t.Errorf("nas.InsecureTLS = false, want true")
	}

	// Update nas
	nas.User = "superadmin"
	if err := Update("nas", nas); err != nil {
		t.Fatalf("update nas: %v", err)
	}
	updated, _ := Find("nas")
	if updated.User != "superadmin" {
		t.Errorf("updated user = %q, want superadmin", updated.User)
	}

	// Delete nextcloud
	if err := Delete("nextcloud"); err != nil {
		t.Fatalf("delete nextcloud: %v", err)
	}
	if _, ok := Find("nextcloud"); ok {
		t.Fatalf("nextcloud still found after delete")
	}

	// Delete non-existent
	if err := Delete("ghost"); err == nil {
		t.Fatalf("expected error deleting non-existent site")
	}

	// Verify permissions (0600)
	cfgPath, _ := getConfigPath()
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}

func TestWebDAVConfigSaveSorting(t *testing.T) {
	setupTestDir(t)

	s1 := WebDAVSite{Name: "zeta", URL: "https://z.com"}
	s2 := WebDAVSite{Name: "alpha", URL: "https://a.com"}
	if err := Save([]WebDAVSite{s1, s2}); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 2 || loaded[0].Name != "alpha" || loaded[1].Name != "zeta" {
		t.Fatalf("expected sorted [alpha, zeta], got %+v", loaded)
	}
}
