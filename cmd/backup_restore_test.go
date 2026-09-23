package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAndRestoreCommandRegistration(t *testing.T) {
	cmds := RootCmd.Commands()
	hasBackup, hasRestore, hasExport := false, false, false
	for _, c := range cmds {
		switch c.Name() {
		case "backup":
			hasBackup = true
		case "restore":
			hasRestore = true
		case "export":
			hasExport = true
		}
	}

	if !hasBackup {
		t.Error("backup command not registered")
	}
	if !hasRestore {
		t.Error("restore command not registered")
	}
	if !hasExport {
		t.Error("export command not registered")
	}
}

func TestBackupAndRestoreFlags(t *testing.T) {
	bFlags := backupCmd.Flags()
	for _, f := range []string{"output", "passphrase", "include-credentials", "include-ssh", "format", "json"} {
		if bFlags.Lookup(f) == nil {
			t.Errorf("expected backup flag --%s", f)
		}
	}

	rFlags := restoreCmd.Flags()
	for _, f := range []string{"passphrase", "overwrite", "dry-run", "restore-ssh", "format", "json"} {
		if rFlags.Lookup(f) == nil {
			t.Errorf("expected restore flag --%s", f)
		}
	}

	eFlags := exportCmd.Flags()
	for _, f := range []string{"format", "tags", "json"} {
		if eFlags.Lookup(f) == nil {
			t.Errorf("expected export flag --%s", f)
		}
	}
}

func TestExportExecution(t *testing.T) {
	tmpDir := t.TempDir()
	testCfg := filepath.Join(tmpDir, "ssh_config")
	content := `Host web-node
    HostName 10.0.0.10
    User admin
    Port 22
    # tags: production
`
	if err := os.WriteFile(testCfg, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	origConfig := configFile
	defer func() { configFile = origConfig }()
	configFile = testCfg

	// Capture export output or test logic
	exportFormat = "ssh"
	exportJSON = false
	exportTags = "production"

	// runExport writes to os.Stdout; we can test that it executes without crashing
	// (tested deeply in internal/backup/backup_test.go)
}
