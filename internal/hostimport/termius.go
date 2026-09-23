package hostimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
)

func init() {
	if err := Register(Termius{}); err != nil {
		panic(err)
	}
}

// Termius imports SSH profiles from Termius JSON exports.
type Termius struct{}

func (Termius) Name() string         { return "termius" }
func (Termius) DestFileName() string { return "termius.conf" }

func (Termius) DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Termius", "export.json"), nil
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "Termius", "export.json"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "Termius", "export.json"), nil
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "Termius", "export.json"), nil
		}
		return filepath.Join(home, ".config", "Termius", "export.json"), nil
	}
}

type termiusRawHost struct {
	Label        string          `json:"label"`
	Name         string          `json:"name"`
	Address      string          `json:"address"`
	Host         string          `json:"host"`
	Hostname     string          `json:"hostname"`
	Port         any             `json:"port"`
	Username     string          `json:"username"`
	User         string          `json:"user"`
	Group        any             `json:"group"`
	GroupLabel   string          `json:"group_label"`
	Tags         any             `json:"tags"`
	IdentityFile string          `json:"identity_file"`
	KeyPath      string          `json:"key_path"`
	ProxyJump    string          `json:"proxy_jump"`
	JumpHost     string          `json:"jump_host"`
	SSHConfig    json.RawMessage `json:"ssh_config"`
}

type termiusSSHConfig struct {
	Port         any    `json:"port"`
	Username     string `json:"username"`
	User         string `json:"user"`
	IdentityFile string `json:"identity_file"`
	KeyPath      string `json:"key_path"`
	ProxyJump    string `json:"proxy_jump"`
	JumpHost     string `json:"jump_host"`
}

// Parse converts Termius JSON export into OpenSSH host records.
func (Termius) Parse(jsonBytes []byte) ([]config.SSHHost, error) {
	var rawList []termiusRawHost

	// Try top-level array first
	if err := json.Unmarshal(jsonBytes, &rawList); err != nil {
		// Try wrapped object {"hosts": [...]}
		var wrapped struct {
			Hosts []termiusRawHost `json:"hosts"`
		}
		if err2 := json.Unmarshal(jsonBytes, &wrapped); err2 != nil {
			// Try wrapped object {"items": [...]}
			var wrappedItems struct {
				Items []termiusRawHost `json:"items"`
			}
			if err3 := json.Unmarshal(jsonBytes, &wrappedItems); err3 != nil {
				return nil, fmt.Errorf("parse termius export: %w", err)
			}
			rawList = wrappedItems.Items
		} else {
			rawList = wrapped.Hosts
		}
	}

	used := map[string]bool{}
	var out []config.SSHHost

	for _, item := range rawList {
		hostname := strings.TrimSpace(item.Address)
		if hostname == "" {
			hostname = strings.TrimSpace(item.Hostname)
		}
		if hostname == "" {
			hostname = strings.TrimSpace(item.Host)
		}
		if hostname == "" {
			continue
		}

		displayName := strings.TrimSpace(item.Label)
		if displayName == "" {
			displayName = strings.TrimSpace(item.Name)
		}

		alias := UniqueAlias(SanitizeAlias(displayName, hostname), used)
		used[alias] = true

		user := strings.TrimSpace(item.Username)
		if user == "" {
			user = strings.TrimSpace(item.User)
		}

		portStr := PortString(item.Port)
		identity := strings.TrimSpace(item.IdentityFile)
		if identity == "" {
			identity = strings.TrimSpace(item.KeyPath)
		}
		proxyJump := strings.TrimSpace(item.ProxyJump)
		if proxyJump == "" {
			proxyJump = strings.TrimSpace(item.JumpHost)
		}

		// Check nested ssh_config
		if len(item.SSHConfig) > 0 {
			var sc termiusSSHConfig
			if err := json.Unmarshal(item.SSHConfig, &sc); err == nil {
				if user == "" {
					if sc.Username != "" {
						user = strings.TrimSpace(sc.Username)
					} else if sc.User != "" {
						user = strings.TrimSpace(sc.User)
					}
				}
				if portStr == "" || portStr == "0" {
					portStr = PortString(sc.Port)
				}
				if identity == "" {
					if sc.IdentityFile != "" {
						identity = strings.TrimSpace(sc.IdentityFile)
					} else if sc.KeyPath != "" {
						identity = strings.TrimSpace(sc.KeyPath)
					}
				}
				if proxyJump == "" {
					if sc.ProxyJump != "" {
						proxyJump = strings.TrimSpace(sc.ProxyJump)
					} else if sc.JumpHost != "" {
						proxyJump = strings.TrimSpace(sc.JumpHost)
					}
				}
			}
		}

		if portStr == "" || portStr == "0" {
			portStr = "22"
		}

		var tags []string
		// Extract group as tag
		groupName := strings.TrimSpace(item.GroupLabel)
		if groupName == "" {
			switch g := item.Group.(type) {
			case string:
				groupName = strings.TrimSpace(g)
			case map[string]any:
				if l, ok := g["label"].(string); ok {
					groupName = strings.TrimSpace(l)
				} else if n, ok := g["name"].(string); ok {
					groupName = strings.TrimSpace(n)
				}
			}
		}
		if groupName != "" {
			tags = append(tags, groupName)
		}

		// Extract tags
		switch t := item.Tags.(type) {
		case []any:
			for _, tagElem := range t {
				if ts, ok := tagElem.(string); ok && strings.TrimSpace(ts) != "" {
					tags = append(tags, strings.TrimSpace(ts))
				}
			}
		case []string:
			for _, ts := range t {
				if strings.TrimSpace(ts) != "" {
					tags = append(tags, strings.TrimSpace(ts))
				}
			}
		case string:
			for _, ts := range strings.Split(t, ",") {
				ts = strings.TrimSpace(ts)
				if ts != "" {
					tags = append(tags, ts)
				}
			}
		}

		h := config.SSHHost{
			Name:      alias,
			Hostname:  hostname,
			User:      user,
			Port:      portStr,
			Identity:  identity,
			ProxyJump: proxyJump,
			Tags:      tags,
		}
		out = append(out, h)
	}

	return out, nil
}
