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
	restorePassphrase string
	restoreOverwrite  bool
	restoreDryRun     bool
	restoreSSH        bool
	restoreFormat     string
	restoreJSON       bool
)

var restoreCmd = &cobra.Command{
	Use:   "restore <backup-file>",
	Short: "Restore ctty configurations and credentials from a backup archive",
	Long: `Restore configuration files (snippets, FTP/WebDAV sites, serial/telnet devices,
preferences, and credentials) from a ctty backup archive.

If the archive is encrypted, supply the passphrase with -p or --passphrase.

Examples:
  ctty restore ctty-backup-20260921.tar.gz          # Restore unencrypted backup
  ctty restore ctty-backup-20260921.ctty -p secret  # Restore encrypted backup
  ctty restore my-backup.tar.gz --dry-run           # Preview what would be restored
  ctty restore my-backup.tar.gz --overwrite         # Overwrite existing files
  ctty restore my-backup.tar.gz --restore-ssh       # Also restore OpenSSH config`,
	Args: cobra.ExactArgs(1),
	Run:  runRestore,
}

func runRestore(cmd *cobra.Command, args []string) {
	backupFile := args[0]
	isJSON := restoreJSON || strings.ToLower(restoreFormat) == "json"

	res, err := backup.RestoreBackup(backup.RestoreOptions{
		BackupFile: backupFile,
		Passphrase: restorePassphrase,
		Overwrite:  restoreOverwrite,
		DryRun:     restoreDryRun,
		RestoreSSH: restoreSSH,
		TargetSSH:  configFile,
	})
	if err != nil {
		if isJSON {
			b, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
			fmt.Println(string(b))
		} else {
			fmt.Fprintf(os.Stderr, "Error restoring backup: %v\n", err)
		}
		os.Exit(1)
	}

	if isJSON {
		b, err := json.MarshalIndent(map[string]any{
			"ok":             true,
			"dry_run":        restoreDryRun,
			"restored_files": res.RestoredFiles,
			"skipped_files":  res.SkippedFiles,
			"manifest":       res.Manifest,
		}, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
	} else {
		if restoreDryRun {
			fmt.Println("[Dry Run] Would restore the following files:")
		} else {
			fmt.Println("Backup restored successfully:")
		}

		if len(res.RestoredFiles) > 0 {
			fmt.Printf("  Restored: %s\n", strings.Join(res.RestoredFiles, ", "))
		} else {
			fmt.Println("  Restored: (none)")
		}

		if len(res.SkippedFiles) > 0 {
			fmt.Printf("  Skipped:  %s (already exist; use --overwrite to replace)\n", strings.Join(res.SkippedFiles, ", "))
		}
	}
}

func init() {
	RootCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().StringVarP(&restorePassphrase, "passphrase", "p", "", "Passphrase to decrypt the backup bundle if encrypted")
	restoreCmd.Flags().BoolVar(&restoreOverwrite, "overwrite", false, "Overwrite existing configuration files")
	restoreCmd.Flags().BoolVar(&restoreDryRun, "dry-run", false, "Simulate restore without modifying files")
	restoreCmd.Flags().BoolVar(&restoreSSH, "restore-ssh", false, "Restore OpenSSH config if present in the backup")
	restoreCmd.Flags().StringVar(&restoreFormat, "format", "", "Output format: table (default) or json")
	restoreCmd.Flags().BoolVar(&restoreJSON, "json", false, "Output in JSON format (shorthand for --format json)")
}
