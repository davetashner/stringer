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

// LoadRaw reads a YAML file as a raw map[string]any.
// If the file does not exist, it returns an empty map and nil error.
func LoadRaw(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-provided path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return make(map[string]any), nil
		}
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = make(map[string]any)
	}
	return m, nil
}

// WriteFile marshals a raw map to YAML and writes it to the given path,
// creating parent directories as needed.
func WriteFile(path string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.Create(path) //nolint:gosec // user-provided path
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // best-effort close
	enc := yaml.NewEncoder(f)
	defer enc.Close() //nolint:errcheck // best-effort close
	enc.SetIndent(2)
	return enc.Encode(data)
}
