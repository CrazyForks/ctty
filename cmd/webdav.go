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
	"github.com/zsuroy/ctty/internal/ui"
	"github.com/zsuroy/ctty/internal/webdavclient"
	"github.com/zsuroy/ctty/internal/webdavconfig"
)

var webdavFormat string

// webdavCmd opens the WebDAV site manager / dual-pane browser, or queries saved sites.
//
// Forms:
//
//	ctty webdav                         → site manager TUI (humans)
//	ctty webdav list|search|info …      → non-interactive JSON/human query (agents)
//	ctty webdav ls <site> [path]        → list remote directory (headless)
//	ctty webdav get <site> <remote> <local> → download
//	ctty webdav put <site> <local> <remote> → upload
//	ctty webdav <name>                  → dual-pane WebDAV browser for a saved site
//
// Agents must use list|search|info --format json and never open the TUI.
// WebDAV passwords live in the encrypted vault (credentials.json, webdav: prefix).
var webdavCmd = &cobra.Command{
	Use:   "webdav [list|search|info|ls|get|put|mkdir|rm|rmdir|rename|name]",
	Short: "Open WebDAV site manager / browser, or query saved sites",
	Long: `Open the WebDAV site manager TUI, query saved sites, or browse/transfer via WebDAV.

Site inventory: ~/.config/ctty/webdav.json
Passwords (encrypted vault): ~/.config/ctty/credentials.json under webdav: names.

Forms:
  ctty webdav                          List and manage saved WebDAV sites (TUI)
  ctty webdav list [--format json]     List saved sites
  ctty webdav search [query] [--format json]
  ctty webdav info <name> [--format json]
  ctty webdav ls <site> [remotePath] [--format json]  List remote directory
  ctty webdav get <site> <remote> <local>             Download file or directory
  ctty webdav put <site> <local> <remote>             Upload file or directory
  ctty webdav mkdir <site> <remotePath>               Create remote directory
  ctty webdav rm <site> <remotePath>                  Delete remote file
  ctty webdav rmdir <site> <remotePath>               Delete remote directory (recursive)
  ctty webdav rename <site> <old> <new>               Rename / move remote file
  ctty webdav nextcloud                               Open dual-pane local|remote browser for a site

Agents: use list|search|info|ls --format json only; never open the WebDAV TUI.`,
	Args: cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			if args[0] == "info" && len(args) == 1 {
				return completeWebDAVNames(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			if (args[0] == "ls" || args[0] == "get" || args[0] == "put" || args[0] == "mkdir" || args[0] == "rm" || args[0] == "rmdir" || args[0] == "rename") && len(args) == 1 {
				return completeWebDAVNames(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		base := []string{"list", "search", "info", "ls", "get", "put", "mkdir", "rm", "rmdir", "rename"}
		names := completeWebDAVNames(toComplete)
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
			if err := ui.RunWebDAVMode(AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running WebDAV mode: %v", err)
			}
			fmt.Println()
			return
		}

		switch args[0] {
		case "list":
			runWebDAVList()
			return
		case "search":
			q := ""
			if len(args) > 1 {
				q = strings.Join(args[1:], " ")
			}
			runWebDAVSearch(q)
			return
		case "info":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: webdav info requires a site name\n")
				os.Exit(1)
			}
			runWebDAVInfo(args[1])
			return
		}

		name := args[0]
		if _, ok := webdavconfig.Find(name); !ok {
			fmt.Fprintf(os.Stderr, "Error: webdav site %q not found\n", name)
			os.Exit(2)
		}
		if err := ui.RunWebDAVBrowserMode(name, AppVersion, noUpdateCheck); err != nil {
			log.Fatalf("Error running WebDAV browser: %v", err)
		}
		fmt.Println()
	},
}

func completeWebDAVNames(toComplete string) []string {
	sites, err := webdavconfig.Load()
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

func runWebDAVList() {
	sites, err := webdavconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	outputWebDAVSites(sites)
}

func runWebDAVSearch(query string) {
	sites, err := webdavconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		outputWebDAVSites(sites)
		return
	}
	words := strings.Fields(strings.ToLower(query))
	var matched []webdavconfig.WebDAVSite
	for _, s := range sites {
		hay := strings.ToLower(s.Name + " " + s.URL + " " + s.User + " " + strings.Join(s.Tags, " "))
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
	outputWebDAVSites(matched)
}

func runWebDAVInfo(name string) {
	site, ok := webdavconfig.Find(name)
	if !ok {
		if webdavFormat == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"ok": false, "error": "NOT_FOUND", "name": name,
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: webdav site %q not found\n", name)
		}
		os.Exit(2)
	}
	if webdavFormat == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(site)
		return
	}
	fmt.Printf("Name: %s\n", site.Name)
	fmt.Printf("URL: %s\n", site.URL)
	if site.User != "" {
		fmt.Printf("User: %s\n", site.User)
	}
	if site.InsecureTLS {
		fmt.Println("InsecureTLS: true")
	}
	if len(site.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(site.Tags, ", "))
	}
}

