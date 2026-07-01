package registry

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"go.yaml.in/yaml/v4"
)

type indexEntry struct {
	Version string    `yaml:"version"`
	Created time.Time `yaml:"created"`
	Sources []string  `yaml:"sources"`
	URLs    []string  `yaml:"urls"`
}

type helmIndex struct {
	Entries map[string][]indexEntry `yaml:"entries"`
}

func fetchIndex(repoURL string) (*helmIndex, error) {
	url := strings.TrimRight(repoURL, "/") + "/index.yaml"
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("fetch index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("index returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	var idx helmIndex
	if err := yaml.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return &idx, nil
}

// CheckHTTP fetches <repoURL>/index.yaml and returns VersionInfo for both
// the pinned version and the latest available version.
func CheckHTTP(repoURL, chartName, currentVersion string, stableOnly bool) (current, latest VersionInfo, err error) {
	idx, err := fetchIndex(repoURL)
	if err != nil {
		return VersionInfo{}, VersionInfo{}, err
	}

	entries, ok := idx.Entries[chartName]
	if !ok || len(entries) == 0 {
		return VersionInfo{}, VersionInfo{}, fmt.Errorf("chart %q not found in index", chartName)
	}

	current = VersionInfo{Version: currentVersion}
	for _, e := range entries {
		if e.Version == currentVersion {
			current.Created = e.Created
			break
		}
	}

	type candidate struct {
		v       *semver.Version
		created time.Time
		raw     string
	}
	var candidates []candidate
	for _, e := range entries {
		v, err := semver.NewVersion(e.Version)
		if err != nil {
			continue
		}
		if stableOnly && v.Prerelease() != "" {
			continue
		}
		candidates = append(candidates, candidate{v: v, created: e.Created, raw: e.Version})
	}
	if len(candidates) == 0 {
		return current, VersionInfo{}, fmt.Errorf("no valid versions for %q", chartName)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].v.GreaterThan(candidates[j].v)
	})

	latest = VersionInfo{
		Version: candidates[0].raw,
		Created: candidates[0].created,
	}
	return current, latest, nil
}

// EntriesInRange returns index entries with versions in (fromExclusive, toInclusive],
// sorted ascending. Used to walk the versions between a pinned and latest release.
func EntriesInRange(repoURL, chartName, fromExclusive, toInclusive string) ([]indexEntry, error) {
	idx, err := fetchIndex(repoURL)
	if err != nil {
		return nil, err
	}

	from, err := semver.NewVersion(fromExclusive)
	if err != nil {
		return nil, fmt.Errorf("parse from version: %w", err)
	}
	to, err := semver.NewVersion(toInclusive)
	if err != nil {
		return nil, fmt.Errorf("parse to version: %w", err)
	}

	var result []indexEntry
	for _, e := range idx.Entries[chartName] {
		v, err := semver.NewVersion(e.Version)
		if err != nil {
			continue
		}
		if v.GreaterThan(from) && !v.GreaterThan(to) {
			result = append(result, e)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		vi, _ := semver.NewVersion(result[i].Version)
		vj, _ := semver.NewVersion(result[j].Version)
		return vi.LessThan(vj)
	})
	return result, nil
}
