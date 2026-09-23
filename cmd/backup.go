package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/backup"
)

var (
	backupOutput       string
	backupPassphrase   string
	backupIncludeCreds bool
	backupIncludeSSH   bool
	backupFormat       string
	backupJSON         bool
)

var backupCmd = &cobra.Command{
	Use:   "backup [-o output-file] [--passphrase secret]",
	Short: "Back up ctty configurations and credentials into an archive",
	Long: `Create a backup archive containing ctty configuration files (snippets, FTP/WebDAV sites,
serial/telnet devices, preferences, and saved credentials).

Optionally encrypt the archive with AES-256-GCM using a passphrase so it can be safely stored
or transferred across machines.

Examples:
  ctty backup                                # Save unencrypted tar.gz with default timestamp name
  ctty backup -o my-backup.tar.gz            # Save to specific output file
  ctty backup -p "mypassword"                # Save encrypted archive (ctty-backup-*.ctty)
  ctty backup --include-ssh                  # Also include OpenSSH config file
  ctty backup --format json                  # Machine-readable output`,
	Run: runBackup,
}

func runBackup(cmd *cobra.Command, args []string) {
	backup.AppVersion = AppVersion

	isJSON := backupJSON || strings.ToLower(backupFormat) == "json"

	res, err := backup.CreateBackup(backup.BackupOptions{
		OutputFile:         backupOutput,
		Passphrase:         backupPassphrase,
		IncludeCredentials: backupIncludeCreds,
		IncludeSSH:         backupIncludeSSH,
		SSHConfigFile:      configFile,
	})
	if err != nil {
		if isJSON {
			b, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
			fmt.Println(string(b))
		} else {
			fmt.Fprintf(os.Stderr, "Error creating backup: %v\n", err)
		}
		os.Exit(1)
	}

	if isJSON {
		b, err := json.MarshalIndent(map[string]any{
			"ok":           true,
			"file_path":    res.FilePath,
			"size_bytes":   res.SizeBytes,
			"files_backed": res.FilesBacked,
			"encrypted":    res.Encrypted,
		}, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
	} else {
		encStr := "No"
		if res.Encrypted {
			encStr = "Yes (AES-256-GCM)"
		}
		fmt.Println("Backup created successfully:")
		fmt.Printf("  File:      %s\n", res.FilePath)
		fmt.Printf("  Size:      %d bytes\n", res.SizeBytes)
		fmt.Printf("  Encrypted: %s\n", encStr)
		if len(res.FilesBacked) > 0 {
			fmt.Printf("  Contents:  %s\n", strings.Join(res.FilesBacked, ", "))
		}
	}
}

func init() {
	RootCmd.AddCommand(backupCmd)
	backupCmd.Flags().StringVarP(&backupOutput, "output", "o", "", "Output file path (default: ./ctty-backup-TIMESTAMP.tar.gz or .ctty)")
	backupCmd.Flags().StringVarP(&backupPassphrase, "passphrase", "p", "", "Passphrase to encrypt the backup bundle with AES-256-GCM")
	backupCmd.Flags().BoolVar(&backupIncludeCreds, "include-credentials", true, "Include saved credentials in the backup bundle")
	backupCmd.Flags().BoolVar(&backupIncludeSSH, "include-ssh", false, "Include OpenSSH configuration in the backup bundle")
	backupCmd.Flags().StringVar(&backupFormat, "format", "", "Output format: table (default) or json")
	backupCmd.Flags().BoolVar(&backupJSON, "json", false, "Output in JSON format (shorthand for --format json)")
}
