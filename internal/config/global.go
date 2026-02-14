// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// GlobalConfigDir returns the directory for global stringer configuration.
// It uses $XDG_CONFIG_HOME/stringer if set, otherwise ~/.config/stringer.
func GlobalConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "stringer")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "stringer")
}

// GlobalConfigPath returns the path to the global config file.
func GlobalConfigPath() string {
	return filepath.Join(GlobalConfigDir(), "config.yaml")
}

// LoadGlobal loads the global config file.
// If the file does not exist, it returns a zero-value Config and nil error.
func LoadGlobal() (*Config, error) {
	path := GlobalConfigPath()
	data, err := os.ReadFile(path) //nolint:gosec // user config path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
