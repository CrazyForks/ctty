// Package s3config stores saved S3 / object storage site configurations.
//
// S3 sites (MinIO, AWS S3, Cloudflare R2, Aliyun OSS, Ceph, etc.) live at
// ~/.config/ctty/s3.json with 0600 permissions. Secret keys are NEVER stored
// here — see internal/s3cred for vault-backed S3 secret access keys.
package s3config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// S3Site represents a saved S3 connection configuration.
type S3Site struct {
	Name        string   `json:"name"`                   // User-friendly alias
	Endpoint    string   `json:"endpoint"`               // Hostname or host:port (e.g. s3.amazonaws.com, 127.0.0.1:9000)
	Region      string   `json:"region,omitempty"`       // AWS region (e.g. us-east-1, auto, cn-hangzhou)
	Bucket      string   `json:"bucket,omitempty"`       // Optional default bucket name
	AccessKey   string   `json:"access_key,omitempty"`   // Access key ID (username/identity)
	UseSSL      bool     `json:"use_ssl"`                // Connect via HTTPS (default true unless http:// specified)
	InsecureTLS bool     `json:"insecure_tls,omitempty"` // Skip TLS certificate verification (for self-signed certs)
	PathStyle   bool     `json:"path_style,omitempty"`   // Force path-style addressing (endpoint/bucket vs bucket.endpoint)
	Tags        []string `json:"tags,omitempty"`         // Optional organizational tags
}

// Config is the on-disk JSON structure for saved S3 sites.
type Config struct {
	Sites []S3Site `json:"sites"`
}

func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ctty", "s3.json"), nil
	}
	return filepath.Join(home, ".config", "ctty", "s3.json"), nil
}

func getConfigDir() (string, error) {
	p, err := getConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// Load reads saved S3 sites from disk. Missing file → empty list.
func Load() ([]S3Site, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []S3Site{}, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing s3 config: %w", err)
	}
	for i := range cfg.Sites {
		cfg.Sites[i].normalize()
	}
	return cfg.Sites, nil
}

// Save writes S3 sites atomically (temp + rename), creating the directory.
func Save(sites []S3Site) error {
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

	sorted := make([]S3Site, len(sites))
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

// Add appends a new S3 site and saves.
func Add(site S3Site) error {
	site.normalize()
	if site.Name == "" || site.Endpoint == "" {
		return errors.New("s3 site name and endpoint are required")
	}
	sites, err := Load()
	if err != nil {
		sites = nil
	}
	for _, s := range sites {
		if s.Name == site.Name {
			return fmt.Errorf("an s3 site named %q already exists", site.Name)
		}
	}
	sites = append(sites, site)
	return Save(sites)
}

// Update replaces the saved site identified by oldName and saves.
func Update(oldName string, site S3Site) error {
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
		return fmt.Errorf("no saved s3 site named %q", oldName)
	}
	for i, s := range sites {
		if i != idx && s.Name == site.Name {
			return fmt.Errorf("an s3 site named %q already exists", site.Name)
		}
	}
	sites[idx] = site
	return Save(sites)
}

// Delete removes a saved S3 site by name and saves.
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
		return fmt.Errorf("no saved s3 site named %q", name)
	}
	return Save(out)
}

// Find returns the saved site with the given name.
func Find(name string) (S3Site, bool) {
	sites, err := Load()
	if err != nil {
		return S3Site{}, false
	}
	for _, s := range sites {
		if s.Name == name {
			return s, true
		}
	}
	return S3Site{}, false
}

// DefaultSite returns sensible defaults for a new S3 site.
func DefaultSite() S3Site {
	return S3Site{
		Endpoint:  "s3.amazonaws.com",
		Region:    "us-east-1",
		UseSSL:    true,
		PathStyle: false,
	}
}

func (s *S3Site) normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Endpoint = strings.TrimSpace(s.Endpoint)
	s.Region = strings.TrimSpace(s.Region)
	s.Bucket = strings.TrimSpace(s.Bucket)
	s.AccessKey = strings.TrimSpace(s.AccessKey)

	// Strip URL scheme if user pasted full URL (e.g. http://127.0.0.1:9000 or https://play.min.io)
	ep := s.Endpoint
	if strings.HasPrefix(strings.ToLower(ep), "http://") {
		s.UseSSL = false
		ep = ep[7:]
	} else if strings.HasPrefix(strings.ToLower(ep), "https://") {
		s.UseSSL = true
		ep = ep[8:]
	}
	ep = strings.TrimRight(ep, "/")
	s.Endpoint = ep

	if s.Region == "" {
		s.Region = "us-east-1"
	}

	tags := s.Tags[:0]
	for _, t := range s.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	s.Tags = tags
}
