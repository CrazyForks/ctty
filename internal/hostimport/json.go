package hostimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
)

func init() {
	if err := Register(GenericJSON{}); err != nil {
		panic(err)
	}
}

// GenericJSON imports SSH profiles from standard/custom JSON files.
type GenericJSON struct{}

func (GenericJSON) Name() string         { return "json" }
func (GenericJSON) DestFileName() string { return "json.conf" }

func (GenericJSON) DefaultPath() (string, error) {
	if _, err := os.Stat("hosts.json"); err == nil {
		return "hosts.json", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "hosts.json", nil
	}
	return filepath.Join(home, "hosts.json"), nil
}

type genericRawHost struct {
	Name              string `json:"name"`
	Label             string `json:"label"`
	Alias             string `json:"alias"`
	Title             string `json:"title"`
	ID                string `json:"id"`
	Host              string `json:"host"`
	Hostname          string `json:"hostname"`
	IP                string `json:"ip"`
	Address           string `json:"address"`
	Server            string `json:"server"`
	Port              any    `json:"port"`
	User              string `json:"user"`
	Username          string `json:"username"`
	UserName          string `json:"user_name"`
	Login             string `json:"login"`
	Identity          string `json:"identity"`
	IdentityFile      string `json:"identity_file"`
	IdentityFileCamel string `json:"identityFile"`
	Key               string `json:"key"`
	KeyPath           string `json:"key_path"`
	PrivateKey        string `json:"private_key"`
	PrivateKeyCamel   string `json:"privateKey"`
	ProxyJump         string `json:"proxy_jump"`
	ProxyJumpCamel    string `json:"proxyJump"`
	JumpHost          string `json:"jump_host"`
	JumpHostCamel     string `json:"jumpHost"`
	Bastion           string `json:"bastion"`
	ProxyCommand      string `json:"proxy_command"`
	ProxyCommandCamel string `json:"proxyCommand"`
	Tags              any    `json:"tags"`
	Tag               any    `json:"tag"`
	Group             any    `json:"group"`
	Groups            any    `json:"groups"`
	Options           string `json:"options"`
	SSHOptions        string `json:"ssh_options"`
}

// Parse converts a generic JSON file into OpenSSH host records.
func (GenericJSON) Parse(jsonBytes []byte) ([]config.SSHHost, error) {
	var rawList []genericRawHost

	// 1. Try top-level array
	if err := json.Unmarshal(jsonBytes, &rawList); err != nil || len(rawList) == 0 {
		// 2. Try wrapped object: {"hosts": [...]}, {"servers": [...]}, {"nodes": [...]}, {"items": [...]}
		var wrapped struct {
			Hosts   []genericRawHost `json:"hosts"`
			Servers []genericRawHost `json:"servers"`
			Nodes   []genericRawHost `json:"nodes"`
			Items   []genericRawHost `json:"items"`
		}
		if err2 := json.Unmarshal(jsonBytes, &wrapped); err2 == nil && (len(wrapped.Hosts) > 0 || len(wrapped.Servers) > 0 || len(wrapped.Nodes) > 0 || len(wrapped.Items) > 0) {
			if len(wrapped.Hosts) > 0 {
				rawList = wrapped.Hosts
			} else if len(wrapped.Servers) > 0 {
				rawList = wrapped.Servers
			} else if len(wrapped.Nodes) > 0 {
				rawList = wrapped.Nodes
			} else if len(wrapped.Items) > 0 {
				rawList = wrapped.Items
			}
		} else {
			// 3. Try single host object
			var single genericRawHost
			if err3 := json.Unmarshal(jsonBytes, &single); err3 == nil {
				if firstNonEmpty(single.Hostname, single.Host, single.IP, single.Address, single.Server) != "" {
					rawList = []genericRawHost{single}
				}
			} else {
				return nil, fmt.Errorf("parse json hosts: %w", err)
			}
		}
	}

	used := map[string]bool{}
	var out []config.SSHHost

	for _, item := range rawList {
		hostname := firstNonEmpty(item.Hostname, item.Host, item.IP, item.Address, item.Server)
		if hostname == "" {
			continue
		}

		displayName := firstNonEmpty(item.Name, item.Label, item.Alias, item.Title, item.ID)
		alias := UniqueAlias(SanitizeAlias(displayName, hostname), used)
		used[alias] = true

		user := firstNonEmpty(item.User, item.Username, item.UserName, item.Login)

		portStr := PortString(item.Port)
		if portStr == "" || portStr == "0" {
			portStr = "22"
		}

		identity := firstNonEmpty(
			item.Identity, item.IdentityFile, item.IdentityFileCamel,
			item.Key, item.KeyPath, item.PrivateKey, item.PrivateKeyCamel,
		)

		proxyJump := firstNonEmpty(
			item.ProxyJump, item.ProxyJumpCamel,
			item.JumpHost, item.JumpHostCamel, item.Bastion,
		)

		proxyCommand := firstNonEmpty(item.ProxyCommand, item.ProxyCommandCamel)
		options := firstNonEmpty(item.Options, item.SSHOptions)

		var tags []string
		extractTags(&tags, item.Tags)
		extractTags(&tags, item.Tag)
		extractTags(&tags, item.Group)
		extractTags(&tags, item.Groups)

		h := config.SSHHost{
			Name:         alias,
			Hostname:     hostname,
			User:         user,
			Port:         portStr,
			Identity:     identity,
			ProxyJump:    proxyJump,
			ProxyCommand: proxyCommand,
			Options:      options,
			Tags:         tags,
		}
		out = append(out, h)
	}

	return out, nil
}

func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		trimmed := strings.TrimSpace(c)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func extractTags(out *[]string, v any) {
	if v == nil {
		return
	}
	switch t := v.(type) {
	case string:
		for _, part := range strings.Split(t, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				*out = append(*out, part)
			}
		}
	case []string:
		for _, part := range t {
			part = strings.TrimSpace(part)
			if part != "" {
				*out = append(*out, part)
			}
		}
	case []any:
		for _, elem := range t {
			if s, ok := elem.(string); ok && strings.TrimSpace(s) != "" {
				*out = append(*out, strings.TrimSpace(s))
			}
		}
	}
}
