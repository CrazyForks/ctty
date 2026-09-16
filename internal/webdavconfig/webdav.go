// Package webdavconfig stores saved WebDAV site configurations.
//
// WebDAV sites (Nextcloud, NAS shares, Alist, ownCloud) live at
// ~/.config/ctty/webdav.json with 0600 permissions. Passwords are NEVER stored
// here — see internal/webdavcred for vault-backed WebDAV passwords.
package webdavconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WebDAVSite represents a saved WebDAV connection configuration.
type WebDAVSite struct {
	Name        string   `json:"name"`                   // User-friendly alias
	URL         string   `json:"url"`                    // Full endpoint URL (e.g. https://dav.example.com/remote.php/dav/files/user/)
	User        string   `json:"user,omitempty"`         // Login user (optional/empty if anonymous)
	InsecureTLS bool     `json:"insecure_tls,omitempty"` // Skip TLS certificate verification (self-signed certs)
	Tags        []string `json:"tags,omitempty"`         // Optional organizational tags
}

// Config is the on-disk JSON structure for saved WebDAV sites.
type Config struct {
	Sites []WebDAVSite `json:"sites"`
}

func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ctty", "webdav.json"), nil
	}
	return filepath.Join(home, ".config", "ctty", "webdav.json"), nil
}

func getConfigDir() (string, error) {
	p, err := getConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// Load reads saved WebDAV sites from disk. Missing file → empty list.
func Load() ([]WebDAVSite, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []WebDAVSite{}, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing webdav config: %w", err)
	}
	for i := range cfg.Sites {
		cfg.Sites[i].normalize()
	}
	return cfg.Sites, nil
}

// Save writes WebDAV sites atomically (temp + rename), creating the directory.
func Save(sites []WebDAVSite) error {
	dir, err := getConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating ctty config directory: %w", err)
	}

	path, err := getConfigPath()
	if err != nil {
		return err
	}

	sorted := make([]WebDAVSite, len(sites))
	copy(sorted, sites)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for i := range sorted {
		sorted[i].normalize()
	}

	data, err := json.MarshalIndent(Config{Sites: sorted}, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Add appends a new WebDAV site and saves.
func Add(site WebDAVSite) error {
	site.normalize()
	if site.Name == "" || site.URL == "" {
		return errors.New("webdav site name and URL are required")
	}
	sites, err := Load()
	if err != nil {
		sites = nil
	}
	for _, s := range sites {
		if s.Name == site.Name {
			return fmt.Errorf("a webdav site named %q already exists", site.Name)
		}
	}
	sites = append(sites, site)
	return Save(sites)
}

// Update replaces the saved site identified by oldName and saves.
func Update(oldName string, site WebDAVSite) error {
	site.normalize()
	sites, err := Load()
	if err != nil {
		return err
	}
	idx := -1
	for i, s := range sites {
		if s.Name == oldName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no saved webdav site named %q", oldName)
	}
	for i, s := range sites {
		if i != idx && s.Name == site.Name {
			return fmt.Errorf("a webdav site named %q already exists", site.Name)
		}
	}
	sites[idx] = site
	return Save(sites)
}

// Delete removes a saved WebDAV site by name and saves.
func Delete(name string) error {
	sites, err := Load()
	if err != nil {
		return err
	}
	out := sites[:0]
	found := false
	for _, s := range sites {
		if s.Name == name {
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		return fmt.Errorf("no saved webdav site named %q", name)
	}
	return Save(out)
}

// Find returns the saved site with the given name.
func Find(name string) (WebDAVSite, bool) {
	sites, err := Load()
	if err != nil {
		return WebDAVSite{}, false
	}
	for _, s := range sites {
		if s.Name == name {
			return s, true
		}
	}
	return WebDAVSite{}, false
}

// DefaultSite returns sensible defaults for a new WebDAV site.
func DefaultSite() WebDAVSite {
	return WebDAVSite{URL: "https://"}
}

func (s *WebDAVSite) normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.URL = strings.TrimSpace(s.URL)
	s.User = strings.TrimSpace(s.User)

	if s.URL != "" && !strings.Contains(s.URL, "://") {
		s.URL = "http://" + s.URL
	}
	if u, err := url.Parse(s.URL); err == nil && u.Scheme != "" {
		s.URL = u.String()
	}

	tags := s.Tags[:0]
	for _, t := range s.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	s.Tags = tags
}
