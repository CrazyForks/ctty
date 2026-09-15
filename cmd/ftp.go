package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/ftpclient"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ui"
)

var ftpFormat string

// ftpCmd opens the FTP site manager / dual-pane browser, or queries saved sites.
//
// Forms:
//
//	ctty ftp                         → site manager TUI (humans)
//	ctty ftp list|search|info …      → non-interactive JSON/human query (agents)
//	ctty ftp ls <site> [path]        → list remote directory (headless)
//	ctty ftp get <site> <remote> <local> → download
//	ctty ftp put <site> <local> <remote> → upload
//	ctty ftp <name>                  → dual-pane FTP browser for a saved site
//
// Agents must use list|search|info --format json and never open the TUI.
// FTP passwords live in the encrypted vault (credentials.json, ftp: prefix).
var ftpCmd = &cobra.Command{
	Use:   "ftp [list|search|info|ls|get|put|mkdir|rm|rmdir|rename|name]",
	Short: "Open FTP site manager / browser, or query saved sites",
	Long: `Open the FTP site manager TUI, query saved sites, or browse/transfer via FTP.

FTP traffic (including passwords) is cleartext unless FTPS is used (follow-up).
Site inventory: ~/.config/ctty/ftp.json
Passwords (encrypted vault): ~/.config/ctty/credentials.json under ftp: names.

Forms:
  ctty ftp                          List and manage saved FTP sites (TUI)
  ctty ftp list [--format json]     List saved sites
  ctty ftp search [query] [--format json]
  ctty ftp info <name> [--format json]
  ctty ftp ls <site> [remotePath] [--format json]  List remote directory
  ctty ftp get <site> <remote> <local>             Download file or directory
  ctty ftp put <site> <local> <remote>             Upload file or directory
  ctty ftp mkdir <site> <remotePath>               Create remote directory
  ctty ftp rm <site> <remotePath>                  Delete remote file
  ctty ftp rmdir <site> <remotePath>               Delete remote directory (recursive)
  ctty ftp rename <site> <old> <new>               Rename / move remote file
  ctty ftp lab-nas                  Open dual-pane local|remote browser for a site

Agents: use list|search|info|ls --format json only; never open the FTP TUI.`,
	Args: cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			if args[0] == "info" && len(args) == 1 {
				return completeFTPNames(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			if (args[0] == "ls" || args[0] == "get" || args[0] == "put" || args[0] == "mkdir" || args[0] == "rm" || args[0] == "rmdir" || args[0] == "rename") && len(args) == 1 {
				return completeFTPNames(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			// ls second arg is remote path — file completion for remote is not available, fallback to no completion
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		base := []string{"list", "search", "info", "ls", "get", "put", "mkdir", "rm", "rmdir", "rename"}
		names := completeFTPNames(toComplete)
		var out []string
		lower := strings.ToLower(toComplete)
		for _, b := range base {
			if strings.HasPrefix(b, lower) {
				out = append(out, b)
			}
		}
		out = append(out, names...)
		return out, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := ui.RunFTPMode(AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running FTP mode: %v", err)
			}
			fmt.Println()
			return
		}

		switch args[0] {
		case "list":
			runFTPList()
			return
		case "search":
			q := ""
			if len(args) > 1 {
				q = strings.Join(args[1:], " ")
			}
			runFTPSearch(q)
			return
		case "info":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: ftp info requires a site name\n")
				os.Exit(1)
			}
			runFTPInfo(args[1])
			return
		}

		name := args[0]
		if _, ok := ftpconfig.Find(name); !ok {
			fmt.Fprintf(os.Stderr, "Error: ftp site %q not found\n", name)
			os.Exit(2)
		}
		if err := ui.RunFTPBrowserMode(name, AppVersion, noUpdateCheck); err != nil {
			log.Fatalf("Error running FTP browser: %v", err)
		}
		fmt.Println()
	},
}

func completeFTPNames(toComplete string) []string {
	sites, err := ftpconfig.Load()
	if err != nil {
		return nil
	}
	lower := strings.ToLower(toComplete)
	var out []string
	for _, s := range sites {
		if strings.HasPrefix(strings.ToLower(s.Name), lower) {
			out = append(out, s.Name)
		}
	}
	return out
}

func runFTPList() {
	sites, err := ftpconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	outputFTPSites(sites)
}

func runFTPSearch(query string) {
	sites, err := ftpconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		outputFTPSites(sites)
		return
	}
	words := strings.Fields(strings.ToLower(query))
	var matched []ftpconfig.FTPSite
	for _, s := range sites {
		hay := strings.ToLower(s.Name + " " + s.Host + " " + s.User + " " + strings.Join(s.Tags, " "))
		ok := true
		for _, w := range words {
			w = strings.TrimPrefix(w, "#")
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, s)
		}
	}
	outputFTPSites(matched)
}

