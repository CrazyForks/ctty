package s3cred

import (
	"testing"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestS3CredRoundTrip(t *testing.T) {
	setupTestDir(t)

	// Missing key returns false
	if _, ok := GetSecretKey("my-minio"); ok {
		t.Fatalf("expected no secret key for non-existent site")
	}

	// Store secret key
	if err := SetSecretKey("my-minio", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"); err != nil {
		t.Fatalf("set secret key: %v", err)
	}

	got, ok := GetSecretKey("my-minio")
	if !ok || got != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Fatalf("get secret key = %q, %v, want secret, true", got, ok)
	}

	// Overwrite
	if err := SetSecretKey("my-minio", "newSecretKey123"); err != nil {
		t.Fatalf("update secret key: %v", err)
	}
	got, ok = GetSecretKey("my-minio")
	if !ok || got != "newSecretKey123" {
		t.Fatalf("updated secret key = %q, want newSecretKey123", got)
	}

	// Delete
	if err := DeleteSecretKey("my-minio"); err != nil {
		t.Fatalf("delete secret key: %v", err)
	}
	if _, ok := GetSecretKey("my-minio"); ok {
		t.Fatalf("secret key still found after delete")
	}
}
