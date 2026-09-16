package webdavclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zsuroy/ctty/internal/webdavconfig"
)

func mockWebDAVServer() *httptest.Server {
	files := map[string][]byte{
		"/test/hello.txt": []byte("hello webdav"),
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "OPTIONS":
			w.Header().Set("DAV", "1, 2")
			w.WriteHeader(http.StatusOK)

		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(207) // Multi-Status
			depth := r.Header.Get("Depth")
			p := r.URL.Path

			if depth == "0" {
				// Stat
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>` + p + `</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype/>
        <D:getcontentlength>12</D:getcontentlength>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
				return
			}

			// ReadDir (Depth: 1)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>` + p + `</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype><D:collection/></D:resourcetype>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>` + p + `/hello.txt</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype/>
        <D:getcontentlength>12</D:getcontentlength>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>` + p + `/subfolder/</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype><D:collection/></D:resourcetype>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))

		case "GET":
			content, ok := files[r.URL.Path]
			if !ok {
				content = []byte("hello webdav stream")
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)

		case "PUT":
			body, _ := io.ReadAll(r.Body)
			files[r.URL.Path] = body
			w.WriteHeader(http.StatusCreated)

		case "MKCOL":
			w.WriteHeader(http.StatusCreated)

		case "DELETE":
			delete(files, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)

		case "MOVE":
			dest := r.Header.Get("Destination")
			if dest != "" {
				w.WriteHeader(http.StatusCreated)
			} else {
				w.WriteHeader(http.StatusBadRequest)
			}

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func TestWebDAVClientOperations(t *testing.T) {
	ts := mockWebDAVServer()
	defer ts.Close()

	site := webdavconfig.WebDAVSite{
		Name: "test-site",
		URL:  ts.URL,
		User: "user",
	}

	// 1. Probe
	if err := Probe(site, 2*time.Second); err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	// 2. Connect
	client, err := Connect(site, "secret")
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer client.Close()

	// 3. List
	entries, err := client.List("/test")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	// Check entries: hello.txt and subfolder
	var foundFile, foundDir bool
	for _, e := range entries {
		if e.Name == "hello.txt" && !e.IsDir {
			foundFile = true
		}
		if e.Name == "subfolder" && e.IsDir {
			foundDir = true
		}
	}
	if !foundFile || !foundDir {
		t.Errorf("expected hello.txt file and subfolder dir; got %+v", entries)
	}

	// 4. Download with progress
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "downloaded.txt")
	var progressCalled bool
	err = client.DownloadWithProgressCtx(context.Background(), "/test/hello.txt", localFile, func(done, total int64) {
		progressCalled = true
	})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !progressCalled {
		t.Errorf("progress callback not called on download")
	}
	content, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !strings.Contains(string(content), "hello webdav") {
		t.Errorf("downloaded content mismatch: %q", string(content))
	}

	// 5. Upload with progress
	uploadSource := filepath.Join(tmpDir, "upload.txt")
	_ = os.WriteFile(uploadSource, []byte("data to upload"), 0644)
	progressCalled = false
	err = client.UploadWithProgressCtx(context.Background(), uploadSource, "/test/upload.txt", func(done, total int64) {
		progressCalled = true
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if !progressCalled {
		t.Errorf("progress callback not called on upload")
	}

	// 6. Mkdir
	if err := client.Mkdir("/test/newdir"); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// 7. Rename
	if err := client.Rename("/test/upload.txt", "/test/renamed.txt"); err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	// 8. Delete
	if err := client.Delete("/test/renamed.txt"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
}

func TestWebDAVDownloadCancelCtx(t *testing.T) {
	ts := mockWebDAVServer()
	defer ts.Close()

	site := webdavconfig.WebDAVSite{Name: "test", URL: ts.URL}
	client, err := Connect(site, "")
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	localFile := filepath.Join(t.TempDir(), "cancel.txt")
	err = client.DownloadWithProgressCtx(ctx, "/test/hello.txt", localFile, nil)
	if err == nil {
		t.Fatalf("expected context canceled error, got nil")
	}
	if _, err := os.Stat(localFile); !os.IsNotExist(err) {
		t.Errorf("canceled download file should not exist on disk")
	}
}
