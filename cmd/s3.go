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
	"github.com/zsuroy/ctty/internal/s3client"
	"github.com/zsuroy/ctty/internal/s3config"
	"github.com/zsuroy/ctty/internal/ui"
)

var (
	s3Format      string
	s3LsFormat    string
	s3RmRecursive bool
)

// s3Cmd opens the S3 site manager / dual-pane browser, or queries saved sites.
//
// Forms:
//
//	ctty s3                         → site manager TUI (humans)
//	ctty s3 list|search|info …      → non-interactive JSON/human query (agents)
//	ctty s3 ls <site> [path]        → list remote bucket/directory (headless)
//	ctty s3 get <site> <remote> <local> → download file or directory
//	ctty s3 put <site> <local> <remote> → upload file or directory
//	ctty s3 rm <site> <remote> [-r]     → delete object or prefix
//	ctty s3 mkdir <site> <remote>       → create directory marker or bucket
//	ctty s3 <name>                  → dual-pane S3 browser for a saved site
//
// Agents must use list|search|info|ls --format json and never open the TUI.
// S3 secret access keys live in the encrypted vault (credentials.json, s3: prefix).
var s3Cmd = &cobra.Command{
	Use:   "s3 [list|search|info|ls|get|put|mkdir|rm|rename|name]",
	Short: "Open S3 site manager / browser, or query saved S3 sites",
	Long: `Open the S3 site manager TUI, query saved sites, or browse/transfer via S3-compatible storage.

Site inventory: ~/.config/ctty/s3.json
Secret keys (encrypted vault): ~/.config/ctty/credentials.json under s3: names.

Forms:
  ctty s3                                List and manage saved S3 sites (TUI)
  ctty s3 list [--format json]           List saved sites
  ctty s3 search [query] [--format json] Search saved sites
  ctty s3 info <name> [--format json]    Show site details
  ctty s3 ls <site> [remotePath] [--format json]  List buckets or remote directory
  ctty s3 get <site> <remote> <local>    Download file or directory
  ctty s3 put <site> <local> <remote>    Upload file or directory
  ctty s3 mkdir <site> <remotePath>      Create directory marker or bucket
  ctty s3 rm <site> <remotePath> [-r]    Delete remote object or directory
  ctty s3 rename <site> <old> <new>      Rename / move remote object or directory
  ctty s3 <name>                         Open dual-pane local|remote browser for a site

Agents: use list|search|info|ls --format json only; never open the S3 TUI.`,
	Args: cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			if args[0] == "info" && len(args) == 1 {
				return completeS3Names(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			if (args[0] == "ls" || args[0] == "get" || args[0] == "put" || args[0] == "mkdir" || args[0] == "rm" || args[0] == "rename") && len(args) == 1 {
				return completeS3Names(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		base := []string{"list", "search", "info", "ls", "get", "put", "mkdir", "rm", "rename"}
		names := completeS3Names(toComplete)
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
			if err := ui.RunS3Mode(AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running S3 mode: %v", err)
			}
			fmt.Println()
			return
		}

		switch args[0] {
		case "list":
			runS3List()
			return
		case "search":
			q := ""
			if len(args) > 1 {
				q = strings.Join(args[1:], " ")
			}
			runS3Search(q)
			return
		case "info":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: s3 info requires a site name\n")
				os.Exit(1)
			}
			runS3Info(args[1])
			return
		}

		name := args[0]
		if _, ok := s3config.Find(name); !ok {
			fmt.Fprintf(os.Stderr, "Error: s3 site %q not found\n", name)
			os.Exit(2)
		}
		if err := ui.RunS3BrowserMode(name, AppVersion, noUpdateCheck); err != nil {
			log.Fatalf("Error running S3 browser: %v", err)
		}
		fmt.Println()
	},
}

func completeS3Names(toComplete string) []string {
	sites, err := s3config.Load()
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

func runS3List() {
	sites, err := s3config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	outputS3Sites(sites)
}

func runS3Search(query string) {
	sites, err := s3config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		outputS3Sites(sites)
		return
	}
	words := strings.Fields(strings.ToLower(query))
	var matched []s3config.S3Site
	for _, s := range sites {
		hay := strings.ToLower(s.Name + " " + s.Endpoint + " " + s.Bucket + " " + s.Region + " " + s.AccessKey + " " + strings.Join(s.Tags, " "))
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
	outputS3Sites(matched)
}

func runS3Info(name string) {
	site, ok := s3config.Find(name)
	if !ok {
		if s3Format == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"ok": false, "error": "NOT_FOUND", "name": name,
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: s3 site %q not found\n", name)
		}
		os.Exit(2)
	}
	if s3Format == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(site)
		return
	}
	fmt.Printf("Name: %s\n", site.Name)
	fmt.Printf("Endpoint: %s\n", site.Endpoint)
	if site.Bucket != "" {
		fmt.Printf("Bucket: %s\n", site.Bucket)
	}
	if site.Region != "" {
		fmt.Printf("Region: %s\n", site.Region)
	}
	if site.AccessKey != "" {
		fmt.Printf("AccessKey: %s\n", site.AccessKey)
	}
	fmt.Printf("UseSSL: %t\n", site.UseSSL)
	if site.PathStyle {
		fmt.Println("PathStyle: true")
	}
	if site.InsecureTLS {
		fmt.Println("InsecureTLS: true")
	}
	if len(site.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(site.Tags, ", "))
	}
}

