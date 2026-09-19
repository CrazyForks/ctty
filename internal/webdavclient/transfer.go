package webdavclient

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ProgressFunc reports transfer progress for a file.
type ProgressFunc func(transferred, total int64)

// Stat returns FileInfo for remotePath.
func (c *Client) Stat(remotePath string) (os.FileInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.c.Stat(remotePath)
}

// IsRemoteDir reports whether remotePath is a directory.
func (c *Client) IsRemoteDir(remotePath string) (bool, error) {
	clean := path.Clean(remotePath)
	if clean == "." || clean == "/" || clean == "" {
		return true, nil
	}
	info, err := c.Stat(remotePath)
	if err == nil {
		return info.IsDir(), nil
	}

	// Fallback to parent listing check if Stat fails (some servers deny Stat on collections)
	dir := path.Dir(clean)
	base := path.Base(clean)
	entries, listErr := c.List(dir)
	if listErr == nil {
		for _, e := range entries {
			if e.Name == base {
				return e.IsDir, nil
			}
		}
	}
	return false, err
}

// MkdirAll creates remotePath and all parent directories.
func (c *Client) MkdirAll(remotePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.c.MkdirAll(remotePath, 0755)
}

// UploadPath uploads a local file or directory to remotePath.
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

func (c *Client) uploadDir(ctx context.Context, localDir, remoteDir string, progress ProgressFunc) error {
	if err := c.MkdirAll(remoteDir); err != nil && !strings.Contains(strings.ToLower(err.Error()), "exist") {
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

// DownloadPath downloads remotePath to localPath.
// Directories are transferred recursively.
func (c *Client) DownloadPath(ctx context.Context, remotePath, localPath string, progress ProgressFunc) error {
	isDir, err := c.IsRemoteDir(remotePath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(err.Error(), "404") {
			return err
		}
		// Fallback: try file download
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

func (c *Client) downloadDir(ctx context.Context, remoteDir, localDir string, progress ProgressFunc) error {
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return err
	}
	entries, err := c.List(remoteDir)
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
