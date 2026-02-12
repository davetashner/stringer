package main

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/davetashner/stringer/internal/config"
)

// Config command flags.
var configGlobal bool

// configCmd is the parent command for config subcommands.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and modify stringer configuration",
	Long: `View and modify stringer configuration.

Stringer reads configuration from .stringer.yaml in the repository root.
A global config at ~/.config/stringer/config.yaml provides defaults.
Repo-level settings override global settings.

Note: config set does a YAML round-trip and will not preserve comments.
If you need to keep comments, edit the file directly.`,
}

// configGetCmd retrieves a configuration value by dot-notation key path.
var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Long: `Get a configuration value by dot-notation key path.

Examples:
  stringer config get output_format
  stringer config get collectors.todos.min_confidence
  stringer config get collectors.todos
  stringer config get --global no_llm`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

// configSetCmd sets a configuration value.
var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value in the config file.

Values are auto-detected as bool, int, float, or string.
By default, writes to .stringer.yaml in the current directory.
Use --global to write to ~/.config/stringer/config.yaml.

Note: This does a YAML round-trip and will not preserve comments.

Examples:
  stringer config set output_format json
  stringer config set max_issues 50
  stringer config set no_llm true
  stringer config set collectors.todos.min_confidence 0.8
  stringer config set --global no_llm true`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

// configListCmd lists all configuration values with their source.
var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration values",
	Long: `List all configuration values with their source annotation.

Shows every set configuration value, annotated with whether it comes
from the repo config (.stringer.yaml) or global config
(~/.config/stringer/config.yaml). Repo values override global values.`,
	Args: cobra.NoArgs,
	RunE: runConfigList,
}

func init() {
	configGetCmd.Flags().BoolVar(&configGlobal, "global", false, "use global config (~/.config/stringer/config.yaml)")
	configSetCmd.Flags().BoolVar(&configGlobal, "global", false, "write to global config (~/.config/stringer/config.yaml)")

	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configListCmd)
}

// resetConfigFlags resets config command flags for testing.
func resetConfigFlags() {
	configGlobal = false
	if f := configGetCmd.Flags().Lookup("global"); f != nil {
		_ = f.Value.Set("false")
	}
	if f := configSetCmd.Flags().Lookup("global"); f != nil {
		_ = f.Value.Set("false")
	}
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	keyPath := args[0]

	var cfg *config.Config
	var err error

	if configGlobal {
		cfg, err = config.LoadGlobal()
	} else {
		// Load repo config, falling back to merged view.
		repoCfg, repoErr := config.Load(".")
		if repoErr != nil {
			return fmt.Errorf("loading repo config: %w", repoErr)
		}
		globalCfg, globalErr := config.LoadGlobal()
		if globalErr != nil {
			return fmt.Errorf("loading global config: %w", globalErr)
		}
		cfg = mergeConfigs(globalCfg, repoCfg)
	}
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	val, err := config.GetValue(cfg, keyPath)
	if err != nil {
		return err
	}

	return printValue(cmd, val)
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	keyPath := args[0]
	rawValue := args[1]

	if err := config.ValidateKeyPath(keyPath); err != nil {
		return err
	}

	// Determine target file path.
	targetPath := filepath.Join(".", config.FileName)
	if configGlobal {
		targetPath = config.GlobalConfigPath()
	}

	// Load existing file as raw map.
	data, err := config.LoadRaw(targetPath)
	if err != nil {
		return fmt.Errorf("loading config file: %w", err)
	}

	// Set the value.
	if err := config.SetValue(data, keyPath, rawValue); err != nil {
		return fmt.Errorf("setting value: %w", err)
	}

	// Round-trip validate: unmarshal to Config and validate.
	roundTrip, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	var validCfg config.Config
	if err := yaml.Unmarshal(roundTrip, &validCfg); err != nil {
		return fmt.Errorf("invalid config after set: %w", err)
	}
	if err := config.Validate(&validCfg); err != nil {
		return err
	}

	// Write back.
	if err := config.WriteFile(targetPath, data); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", keyPath, rawValue)
	return nil
}

func runConfigList(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()

	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading global config: %w", err)
	}
	repoCfg, err := config.Load(".")
	if err != nil {
		return fmt.Errorf("loading repo config: %w", err)
	}

	globalMap, err := configToFlatMap(globalCfg)
	if err != nil {
		return err
	}
	repoMap, err := configToFlatMap(repoCfg)
	if err != nil {
		return err
	}

	// Merge: repo overrides global, track source.
	type entry struct {
		key    string
		value  any
		source string
	}

	seen := make(map[string]entry)
	for k, v := range globalMap {
		seen[k] = entry{key: k, value: v, source: "global"}
	}
	for k, v := range repoMap {
		seen[k] = entry{key: k, value: v, source: "repo"}
	}

	if len(seen) == 0 {
		_, _ = fmt.Fprintln(w, "No configuration set.")
		_, _ = fmt.Fprintln(w, "Run 'stringer init' to create a config, or 'stringer config set <key> <value>' to set values.")
		return nil
	}

	// Sort keys for stable output.
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	globalColor := color.New(color.FgCyan)
	repoColor := color.New(color.FgGreen)

	for _, k := range keys {
		e := seen[k]
		sourceLabel := formatSource(e.source, globalColor, repoColor)
		_, _ = fmt.Fprintf(w, "%s = %v %s\n", k, e.value, sourceLabel)
	}

	return nil
}

// printValue outputs a value: scalars as plain text, maps/slices as YAML.
func printValue(cmd *cobra.Command, val any) error {
	switch v := val.(type) {
	case map[string]any, []any:
		data, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(cmd.OutOrStdout(), string(data))
	default:
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), v)
	}
	return nil
}

// mergeConfigs merges global and repo configs. Repo values take precedence.
// Only non-zero repo values override global values.
func mergeConfigs(global, repo *config.Config) *config.Config {
	merged := *global

	if repo.OutputFormat != "" {
		merged.OutputFormat = repo.OutputFormat
	}
	if repo.MaxIssues != 0 {
		merged.MaxIssues = repo.MaxIssues
	}
	if repo.NoLLM {
		merged.NoLLM = repo.NoLLM
	}
	if repo.BeadsAware != nil {
		merged.BeadsAware = repo.BeadsAware
	}
	if len(repo.PriorityOverrides) > 0 {
		merged.PriorityOverrides = repo.PriorityOverrides
	}

	// Merge collector configs: repo overrides global per collector.
	if len(repo.Collectors) > 0 {
		if merged.Collectors == nil {
			merged.Collectors = make(map[string]config.CollectorConfig)
		}
		for name, repoCC := range repo.Collectors {
			merged.Collectors[name] = repoCC
		}
	}

	return &merged
}

// configToFlatMap converts a Config to a flat dot-notation map, omitting zero values.
func configToFlatMap(cfg *config.Config) (map[string]any, error) {
	m, err := configToMapViaYAML(cfg)
	if err != nil {
		return nil, err
	}
	return config.FlattenMap(m, ""), nil
}

// configToMapViaYAML converts a Config to a map via YAML round-trip.
func configToMapViaYAML(cfg *config.Config) (map[string]any, error) {
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

// formatSource returns a colorized source annotation.
func formatSource(source string, globalColor, repoColor *color.Color) string {
	switch source {
	case "global":
		return globalColor.Sprintf("(global)")
	case "repo":
		return repoColor.Sprintf("(repo)")
	default:
		return fmt.Sprintf("(%s)", source)
	}
}
