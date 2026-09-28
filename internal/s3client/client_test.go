package s3client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zsuroy/ctty/internal/s3config"
	"github.com/zsuroy/ctty/internal/s3cred"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestResolveBucketAndPrefix(t *testing.T) {
	// Case 1: Site has a default bucket configured
	client1 := &Client{
		site: s3config.S3Site{
			Name:   "site-with-bucket",
			Bucket: "default-bucket",
		},
	}

	testsWithDefault := []struct {
		input        string
		expectBucket string
		expectPrefix string
	}{
		{"", "default-bucket", ""},
		{"/", "default-bucket", ""},
		{".", "default-bucket", ""},
		{"photos", "default-bucket", "photos"},
		{"/photos", "default-bucket", "photos"},
		{"/photos/2026/img.jpg", "default-bucket", "photos/2026/img.jpg"},
		{"default-bucket", "default-bucket", ""},
		{"default-bucket/", "default-bucket", ""},
		{"default-bucket/photos/img.jpg", "default-bucket", "photos/img.jpg"},
	}

	for _, tt := range testsWithDefault {
		b, p := client1.ResolveBucketAndPrefix(tt.input)
		if b != tt.expectBucket || p != tt.expectPrefix {
			t.Errorf("ResolveBucketAndPrefix(%q) with default bucket = (%q, %q), want (%q, %q)",
				tt.input, b, p, tt.expectBucket, tt.expectPrefix)
		}
	}

	// Case 2: Site has NO default bucket configured
	client2 := &Client{
		site: s3config.S3Site{
			Name: "site-no-bucket",
		},
	}

	testsWithoutDefault := []struct {
		input        string
		expectBucket string
		expectPrefix string
	}{
		{"", "", ""},
		{"/", "", ""},
		{".", "", ""},
		{"my-bucket", "my-bucket", ""},
		{"/my-bucket", "my-bucket", ""},
		{"/my-bucket/", "my-bucket", ""},
		{"my-bucket/photos", "my-bucket", "photos"},
		{"/my-bucket/photos/img.jpg", "my-bucket", "photos/img.jpg"},
	}

	for _, tt := range testsWithoutDefault {
		b, p := client2.ResolveBucketAndPrefix(tt.input)
		if b != tt.expectBucket || p != tt.expectPrefix {
			t.Errorf("ResolveBucketAndPrefix(%q) without default bucket = (%q, %q), want (%q, %q)",
				tt.input, b, p, tt.expectBucket, tt.expectPrefix)
		}
	}
}

func TestBuildURLAddressingStyle(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		bucket     string
		key        string
		pathStyle  bool
		expectHost string
		expectPath string
	}{
		{
			name:       "Custom domain with port must use path style",
			endpoint:   "xxxxxx.cn:9090",
			bucket:     "a-bucket",
			key:        "",
			pathStyle:  false,
			expectHost: "xxxxxx.cn:9090",
			expectPath: "/a-bucket",
		},
		{
			name:       "Custom domain with port entering folder",
			endpoint:   "xxxxxx.cn:9090",
			bucket:     "a-bucket",
			key:        "subfolder",
			pathStyle:  false,
			expectHost: "xxxxxx.cn:9090",
			expectPath: "/a-bucket/subfolder",
		},
		{
			name:       "IP with port uses path style",
			endpoint:   "192.168.1.100:9000",
			bucket:     "b1",
			key:        "",
			pathStyle:  false,
			expectHost: "192.168.1.100:9000",
			expectPath: "/b1",
		},
		{
			name:       "Localhost with port uses path style",
			endpoint:   "localhost:9000",
			bucket:     "b1",
			key:        "",
			pathStyle:  false,
			expectHost: "localhost:9000",
			expectPath: "/b1",
		},
		{
			name:       "AWS endpoint uses virtual-hosted style when pathStyle is false",
			endpoint:   "s3.amazonaws.com",
			bucket:     "mybucket",
			key:        "",
			pathStyle:  false,
			expectHost: "mybucket.s3.amazonaws.com",
			expectPath: "/",
		},
		{
			name:       "AWS endpoint uses path style when pathStyle is true",
			endpoint:   "s3.amazonaws.com",
			bucket:     "mybucket",
			key:        "",
			pathStyle:  true,
			expectHost: "s3.amazonaws.com",
			expectPath: "/mybucket",
		},
		{
			name:       "Bucket with dots uses path style even on AWS",
			endpoint:   "s3.amazonaws.com",
			bucket:     "my.bucket.with.dots",
			key:        "",
			pathStyle:  false,
			expectHost: "s3.amazonaws.com",
			expectPath: "/my.bucket.with.dots",
		},
		{
			name:       "Custom non-AWS domain without port uses path style",
			endpoint:   "minio.mycompany.com",
			bucket:     "mybucket",
			key:        "",
			pathStyle:  false,
			expectHost: "minio.mycompany.com",
			expectPath: "/mybucket",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				endpoint: tt.endpoint,
				site: s3config.S3Site{
					PathStyle: tt.pathStyle,
				},
			}
			u, err := client.buildURL(tt.bucket, tt.key, nil)
			if err != nil {
				t.Fatalf("buildURL failed: %v", err)
			}
			if u.Host != tt.expectHost {
				t.Errorf("u.Host = %q, want %q", u.Host, tt.expectHost)
			}
			if u.Path != tt.expectPath {
				t.Errorf("u.Path = %q, want %q", u.Path, tt.expectPath)
			}
		})
	}
}

