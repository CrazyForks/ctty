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
	if err := Register(FinalShell{}); err != nil {
		panic(err)
	}
}

// FinalShell imports SSH profiles from FinalShell config JSON files.
type FinalShell struct{}

func (FinalShell) Name() string         { return "finalshell" }
func (FinalShell) DestFileName() string { return "finalshell.conf" }

func (FinalShell) DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "finalshell", "conn"), nil
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "finalshell", "conn"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "finalshell", "conn"), nil
	default:
		return filepath.Join(home, ".finalshell", "conn"), nil
	}
}

type finalShellRawHost struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Host        string `json:"host"`
	IP          string `json:"ip"`
	Hostname    string `json:"hostname"`
	Port        any    `json:"port"`
	UserName    string `json:"user_name"`
	AltUserName string `json:"userName"`
	User        string `json:"user"`
	Tags        any    `json:"tags"`
	Group       string `json:"group"`
	PrivateKey  string `json:"private_key"`
	KeyPath     string `json:"key_path"`
	Description string `json:"description"`
}

// Parse converts FinalShell JSON into OpenSSH host records.
func (FinalShell) Parse(jsonBytes []byte) ([]config.SSHHost, error) {
	var rawList []finalShellRawHost

	// Try array first
	if err := json.Unmarshal(jsonBytes, &rawList); err != nil {
		// Try single object
		var single finalShellRawHost
		if err2 := json.Unmarshal(jsonBytes, &single); err2 != nil {
			// Try wrapped object {"connections": [...]}
			var wrapped struct {
				Connections []finalShellRawHost `json:"connections"`
				Conn        []finalShellRawHost `json:"conn"`
			}
			if err3 := json.Unmarshal(jsonBytes, &wrapped); err3 != nil {
				return nil, fmt.Errorf("parse finalshell config: %w", err)
			}
			if len(wrapped.Connections) > 0 {
				rawList = wrapped.Connections
			} else {
				rawList = wrapped.Conn
			}
		} else {
			if single.Host != "" || single.IP != "" || single.Hostname != "" {
				rawList = []finalShellRawHost{single}
			}
		}
	}

	used := map[string]bool{}
	var out []config.SSHHost

	for _, item := range rawList {
		hostname := strings.TrimSpace(item.Host)
		if hostname == "" {
			hostname = strings.TrimSpace(item.IP)
		}
		if hostname == "" {
			hostname = strings.TrimSpace(item.Hostname)
		}
		if hostname == "" {
			continue
		}

		displayName := strings.TrimSpace(item.Name)
		if displayName == "" {
			displayName = strings.TrimSpace(item.Label)
		}

		alias := UniqueAlias(SanitizeAlias(displayName, hostname), used)
		used[alias] = true

		user := strings.TrimSpace(item.UserName)
		if user == "" {
			user = strings.TrimSpace(item.AltUserName)
		}
		if user == "" {
			user = strings.TrimSpace(item.User)
		}

		portStr := PortString(item.Port)
		if portStr == "" || portStr == "0" {
			portStr = "22"
		}

		identity := strings.TrimSpace(item.PrivateKey)
		if identity == "" {
			identity = strings.TrimSpace(item.KeyPath)
		}

		var tags []string
		if item.Group != "" {
			tags = append(tags, strings.TrimSpace(item.Group))
		}
		switch t := item.Tags.(type) {
		case string:
			for _, part := range strings.Split(t, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					tags = append(tags, part)
				}
			}
		case []any:
			for _, part := range t {
				if s, ok := part.(string); ok && strings.TrimSpace(s) != "" {
					tags = append(tags, strings.TrimSpace(s))
				}
			}
		case []string:
			for _, part := range t {
				if strings.TrimSpace(part) != "" {
					tags = append(tags, strings.TrimSpace(part))
				}
			}
		}

		h := config.SSHHost{
			Name:     alias,
			Hostname: hostname,
			User:     user,
			Port:     portStr,
			Identity: identity,
			Tags:     tags,
		}
		out = append(out, h)
	}

	return out, nil
}
