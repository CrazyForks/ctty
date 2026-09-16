package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/zsuroy/ctty/internal/webdavconfig"
)

func TestWebDAVListSearchInfoJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if err := webdavconfig.Add(webdavconfig.WebDAVSite{
		Name: "nextcloud", URL: "https://cloud.example.test/remote.php/dav/files/user/", User: "admin", Tags: []string{"cloud"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := webdavconfig.Add(webdavconfig.WebDAVSite{
		Name: "owncloud", URL: "https://own.example.test/remote.php/webdav/", User: "guest",
	}); err != nil {
		t.Fatal(err)
	}

	webdavFormat = "json"
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	runWebDAVList()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	var sites []webdavconfig.WebDAVSite
	if err := json.Unmarshal(buf.Bytes(), &sites); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(sites) != 2 {
		t.Fatalf("want 2 sites, got %d", len(sites))
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	runWebDAVSearch("cloud")
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	sites = nil
	if err := json.Unmarshal(buf.Bytes(), &sites); err != nil {
		t.Fatal(err)
	}
	// "cloud" matches both nextcloud (name and URL) and owncloud (URL)
	if len(sites) != 2 {
		t.Fatalf("search 'cloud': want 2, got %d", len(sites))
	}

	r3, w3, _ := os.Pipe()
	os.Stdout = w3
	runWebDAVInfo("nextcloud")
	_ = w3.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r3)
	var one webdavconfig.WebDAVSite
	if err := json.Unmarshal(buf.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.User != "admin" || one.Name != "nextcloud" {
		t.Fatalf("info: %+v", one)
	}
}

func TestWebDAVCompletions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	_ = webdavconfig.Add(webdavconfig.WebDAVSite{Name: "alpha", URL: "https://alpha.test"})
	_ = webdavconfig.Add(webdavconfig.WebDAVSite{Name: "beta", URL: "https://beta.test"})

	names := completeWebDAVNames("al")
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("expected [alpha], got %v", names)
	}
}