func TestConnectLoadsPasswordFromVault(t *testing.T) {
	setupTestDir(t)

	site := s3config.S3Site{
		Name:      "test-vault-site",
		Endpoint:  "127.0.0.1:9000",
		AccessKey: "admin",
		UseSSL:    false,
	}

	// Save secret key in vault
	if err := s3cred.SetSecretKey(site.Name, "superSecretKeyVault"); err != nil {
		t.Fatalf("save secret: %v", err)
	}

	client, err := Connect(site, "")
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// mockS3Server creates a minimal HTTP test server that responds to S3 XML queries.
func mockS3Server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")

		// Root request: ListBuckets
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Owner>
        <ID>test-owner</ID>
        <DisplayName>test-user</DisplayName>
    </Owner>
    <Buckets>
        <Bucket>
            <Name>test-bucket-1</Name>
            <CreationDate>2026-01-01T00:00:00.000Z</CreationDate>
        </Bucket>
        <Bucket>
            <Name>test-bucket-2</Name>
            <CreationDate>2026-01-02T00:00:00.000Z</CreationDate>
        </Bucket>
    </Buckets>
</ListAllMyBucketsResult>`))
			return
		}

		// GetBucketLocation
		if strings.Contains(r.URL.RawQuery, "location") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`))
			return
		}

		// Bucket query: ListObjectsV2
		if strings.HasPrefix(r.URL.Path, "/test-bucket-1") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "list-type=2") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Name>test-bucket-1</Name>
    <Prefix></Prefix>
    <KeyCount>2</KeyCount>
    <MaxKeys>1000</MaxKeys>
    <IsTruncated>false</IsTruncated>
    <Contents>
        <Key>README.md</Key>
        <LastModified>2026-01-01T12:00:00.000Z</LastModified>
        <ETag>&quot;d41d8cd98f00b204e9800998ecf8427e&quot;</ETag>
        <Size>1024</Size>
        <StorageClass>STANDARD</StorageClass>
    </Contents>
    <CommonPrefixes>
        <Prefix>documents/</Prefix>
    </CommonPrefixes>
