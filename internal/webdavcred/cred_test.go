package webdavcred

import (
	"testing"
)

func TestWebDAVCredRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Missing entry
	if _, ok := GetPassword("nextcloud"); ok {
		t.Fatalf("expected missing password, got found")
	}

	// Set password
	if err := SetPassword("nextcloud", "secret123"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	pw, ok := GetPassword("nextcloud")
	if !ok || pw != "secret123" {
		t.Fatalf("got password %q, ok=%v; want secret123, true", pw, ok)
	}

	// Update password
	if err := SetPassword("nextcloud", "newsecret"); err != nil {
		t.Fatalf("update password: %v", err)
	}
	pw, _ = GetPassword("nextcloud")
	if pw != "newsecret" {
		t.Fatalf("got %q, want newsecret", pw)
	}

	// Delete password
	if err := DeletePassword("nextcloud"); err != nil {
		t.Fatalf("delete password: %v", err)
	}
	if _, ok := GetPassword("nextcloud"); ok {
		t.Fatalf("expected deleted password, but still found")
	}
}
