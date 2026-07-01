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
