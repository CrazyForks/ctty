// Package ftpcred stores FTP passwords separately from the SSH credential vault.
//
// File: ~/.config/ctty/ftp-credentials.json (mode 0600).
//
// WARNING: Passwords are stored in cleartext JSON. This is intentional for the
// first FTP slice and is documented as insecure — do not use for high-value
// secrets. Never write FTP passwords into credentials.json (SSH vault).
package ftpcred

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one site's stored FTP password.
type Entry struct {
	SiteName  string    `json:"site_name"`
	Password  string    `json:"password"` // cleartext — insecure, see package docs
	UpdatedAt time.Time `json:"updated_at"`
}

type fileStore struct {
	Credentials map[string]Entry `json:"credentials"`
}

var (
	mu       sync.Mutex
	pathOverride string // tests only
)

// SetPathOverride redirects the store path (tests). Empty restores default.
func SetPathOverride(p string) {
	mu.Lock()
	defer mu.Unlock()
	pathOverride = p
}

func storePath() (string, error) {
	if pathOverride != "" {
		return pathOverride, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ctty", "ftp-credentials.json"), nil
	}
	return filepath.Join(home, ".config", "ctty", "ftp-credentials.json"), nil
}

func load() (map[string]Entry, error) {
	path, err := storePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var fs fileStore
	if err := json.Unmarshal(data, &fs); err != nil {
		return nil, fmt.Errorf("parsing ftp-credentials: %w", err)
	}
	if fs.Credentials == nil {
		fs.Credentials = map[string]Entry{}
	}
	return fs.Credentials, nil
}

func save(creds map[string]Entry) error {
	path, err := storePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileStore{Credentials: creds}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// GetPassword returns the stored FTP password for a site name.
func GetPassword(siteName string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	creds, err := load()
	if err != nil {
		return "", false
	}
	e, ok := creds[siteName]
	if !ok || e.Password == "" {
		return "", false
	}
	return e.Password, true
}

// SetPassword stores (or updates) a cleartext FTP password for a site.
func SetPassword(siteName, password string) error {
	mu.Lock()
	defer mu.Unlock()
	creds, err := load()
	if err != nil {
		return err
	}
	if password == "" {
		delete(creds, siteName)
	} else {
		creds[siteName] = Entry{
			SiteName:  siteName,
			Password:  password,
			UpdatedAt: time.Now().UTC(),
		}
	}
	return save(creds)
}

// DeletePassword removes a stored FTP password.
func DeletePassword(siteName string) error {
	return SetPassword(siteName, "")
}
