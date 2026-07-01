package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// Token resolves a GitHub token from (in order): explicit value, GITHUB_TOKEN env, `gh auth token`.
func Token(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

// GitHubChangelog fetches release notes for all versions in (currentVersion, latestVersion]
// from GitHub, walking the index to discover intermediate versions.
func GitHubChangelog(repoURL, chartName, currentVersion, latestVersion, token string) ([]string, error) {
	entries, err := EntriesInRange(repoURL, chartName, currentVersion, latestVersion)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no versions found in range (%s, %s]", currentVersion, latestVersion)
	}

	owner, repo, tagFn, err := ghRepoFromEntries(entries, chartName)
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, e := range entries {
		tag := tagFn(e.Version)
		release, err := fetchRelease(owner, repo, tag, token)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("── %s  %s", e.Version, release.HTMLURL))
		for _, l := range cleanBody(release.Body, 6) {
			lines = append(lines, "   "+l)
		}
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("no GitHub releases found for %s in range (%s, %s]", chartName, currentVersion, latestVersion)
	}
	return lines, nil
}

// ghRepoFromEntries extracts GitHub owner/repo and a tag-builder function from
// index entry metadata. Prefers GitHub download URLs; falls back to source URLs.
func ghRepoFromEntries(entries []indexEntry, chartName string) (owner, repo string, tagFn func(string) string, err error) {
	for _, e := range entries {
		for _, rawURL := range e.URLs {
			o, r, fn, ok := parseGHDownloadURL(rawURL, e.Version)
			if ok {
				return o, r, fn, nil
			}
		}
	}

	// Fall back to source URLs with common tag patterns.
	for _, e := range entries {
		for _, src := range e.Sources {
			o, r, ok := parseGHSourceURL(src)
			if ok {
				fn := ghTagFallback()
				return o, r, fn, nil
			}
		}
	}

	return "", "", nil, fmt.Errorf("no GitHub repo found for chart %q", chartName)
}

// parseGHDownloadURL handles: https://github.com/{owner}/{repo}/releases/download/{tag}/{file}
func parseGHDownloadURL(rawURL, version string) (owner, repo string, tagFn func(string) string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != "github.com" {
		return "", "", nil, false
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 6)
	if len(parts) < 5 || parts[2] != "releases" || parts[3] != "download" {
		return "", "", nil, false
	}
	knownTag := parts[4]
	// Derive a template by replacing the known version inside the tag.
	template := strings.Replace(knownTag, version, "{{v}}", 1)
	return parts[0], parts[1], func(v string) string {
		return strings.Replace(template, "{{v}}", v, 1)
	}, true
}

// parseGHSourceURL handles: https://github.com/{owner}/{repo}[/...]
func parseGHSourceURL(src string) (owner, repo string, ok bool) {
	u, err := url.Parse(src)
	if err != nil || u.Host != "github.com" {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ghTagFallback tries v{version} (most common for source repos like cilium).
func ghTagFallback() func(string) string {
	return func(version string) string {
		return "v" + version
	}
}

func fetchRelease(owner, repo, tag, token string) (ghRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return ghRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ghRelease{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ghRelease{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ghRelease{}, fmt.Errorf("status %d for %s/%s@%s", resp.StatusCode, owner, repo, tag)
	}

	var r ghRelease
	if err := json.Unmarshal(body, &r); err != nil {
		return ghRelease{}, err
	}
	return r, nil
}

// cleanBody extracts meaningful bullet points from a GitHub release body,
// stripping attribution, URLs, section headers, and other noise.
// Falls back to the first non-trivial prose line if there are no bullets.
func cleanBody(body string, maxLines int) []string {
	var bullets []string
	stop := false

	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimRight(l, "\r")
		trimmed := strings.TrimSpace(l)

		if strings.Contains(trimmed, "New Contributors") || strings.HasPrefix(trimmed, "**Full Changelog**") {
			stop = true
		}
		if stop {
			continue
		}

		if strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "- ") {
			if b := cleanBullet(trimmed[2:]); b != "" {
				bullets = append(bullets, "  • "+b)
			}
		}
		if len(bullets) >= maxLines {
			break
		}
	}

	if len(bullets) > 0 {
		return bullets
	}

	// Fallback: first non-empty, non-header, non-separator prose line.
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimRight(l, "\r")
		trimmed := strings.TrimSpace(l)
		if trimmed == "" ||
			strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "**") ||
			strings.HasPrefix(trimmed, "[") ||
			strings.HasPrefix(trimmed, "**Full Changelog**") ||
			isSeparator(trimmed) {
			continue
		}
		return []string{"  " + trimmed}
	}
	return nil
}

func isSeparator(s string) bool {
	if len(s) < 3 {
		return false
	}
	for _, c := range s {
		if c != '-' && c != '=' && c != '*' {
			return false
		}
	}
	return true
}

// cleanBullet strips attribution ("by @user in https://..."), backport noise,
// and "[chart-name]: " prefixes that some repos add.
func cleanBullet(text string) string {
	if i := strings.Index(text, " by @"); i != -1 {
		text = text[:i]
	}
	if i := strings.Index(text, " (Backport PR "); i != -1 {
		text = text[:i]
	}
	if i := strings.Index(text, "]: "); i != -1 {
		text = text[i+3:]
	}
	return strings.TrimSpace(text)
}
