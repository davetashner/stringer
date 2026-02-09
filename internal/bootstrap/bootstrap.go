package bootstrap

import (
	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/testable"
)

// FS is the file system implementation used by this package.
// Override in tests with a testable.MockFileSystem.
var FS testable.FileSystem = testable.DefaultFS

// InitConfig holds the inputs for the init command.
type InitConfig struct {
	RepoPath string
	Force    bool
}

// Action records a single file operation performed during init.
type Action struct {
	File        string // e.g. ".stringer.yaml", "AGENTS.md"
	Operation   string // "created", "updated", "skipped"
	Description string // human-readable detail
}

// InitResult holds the outcome of an init run.
type InitResult struct {
	Actions   []Action
	Language  string
	HasGitHub bool
}

// Run orchestrates the init process: detect repo characteristics, generate
// config, and append the AGENTS.md snippet.
func Run(cfg InitConfig) (*InitResult, error) {
	// 1. Analyze repo for language/tech detection.
	analysis, err := docs.Analyze(cfg.RepoPath)
	if err != nil {
		return nil, err
	}

	// 2. Detect GitHub remote.
	remote := DetectGitHubRemote(cfg.RepoPath)
	hasGitHub := remote != nil

	result := &InitResult{
		Language:  analysis.Language,
		HasGitHub: hasGitHub,
	}

	// 3. Generate .stringer.yaml.
	configAction, err := GenerateConfig(cfg.RepoPath, hasGitHub, cfg.Force)
	if err != nil {
		return nil, err
	}
	result.Actions = append(result.Actions, configAction)

	// 4. Append AGENTS.md snippet.
	agentsAction, err := AppendAgentSnippet(cfg.RepoPath)
	if err != nil {
		return nil, err
	}
	result.Actions = append(result.Actions, agentsAction)

	return result, nil
}
