package config

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads the .stringer.yaml file from the given repository root.
// If the file does not exist, it returns a zero-value Config and nil error.
func Load(repoPath string) (*Config, error) {
	path := filepath.Join(repoPath, FileName)
	data, err := os.ReadFile(path) //nolint:gosec // user-provided repo path
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

// Write marshals the config to YAML and writes it to w.
func Write(w io.Writer, cfg *Config) error {
	enc := yaml.NewEncoder(w)
	defer enc.Close() //nolint:errcheck // best-effort close
	enc.SetIndent(2)
	return enc.Encode(cfg)
}
