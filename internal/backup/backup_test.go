package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupAndRestorePlain(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "ctty_config")
	_ = os.MkdirAll(configDir, 0700)

	// Create some dummy config files
	_ = os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(`{"lang":"zh","theme":"default"}`), 0600)
	_ = os.WriteFile(filepath.Join(configDir, "snippets.json"), []byte(`[{"name":"test","command":"ls"}]`), 0600)

	backupFile := filepath.Join(tmpDir, "test-backup.tar.gz")

	res, err := CreateBackup(BackupOptions{
		OutputFile: backupFile,
		ConfigDir:  configDir,
	})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if res.FilePath != backupFile || res.SizeBytes <= 0 || res.Encrypted {
		t.Fatalf("unexpected backup result: %+v", res)
	}

	// Restore to a new directory
	restoreDir := filepath.Join(tmpDir, "restored_config")
	restRes, err := RestoreBackup(RestoreOptions{
		BackupFile: backupFile,
		TargetDir:  restoreDir,
		Overwrite:  true,
	})
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	if len(restRes.RestoredFiles) != 2 {
		t.Fatalf("expected 2 restored files, got %d (%v)", len(restRes.RestoredFiles), restRes.RestoredFiles)
	}

	restoredSettings, err := os.ReadFile(filepath.Join(restoreDir, "settings.json"))
	if err != nil || !strings.Contains(string(restoredSettings), "zh") {
		t.Fatalf("restored settings invalid: %v, content=%s", err, string(restoredSettings))
	}
}

func TestBackupAndRestoreEncrypted(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "ctty_config")
	_ = os.MkdirAll(configDir, 0700)

	_ = os.WriteFile(filepath.Join(configDir, "settings.json"), []byte(`{"lang":"en"}`), 0600)

	backupFile := filepath.Join(tmpDir, "test-backup.ctty")
	passphrase := "super-secure-passphrase-123"

	res, err := CreateBackup(BackupOptions{
		OutputFile: backupFile,
		ConfigDir:  configDir,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("CreateBackup encrypted: %v", err)
	}
	if !res.Encrypted {
		t.Fatal("expected Encrypted to be true")
	}

	// Restore with wrong passphrase should fail
	restoreDir := filepath.Join(tmpDir, "restored_config")
	_, err = RestoreBackup(RestoreOptions{
		BackupFile: backupFile,
		TargetDir:  restoreDir,
		Passphrase: "wrong-password",
	})
	if err == nil {
		t.Fatal("expected error with wrong passphrase, got nil")
	}

	// Restore without passphrase should fail
	_, err = RestoreBackup(RestoreOptions{
		BackupFile: backupFile,
		TargetDir:  restoreDir,
	})
	if err == nil {
		t.Fatal("expected error without passphrase, got nil")
	}

	// Restore with correct passphrase
	restRes, err := RestoreBackup(RestoreOptions{
		BackupFile: backupFile,
		TargetDir:  restoreDir,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("RestoreBackup with correct passphrase: %v", err)
	}

	if len(restRes.RestoredFiles) != 1 {
		t.Fatalf("expected 1 restored file, got %d", len(restRes.RestoredFiles))
	}

	content, err := os.ReadFile(filepath.Join(restoreDir, "settings.json"))
	if err != nil || !strings.Contains(string(content), "en") {
		t.Fatalf("restored content mismatch: %v, %s", err, string(content))
	}
}

func TestExportData(t *testing.T) {
	tmpDir := t.TempDir()
	sshCfgPath := filepath.Join(tmpDir, "ssh_config")
	sshContent := `Host test-node
    HostName 192.168.1.50
    User dev
    Port 2222
    # tags: prod, web
`
	_ = os.WriteFile(sshCfgPath, []byte(sshContent), 0600)

	// Test JSON export
	jsonData, err := ExportData(ExportOptions{
		Format:        "json",
		SSHConfigFile: sshCfgPath,
	})
	if err != nil {
		t.Fatalf("ExportData JSON: %v", err)
	}

	var bundle ExportBundle
	if err := json.Unmarshal(jsonData, &bundle); err != nil {
		t.Fatalf("Unmarshal ExportBundle: %v", err)
	}
	if len(bundle.Hosts) != 1 || bundle.Hosts[0].Name != "test-node" {
		t.Fatalf("unexpected exported hosts: %+v", bundle.Hosts)
	}

	// Test SSH format export
	sshData, err := ExportData(ExportOptions{
		Format:        "ssh",
		SSHConfigFile: sshCfgPath,
	})
	if err != nil {
		t.Fatalf("ExportData SSH: %v", err)
	}

	if !strings.Contains(string(sshData), "Host test-node") || !strings.Contains(string(sshData), "Port 2222") {
		t.Fatalf("unexpected ssh format export: %s", string(sshData))
	}
}