</ListBucketResult>`))
			return
		}

		// HeadBucket or BucketExists
		if r.URL.Path == "/test-bucket-1" && (r.Method == http.MethodHead || r.Method == http.MethodGet) {
			w.WriteHeader(http.StatusOK)
			return
		}

		// GetObject README.md
		body := "Hello, S3 CTTY"
		if r.URL.Path == "/test-bucket-1/README.md" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}

		// StatObject README.md
		if r.URL.Path == "/test-bucket-1/README.md" && r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Prefix query: ListObjectsV2 under documents/
		if strings.HasPrefix(r.URL.Path, "/test-bucket-1") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "prefix=documents/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Name>test-bucket-1</Name>
    <Prefix>documents/</Prefix>
    <KeyCount>1</KeyCount>
    <MaxKeys>1000</MaxKeys>
    <IsTruncated>false</IsTruncated>
    <Contents>
        <Key>documents/doc1.txt</Key>
        <LastModified>2026-01-01T12:00:00.000Z</LastModified>
        <ETag>&quot;d41d8cd98f00b204e9800998ecf8427e&quot;</ETag>
        <Size>100</Size>
        <StorageClass>STANDARD</StorageClass>
    </Contents>
</ListBucketResult>`))
			return
		}

		// CopyObject (PUT with x-amz-copy-source)
		if strings.HasPrefix(r.URL.Path, "/test-bucket-1/") && r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "" {
			copySource := r.Header.Get("x-amz-copy-source")
			if copySource == "/test-bucket-1/error.txt" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>InternalError</Code><Message>Failed to copy</Message></Error>`))
				return
			}
			w.Header().Set("ETag", `"copy-etag"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>&quot;copy-etag&quot;</ETag><LastModified>2026-01-01T12:00:00.000Z</LastModified></CopyObjectResult>`))
			return
		}

		// PutObject
		if strings.HasPrefix(r.URL.Path, "/test-bucket-1/") && r.Method == http.MethodPut {
			w.Header().Set("ETag", `"dummy-etag"`)
			w.WriteHeader(http.StatusOK)
			return
		}

		// DeleteObject
		if strings.HasPrefix(r.URL.Path, "/test-bucket-1/") && r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestMockS3ClientOperations(t *testing.T) {
	ts := mockS3Server(t)
	defer ts.Close()

	endpoint := strings.TrimPrefix(ts.URL, "http://")

	site := s3config.S3Site{
		Name:      "mock-s3",
		Endpoint:  endpoint,
		AccessKey: "test-access-key",
		UseSSL:    false,
		PathStyle: true,
	}

	client, err := Connect(site, "test-secret-key")
	if err != nil {
		t.Fatalf("connect mock: %v", err)
	}

	// 1. List root -> returns buckets
	entries, err := client.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("list root buckets: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(entries))
	}
	if entries[0].Name != "test-bucket-1" || !entries[0].IsDir {
		t.Errorf("bucket 0 = %+v", entries[0])
	}

	// 2. List bucket-1 -> returns contents + common prefix
	entries, err = client.List(context.Background(), "test-bucket-1")
	if err != nil {
		t.Fatalf("list bucket-1: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries in bucket-1, got %d", len(entries))
	}
	// documents/ should be dir, README.md should be file
	if !entries[0].IsDir || entries[0].Name != "documents" {
		t.Errorf("expected dir documents, got %+v", entries[0])
	}
	if entries[1].IsDir || entries[1].Name != "README.md" || entries[1].Size != 1024 {
		t.Errorf("expected file README.md, got %+v", entries[1])
	}

	// 3. Download single object
	tmpLocal := filepath.Join(t.TempDir(), "README.md")
	var transferredBytes int64
	err = client.DownloadWithProgressCtx(context.Background(), "test-bucket-1", "README.md", tmpLocal, func(done, total int64) {
		transferredBytes = done
	})
	if err != nil {
		t.Fatalf("download object: %v", err)
	}
	content, err := os.ReadFile(tmpLocal)
	if err != nil {
		t.Fatalf("read downloaded: %v", err)
	}
	if string(content) != "Hello, S3 CTTY" {
		t.Fatalf("downloaded content = %q, want 'Hello, S3 CTTY'", string(content))
	}
	if transferredBytes != 14 {
		t.Errorf("transferredBytes = %d, want 14", transferredBytes)
	}

	// 4. Upload single object
	err = client.UploadWithProgressCtx(context.Background(), tmpLocal, "test-bucket-1", "upload.txt", nil)
	if err != nil {
		t.Fatalf("upload object: %v", err)
	}

	// 5. Delete object
	err = client.Delete(context.Background(), "test-bucket-1/README.md")
	if err != nil {
		t.Fatalf("delete object: %v", err)
	}

	// 6. Probe
	err = Probe(site, 2*time.Second)
	if err != nil {
		t.Fatalf("probe site: %v", err)
	}
}

func TestFormatCopySource(t *testing.T) {
	tests := []struct {
		bucket string
		key    string
		want   string
	}{
		{"my-bucket", "file.txt", "/my-bucket/file.txt"},
		{"my-bucket", "/file.txt", "/my-bucket/file.txt"},
		{"my-bucket", "folder/sub/doc.pdf", "/my-bucket/folder/sub/doc.pdf"},
		{"my-bucket", "folder/my photo [1].jpg", "/my-bucket/folder/my%20photo%20%5B1%5D.jpg"},
		{"my-bucket", "dir/", "/my-bucket/dir/"},
		{"my.bucket.org", "file+name.txt", "/my.bucket.org/file%2Bname.txt"},
	}
	for _, tt := range tests {
		got := formatCopySource(tt.bucket, tt.key)
		if got != tt.want {
			t.Errorf("formatCopySource(%q, %q) = %q, want %q", tt.bucket, tt.key, got, tt.want)
		}
	}
}

func TestS3RenameOperations(t *testing.T) {
	ts := mockS3Server(t)
	defer ts.Close()

	endpoint := strings.TrimPrefix(ts.URL, "http://")
	site := s3config.S3Site{
		Name:      "mock-rename",
		Endpoint:  endpoint,
		AccessKey: "test-access-key",
		UseSSL:    false,
		PathStyle: true,
	}

	client, err := Connect(site, "test-secret-key")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// 1. CopyObject success
	err = client.CopyObject(context.Background(), "test-bucket-1", "README.md", "test-bucket-1", "README.bak")
	if err != nil {
		t.Fatalf("copy object: %v", err)
	}

	// 2. CopyObject error returned in XML
	err = client.CopyObject(context.Background(), "test-bucket-1", "error.txt", "test-bucket-1", "dest.txt")
	if err == nil || !strings.Contains(err.Error(), "InternalError") {
		t.Fatalf("expected InternalError, got: %v", err)
	}

	// 3. Rename single file
	err = client.Rename(context.Background(), "test-bucket-1/README.md", "test-bucket-1/README-renamed.md")
	if err != nil {
		t.Fatalf("rename single file: %v", err)
	}

	// 4. Rename prefix (directory)
	err = client.Rename(context.Background(), "test-bucket-1/documents", "test-bucket-1/docs")
	if err != nil {
		t.Fatalf("rename prefix: %v", err)
	}

	// 5. Attempting to rename bucket root must be rejected
	err = client.Rename(context.Background(), "test-bucket-1", "test-bucket-2")
	if err == nil || !strings.Contains(err.Error(), "cannot rename bucket") {
		t.Fatalf("expected cannot rename bucket error, got: %v", err)
	}
}

func TestS3RenameDeletesOldObjectAndDirMarker(t *testing.T) {
	var deletedPaths []string
	var copiedSources []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			if r.URL.Path == "/test-bucket/doc.txt" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		case http.MethodGet:
			if r.URL.Query().Get("prefix") == "myfolder/" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
    <Name>test-bucket</Name>
    <Prefix>myfolder/</Prefix>
    <Contents>
        <Key>myfolder/</Key>
    </Contents>
    <Contents>
        <Key>myfolder/item.txt</Key>
    </Contents>
</ListBucketResult>`))
				return
			}
		case http.MethodPut:
			if copySrc := r.Header.Get("x-amz-copy-source"); copySrc != "" {
				copiedSources = append(copiedSources, copySrc)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult><ETag>"etag"</ETag></CopyObjectResult>`))
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		case http.MethodDelete:
			deletedPaths = append(deletedPaths, r.URL.Path)
			if strings.Contains(r.URL.Path, "forbidden") {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Forbidden</Message></Error>`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := &Client{
		endpoint:   strings.TrimPrefix(ts.URL, "http://"),
		useSSL:     false,
		httpClient: ts.Client(),
		site: s3config.S3Site{
			Name:      "test-rename-delete",
			PathStyle: true,
		},
	}

	// 1. Rename single file deletes old file key
	deletedPaths = nil
	err := client.Rename(context.Background(), "test-bucket/doc.txt", "test-bucket/doc2.txt")
	if err != nil {
		t.Fatalf("rename file failed: %v", err)
	}
	foundDocDelete := false
	for _, p := range deletedPaths {
		if p == "/test-bucket/doc.txt" {
			foundDocDelete = true
		}
	}
	if !foundDocDelete {
		t.Fatalf("expected /test-bucket/doc.txt to be deleted, got deletes: %v", deletedPaths)
	}

	// 2. RenamePrefix deletes both child objects and the directory marker WITH trailing slash
	deletedPaths = nil
	err = client.RenamePrefix(context.Background(), "test-bucket/myfolder", "test-bucket/myfolder2")
	if err != nil {
		t.Fatalf("rename prefix failed: %v", err)
	}
	foundDirMarkerDelete := false
	foundItemDelete := false
	for _, p := range deletedPaths {
		if p == "/test-bucket/myfolder/" {
			foundDirMarkerDelete = true
		}
		if p == "/test-bucket/myfolder/item.txt" {
			foundItemDelete = true
		}
	}
	if !foundDirMarkerDelete {
		t.Fatalf("expected directory marker /test-bucket/myfolder/ with trailing slash to be deleted, got: %v", deletedPaths)
	}
	if !foundItemDelete {
		t.Fatalf("expected item /test-bucket/myfolder/item.txt to be deleted, got: %v", deletedPaths)
	}

	// 3. Rename returns error if delete fails
	err = client.Rename(context.Background(), "test-bucket/forbidden.txt", "test-bucket/forbidden2.txt")
	if err == nil || !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("expected delete failure error, got: %v", err)
	}
}
