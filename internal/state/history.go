// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// historyFile is the filename for scan history.
const historyFile = "scan-history.json"

// historySchemaVersion is the current history file schema version.
const historySchemaVersion = "1"

// maxHistoryEntries is the FIFO cap for history entries.
const maxHistoryEntries = 100

// HistoryEntry captures summary metrics from a single scan.
type HistoryEntry struct {
	Timestamp       time.Time      `json:"timestamp"`
	GitHead         string         `json:"git_head"`
	TotalSignals    int            `json:"total_signals"`
	CollectorCounts map[string]int `json:"collector_counts"`
	KindCounts      map[string]int `json:"kind_counts"`
}

// ScanHistory stores a time-series of scan summary entries.
type ScanHistory struct {
	Version string         `json:"version"`
	Entries []HistoryEntry `json:"entries"`
}

// LoadHistory reads the scan history file from <repoPath>/.stringer/scan-history.json.
// If the file does not exist, it returns (nil, nil).
func LoadHistory(repoPath string) (*ScanHistory, error) {
	return LoadHistoryWorkspace(repoPath, "")
}

// LoadHistoryWorkspace reads the scan history file for a specific workspace.
// When workspace is empty, it reads from <repoPath>/.stringer/scan-history.json.
// When set, it reads from <repoPath>/.stringer/<workspace>/scan-history.json.
func LoadHistoryWorkspace(repoPath, workspace string) (*ScanHistory, error) {
	path := historyPath(repoPath, workspace)
	data, err := FS.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var h ScanHistory
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// SaveHistory writes the scan history to <repoPath>/.stringer/scan-history.json.
// It creates the .stringer directory if it does not exist.
func SaveHistory(repoPath string, h *ScanHistory) error {
	return SaveHistoryWorkspace(repoPath, "", h)
}

// SaveHistoryWorkspace writes the scan history for a specific workspace.
// When workspace is empty, it writes to <repoPath>/.stringer/scan-history.json.
// When set, it writes to <repoPath>/.stringer/<workspace>/scan-history.json.
func SaveHistoryWorkspace(repoPath, workspace string, h *ScanHistory) error {
	dir := stateDirectory(repoPath, workspace)
	if err := FS.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}

	if err := FS.WriteFile(filepath.Join(dir, historyFile), data, 0o644); err != nil {
		return fmt.Errorf("write history file: %w", err)
	}
	return nil
}

// AppendEntry adds an entry to the history and enforces the FIFO cap.
func AppendEntry(h *ScanHistory, entry HistoryEntry) *ScanHistory {
	if h == nil {
		h = &ScanHistory{Version: historySchemaVersion}
	}
	h.Version = historySchemaVersion
	h.Entries = append(h.Entries, entry)
	if len(h.Entries) > maxHistoryEntries {
		h.Entries = h.Entries[len(h.Entries)-maxHistoryEntries:]
	}
	return h
}

// BuildHistoryEntry creates a HistoryEntry from a scan result.
func BuildHistoryEntry(repoPath string, result *signal.ScanResult) HistoryEntry {
	collectorCounts := make(map[string]int)
	for _, cr := range result.Results {
		collectorCounts[cr.Collector] = len(cr.Signals)
	}

	kindCounts := make(map[string]int)
	for _, sig := range result.Signals {
		kindCounts[sig.Kind]++
	}

	// Sort map keys for deterministic output.
	sortedCollector := make(map[string]int, len(collectorCounts))
	for k, v := range collectorCounts {
		sortedCollector[k] = v
	}
	sortedKind := make(map[string]int, len(kindCounts))
	for k, v := range kindCounts {
		sortedKind[k] = v
	}

	return HistoryEntry{
		Timestamp:       time.Now().UTC(),
		GitHead:         resolveHead(repoPath),
		TotalSignals:    len(result.Signals),
		CollectorCounts: sortedCollector,
		KindCounts:      sortedKind,
	}
}

// historyPath returns the full path to the history file for a workspace.
func historyPath(repoPath, workspace string) string {
	return filepath.Join(stateDirectory(repoPath, workspace), historyFile)
}

// SortedKeys returns the sorted keys from a map[string]int.
func SortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
