// Package webdavclient wraps gowebdav for ctty's WebDAV TUI and CLI.
package webdavclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/studio-b12/gowebdav"
	"github.com/zsuroy/ctty/internal/webdavconfig"
	"github.com/zsuroy/ctty/internal/webdavcred"
)

// RemoteEntry represents a remote WebDAV file or directory listing row.
type RemoteEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// Client wraps a gowebdav client session used by the dual-pane TUI and CLI.
type Client struct {
	c    *gowebdav.Client
	site webdavconfig.WebDAVSite
	mu   sync.Mutex
}

// Connect creates and validates a WebDAV client connection for the given site.
// If password is empty, it attempts to load a saved password from the vault.
func Connect(site webdavconfig.WebDAVSite, password string) (*Client, error) {
	if password == "" {
		if saved, ok := webdavcred.GetPassword(site.Name); ok {
			password = saved
		}
	}

	c := gowebdav.NewClient(site.URL, site.User, password)

	if site.InsecureTLS {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		c.SetTransport(tr)
	}
	c.SetTimeout(30 * time.Second)

	if err := c.Connect(); err != nil {
		return nil, fmt.Errorf("webdav connect to %s failed: %w", site.URL, err)
	}

	return &Client{
		c:    c,
		site: site,
	}, nil
}

// Probe checks if a WebDAV site is reachable within the given timeout.
func Probe(site webdavconfig.WebDAVSite, timeout time.Duration) error {
	pw, _ := webdavcred.GetPassword(site.Name)
	c := gowebdav.NewClient(site.URL, site.User, pw)
	if site.InsecureTLS {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		c.SetTransport(tr)
	}
	c.SetTimeout(timeout)
	return c.Connect()
}

// List returns the directory entries at remotePath.
func (c *Client) List(remotePath string) ([]RemoteEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	infos, err := c.c.ReadDir(remotePath)
	if err != nil {
		return nil, err
	}

	entries := make([]RemoteEntry, 0, len(infos))
	for _, info := range infos {
		name := info.Name()
		if name == "." || name == ".." {
			continue
		}
		entries = append(entries, RemoteEntry{
			Name:    name,
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return entries, nil
}

// DownloadWithProgressCtx downloads remotePath to localPath, reporting progress.
func (c *Client) DownloadWithProgressCtx(ctx context.Context, remotePath, localPath string, onProgress func(done, total int64)) error {
	c.mu.Lock()
	stat, err := c.c.Stat(remotePath)
	var total int64 = -1
	if err == nil {
		total = stat.Size()
	}

	stream, err := c.c.ReadStream(remotePath)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	defer stream.Close()

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp := localPath + ".ctty.tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	buf := make([]byte, 32*1024)
	var done int64
	for {
		select {
		case <-ctx.Done():
			_ = f.Close()
			_ = os.Remove(tmp)
			return ctx.Err()
		default:
		}

		n, rErr := stream.Read(buf)
		if n > 0 {
			if _, wErr := f.Write(buf[:n]); wErr != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return wErr
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			_ = f.Close()
			_ = os.Remove(tmp)
			return rErr
		}
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, localPath)
}

type progressReader struct {
	r          io.Reader
	ctx        context.Context
	done       int64
	total      int64
	onProgress func(done, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	select {
	case <-pr.ctx.Done():
		return 0, pr.ctx.Err()
	default:
	}

	n, err := pr.r.Read(p)
	if n > 0 {
		pr.done += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.done, pr.total)
		}
	}
	return n, err
}

// UploadWithProgressCtx uploads localPath to remotePath, reporting progress.
func (c *Client) UploadWithProgressCtx(ctx context.Context, localPath, remotePath string, onProgress func(done, total int64)) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	total := fi.Size()

	pr := &progressReader{
		r:          f,
		ctx:        ctx,
		total:      total,
		onProgress: onProgress,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.c.WriteStreamWithLength(remotePath, pr, total, 0644)
}

// Mkdir creates a directory at remotePath.
func (c *Client) Mkdir(remotePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.c.Mkdir(remotePath, 0755)
}

// Delete removes the file or directory at remotePath.
func (c *Client) Delete(remotePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.c.RemoveAll(remotePath)
}

// Rename moves/renames remotePath to newPath.
func (c *Client) Rename(remotePath, newPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.c.Rename(remotePath, newPath, false)
}

// Download downloads remotePath to localPath synchronously without progress callback.
func (c *Client) Download(remotePath, localPath string) error {
	return c.DownloadWithProgressCtx(context.Background(), remotePath, localPath, nil)
}

// Upload uploads localPath to remotePath synchronously without progress callback.
func (c *Client) Upload(localPath, remotePath string) error {
	return c.UploadWithProgressCtx(context.Background(), localPath, remotePath, nil)
}

// Close closes the WebDAV client.
func (c *Client) Close() error {
	return nil
}
