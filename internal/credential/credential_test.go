package credential

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func resetDefaultStore(t *testing.T) {
	t.Helper()
	storeMu.Lock()
	previous := defaultStore
	defaultStore = nil
	storeMu.Unlock()
	t.Cleanup(func() {
		storeMu.Lock()
		defaultStore = previous
		storeMu.Unlock()
	})
}

func TestEncryptDecrypt(t *testing.T) {
	key := deriveMachineKey()
	original := "superSecretP@ssw0rd!123"

	encrypted, err := encrypt(original, key)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	if encrypted == original {
		t.Fatal("Encrypted text should not match original")
	}

	decrypted, err := decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if decrypted != original {
		t.Fatalf("Expected %s, got %s", original, decrypted)
	}
}

func TestCredentialStoreOperations(t *testing.T) {
	resetDefaultStore(t)

	tmpDir, err := os.MkdirTemp("", "ctty-cred-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "credentials.json")
	key := deriveMachineKey()

	store := &CredentialStore{
		credentials: make(map[string]Credential),
		filePath:    filePath,
		masterKey:   key,
	}

	storeMu.Lock()
	defaultStore = store
	storeMu.Unlock()

	// 1. Set password
	err = SetPassword("test-host", "mypassword123")
	if err != nil {
		t.Fatalf("Failed to set password: %v", err)
	}

	// 2. Get password
	pass, found := GetPassword("test-host")
	if !found {
		t.Fatal("Expected password to be found")
	}
	if pass != "mypassword123" {
		t.Fatalf("Expected mypassword123, got %s", pass)
	}

	// 3. Rename host
	err = RenameHost("test-host", "test-host-renamed")
	if err != nil {
		t.Fatalf("Failed to rename host: %v", err)
	}

	_, found = GetPassword("test-host")
	if found {
		t.Fatal("Old host name should not have password")
	}

	pass, found = GetPassword("test-host-renamed")
	if !found || pass != "mypassword123" {
		t.Fatalf("Expected renamed host to have password, got %s (found: %v)", pass, found)
	}

	// 4. Delete password
	err = DeletePassword("test-host-renamed")
	if err != nil {
		t.Fatalf("Failed to delete password: %v", err)
	}

	_, found = GetPassword("test-host-renamed")
	if found {
		t.Fatal("Deleted password should not be found")
	}
}

func TestCredentialStoreFailsClosedAndRetriesAfterRepair(t *testing.T) {
	resetDefaultStore(t)

	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	configDir := filepath.Join(configRoot, "ctty")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	vaultPath := filepath.Join(configDir, "credentials.json")
	corrupt := []byte(`{"host_name":`)
	if err := os.WriteFile(vaultPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SetPassword("new-host", "new-password"); err == nil {
		t.Fatal("SetPassword should reject an unreadable credential vault")
	}
	got, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Fatalf("corrupt vault was overwritten: got %q", got)
	}

	if err := os.WriteFile(vaultPath, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetPassword("new-host", "new-password"); err != nil {
		t.Fatalf("SetPassword should retry after the vault is repaired: %v", err)
	}

	data, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved []Credential
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("saved vault is invalid JSON: %v", err)
	}
	if len(saved) != 1 || saved[0].HostName != "new-host" {
		t.Fatalf("saved credentials = %#v, want new-host", saved)
	}
	if password, ok := GetPassword("new-host"); !ok || password != "new-password" {
		t.Fatalf("GetPassword = %q, %v; want repaired credential", password, ok)
	}
}

func TestNormalizeVaultGOOS(t *testing.T) {
	if got := normalizeVaultGOOS("android"); got != "linux" {
		t.Fatalf("normalizeVaultGOOS(android) = %q, want linux", got)
	}
	for _, p := range []string{"linux", "darwin", "windows", ""} {
		if got := normalizeVaultGOOS(p); got != p {
			t.Fatalf("normalizeVaultGOOS(%q) = %q, want unchanged", p, got)
		}
	}
}

func TestExportAndImportAll(t *testing.T) {
	resetDefaultStore(t)
	tmpDir, err := os.MkdirTemp("", "ctty-cred-export-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "credentials.json")
	key := deriveMachineKey()

	store := &CredentialStore{
		credentials: make(map[string]Credential),
		filePath:    filePath,
		masterKey:   key,
	}

	storeMu.Lock()
	defaultStore = store
	storeMu.Unlock()

	_ = SetPassword("host1", "secret1")
	_ = SetPassword("host2", "secret2")

	exported, err := ExportAll()
	if err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	if len(exported) != 2 || exported["host1"] != "secret1" || exported["host2"] != "secret2" {
		t.Fatalf("unexpected exported: %+v", exported)
	}

	// Create a new fresh store and import
	filePath2 := filepath.Join(tmpDir, "credentials2.json")
	store2 := &CredentialStore{
		credentials: make(map[string]Credential),
		filePath:    filePath2,
		masterKey:   key,
	}
	storeMu.Lock()
	defaultStore = store2
	storeMu.Unlock()

	if err := ImportAll(exported, true); err != nil {
		t.Fatalf("ImportAll: %v", err)
	}

	pw1, ok1 := GetPassword("host1")
	pw2, ok2 := GetPassword("host2")
	if !ok1 || pw1 != "secret1" || !ok2 || pw2 != "secret2" {
		t.Fatalf("unexpected imported passwords: %q (%v), %q (%v)", pw1, ok1, pw2, ok2)
	}
}
