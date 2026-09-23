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
	"github.com/zsuroy/ctty/internal/connectivity"
)

var (
	pingAll         bool
	pingTags        string
	pingFormat      string
	pingJSON        bool
	pingTimeout     time.Duration
	pingConcurrency int
)

type pingHostResult struct {
	Host      string  `json:"host"`
	OK        bool    `json:"ok"`
	Status    string  `json:"status"`
	LatencyMS float64 `json:"latency_ms"`
	Error     string  `json:"error,omitempty"`
}

var pingCmd = &cobra.Command{
	Use:   "ping [hosts...]",
	Short: "Check connectivity and latency of SSH hosts",
	Long: `Check network connectivity and SSH port reachability for one or more hosts.

Selection:
  ctty ping <host>              Ping a single host by alias
  ctty ping host1 host2         Ping multiple hosts
  ctty ping --tags prod,web     Ping hosts matching ANY of the tags
  ctty ping --all               Ping all visible hosts in SSH configuration

Output:
  Aligned status table by default.
  Use --format json or --json for machine-readable JSON array.

Exit code:
  0 if all pinged hosts are online, 1 if any host is offline or not found.

Examples:
  ctty ping web1
  ctty ping web1 web2 --json
  ctty ping --tags prod --concurrency 10
  ctty ping --all --timeout 3s`,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return RootCmd.ValidArgsFunction(cmd, args, toComplete)
	},
	Run: runPing,
}

func runPing(cmd *cobra.Command, args []string) {
	tagList := parseTagsCSV(pingTags)
	hostNames := args

	if len(hostNames) == 0 && len(tagList) == 0 && !pingAll {
		fmt.Fprintf(os.Stderr, "Error: specify host name(s), --tags, or --all\n")
		os.Exit(1)
	}

	hosts, err := loadSSHHosts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading SSH config: %v\n", err)
		os.Exit(1)
	}

	isJSON := pingJSON || strings.ToLower(pingFormat) == "json"

	hostMap := make(map[string]config.SSHHost, len(hosts))
	for _, h := range hosts {
		hostMap[h.Name] = h
	}

	var selectedHosts []config.SSHHost
	var missingHostNames []string
	seen := make(map[string]bool)

	visibleHosts := config.FilterVisibleHosts(hosts)

	if pingAll {
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

	if pingConcurrency < 1 {
		pingConcurrency = 16
	}
	if pingTimeout <= 0 {
		pingTimeout = 5 * time.Second
	}

	pm := connectivity.NewPingManager(pingTimeout, configFile)
	results := make([]pingHostResult, 0, len(selectedHosts)+len(missingHostNames))

	// Pre-fill missing hosts as errors
	for _, name := range missingHostNames {
		results = append(results, pingHostResult{
			Host:   name,
			OK:     false,
			Status: "offline",
			Error:  "host not found in SSH configuration",
		})
	}

	if len(selectedHosts) > 0 {
		pingedResults := make([]pingHostResult, len(selectedHosts))
		sem := make(chan struct{}, pingConcurrency)
		var wg sync.WaitGroup

		for i, h := range selectedHosts {
			wg.Add(1)
			go func(idx int, host config.SSHHost) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				res := pm.PingHost(context.Background(), host)
				isOnline := res.Status == connectivity.StatusOnline
				latencyMS := 0.0
				if isOnline {
					latencyMS = float64(res.Duration.Microseconds()) / 1000.0
				}
				errStr := ""
				if res.Error != nil {
					errStr = res.Error.Error()
				}
				pingedResults[idx] = pingHostResult{
					Host:      host.Name,
					OK:        isOnline,
					Status:    res.Status.String(),
					LatencyMS: latencyMS,
					Error:     errStr,
				}
			}(i, h)
		}
		wg.Wait()
		results = append(results, pingedResults...)
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
		// Calculate column widths
		hostWidth := 4
		for _, r := range results {
			if len(r.Host) > hostWidth {
				hostWidth = len(r.Host)
			}
		}

		fmt.Printf("%-*s  %-7s  %-10s  %s\n", hostWidth, "HOST", "STATUS", "LATENCY", "DETAILS")
		for _, r := range results {
			statusStr := strings.ToUpper(r.Status)
			latencyStr := "-"
			if r.OK {
				latencyStr = fmt.Sprintf("%.1fms", r.LatencyMS)
			}
			detailStr := "-"
			if r.Error != "" {
				detailStr = r.Error
			}
			fmt.Printf("%-*s  %-7s  %-10s  %s\n", hostWidth, r.Host, statusStr, latencyStr, detailStr)
		}
	}

	if !allOK {
		os.Exit(1)
	}
}

func init() {
	RootCmd.AddCommand(pingCmd)
	pingCmd.Flags().BoolVar(&pingAll, "all", false, "Ping all visible SSH hosts")
	pingCmd.Flags().StringVar(&pingTags, "tags", "", "Comma-separated tags; hosts matching ANY tag are selected")
	pingCmd.Flags().StringVar(&pingFormat, "format", "", "Output format: table (default) or json")
	pingCmd.Flags().BoolVar(&pingJSON, "json", false, "Output in JSON format (shorthand for --format json)")
	pingCmd.Flags().DurationVar(&pingTimeout, "timeout", 5*time.Second, "Timeout for pinging each host")
	pingCmd.Flags().IntVar(&pingConcurrency, "concurrency", 16, "Max parallel ping connections")
}