func outputS3Sites(sites []s3config.S3Site) {
	if s3Format == "json" {
		if sites == nil {
			sites = []s3config.S3Site{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sites)
		return
	}
	if len(sites) == 0 {
		fmt.Println("No S3 sites found.")
		return
	}
	for _, s := range sites {
		tags := ""
		if len(s.Tags) > 0 {
			tags = " [" + strings.Join(s.Tags, ", ") + "]"
		}
		bucket := s.Bucket
		if bucket == "" {
			bucket = "-"
		}
		fmt.Printf("%-20s %-16s %s%s\n", s.Name, bucket, s.Endpoint, tags)
	}
}

// --- headless S3 operations ---

func connectS3Client(siteName string) (*s3client.Client, s3config.S3Site) {
	site, ok := s3config.Find(siteName)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: s3 site %q not found\n", siteName)
		os.Exit(2)
	}
	client, err := s3client.Connect(site, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return client, site
}

func s3ProgressPrinter(label string) func(transferred, total int64) {
	return func(transferred, total int64) {
		if total <= 0 {
			fmt.Fprintf(os.Stderr, "\r%s %d bytes", label, transferred)
			return
		}
		pct := float64(transferred) * 100 / float64(total)
		fmt.Fprintf(os.Stderr, "\r%s %d/%d (%.0f%%)", label, transferred, total, pct)
	}
}

func s3FinishProgress() {
	fmt.Fprintln(os.Stderr)
}

// Subcommands

var s3LsCmd = &cobra.Command{
	Use:   "ls <site> [remotePath]",
	Short: "List remote directory or buckets via S3",
	Long: `List buckets or a remote directory/prefix in an S3 bucket.

Example:
  ctty s3 ls minio
  ctty s3 ls minio my-bucket/documents --format json`,
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: s3SiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		siteName := args[0]
		remotePath := "/"
		if len(args) > 1 {
			remotePath = args[1]
		}
		client, _ := connectS3Client(siteName)
		defer client.Close()

		entries, err := client.List(context.Background(), remotePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if s3LsFormat == "json" {
			if entries == nil {
				entries = []s3client.RemoteEntry{}
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

var s3GetCmd = &cobra.Command{
	Use:   "get <site> <remotePath> <localPath>",
	Short: "Download a remote file or directory via S3",
	Long: `Download a remote file or prefix recursively via S3.

Progress is written to stderr. Directories are transferred recursively.

Example:
  ctty s3 get minio my-bucket/file.txt ./file.txt
  ctty s3 get minio my-bucket/photos ./photos`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: s3SiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runS3Get(args[0], args[1], args[2])
	},
}

var s3PutCmd = &cobra.Command{
	Use:   "put <site> <localPath> <remotePath>",
	Short: "Upload a local file or directory via S3",
	Long: `Upload a local file or directory recursively via S3.

Progress is written to stderr. Directories are transferred recursively.

Example:
  ctty s3 put minio ./file.txt my-bucket/file.txt
  ctty s3 put minio ./photos my-bucket/photos`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: s3SiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runS3Put(args[0], args[1], args[2])
	},
}

var s3MkdirCmd = &cobra.Command{
	Use:   "mkdir <site> <remotePath>",
	Short: "Create a remote directory prefix or bucket via S3",
	Long: `Create a directory marker prefix or a bucket in S3.

Example:
  ctty s3 mkdir minio new-bucket
  ctty s3 mkdir minio my-bucket/new-folder`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: s3SiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectS3Client(args[0])
		defer client.Close()
		if err := client.Mkdir(context.Background(), args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created %s:%s\n", args[0], args[1])
	},
}

var s3RmCmd = &cobra.Command{
	Use:   "rm <site> <remotePath>",
	Short: "Delete a remote object or prefix via S3",
	Long: `Delete a remote object or recursively delete a prefix via S3.

Example:
  ctty s3 rm minio my-bucket/file.txt
  ctty s3 rm minio my-bucket/folder -r`,
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: s3SiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectS3Client(args[0])
		defer client.Close()
		var err error
		if s3RmRecursive {
			err = client.DeletePrefix(context.Background(), args[1])
		} else {
			err = client.Delete(context.Background(), args[1])
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted %s:%s\n", args[0], args[1])
	},
}

var s3RenameCmd = &cobra.Command{
	Use:     "rename <site> <oldPath> <newPath>",
	Aliases: []string{"mv"},
	Short:   "Rename / move a remote object or prefix via S3",
	Long: `Rename or move a remote object or prefix via S3.

Example:
  ctty s3 rename minio my-bucket/file.txt my-bucket/file2.txt
  ctty s3 rename minio my-bucket/folder my-bucket/new-folder
  ctty s3 mv minio my-bucket/file.txt file2.txt`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: s3SiteCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client, _ := connectS3Client(args[0])
		defer client.Close()
		if err := client.Rename(context.Background(), args[1], args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Renamed %s:%s -> %s\n", args[0], args[1], args[2])
	},
}

func s3SiteCompletionFirstArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return completeS3Names(toComplete), cobra.ShellCompDirectiveNoFileComp
}

func runS3Get(siteName, remotePath, localPath string) {
	client, _ := connectS3Client(siteName)
	defer client.Close()

	isDir, err := client.IsRemoteDir(context.Background(), remotePath)
	if err == nil && isDir {
		localPath = strings.TrimRight(localPath, string(os.PathSeparator))
	} else if strings.HasSuffix(localPath, "/") || strings.HasSuffix(localPath, string(os.PathSeparator)) {
		localPath = filepath.Join(localPath, path.Base(remotePath))
	}

	label := fmt.Sprintf("get %s:%s → %s", siteName, remotePath, localPath)
	progress := s3ProgressPrinter(label)
	if err := client.DownloadPath(context.Background(), remotePath, localPath, progress); err != nil {
		s3FinishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	s3FinishProgress()
	fmt.Printf("Downloaded %s:%s to %s\n", siteName, remotePath, localPath)
}

func runS3Put(siteName, localPath, remotePath string) {
	client, _ := connectS3Client(siteName)
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
	progress := s3ProgressPrinter(label)
	if err := client.UploadPath(context.Background(), localPath, remotePath, progress); err != nil {
		s3FinishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	s3FinishProgress()
	fmt.Printf("Uploaded %s to %s:%s\n", localPath, siteName, remotePath)
}

func init() {
	RootCmd.AddCommand(s3Cmd)
	s3Cmd.Flags().StringVar(&s3Format, "format", "", "Output format: json (for list/search/info)")
	s3LsCmd.Flags().StringVar(&s3LsFormat, "format", "", "Output format: json (for ls)")
	s3RmCmd.Flags().BoolVarP(&s3RmRecursive, "recursive", "r", false, "Delete prefix recursively")

	s3Cmd.AddCommand(s3LsCmd)
	s3Cmd.AddCommand(s3GetCmd)
	s3Cmd.AddCommand(s3PutCmd)
	s3Cmd.AddCommand(s3MkdirCmd)
	s3Cmd.AddCommand(s3RmCmd)
	s3Cmd.AddCommand(s3RenameCmd)
}
