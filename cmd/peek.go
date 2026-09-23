package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/peek"
)

var (
	peekAll         bool
	peekTags        string
	peekFormat      string
	peekJSON        bool
	peekTimeout     time.Duration
	peekConcurrency int
)

type peekHostResult struct {
	Host  string          `json:"host"`
	OK    bool            `json:"ok"`
	Stats *peek.HostStats `json:"stats,omitempty"`
	Error string          `json:"error,omitempty"`
}

var peekCmd = &cobra.Command{
	Use:   "peek [hosts...]",
	Short: "Inspect real-time system metrics of SSH hosts",
	Long: `Inspect real-time system metrics (uptime, CPU load, memory %, disk % usage)
for one or more SSH hosts in non-interactive CLI mode.

Selection:
  ctty peek <host>              Inspect a single host by alias
  ctty peek host1 host2         Inspect multiple hosts
  ctty peek --tags prod,web     Inspect hosts matching ANY of the tags
  ctty peek --all               Inspect all visible hosts in SSH configuration

Output:
  Human-readable card view by default.
  Use --format json or --json for machine-readable JSON array.

Exit code:
  0 if all inspected hosts succeeded, 1 if any host failed or was not found.

Examples:
  ctty peek web1
  ctty peek web1 web2 --json
  ctty peek --tags prod --concurrency 4
  ctty peek --all --timeout 5s`,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return RootCmd.ValidArgsFunction(cmd, args, toComplete)
	},
	Run: runPeek,
}

func runPeek(cmd *cobra.Command, args []string) {
	tagList := parseTagsCSV(peekTags)
	hostNames := args

	if len(hostNames) == 0 && len(tagList) == 0 && !peekAll {
		fmt.Fprintf(os.Stderr, "Error: specify host name(s), --tags, or --all\n")
		os.Exit(1)
	}

	hosts, err := loadSSHHosts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading SSH config: %v\n", err)
		os.Exit(1)
	}

	isJSON := peekJSON || strings.ToLower(peekFormat) == "json"

	// Map hosts by name for lookup
	hostMap := make(map[string]config.SSHHost, len(hosts))
	for _, h := range hosts {
		hostMap[h.Name] = h
	}

	var selectedHosts []config.SSHHost
	var missingHostNames []string
	seen := make(map[string]bool)

	visibleHosts := config.FilterVisibleHosts(hosts)

	if peekAll {
		for _, h := range visibleHosts {
			if !seen[h.Name] {
				seen[h.Name] = true
				selectedHosts = append(selectedHosts, h)
			}
		}
	}

	if len(tagList) > 0 {
		for _, h := range visibleHosts {
			if config.HostHasAnyTag(h.Tags, tagList) && !seen[h.Name] {
				seen[h.Name] = true
				selectedHosts = append(selectedHosts, h)
			}
		}
	}

	for _, name := range hostNames {
		if seen[name] {
			continue
		}
		if h, ok := hostMap[name]; ok {
			seen[name] = true
			selectedHosts = append(selectedHosts, h)
		} else {
			missingHostNames = append(missingHostNames, name)
		}
	}

	if len(selectedHosts) == 0 && len(missingHostNames) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no hosts matched the selection\n")
		os.Exit(1)
	}

	if peekConcurrency < 1 {
		peekConcurrency = 8
	}
	if peekTimeout <= 0 {
		peekTimeout = 7 * time.Second
	}

	results := make([]peekHostResult, 0, len(selectedHosts)+len(missingHostNames))

	// Pre-fill missing hosts as errors
	for _, name := range missingHostNames {
		results = append(results, peekHostResult{
			Host:  name,
			OK:    false,
			Error: "host not found in SSH configuration",
		})
	}

	// Probe matched hosts concurrently
	if len(selectedHosts) > 0 {
		probedResults := make([]peekHostResult, len(selectedHosts))
		sem := make(chan struct{}, peekConcurrency)
		var wg sync.WaitGroup

		for i, h := range selectedHosts {
			wg.Add(1)
			go func(idx int, host config.SSHHost) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				stats, raw, err := peek.FetchHostStats(context.Background(), host, configFile, peekTimeout)
				if err != nil {
					probedResults[idx] = peekHostResult{
						Host:  host.Name,
						OK:    false,
						Error: peek.FormatError(err, raw),
					}
				} else {
					probedResults[idx] = peekHostResult{
						Host:  host.Name,
						OK:    true,
						Stats: stats,
					}
				}
			}(i, h)
		}
		wg.Wait()
		results = append(results, probedResults...)
	}

	allOK := true
	for _, r := range results {
		if !r.OK {
			allOK = false
			break
		}
	}

	if isJSON {
		b, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
	} else {
		for i, r := range results {
			if i > 0 {
				fmt.Println()
			}
			if !r.OK {
				fmt.Printf("=== %s: FAIL ===\n", r.Host)
				fmt.Printf("  Error:      %s\n", r.Error)
				continue
			}

			fmt.Printf("=== %s: OK ===\n", r.Host)
			if r.Stats != nil {
				stats := r.Stats
				if stats.Uptime != "" {
					usersPart := ""
					if stats.Users != "" {
						usersPart = fmt.Sprintf(" (%s users)", stats.Users)
					}
					fmt.Printf("  Uptime:     up %s%s\n", stats.Uptime, usersPart)
				}
				if stats.Load1 != "" {
					fmt.Printf("  Load:       %s, %s, %s\n", stats.Load1, stats.Load5, stats.Load15)
				}
				if stats.MemTotalMB > 0 {
					bar := peek.RenderProgressBar(stats.MemPercent, 14)
					fmt.Printf("  Memory:     %s (%dMB / %dMB)\n", bar, stats.MemUsedMB, stats.MemTotalMB)
				}
				if stats.DiskTotal != "" {
					bar := peek.RenderProgressBar(stats.DiskPercent, 14)
					fmt.Printf("  Disk (/):   %s (%s / %s)\n", bar, stats.DiskUsed, stats.DiskTotal)
				}
				if stats.Uptime == "" && stats.MemTotalMB == 0 && stats.DiskTotal == "" && stats.RawOutput != "" {
					rawLines := strings.Split(stats.RawOutput, "\n")
					for _, rl := range rawLines {
						if t := strings.TrimSpace(rl); t != "" && t != "---" {
							fmt.Printf("  %s\n", t)
						}
					}
				}
			}
		}
	}

	if !allOK {
		os.Exit(1)
	}
}

func init() {
	RootCmd.AddCommand(peekCmd)
	peekCmd.Flags().BoolVar(&peekAll, "all", false, "Inspect all visible SSH hosts")
	peekCmd.Flags().StringVar(&peekTags, "tags", "", "Comma-separated tags; hosts matching ANY tag are selected")
	peekCmd.Flags().StringVar(&peekFormat, "format", "", "Output format: table (default) or json")
	peekCmd.Flags().BoolVar(&peekJSON, "json", false, "Output in JSON format (shorthand for --format json)")
	peekCmd.Flags().DurationVar(&peekTimeout, "timeout", 7*time.Second, "Timeout for probing each host")
	peekCmd.Flags().IntVar(&peekConcurrency, "concurrency", 8, "Max parallel SSH probes")
}
