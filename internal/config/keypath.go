package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"gopkg.in/yaml.v3"
)

// GetValue retrieves a value from a Config by dot-notation key path.
// It returns scalar values as-is, and maps/slices for intermediate nodes.
func GetValue(cfg *Config, keyPath string) (any, error) {
	m, err := configToMap(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}
	return navigateMap(m, keyPath)
}

// SetValue sets a value in a raw YAML map by dot-notation key path,
// creating intermediate maps as needed.
func SetValue(data map[string]any, keyPath string, rawValue string) error {
	parts := strings.Split(keyPath, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty key path")
	}

	// Navigate to the parent, creating intermediate maps.
	current := data
	for _, part := range parts[:len(parts)-1] {
		child, ok := current[part]
		if !ok {
			next := make(map[string]any)
			current[part] = next
			current = next
			continue
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("key %q is not a map", part)
		}
		current = next
	}

	current[parts[len(parts)-1]] = coerceValue(rawValue)
	return nil
}

// FlattenMap recursively flattens a nested map to dot-notation keys.
func FlattenMap(m map[string]any, prefix string) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			for sk, sv := range FlattenMap(sub, key) {
				result[sk] = sv
			}
		} else {
			result[key] = v
		}
	}
	return result
}

// ValidateKeyPath checks that a dot-notation key path corresponds to a valid
// Config field. It uses yaml struct tags to build the valid key set.
func ValidateKeyPath(keyPath string) error {
	parts := strings.Split(keyPath, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty key path")
	}

	topKeys := yamlKeys(reflect.TypeOf(Config{}))
	first := parts[0]

	if first == "priority_overrides" {
		return fmt.Errorf("priority_overrides cannot be set via config set; edit %s directly", FileName)
	}

	if _, ok := topKeys[first]; !ok {
		return fmt.Errorf("unknown key %q; valid top-level keys: %s", first, sortedKeys(topKeys))
	}

	if first != "collectors" {
		if len(parts) > 1 {
			return fmt.Errorf("key %q is a scalar; cannot use sub-keys", first)
		}
		return nil
	}

	// collectors.<name>[.<field>]
	if len(parts) < 2 {
		return fmt.Errorf("collectors requires a collector name (e.g. collectors.todos)")
	}

	collectorName := parts[1]
	if collector.Get(collectorName) == nil {
		return fmt.Errorf("unknown collector %q; registered collectors: %s",
			collectorName, strings.Join(collector.List(), ", "))
	}

	if len(parts) == 2 {
		return nil // setting the whole collector block
	}

	if len(parts) == 3 {
		ccKeys := yamlKeys(reflect.TypeOf(CollectorConfig{}))
		if _, ok := ccKeys[parts[2]]; !ok {
			return fmt.Errorf("unknown collector field %q; valid fields: %s", parts[2], sortedKeys(ccKeys))
		}
		return nil
	}

	return fmt.Errorf("key path too deep: %q", keyPath)
}

// configToMap marshals a Config to a map via YAML round-trip.
func configToMap(cfg *Config) (map[string]any, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
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

// navigateMap traverses a nested map using a dot-notation key path.
func navigateMap(m map[string]any, keyPath string) (any, error) {
	parts := strings.Split(keyPath, ".")
	var current any = m
	for _, part := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("key %q: parent is not a map", part)
		}
		val, exists := cm[part]
		if !exists {
			return nil, fmt.Errorf("key %q not found", keyPath)
		}
		current = val
	}
	return current, nil
}

// coerceValue parses a string into bool, int, float64, or keeps it as string.
func coerceValue(s string) any {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		// Only use float if it has a decimal point (avoid converting "3" to 3.0).
		if strings.Contains(s, ".") {
			return f
		}
	}
	return s
}

// yamlKeys extracts yaml tag names from a struct type.
func yamlKeys(t reflect.Type) map[string]bool {
	keys := make(map[string]bool)
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			keys[name] = true
		}
	}
	return keys
}

// sortedKeys returns a comma-separated sorted list of map keys.
func sortedKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort for small key sets.
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return strings.Join(keys, ", ")
}
