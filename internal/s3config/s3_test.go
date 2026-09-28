package s3config

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

func TestS3ConfigRoundTrip(t *testing.T) {
	setupTestDir(t)

	sites, err := Load()
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("expected empty sites, got %d", len(sites))
	}

	site1 := S3Site{
		Name:      "aws-prod",
		Endpoint:  "s3.amazonaws.com",
		Region:    "us-west-2",
		Bucket:    "my-data-bucket",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		UseSSL:    true,
		Tags:      []string{"prod", "aws"},
	}
	if err := Add(site1); err != nil {
		t.Fatalf("add site1: %v", err)
	}

	// Duplicate add must fail
	if err := Add(site1); err == nil {
		t.Fatalf("expected duplicate error, got nil")
	}

	site2 := S3Site{
		Name:        "minio-local",
		Endpoint:    "http://127.0.0.1:9000/", // should normalize to 127.0.0.1:9000 and UseSSL = false
		AccessKey:   "minioadmin",
		InsecureTLS: true,
		PathStyle:   true,
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
	minio, ok := Find("minio-local")
	if !ok {
		t.Fatalf("find minio-local failed")
	}
	if minio.Endpoint != "127.0.0.1:9000" {
		t.Errorf("minio.Endpoint = %q, want 127.0.0.1:9000", minio.Endpoint)
	}
	if minio.UseSSL {
		t.Errorf("minio.UseSSL = true, want false (from http:// prefix)")
	}
	if !minio.PathStyle {
		t.Errorf("minio.PathStyle = false, want true")
	}
	if minio.Region != "us-east-1" {
		t.Errorf("minio.Region = %q, want default us-east-1", minio.Region)
	}

	// Update minio
	minio.Bucket = "test-bucket"
	if err := Update("minio-local", minio); err != nil {
		t.Fatalf("update minio: %v", err)
	}
	updated, _ := Find("minio-local")
	if updated.Bucket != "test-bucket" {
		t.Errorf("updated bucket = %q, want test-bucket", updated.Bucket)
	}

	// Delete aws-prod
	if err := Delete("aws-prod"); err != nil {
		t.Fatalf("delete aws-prod: %v", err)
	}
	if _, ok := Find("aws-prod"); ok {
		t.Fatalf("aws-prod still found after delete")
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
		t.Errorf("permissions = %04o, want 0600", perm)
	}
}
