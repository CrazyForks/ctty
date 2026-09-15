package ftpclient

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ProgressFunc reports transfer progress for a single file.
type ProgressFunc func(transferred, total int64)

// UploadPath uploads a local file or directory to the remote path.
// Directories are transferred recursively.
func (c *Client) UploadPath(ctx context.Context, localPath, remotePath string, progress ProgressFunc) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("local path: %w", err)
	}
	if info.IsDir() {
		return c.uploadDir(ctx, localPath, remotePath, progress)
	}
	return c.UploadWithProgressCtx(ctx, localPath, remotePath, progress)
}

// DownloadPath downloads a remote file or directory to the local path.
// Directories are transferred recursively.
func (c *Client) DownloadPath(ctx context.Context, remotePath, localPath string, progress ProgressFunc) error {
	isDir, err := c.IsRemoteDir(remotePath)
	if err != nil {
		// If we can't determine, try single file download; error will surface.
		if strings.Contains(err.Error(), "not found") {
			return err
		}
		// Fallback: attempt file download and let it fail naturally.
		if mkErr := os.MkdirAll(filepath.Dir(localPath), 0755); mkErr != nil {
			return mkErr
		}
		return c.DownloadWithProgressCtx(ctx, remotePath, localPath, progress)
	}
	if isDir {
		return c.downloadDir(ctx, remotePath, localPath, progress)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	return c.DownloadWithProgressCtx(ctx, remotePath, localPath, progress)
}

// IsRemoteDir reports whether remotePath is a directory.
// It probes via ListDir on the parent.
func (c *Client) IsRemoteDir(remotePath string) (bool, error) {
	clean := path.Clean(remotePath)
	if clean == "." || clean == "/" || clean == "" {
		return true, nil
	}
	dir := path.Dir(clean)
	base := path.Base(clean)
	// If dir == "." and remotePath was relative like "foo", treat dir as "."
	entries, err := c.ListDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name == base {
			return e.IsDir, nil
		}
	}
	return false, fmt.Errorf("ftp: %s not found", remotePath)
}

func (c *Client) uploadDir(ctx context.Context, localDir, remoteDir string, progress ProgressFunc) error {
	if err := c.MkdirAll(remoteDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		src := filepath.Join(localDir, entry.Name())
		dst := path.Join(remoteDir, entry.Name())
		if entry.IsDir() {
			if err := c.uploadDir(ctx, src, dst, progress); err != nil {
				return err
			}
			continue
		}
		if err := c.UploadWithProgressCtx(ctx, src, dst, progress); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) downloadDir(ctx context.Context, remoteDir, localDir string, progress ProgressFunc) error {
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return err
	}
	entries, err := c.ListDir(remoteDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		src := path.Join(remoteDir, entry.Name)
		dst := filepath.Join(localDir, entry.Name)
		if entry.IsDir {
			if err := c.downloadDir(ctx, src, dst, progress); err != nil {
				return err
			}
			continue
		}
		if err := c.DownloadWithProgressCtx(ctx, src, dst, progress); err != nil {
			return err
		}
	}
	return nil
}

// MkdirAll creates a remote directory and any missing parents.
func (c *Client) MkdirAll(remotePath string) error {
	remotePath = path.Clean(remotePath)
	if remotePath == "." || remotePath == "/" || remotePath == "" {
		return nil
	}
	parts := strings.Split(remotePath, "/")
	cur := ""
	if strings.HasPrefix(remotePath, "/") {
		cur = "/"
	}
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if cur == "/" {
			cur = "/" + p
		} else if cur == "" {
			cur = p
		} else {
			cur = path.Join(cur, p)
		}
		// Probe if exists via parent listing to avoid Mkdir error noise.
		parent := path.Dir(cur)
		base := path.Base(cur)
		entries, err := c.ListDir(parent)
		if err == nil {
			found := false
			isDir := false
			for _, e := range entries {
				if e.Name == base {
					found = true
					isDir = e.IsDir
					break
				}
			}
			if found {
				if !isDir {
					return fmt.Errorf("%s exists and is not a directory", cur)
				}
				continue
			}
		}
		if err := c.MakeDir(cur); err != nil {
			// Re-check: may have been created concurrently
			if entries2, err2 := c.ListDir(parent); err2 == nil {
				for _, e := range entries2 {
					if e.Name == base && e.IsDir {
						continue
					}
				}
			}
			// Try to distinguish already-exists
			if strings.Contains(err.Error(), "exists") || strings.Contains(err.Error(), "550") {
				continue
			}
			return fmt.Errorf("mkdir %s: %w", cur, err)
		}
	}
	return nil
}
