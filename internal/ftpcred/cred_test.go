package ftpcred

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetGetDeletePassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	SetPathOverride("")
	t.Cleanup(func() { SetPathOverride("") })

	if _, ok := GetPassword("lab-nas"); ok {
		t.Fatal("expected miss")
	}
	if err := SetPassword("lab-nas", "s3cret"); err != nil {
		t.Fatal(err)
	}
	got, ok := GetPassword("lab-nas")
	if !ok || got != "s3cret" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	info, err := os.Stat(filepath.Join(dir, "ctty", "ftp-credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("perms %o", info.Mode().Perm())
	}
	if err := DeletePassword("lab-nas"); err != nil {
		t.Fatal(err)
	}
	if _, ok := GetPassword("lab-nas"); ok {
		t.Fatal("expected deleted")
	}
}
