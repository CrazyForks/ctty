package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/serialconfig"
	"github.com/zsuroy/ctty/internal/telnetconfig"
	"github.com/zsuroy/ctty/internal/webdavconfig"
)

const (
	encMagic        = "CTTYENC1"
	pbkdf2IterCount = 100000
	pbkdf2KeyLen    = 32
	saltLen         = 16
	nonceLen        = 12
)

// AppVersion can be set by callers (e.g. cmd package).
var AppVersion = "dev"

// BackupManifest stores metadata about a backup archive.
type BackupManifest struct {
	Version        string    `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	CttyVersion    string    `json:"ctty_version,omitempty"`
	Hostname       string    `json:"hostname,omitempty"`
	Files          []string  `json:"files"`
	Encrypted      bool      `json:"encrypted"`
	HasCredentials bool      `json:"has_credentials"`
	HasSSH         bool      `json:"has_ssh"`
}

// BackupOptions configures a backup operation.
type BackupOptions struct {
	OutputDir          string
	OutputFile         string
	ConfigDir          string
	SSHConfigFile      string
	IncludeSSH         bool
	IncludeCredentials bool
	Passphrase         string
}

// BackupResult describes the generated backup.
type BackupResult struct {
	FilePath    string   `json:"file_path"`
	SizeBytes   int64    `json:"size_bytes"`
	FilesBacked []string `json:"files_backed"`
	Encrypted   bool     `json:"encrypted"`
}

// RestoreOptions configures a restore operation.
type RestoreOptions struct {
	BackupFile string
	TargetDir  string
	TargetSSH  string
	RestoreSSH bool
	Passphrase string
	Overwrite  bool
	DryRun     bool
}

// RestoreResult describes the results of a restore.
type RestoreResult struct {
	RestoredFiles []string       `json:"restored_files"`
	SkippedFiles  []string       `json:"skipped_files"`
	Manifest      BackupManifest `json:"manifest"`
}

// standardConfigFiles lists candidate files inside ctty config directory to back up.
var standardConfigFiles = []string{
	"snippets.json",
	"serial.json",
	"telnet.json",
	"ftp.json",
	"webdav.json",
	"history.json",
	"settings.json",
}

// CreateBackup generates a backup archive (.tar.gz or encrypted .ctty).
func CreateBackup(opts BackupOptions) (*BackupResult, error) {
	configDir := opts.ConfigDir
	if configDir == "" {
		var err error
		configDir, err = config.GetcttyConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve ctty config dir: %w", err)
		}
	}

	var tarBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&tarBuf)
	tarWriter := tar.NewWriter(gzWriter)

	var backedFiles []string

	// 1. Pack standard config files
	for _, relName := range standardConfigFiles {
		filePath := filepath.Join(configDir, relName)
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue // skip if file doesn't exist
		}
		if err := writeTarEntry(tarWriter, relName, content); err != nil {
			return nil, fmt.Errorf("write %s to archive: %w", relName, err)
		}
		backedFiles = append(backedFiles, relName)
	}

	// 2. Export credentials if requested
	hasCreds := false
	if opts.IncludeCredentials {
		creds, err := credential.ExportAll()
		if err == nil && len(creds) > 0 {
			vaultBytes, err := json.MarshalIndent(creds, "", "  ")
			if err == nil {
				if err := writeTarEntry(tarWriter, "vault.json", vaultBytes); err != nil {
					return nil, fmt.Errorf("write vault to archive: %w", err)
				}
				backedFiles = append(backedFiles, "vault.json")
				hasCreds = true
			}
		}
	}

	// 3. Include SSH config if requested
	hasSSH := false
	if opts.IncludeSSH {
		sshPath := opts.SSHConfigFile
		if sshPath == "" {
			sshPath, _ = config.GetDefaultSSHConfigPath()
		}
		if sshPath != "" {
			sshContent, err := os.ReadFile(sshPath)
			if err == nil && len(sshContent) > 0 {
				if err := writeTarEntry(tarWriter, "ssh_config", sshContent); err != nil {
					return nil, fmt.Errorf("write ssh config to archive: %w", err)
				}
				backedFiles = append(backedFiles, "ssh_config")
				hasSSH = true
			}
		}
	}

	// 4. Write manifest
	hostName, _ := os.Hostname()
	manifest := BackupManifest{
		Version:        "1.0",
		CreatedAt:      time.Now().UTC(),
		CttyVersion:    AppVersion,
		Hostname:       hostName,
		Files:          backedFiles,
		Encrypted:      opts.Passphrase != "",
		HasCredentials: hasCreds,
		HasSSH:         hasSSH,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := writeTarEntry(tarWriter, "manifest.json", manifestBytes); err != nil {
		return nil, fmt.Errorf("write manifest to archive: %w", err)
	}

	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}

	finalPayload := tarBuf.Bytes()

	// 5. Encrypt if passphrase given
	if opts.Passphrase != "" {
		encrypted, err := encryptPayload(finalPayload, opts.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("encrypt backup: %w", err)
		}
		finalPayload = encrypted
	}

	// 6. Determine output file path
	outPath := opts.OutputFile
	if outPath == "" {
		dir := opts.OutputDir
		if dir == "" {
			dir = "."
		}
		timestamp := time.Now().Format("20060102-150405")
		ext := ".tar.gz"
		if opts.Passphrase != "" {
			ext = ".ctty"
		}
		outPath = filepath.Join(dir, fmt.Sprintf("ctty-backup-%s%s", timestamp, ext))
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outPath, finalPayload, 0600); err != nil {
		return nil, fmt.Errorf("write backup file: %w", err)
	}

	return &BackupResult{
		FilePath:    outPath,
		SizeBytes:   int64(len(finalPayload)),
		FilesBacked: backedFiles,
		Encrypted:   opts.Passphrase != "",
	}, nil
}

// RestoreBackup restores configurations from a backup archive.
func RestoreBackup(opts RestoreOptions) (*RestoreResult, error) {
	rawBytes, err := os.ReadFile(opts.BackupFile)
	if err != nil {
		return nil, fmt.Errorf("read backup file: %w", err)
	}

	payload := rawBytes
	if strings.HasPrefix(string(rawBytes[:min(len(rawBytes), len(encMagic))]), encMagic) {
		if opts.Passphrase == "" {
			return nil, errors.New("backup is encrypted; please provide --passphrase")
		}
		decrypted, err := decryptPayload(rawBytes, opts.Passphrase)
		if err != nil {
			return nil, err
		}
		payload = decrypted
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("decompress backup: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	targetDir := opts.TargetDir
	if targetDir == "" {
		targetDir, err = config.GetcttyConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve ctty config dir: %w", err)
		}
	}

	res := &RestoreResult{}
	fileContents := make(map[string][]byte)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, fmt.Errorf("read archive entry %s: %w", header.Name, err)
		}
		fileContents[header.Name] = data
	}

	if manifestBytes, ok := fileContents["manifest.json"]; ok {
		_ = json.Unmarshal(manifestBytes, &res.Manifest)
	}

	for name, content := range fileContents {
		if name == "manifest.json" {
			continue
		}

		if name == "vault.json" {
			if opts.DryRun {
				res.RestoredFiles = append(res.RestoredFiles, "credentials (vault)")
				continue
			}
			var creds map[string]string
			if err := json.Unmarshal(content, &creds); err == nil {
				if err := credential.ImportAll(creds, opts.Overwrite); err != nil {
					return nil, fmt.Errorf("import credentials: %w", err)
				}
				res.RestoredFiles = append(res.RestoredFiles, "credentials (vault)")
			}
			continue
		}

		if name == "ssh_config" {
			if !opts.RestoreSSH {
				res.SkippedFiles = append(res.SkippedFiles, "ssh_config (use --restore-ssh to restore)")
				continue
			}
			targetSSH := opts.TargetSSH
			if targetSSH == "" {
				targetSSH, _ = config.GetDefaultSSHConfigPath()
			}
			if targetSSH == "" {
				res.SkippedFiles = append(res.SkippedFiles, "ssh_config (no destination)")
				continue
			}
			if _, err := os.Stat(targetSSH); err == nil && !opts.Overwrite {
				res.SkippedFiles = append(res.SkippedFiles, targetSSH)
				continue
			}
			if opts.DryRun {
				res.RestoredFiles = append(res.RestoredFiles, targetSSH)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(targetSSH), 0700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(targetSSH, content, 0600); err != nil {
				return nil, err
			}
			res.RestoredFiles = append(res.RestoredFiles, targetSSH)
			continue
		}

		// Standard config file
		destPath := filepath.Join(targetDir, name)
		if _, err := os.Stat(destPath); err == nil && !opts.Overwrite {
			res.SkippedFiles = append(res.SkippedFiles, name)
			continue
		}
		if opts.DryRun {
			res.RestoredFiles = append(res.RestoredFiles, name)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(destPath, content, 0600); err != nil {
			return nil, err
		}
		res.RestoredFiles = append(res.RestoredFiles, name)
	}

	return res, nil
}

// ExportOptions specifies options for exporting configuration.
type ExportOptions struct {
	Format        string
	Tags          []string
	SSHConfigFile string
}

// ExportBundle holds aggregated exported configuration data.
type ExportBundle struct {
	Schema        string                      `json:"schema"`
	ExportedAt    time.Time                   `json:"exported_at"`
	CttyVersion   string                      `json:"ctty_version"`
	Hosts         []config.SSHHost            `json:"hosts,omitempty"`
	FTPSites      []ftpconfig.FTPSite         `json:"ftp_sites,omitempty"`
	WebDAVSites   []webdavconfig.WebDAVSite   `json:"webdav_sites,omitempty"`
	SerialDevices []serialconfig.SerialDevice `json:"serial_devices,omitempty"`
	TelnetHosts   []telnetconfig.TelnetHost   `json:"telnet_hosts,omitempty"`
}

// ExportData exports configuration in JSON or OpenSSH config format.
func ExportData(opts ExportOptions) ([]byte, error) {
	var hosts []config.SSHHost
	var err error
	if opts.SSHConfigFile != "" {
		hosts, err = config.ParseSSHConfigFile(opts.SSHConfigFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}
	if err != nil {
		hosts = nil
	}

	// Filter visible hosts
	hosts = config.FilterVisibleHosts(hosts)
	if len(opts.Tags) > 0 {
		var filtered []config.SSHHost
		for _, h := range hosts {
			if config.HostHasAnyTag(h.Tags, opts.Tags) {
				filtered = append(filtered, h)
			}
		}
		hosts = filtered
	}

	if strings.ToLower(opts.Format) == "ssh" {
		var sb strings.Builder
		for i, h := range hosts {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("Host %s\n", h.Name))
			if h.Hostname != "" {
				sb.WriteString(fmt.Sprintf("    HostName %s\n", h.Hostname))
			}
			if h.User != "" {
				sb.WriteString(fmt.Sprintf("    User %s\n", h.User))
			}
			if h.Port != "" && h.Port != "22" {
				sb.WriteString(fmt.Sprintf("    Port %s\n", h.Port))
			}
			if h.Identity != "" {
				sb.WriteString(fmt.Sprintf("    IdentityFile %s\n", h.Identity))
			}
			if h.ProxyJump != "" {
				sb.WriteString(fmt.Sprintf("    ProxyJump %s\n", h.ProxyJump))
			}
			if h.ProxyCommand != "" {
				sb.WriteString(fmt.Sprintf("    ProxyCommand %s\n", h.ProxyCommand))
			}
			if len(h.Tags) > 0 {
				sb.WriteString(fmt.Sprintf("    # tags: %s\n", strings.Join(h.Tags, ", ")))
			}
		}
		return []byte(sb.String()), nil
	}

	// Default JSON format: gather all connection types
	ftpSites, _ := ftpconfig.Load()
	webdavSites, _ := webdavconfig.Load()
	serialDevs, _ := serialconfig.Load()
	telnetHosts, _ := telnetconfig.Load()

	bundle := ExportBundle{
		Schema:        "ctty.export.v1",
		ExportedAt:    time.Now().UTC(),
		CttyVersion:   AppVersion,
		Hosts:         hosts,
		FTPSites:      ftpSites,
		WebDAVSites:   webdavSites,
		SerialDevices: serialDevs,
		TelnetHosts:   telnetHosts,
	}

	return json.MarshalIndent(bundle, "", "  ")
}

func writeTarEntry(tw *tar.Writer, name string, content []byte) error {
	header := &tar.Header{
		Name:     name,
		Size:     int64(len(content)),
		Mode:     0600,
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(content)
	return err
}

func encryptPayload(plain []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}

	key := pbkdf2.Key([]byte(passphrase), salt, pbkdf2IterCount, pbkdf2KeyLen, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plain, nil)

	var out bytes.Buffer
	out.WriteString(encMagic)
	out.Write(salt)
	out.Write(nonce)
	out.Write(ciphertext)
	return out.Bytes(), nil
}

func decryptPayload(enc []byte, passphrase string) ([]byte, error) {
	minHeaderLen := len(encMagic) + saltLen + nonceLen
	if len(enc) < minHeaderLen {
		return nil, errors.New("invalid or truncated encrypted backup file")
	}

	magic := string(enc[:len(encMagic)])
	if magic != encMagic {
		return nil, errors.New("not a valid encrypted ctty backup archive")
	}

	offset := len(encMagic)
	salt := enc[offset : offset+saltLen]
	offset += saltLen
	nonce := enc[offset : offset+nonceLen]
	offset += nonceLen
	ciphertext := enc[offset:]

	key := pbkdf2.Key([]byte(passphrase), salt, pbkdf2IterCount, pbkdf2KeyLen, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: incorrect passphrase or corrupted backup")
	}
	return plain, nil
}
