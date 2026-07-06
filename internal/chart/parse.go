package chart

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
)

type Dependency struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
	Alias      string `yaml:"alias"`
}

type Chart struct {
	Name         string       `yaml:"name"`
	Dependencies []Dependency `yaml:"dependencies"`
	Dir          string
}

func LoadAll(chartsDir string) ([]Chart, error) {
	entries, err := os.ReadDir(chartsDir)
	if err != nil {
		return nil, fmt.Errorf("read charts dir: %w", err)
	}

	var charts []Chart
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(chartsDir, e.Name(), "Chart.yaml")
		c, err := load(path, e.Name())
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", e.Name(), err)
		}
		charts = append(charts, c)
	}
	return charts, nil
}

func load(path, dir string) (Chart, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Chart{}, err
	}
	var c Chart
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Chart{}, err
	}
	c.Dir = dir
	return c, nil
}

// UpdateVersions rewrites Chart.yaml for the given chart, replacing dependency
// versions according to the updates map (dep name -> new version). Uses the
// yaml.Node API so comments and key order are preserved.
func UpdateVersions(chartsDir, chartDir string, updates map[string]string) error {
	path := filepath.Join(chartsDir, chartDir, "Chart.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if len(root.Content) == 0 {
		return nil
	}

	mapping := root.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "dependencies" {
			continue
		}
		for _, depNode := range mapping.Content[i+1].Content {
			var depName string
			for j := 0; j+1 < len(depNode.Content); j += 2 {
				if depNode.Content[j].Value == "name" {
					depName = depNode.Content[j+1].Value
				}
			}
			for j := 0; j+1 < len(depNode.Content); j += 2 {
				if depNode.Content[j].Value == "version" {
					if newVer, ok := updates[depName]; ok {
						depNode.Content[j+1].Value = newVer
					}
				}
			}
		}
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}