func runFTPInfo(name string) {
	site, ok := ftpconfig.Find(name)
	if !ok {
		if ftpFormat == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"ok": false, "error": "NOT_FOUND", "name": name,
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: ftp site %q not found\n", name)
		}
		os.Exit(2)
	}
	if ftpFormat == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(site)
		return
	}
	fmt.Printf("Name: %s\n", site.Name)
	fmt.Printf("Host: %s\n", site.Host)
	fmt.Printf("Port: %d\n", site.Port)
	fmt.Printf("User: %s\n", site.User)
	if len(site.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(site.Tags, ", "))
	}
}

func outputFTPSites(sites []ftpconfig.FTPSite) {
	if ftpFormat == "json" {
		if sites == nil {
			sites = []ftpconfig.FTPSite{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sites)
		return
	}
	if len(sites) == 0 {
		fmt.Println("No FTP sites found.")
		return
	}
	for _, s := range sites {
		tags := ""
		if len(s.Tags) > 0 {
			tags = " [" + strings.Join(s.Tags, ", ") + "]"
		}
		user := s.User
		if user == "" {
			user = "anonymous"
		}
		fmt.Printf("%-20s %s@%s:%d%s\n", s.Name, user, s.Host, s.Port, tags)
	}
}

// --- headless FTP operations ---

func connectFTPClient(siteName string) (*ftpclient.Client, ftpconfig.FTPSite) {
	site, ok := ftpconfig.Find(siteName)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: ftp site %q not found\n", siteName)
		os.Exit(2)
	}
	client, err := ftpclient.Connect(site, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return client, site
}

func ftpProgressPrinter(label string) func(transferred, total int64) {
	return func(transferred, total int64) {
		if total <= 0 {
			fmt.Fprintf(os.Stderr, "\r%s %d bytes", label, transferred)
			return
		}
		pct := float64(transferred) * 100 / float64(total)
		fmt.Fprintf(os.Stderr, "\r%s %d/%d (%.0f%%)", label, transferred, total, pct)
	}
}

func ftpFinishProgress() {
	fmt.Fprintln(os.Stderr)
}

// Subcommands

var ftpLsFormat string

var ftpLsCmd = &cobra.Command{
	Use:   "ls <site> [remotePath]",
	Short: "List remote directory via FTP",
	Long: `List a remote directory on an FTP site.

Example:
  ctty ftp ls lab-nas
  ctty ftp ls lab-nas /pub --format json`,
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: ftpSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		siteName := args[0]
		remotePath := "/"
		if len(args) > 1 {
			remotePath = args[1]
		}
		client, _ := connectFTPClient(siteName)
		defer client.Close()
		entries, err := client.ListDir(remotePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if ftpLsFormat == "json" {
			if entries == nil {
				entries = []ftpclient.RemoteEntry{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(entries)
			return
		}
		if len(entries) == 0 {
			fmt.Println("(empty)")
			return
		}
		for _, e := range entries {
			kind := "file"
			if e.IsDir {
				kind = "dir"
			}
			fmt.Printf("%-6s %10d  %s  %s\n", kind, e.Size, e.ModTime.Format("2006-01-02 15:04"), e.Name)
		}
	},
}

var ftpGetCmd = &cobra.Command{
	Use:   "get <site> <remotePath> <localPath>",
	Short: "Download a remote file or directory via FTP",
	Long: `Download a remote file or directory via FTP.

Progress is written to stderr. Directories are transferred recursively.

Example:
  ctty ftp get lab-nas /pub/file.txt ./file.txt
  ctty ftp get lab-nas /pub/mydir ./mydir`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: ftpSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runFTPGet(args[0], args[1], args[2])
	},
}

var ftpPutCmd = &cobra.Command{
	Use:   "put <site> <localPath> <remotePath>",
	Short: "Upload a local file or directory via FTP",
	Long: `Upload a local file or directory via FTP.

Progress is written to stderr. Directories are transferred recursively.

Example:
  ctty ftp put lab-nas ./file.txt /pub/file.txt
  ctty ftp put lab-nas ./mydir /pub/mydir`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: ftpSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runFTPPut(args[0], args[1], args[2])
	},
}

var ftpMkdirCmd = &cobra.Command{
	Use:               "mkdir <site> <remotePath>",
	Short:             "Create a remote directory via FTP",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: ftpSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectFTPClient(args[0])
		defer client.Close()
		if err := client.MakeDir(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created %s:%s\n", args[0], args[1])
	},
}

var ftpRmCmd = &cobra.Command{
	Use:               "rm <site> <remotePath>",
	Short:             "Delete a remote file via FTP",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: ftpSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectFTPClient(args[0])
		defer client.Close()
		if err := client.Delete(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted %s:%s\n", args[0], args[1])
	},
}

var ftpRmdirCmd = &cobra.Command{
	Use:               "rmdir <site> <remotePath>",
	Short:             "Delete a remote directory recursively via FTP",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: ftpSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectFTPClient(args[0])
		defer client.Close()
		if err := client.RemoveDirRecur(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Removed %s:%s\n", args[0], args[1])
	},
}

var ftpRenameCmd = &cobra.Command{
	Use:               "rename <site> <oldPath> <newPath>",
	Short:             "Rename / move a remote file via FTP",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: ftpSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectFTPClient(args[0])
		defer client.Close()
		if err := client.Rename(args[1], args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Renamed %s:%s -> %s\n", args[0], args[1], args[2])
	},
}

func ftpSiteCompletionFirstArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return completeFTPNames(toComplete), cobra.ShellCompDirectiveNoFileComp
}

