package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/backup"
)

var (
	exportFormat string
	exportTags   string
	exportJSON   bool
)

var exportCmd = &cobra.Command{
	Use:   "export [--format json|ssh] [--tags tag1,tag2]",
	Short: "Export hosts and connections to JSON or OpenSSH format",
	Long: `Export ctty connection profiles (SSH, FTP, WebDAV, Serial, Telnet) into a structured JSON
or standard OpenSSH configuration format.

Examples:
  ctty export                                # Export all connection profiles as JSON to stdout
  ctty export --format ssh                   # Export OpenSSH config format
  ctty export --tags prod,web                # Export only hosts with specified tags
  ctty export --format ssh --tags prod > ~/.ssh/config.d/prod.conf`,
	Run: runExport,
}

func runExport(cmd *cobra.Command, args []string) {
	backup.AppVersion = AppVersion

	format := strings.ToLower(exportFormat)
	if exportJSON || format == "" {
		format = "json"
	}

	tags := parseTagsCSV(exportTags)

	data, err := backup.ExportData(backup.ExportOptions{
		Format:        format,
		Tags:          tags,
		SSHConfigFile: configFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error exporting data: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}

func init() {
	RootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&exportFormat, "format", "json", "Export format: json or ssh")
	exportCmd.Flags().StringVar(&exportTags, "tags", "", "Filter exported hosts by comma-separated tags")
	exportCmd.Flags().BoolVar(&exportJSON, "json", false, "Output in JSON format (shorthand for --format json)")
}
