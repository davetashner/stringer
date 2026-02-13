// Package state manages persisted scan state for delta scanning.
//
// When --delta is active, stringer saves a record of all signal hashes
// produced by a scan. On subsequent runs, this state is loaded and used
// to filter output to only new signals.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// stateDir is the directory name within a repo where state is stored.
const stateDir = ".stringer"

// stateFile is the filename for scan state.
const stateFile = "last-scan.json"

// schemaVersion is the current state file schema version.
const schemaVersion = "2"

// FS is the file system implementation used by this package.
// Override in tests with a testable.MockFileSystem.
var FS testable.FileSystem = testable.DefaultFS

// SignalMeta stores metadata about a signal for diff output.
type SignalMeta struct {
	Hash     string `json:"hash"`
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line,omitempty"`
	Title    string `json:"title"`
}

// ScanState represents persisted state from a previous scan.
type ScanState struct {
	Version       string       `json:"version"`
	ScanTimestamp time.Time    `json:"scan_timestamp"`
	GitHead       string       `json:"git_head"`
	Collectors    []string     `json:"collectors"`
	SignalHashes  []string     `json:"signal_hashes"`
	SignalMetas   []SignalMeta `json:"signal_metas,omitempty"`
	SignalCount   int          `json:"signal_count"`
}

// DiffResult holds the comparison between two scans.
type DiffResult struct {
	Added   []SignalMeta  // signals in current but not previous
	Removed []SignalMeta  // signals in previous but not current
	Moved   []MovedSignal // signals with same title/kind but different location
}

// MovedSignal tracks a signal that moved between scans.
type MovedSignal struct {
	Previous SignalMeta
	Current  SignalMeta
}

// AnnotatedSignal extends SignalMeta with resolution context.
type AnnotatedSignal struct {
	SignalMeta
	Resolution string // "file_deleted", "moved", or ""
}

// Load reads the scan state file from <repoPath>/.stringer/last-scan.json.
// If the file does not exist, it returns (nil, nil).
func Load(repoPath string) (*ScanState, error) {
	return LoadWorkspace(repoPath, "")
}

// LoadWorkspace reads the scan state file for a specific workspace.
// When workspace is empty, it reads from <repoPath>/.stringer/last-scan.json.
// When set, it reads from <repoPath>/.stringer/<workspace>/last-scan.json.
func LoadWorkspace(repoPath, workspace string) (*ScanState, error) {
	path := statePath(repoPath, workspace)
	data, err := FS.ReadFile(path)
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
	return SaveWorkspace(repoPath, "", s)
}