func runFTPGet(siteName, remotePath, localPath string) {
	client, _ := connectFTPClient(siteName)
	defer client.Close()

	// Mirror SFTP get path handling: if remote is file and local ends with slash, join basename.
	isDir, err := client.IsRemoteDir(remotePath)
	if err == nil && isDir {
		localPath = strings.TrimRight(localPath, string(os.PathSeparator))
	} else if strings.HasSuffix(localPath, "/") || strings.HasSuffix(localPath, string(os.PathSeparator)) {
		localPath = filepath.Join(localPath, path.Base(remotePath))
	}

	label := fmt.Sprintf("get %s:%s → %s", siteName, remotePath, localPath)
	progress := ftpProgressPrinter(label)
	if err := client.DownloadPath(context.Background(), remotePath, localPath, progress); err != nil {
		ftpFinishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	ftpFinishProgress()
	fmt.Printf("Downloaded %s:%s to %s\n", siteName, remotePath, localPath)
}

func runFTPPut(siteName, localPath, remotePath string) {
	client, _ := connectFTPClient(siteName)
	defer client.Close()

	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if info.IsDir() {
		remotePath = strings.TrimRight(remotePath, "/")
	} else if strings.HasSuffix(remotePath, "/") {
		remotePath = path.Join(remotePath, filepath.Base(localPath))
	}

	label := fmt.Sprintf("put %s → %s:%s", localPath, siteName, remotePath)
	progress := ftpProgressPrinter(label)
	if err := client.UploadPath(context.Background(), localPath, remotePath, progress); err != nil {
		ftpFinishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	ftpFinishProgress()
	fmt.Printf("Uploaded %s to %s:%s\n", localPath, siteName, remotePath)
}

func init() {
	RootCmd.AddCommand(ftpCmd)
	ftpCmd.Flags().StringVar(&ftpFormat, "format", "", "Output format: json (for list/search/info)")
	ftpLsCmd.Flags().StringVar(&ftpLsFormat, "format", "", "Output format: json (for ls)")
	ftpCmd.AddCommand(ftpLsCmd)
	ftpCmd.AddCommand(ftpGetCmd)
	ftpCmd.AddCommand(ftpPutCmd)
	ftpCmd.AddCommand(ftpMkdirCmd)
	ftpCmd.AddCommand(ftpRmCmd)
	ftpCmd.AddCommand(ftpRmdirCmd)
	ftpCmd.AddCommand(ftpRenameCmd)
}
