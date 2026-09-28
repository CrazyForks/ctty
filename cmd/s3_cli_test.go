package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/zsuroy/ctty/internal/s3config"
)

func TestS3ListSearchInfoJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if err := s3config.Add(s3config.S3Site{
		Name: "minio-local", Endpoint: "127.0.0.1:9000", Bucket: "backups", AccessKey: "minioadmin", Tags: []string{"local", "s3"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s3config.Add(s3config.S3Site{
		Name: "aws-prod", Endpoint: "s3.amazonaws.com", Region: "us-east-1", Bucket: "assets",
	}); err != nil {
		t.Fatal(err)
	}

	s3Format = "json"
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	runS3List()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	var sites []s3config.S3Site
	if err := json.Unmarshal(buf.Bytes(), &sites); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(sites) != 2 {
		t.Fatalf("want 2 sites, got %d", len(sites))
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	runS3Search("minio")
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	sites = nil
	if err := json.Unmarshal(buf.Bytes(), &sites); err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].Name != "minio-local" {
		t.Fatalf("search 'minio': want 1 (minio-local), got %v", sites)
	}

	r3, w3, _ := os.Pipe()
	os.Stdout = w3
	runS3Info("minio-local")
	_ = w3.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r3)
	var one s3config.S3Site
	if err := json.Unmarshal(buf.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.AccessKey != "minioadmin" || one.Name != "minio-local" {
		t.Fatalf("info: %+v", one)
	}
}

func TestS3Completions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	_ = s3config.Add(s3config.S3Site{Name: "alpha-s3", Endpoint: "s3.alpha.test"})
	_ = s3config.Add(s3config.S3Site{Name: "beta-s3", Endpoint: "s3.beta.test"})

	names := completeS3Names("al")
	if len(names) != 1 || names[0] != "alpha-s3" {
		t.Fatalf("expected [alpha-s3], got %v", names)
	}
}

func TestS3RenameCmdRegistered(t *testing.T) {
	found := false
	for _, c := range s3Cmd.Commands() {
		if c.Name() == "rename" {
			found = true
			if len(c.Aliases) == 0 || c.Aliases[0] != "mv" {
				t.Fatalf("expected alias 'mv', got %v", c.Aliases)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected 'rename' subcommand under s3Cmd")
	}
}