// SaveWorkspace writes the scan state for a specific workspace.
// When workspace is empty, it writes to <repoPath>/.stringer/last-scan.json.
// When set, it writes to <repoPath>/.stringer/<workspace>/last-scan.json.
func SaveWorkspace(repoPath, workspace string, s *ScanState) error {
	dir := stateDirectory(repoPath, workspace)
	if err := FS.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	if err := FS.WriteFile(filepath.Join(dir, stateFile), data, 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

// statePath returns the full path to the state file for a workspace.
func statePath(repoPath, workspace string) string {
	return filepath.Join(stateDirectory(repoPath, workspace), stateFile)
}

// stateDirectory returns the directory for state files, scoped by workspace.
func stateDirectory(repoPath, workspace string) string {
	if workspace == "" {
		return filepath.Join(repoPath, stateDir)
	}
	return filepath.Join(repoPath, stateDir, workspace)
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
	metas := make([]SignalMeta, 0, len(signals))
	for _, s := range signals {
		h := pipeline.SignalHash(s)
		hashes = append(hashes, h)
		metas = append(metas, SignalMeta{
			Hash:     h,
			Source:   s.Source,
			Kind:     s.Kind,
			FilePath: s.FilePath,
			Line:     s.Line,
			Title:    s.Title,
		})
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
		SignalMetas:   metas,
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

// ComputeDiff compares previous and current scan states.
// It categorizes signals as added, removed, or moved.
// Moved detection: if a removed and added signal share the same Title+Kind
// but differ in FilePath or Line, they are treated as moved rather than
// added/removed.
func ComputeDiff(prev, current *ScanState) *DiffResult {
	result := &DiffResult{}

	prevByHash := make(map[string]SignalMeta, len(prev.SignalMetas))
	for _, m := range prev.SignalMetas {
		prevByHash[m.Hash] = m
	}

	curByHash := make(map[string]SignalMeta, len(current.SignalMetas))
	for _, m := range current.SignalMetas {
		curByHash[m.Hash] = m
	}

	// Collect raw added/removed by hash.
	var rawAdded []SignalMeta
	for _, m := range current.SignalMetas {
		if _, exists := prevByHash[m.Hash]; !exists {
			rawAdded = append(rawAdded, m)
		}
	}

	var rawRemoved []SignalMeta
	for _, m := range prev.SignalMetas {
		if _, exists := curByHash[m.Hash]; !exists {
			rawRemoved = append(rawRemoved, m)
		}
	}

	// Detect moved signals: same Title+Kind, different location.
	type titleKindKey struct {
		Title string
		Kind  string
	}

	// Index added by title+kind for move matching.
	addedByTK := make(map[titleKindKey][]int, len(rawAdded))
	for i, m := range rawAdded {
		key := titleKindKey{Title: m.Title, Kind: m.Kind}
		addedByTK[key] = append(addedByTK[key], i)
	}

	movedAddedIdx := make(map[int]bool)
	movedRemovedIdx := make(map[int]bool)

	for ri, rm := range rawRemoved {
		key := titleKindKey{Title: rm.Title, Kind: rm.Kind}
		indices, ok := addedByTK[key]
		if !ok {
			continue
		}
		// Match with the first available added signal with this title+kind.
		for _, ai := range indices {
			if movedAddedIdx[ai] {
				continue
			}
			am := rawAdded[ai]
			if am.FilePath != rm.FilePath || am.Line != rm.Line {
				result.Moved = append(result.Moved, MovedSignal{
					Previous: rm,
					Current:  am,
				})
				movedAddedIdx[ai] = true
				movedRemovedIdx[ri] = true
				break
			}
		}
	}

	// Remaining added/removed (not part of moves).
	for i, m := range rawAdded {
		if !movedAddedIdx[i] {
			result.Added = append(result.Added, m)
		}
	}
	for i, m := range rawRemoved {
		if !movedRemovedIdx[i] {
			result.Removed = append(result.Removed, m)
		}
	}

	return result
}

// AnnotateRemovedSignals marks removed signals with resolution context.
// Signals referring to deleted files are marked as "file_deleted".
// This helps users distinguish between resolved work vs stale signals.
func AnnotateRemovedSignals(repoPath string, removed []SignalMeta) []AnnotatedSignal {
	annotated := make([]AnnotatedSignal, len(removed))
	for i, m := range removed {
		annotated[i] = AnnotatedSignal{SignalMeta: m}
		if m.FilePath == "" {
			continue
		}
		fullPath := filepath.Join(repoPath, m.FilePath)
		if _, err := FS.Stat(fullPath); errors.Is(err, fs.ErrNotExist) {
			annotated[i].Resolution = "file_deleted"
		}
	}
	return annotated
}

// BuildResolvedTodoSignals converts removed TODO signals into closed RawSignals.
// When delta scanning detects that a TODO has disappeared, this function creates
// a pre-closed signal representing the resolved work item.
func BuildResolvedTodoSignals(repoPath string, removed []SignalMeta) []signal.RawSignal {
	// Filter to only TODO-sourced signals.
	var todoRemoved []SignalMeta
	for _, m := range removed {
		if m.Source == "todos" {
			todoRemoved = append(todoRemoved, m)
		}
	}
	if len(todoRemoved) == 0 {
		return nil
	}

	// Annotate with file deletion context.
	annotated := AnnotateRemovedSignals(repoPath, todoRemoved)
	now := time.Now()

	signals := make([]signal.RawSignal, 0, len(annotated))
	for _, a := range annotated {
		module := moduleFromFilePath(a.FilePath)
		desc := fmt.Sprintf("Module: %s\nResolved TODO at %s", module, formatLocation(a.SignalMeta))
		if a.Resolution == "file_deleted" {
			desc += " (file deleted)"
		}

		signals = append(signals, signal.RawSignal{
			Source:      "todos",
			Kind:        a.Kind,
			FilePath:    a.FilePath,
			Line:        a.Line,
			Title:       a.Title,
			Description: desc,
			Confidence:  0.3,
			Tags:        []string{a.Kind, "pre-closed", "resolved", "stringer-generated"},
			ClosedAt:    now,
			Timestamp:   now,
		})
	}
	return signals
}

// FormatDiff writes a human-readable diff summary to w.
// The output uses +/- notation similar to git diff.
func FormatDiff(diff *DiffResult, repoPath string, w io.Writer) error {
	addedCount := len(diff.Added)
	removedCount := len(diff.Removed)
	movedCount := len(diff.Moved)

	if addedCount == 0 && removedCount == 0 && movedCount == 0 {
		_, err := fmt.Fprintln(w, "Delta scan summary: no changes")
		return err
	}

	if _, err := fmt.Fprintln(w, "Delta scan summary:"); err != nil {
		return err
	}
	if addedCount > 0 {
		if _, err := fmt.Fprintf(w, "  + %d new signal(s)\n", addedCount); err != nil {
			return err
		}
	}
	if removedCount > 0 {
		if _, err := fmt.Fprintf(w, "  - %d resolved signal(s)\n", removedCount); err != nil {
			return err
		}
	}
	if movedCount > 0 {
		if _, err := fmt.Fprintf(w, "  ~ %d moved signal(s)\n", movedCount); err != nil {
			return err
		}
	}

	if addedCount > 0 {
		if _, err := fmt.Fprintln(w, "\nNew signals:"); err != nil {
			return err
		}
		for _, m := range diff.Added {
			if _, err := fmt.Fprintf(w, "  + [%s] %s (%s)\n", m.Source, m.Title, formatLocation(m)); err != nil {
				return err
			}
		}
	}

	if removedCount > 0 {
		annotated := AnnotateRemovedSignals(repoPath, diff.Removed)
		if _, err := fmt.Fprintln(w, "\nResolved signals:"); err != nil {
			return err
		}
		for _, a := range annotated {
			suffix := ""
			if a.Resolution == "file_deleted" {
				suffix = " [file deleted]"
			}
			if _, err := fmt.Fprintf(w, "  - [%s] %s (%s)%s\n", a.Source, a.Title, formatLocation(a.SignalMeta), suffix); err != nil {
				return err
			}
		}
	}

	if movedCount > 0 {
		if _, err := fmt.Fprintln(w, "\nMoved signals:"); err != nil {
			return err
		}
		for _, mv := range diff.Moved {
			if _, err := fmt.Fprintf(w, "  ~ [%s] %s\n", mv.Current.Source, mv.Current.Title); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "    from: %s\n", formatLocation(mv.Previous)); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "    to:   %s\n", formatLocation(mv.Current)); err != nil {
				return err
			}
		}
	}

	return nil
}

// formatLocation returns a human-readable location string for a signal.
func formatLocation(m SignalMeta) string {
	if m.Line > 0 {
		return fmt.Sprintf("%s:%d", m.FilePath, m.Line)
	}
	return m.FilePath
}

// moduleFromFilePath derives a module name from a file path.
// e.g., "internal/collectors/todos.go" → "internal/collectors"
// Root-level files return ".".
func moduleFromFilePath(path string) string {
	parts := strings.Split(path, "/")
	switch {
	case len(parts) >= 3:
		return parts[0] + "/" + parts[1]
	case len(parts) == 2:
		return parts[0]
	default:
		return "."
	}
}

// GitOpener is the opener used to access git repositories in the state package.
// Defaults to testable.DefaultGitOpener. Tests can replace this to inject mocks.
var GitOpener testable.GitOpener = testable.DefaultGitOpener

// resolveHead returns the git HEAD commit hash, or empty string if not a git repo.
func resolveHead(repoPath string) string {
	repo, err := GitOpener.PlainOpen(repoPath)
	if err != nil {
		return ""
	}
	head, err := repo.Head()
	if err != nil {
		return ""
	}
	return head.Hash().String()
}
