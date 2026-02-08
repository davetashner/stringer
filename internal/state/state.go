// Package state manages persisted scan state for delta scanning.
//
// When --delta is active, stringer saves a record of all signal hashes
// produced by a scan. On subsequent runs, this state is loaded and used
// to filter output to only new signals.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"

	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/signal"
)

// stateDir is the directory name within a repo where state is stored.
const stateDir = ".stringer"

// stateFile is the filename for scan state.
const stateFile = "last-scan.json"

// schemaVersion is the current state file schema version.
const schemaVersion = "1"

// ScanState represents persisted state from a previous scan.
type ScanState struct {
	Version       string    `json:"version"`
	ScanTimestamp time.Time `json:"scan_timestamp"`
	GitHead       string    `json:"git_head"`
	Collectors    []string  `json:"collectors"`
	SignalHashes  []string  `json:"signal_hashes"`
	SignalCount   int       `json:"signal_count"`
}

// Load reads the scan state file from <repoPath>/.stringer/last-scan.json.
// If the file does not exist, it returns (nil, nil).
func Load(repoPath string) (*ScanState, error) {
	path := filepath.Join(repoPath, stateDir, stateFile)
	data, err := os.ReadFile(path) //nolint:gosec // user-provided repo path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var s ScanState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the scan state to <repoPath>/.stringer/last-scan.json.
// It creates the .stringer directory if it does not exist.
func Save(repoPath string, s *ScanState) error {
	dir := filepath.Join(repoPath, stateDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, stateFile), data, 0o644) //nolint:gosec // state file, not secret
}

// FilterNew returns only the signals whose hashes are not present in prev.
// If prev is nil, all signals are considered new.
// The order of signals is preserved.
func FilterNew(signals []signal.RawSignal, prev *ScanState) []signal.RawSignal {
	if prev == nil || len(prev.SignalHashes) == 0 {
		result := make([]signal.RawSignal, len(signals))
		copy(result, signals)
		return result
	}

	seen := make(map[string]struct{}, len(prev.SignalHashes))
	for _, h := range prev.SignalHashes {
		seen[h] = struct{}{}
	}

	var result []signal.RawSignal
	for _, s := range signals {
		hash := pipeline.SignalHash(s)
		if _, exists := seen[hash]; !exists {
			result = append(result, s)
		}
	}
	return result
}

// Build creates a new ScanState from the current scan results.
// It captures the git HEAD (if available) and hashes of all signals.
func Build(repoPath string, collectors []string, signals []signal.RawSignal) *ScanState {
	hashes := make([]string, 0, len(signals))
	for _, s := range signals {
		hashes = append(hashes, pipeline.SignalHash(s))
	}

	sorted := make([]string, len(collectors))
	copy(sorted, collectors)
	sort.Strings(sorted)

	return &ScanState{
		Version:       schemaVersion,
		ScanTimestamp: time.Now().UTC(),
		GitHead:       resolveHead(repoPath),
		Collectors:    sorted,
		SignalHashes:  hashes,
		SignalCount:   len(signals),
	}
}

// CollectorsMatch returns true if the previous state's collector list matches
// the current list (order-independent).
func CollectorsMatch(prev *ScanState, current []string) bool {
	if prev == nil {
		return true // no previous state, nothing to mismatch
	}

	sorted := make([]string, len(current))
	copy(sorted, current)
	sort.Strings(sorted)

	if len(prev.Collectors) != len(sorted) {
		return false
	}
	for i := range prev.Collectors {
		if prev.Collectors[i] != sorted[i] {
			return false
		}
	}
	return true
}

// resolveHead returns the git HEAD commit hash, or empty string if not a git repo.
func resolveHead(repoPath string) string {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return ""
	}
	head, err := repo.Head()
	if err != nil {
		return ""
	}
	return head.Hash().String()
}