func outputWebDAVSites(sites []webdavconfig.WebDAVSite) {
	if webdavFormat == "json" {
		if sites == nil {
			sites = []webdavconfig.WebDAVSite{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sites)
		return
	}
	if len(sites) == 0 {
		fmt.Println("No WebDAV sites found.")
		return
	}
	for _, s := range sites {
		tags := ""
		if len(s.Tags) > 0 {
			tags = " [" + strings.Join(s.Tags, ", ") + "]"
		}
		user := s.User
		if user == "" {
			user = "-"
		}
		fmt.Printf("%-20s %-12s %s%s\n", s.Name, user, s.URL, tags)
	}
}

// --- headless WebDAV operations ---

func connectWebDAVClient(siteName string) (*webdavclient.Client, webdavconfig.WebDAVSite) {
	site, ok := webdavconfig.Find(siteName)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: webdav site %q not found\n", siteName)
		os.Exit(2)
	}
	client, err := webdavclient.Connect(site, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return client, site
}

func webdavProgressPrinter(label string) func(transferred, total int64) {
	return func(transferred, total int64) {
		if total <= 0 {
			fmt.Fprintf(os.Stderr, "\r%s %d bytes", label, transferred)
			return
		}
		pct := float64(transferred) * 100 / float64(total)
		fmt.Fprintf(os.Stderr, "\r%s %d/%d (%.0f%%)", label, transferred, total, pct)
	}
}

func webdavFinishProgress() {
	fmt.Fprintln(os.Stderr)
}

// Subcommands

var webdavLsFormat string

var webdavLsCmd = &cobra.Command{
	Use:   "ls <site> [remotePath]",
	Short: "List remote directory via WebDAV",
	Long: `List a remote directory on a WebDAV site.

Example:
  ctty webdav ls nextcloud
  ctty webdav ls nextcloud /remote.php/webdav/Documents --format json`,
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: webdavSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		siteName := args[0]
		remotePath := "/"
		if len(args) > 1 {
			remotePath = args[1]
		}
		client, _ := connectWebDAVClient(siteName)
		defer client.Close()
		entries, err := client.List(remotePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if webdavLsFormat == "json" {
			if entries == nil {
				entries = []webdavclient.RemoteEntry{}
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

var webdavGetCmd = &cobra.Command{
	Use:   "get <site> <remotePath> <localPath>",
	Short: "Download a remote file or directory via WebDAV",
	Long: `Download a remote file or directory via WebDAV.

Progress is written to stderr. Directories are transferred recursively.

Example:
  ctty webdav get nextcloud /Documents/file.txt ./file.txt
  ctty webdav get nextcloud /Documents/mydir ./mydir`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: webdavSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runWebDAVGet(args[0], args[1], args[2])
	},
}

var webdavPutCmd = &cobra.Command{
	Use:   "put <site> <localPath> <remotePath>",
	Short: "Upload a local file or directory via WebDAV",
	Long: `Upload a local file or directory via WebDAV.

Progress is written to stderr. Directories are transferred recursively.

Example:
  ctty webdav put nextcloud ./file.txt /Documents/file.txt
  ctty webdav put nextcloud ./mydir /Documents/mydir`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: webdavSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runWebDAVPut(args[0], args[1], args[2])
	},
}

var webdavMkdirCmd = &cobra.Command{
	Use:               "mkdir <site> <remotePath>",
	Short:             "Create a remote directory via WebDAV",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: webdavSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectWebDAVClient(args[0])
		defer client.Close()
		if err := client.Mkdir(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created %s:%s\n", args[0], args[1])
	},
}

var webdavRmCmd = &cobra.Command{
	Use:               "rm <site> <remotePath>",
	Short:             "Delete a remote file via WebDAV",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: webdavSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectWebDAVClient(args[0])
		defer client.Close()
		if err := client.Delete(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted %s:%s\n", args[0], args[1])
	},
}

var webdavRmdirCmd = &cobra.Command{
	Use:               "rmdir <site> <remotePath>",
	Short:             "Delete a remote directory recursively via WebDAV",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: webdavSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectWebDAVClient(args[0])
		defer client.Close()
		if err := client.Delete(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Removed %s:%s\n", args[0], args[1])
	},
}

var webdavRenameCmd = &cobra.Command{
	Use:               "rename <site> <oldPath> <newPath>",
	Short:             "Rename / move a remote file via WebDAV",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: webdavSiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectWebDAVClient(args[0])
		defer client.Close()
		if err := client.Rename(args[1], args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Renamed %s:%s -> %s\n", args[0], args[1], args[2])
	},
}

func webdavSiteCompletionFirstArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return completeWebDAVNames(toComplete), cobra.ShellCompDirectiveNoFileComp
}

func runWebDAVGet(siteName, remotePath, localPath string) {
	client, _ := connectWebDAVClient(siteName)
	defer client.Close()

	isDir, err := client.IsRemoteDir(remotePath)
	if err == nil && isDir {
		localPath = strings.TrimRight(localPath, string(os.PathSeparator))
	} else if strings.HasSuffix(localPath, "/") || strings.HasSuffix(localPath, string(os.PathSeparator)) {
		localPath = filepath.Join(localPath, path.Base(remotePath))
	}

	label := fmt.Sprintf("get %s:%s → %s", siteName, remotePath, localPath)
	progress := webdavProgressPrinter(label)
	if err := client.DownloadPath(context.Background(), remotePath, localPath, progress); err != nil {
		webdavFinishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	webdavFinishProgress()
	fmt.Printf("Downloaded %s:%s to %s\n", siteName, remotePath, localPath)
}

func runWebDAVPut(siteName, localPath, remotePath string) {
	client, _ := connectWebDAVClient(siteName)
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
	progress := webdavProgressPrinter(label)
	if err := client.UploadPath(context.Background(), localPath, remotePath, progress); err != nil {
		webdavFinishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	webdavFinishProgress()
	fmt.Printf("Uploaded %s to %s:%s\n", localPath, siteName, remotePath)
}

func init() {
	RootCmd.AddCommand(webdavCmd)
	webdavCmd.Flags().StringVar(&webdavFormat, "format", "", "Output format: json (for list/search/info)")
	webdavLsCmd.Flags().StringVar(&webdavLsFormat, "format", "", "Output format: json (for ls)")
	webdavCmd.AddCommand(webdavLsCmd)
	webdavCmd.AddCommand(webdavGetCmd)
	webdavCmd.AddCommand(webdavPutCmd)
	webdavCmd.AddCommand(webdavMkdirCmd)
	webdavCmd.AddCommand(webdavRmCmd)
	webdavCmd.AddCommand(webdavRmdirCmd)
	webdavCmd.AddCommand(webdavRenameCmd)
}
