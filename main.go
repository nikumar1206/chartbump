package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nikhil/chartbump/internal/chart"
	"github.com/nikhil/chartbump/internal/registry"
)

const usage = `chartbump - check for newer versions of Helm chart dependencies

Usage:
  chartbump [flags]

Flags:
  -charts string    path to charts directory (default "./charts")
  -only string      comma-separated chart folder names to process (default: all)
  -pre              include pre-release versions when finding latest
  -changelog        fetch GitHub release notes for each outdated dependency
  -token string     GitHub token (falls back to GITHUB_TOKEN env, then: gh auth token)
  -verbose          show dependencies that are already up to date
  -wet-run          write updated versions to Chart.yaml and run helm dependency update
  -h                show this help

Output:
  Lists each dependency that has a newer version available, grouped by chart.
  Release dates are shown when available (HTTP repos only; OCI dates are unknown).
  With -changelog, output is piped through bat (or less) when stdout is a terminal.

Examples:
  chartbump
  chartbump -charts /path/to/charts
  chartbump -only myapp,infra
  chartbump -wet-run
  chartbump -wet-run -only myapp
  chartbump -changelog
  chartbump -pre -verbose
`

type result struct {
	chart     string
	dep       string
	current   registry.VersionInfo
	latest    registry.VersionInfo
	changelog []string
	repoURL   string
	err       error
}

func main() {
	chartsDir := flag.String("charts", "./charts", "path to charts directory")
	only := flag.String("only", "", "comma-separated chart folder names to process (default: all)")
	includePre := flag.Bool("pre", false, "include pre-release versions")
	showChangelog := flag.Bool("changelog", false, "fetch GitHub release notes for outdated deps")
	tokenFlag := flag.String("token", "", "GitHub token (overrides GITHUB_TOKEN env and gh auth token)")
	verbose := flag.Bool("verbose", false, "show up-to-date dependencies too")
	bump := flag.Bool("wet-run", false, "write updated versions to Chart.yaml and run helm dependency update")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if flag.NArg() == 1 {
		*chartsDir = flag.Arg(0)
	}

	stableOnly := !*includePre
	token := registry.Token(*tokenFlag)

	charts, err := chart.LoadAll(*chartsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(charts) == 0 {
		fmt.Fprintf(os.Stderr, "no charts found in %s\n", *chartsDir)
		os.Exit(1)
	}

	if *only != "" {
		nameSet := map[string]bool{}
		for n := range strings.SplitSeq(*only, ",") {
			nameSet[strings.TrimSpace(n)] = true
		}
		var filtered []chart.Chart
		for _, c := range charts {
			if nameSet[c.Dir] {
				filtered = append(filtered, c)
			}
		}
		charts = filtered
		if len(charts) == 0 {
			fmt.Fprintf(os.Stderr, "no charts matched -only filter\n")
			os.Exit(1)
		}
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []result
	)

	for _, c := range charts {
		for _, dep := range c.Dependencies {
			wg.Add(1)
			go func(chartName string, dep chart.Dependency) {
				defer wg.Done()

				r := result{
					chart:   chartName,
					dep:     dep.Name,
					repoURL: dep.Repository,
					current: registry.VersionInfo{Version: dep.Version},
				}

				if strings.HasPrefix(dep.Repository, "oci://") {
					ociRef := strings.TrimRight(dep.Repository, "/") + "/" + dep.Name
					r.latest, r.err = registry.CheckOCI(ociRef)
				} else {
					r.current, r.latest, r.err = registry.CheckHTTP(dep.Repository, dep.Name, dep.Version, stableOnly)
				}

				if r.err == nil && r.current.Version != r.latest.Version && *showChangelog && !strings.HasPrefix(dep.Repository, "oci://") {
					r.changelog, _ = registry.GitHubChangelog(dep.Repository, dep.Name, r.current.Version, r.latest.Version, token)
				}

				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}(c.Dir, dep)
		}
	}
	wg.Wait()

	byChart := make(map[string][]result)
	var order []string
	seen := map[string]bool{}
	for _, r := range results {
		if !seen[r.chart] {
			order = append(order, r.chart)
			seen[r.chart] = true
		}
		byChart[r.chart] = append(byChart[r.chart], r)
	}
	sort.Strings(order)
	for k := range byChart {
		sort.Slice(byChart[k], func(i, j int) bool {
			return byChart[k][i].dep < byChart[k][j].dep
		})
	}

	var out strings.Builder
	anyOutput := false

	for _, chartName := range order {
		deps := byChart[chartName]
		var rows []string
		for _, r := range deps {
			if r.err != nil {
				rows = append(rows, fmt.Sprintf("  %-40s %-28s  ERROR: %v", r.dep, r.current.String(), r.err))
				continue
			}
			if r.current.Version == r.latest.Version {
				if *verbose {
					rows = append(rows, fmt.Sprintf("  %-40s %-28s  (up to date)", r.dep, r.current.String()))
				}
				continue
			}
			rows = append(rows, fmt.Sprintf("  %-40s %-28s  ->  %s", r.dep, r.current.String(), r.latest.String()))
			for _, line := range r.changelog {
				rows = append(rows, "    "+line)
			}
		}
		if len(rows) > 0 {
			anyOutput = true
			fmt.Fprintf(&out, "%s\n", chartName)
			fmt.Fprintln(&out, strings.Repeat("-", 80))
			for _, row := range rows {
				fmt.Fprintln(&out, row)
			}
			fmt.Fprintln(&out)
		}
	}

	if !anyOutput {
		fmt.Println("all dependencies are up to date")
		return
	}

	if *bump {
		chartUpdates := make(map[string]map[string]string)
		for _, r := range results {
			if r.err != nil || r.current.Version == r.latest.Version {
				continue
			}
			if chartUpdates[r.chart] == nil {
				chartUpdates[r.chart] = make(map[string]string)
			}
			chartUpdates[r.chart][r.dep] = r.latest.Version
		}
		for _, chartName := range order {
			updates, ok := chartUpdates[chartName]
			if !ok {
				continue
			}
			fmt.Printf("bumping %s\n", chartName)
			if err := chart.UpdateVersions(*chartsDir, chartName, updates); err != nil {
				fmt.Fprintf(os.Stderr, "  error updating Chart.yaml: %v\n", err)
				continue
			}
			chartPath := filepath.Join(*chartsDir, chartName)
			cmd := exec.Command("helm", "dependency", "update", chartPath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "  helm dependency update failed: %v\n", err)
			}
		}
		return
	}

	if *showChangelog && isTerminal() {
		if err := pipeToPager(out.String()); err == nil {
			return
		}
	}
	fmt.Print(out.String())
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func pipeToPager(content string) error {
	pager, args := findPager()
	if pager == "" {
		return fmt.Errorf("no pager found")
	}
	cmd := exec.Command(pager, args...)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func findPager() (string, []string) {
	if p, err := exec.LookPath("bat"); err == nil {
		return p, []string{"--style=plain", "--paging=auto", "--color=never"}
	}
	if p, err := exec.LookPath("less"); err == nil {
		return p, []string{"-FX"}
	}
	return "", nil
}
