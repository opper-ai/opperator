package lsp

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

type Config struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	FileTypes   []string          `json:"file_types,omitempty"`
	RootMarkers []string          `json:"root_markers,omitempty"`
	InitOptions map[string]any    `json:"init_options,omitempty"`
	Options     map[string]any    `json:"options,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
}

// Configs maps language server identifiers to their configuration.
type Configs map[string]Config

func (cfg Config) clone() Config {
	out := cfg
	out.Args = slices.Clone(cfg.Args)
	if len(cfg.FileTypes) > 0 {
		out.FileTypes = slices.Clone(cfg.FileTypes)
	}
	if len(cfg.RootMarkers) > 0 {
		out.RootMarkers = slices.Clone(cfg.RootMarkers)
	}
	if cfg.Env != nil {
		out.Env = maps.Clone(cfg.Env)
	}
	if cfg.InitOptions != nil {
		out.InitOptions = maps.Clone(cfg.InitOptions)
	}
	if cfg.Options != nil {
		out.Options = maps.Clone(cfg.Options)
	}
	return out
}

func (c Configs) Clone() Configs {
	if c == nil {
		return nil
	}
	out := make(Configs, len(c))
	for k, v := range c {
		out[k] = v.clone()
	}
	return out
}

// HandlesFile reports whether cfg should be used for the provided path based on
// its configured file types. If no file types are specified the configuration is
// assumed to support every file.
func (cfg Config) HandlesFile(path string) bool {
	if len(cfg.FileTypes) == 0 {
		return true
	}
	name := strings.ToLower(filepath.Base(path))
	for _, ft := range cfg.FileTypes {
		suffix := strings.ToLower(strings.TrimSpace(ft))
		if suffix == "" {
			continue
		}
		if !strings.HasPrefix(suffix, ".") {
			suffix = "." + suffix
		}
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// defaults intentionally cover only a handful of common environments so that
// users can expand them incrementally.
func DefaultConfigs() Configs {
	return Configs{
		"gopls": {
			Command:     "gopls",
			Args:        []string{"serve"},
			FileTypes:   []string{"go", "mod", "sum", "tmpl"},
			RootMarkers: []string{"go.work", "go.mod"},
		},
	}
}
