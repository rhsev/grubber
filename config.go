package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	defaults map[string]any
	sets     map[string]any
}

func NewConfig() *Config {
	c := &Config{
		defaults: map[string]any{
			"blocks_only":  false,
			"array_fields": []any{},
			"filters":      []any{},
		},
		sets: map[string]any{},
	}
	home, err := os.UserHomeDir()
	if err == nil {
		c.loadConfig(filepath.Join(home, ".config/grubber/config.yaml"))
	}
	return c
}

func (c *Config) loadConfig(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not load config: %v\n", err)
		return
	}
	if defaults, ok := raw["defaults"].(map[string]any); ok {
		for k, v := range defaults {
			c.defaults[k] = v
		}
	}
	if sets, ok := raw["sets"].(map[string]any); ok {
		c.sets = sets
	}
}

func (c *Config) GetSet(name string) map[string]any {
	if set, ok := c.sets[name].(map[string]any); ok {
		return set
	}
	return nil
}

func (c *Config) SetNames() []string {
	names := make([]string, 0, len(c.sets))
	for k := range c.sets {
		names = append(names, k)
	}
	return names
}

func (c *Config) DefaultBlocksOnly() bool {
	if v, ok := c.defaults["blocks_only"].(bool); ok {
		return v
	}
	return false
}

func (c *Config) DefaultArrayFields() []string {
	return toStringSlice(c.defaults["array_fields"])
}

func (c *Config) DefaultFilters() []string {
	return toStringSlice(c.defaults["filters"])
}

func (c *Config) DefaultExtensions() []string {
	return toStringSlice(c.defaults["extensions"])
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
