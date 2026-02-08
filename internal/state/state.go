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
const schemaVersion = "2"

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
		if _, err := os.Stat(fullPath); errors.Is(err, fs.ErrNotExist) {
			annotated[i].Resolution = "file_deleted"
		}
	}
	return annotated
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
