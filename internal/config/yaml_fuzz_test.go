package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func FuzzConfigParse(f *testing.F) {
	f.Add([]byte("output_format: json\nmax_issues: 50\n"))
	f.Add([]byte(""))
	f.Add([]byte("---"))
	f.Add([]byte("collectors:\n  todos:\n    enabled: true\n"))
	f.Add([]byte("{invalid"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return
		}
		// Round-trip: if parse succeeded, marshal should not panic.
		yaml.Marshal(&cfg) //nolint:errcheck,gosec // fuzz: testing crash-freedom
	})
}
