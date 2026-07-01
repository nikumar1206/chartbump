package registry

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"go.yaml.in/yaml/v4"
)

type chartMeta struct {
	Version string `yaml:"version"`
}

// CheckOCI shells out to `helm show chart <ociRef>` (no version = latest tag)
// and returns a VersionInfo. Created date is not available via this method.
// Note: stableOnly has no effect for OCI — latest tag is always resolved by the registry.
func CheckOCI(ociRef string) (VersionInfo, error) {
	cmd := exec.Command("helm", "show", "chart", ociRef)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return VersionInfo{}, fmt.Errorf("helm show chart %s: %s", ociRef, strings.TrimSpace(errBuf.String()))
	}

	var meta chartMeta
	if err := yaml.Unmarshal(out.Bytes(), &meta); err != nil {
		return VersionInfo{}, fmt.Errorf("parse chart output: %w", err)
	}
	if meta.Version == "" {
		return VersionInfo{}, fmt.Errorf("no version in chart output for %s", ociRef)
	}
	return VersionInfo{Version: meta.Version}, nil
}
