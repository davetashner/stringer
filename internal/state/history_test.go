// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestLoadHistory_NonExistentFile(t *testing.T) {
	h, err := LoadHistory(t.TempDir())
	assert.NoError(t, err)
	assert.Nil(t, h)
}

func TestLoadHistory_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".stringer")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "scan-history.json"), []byte("not json"), 0o600))

	h, err := LoadHistory(dir)
	assert.Error(t, err)
	assert.Nil(t, h)
}

func TestSaveHistory_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	h := &ScanHistory{
		Version: "1",
		Entries: []HistoryEntry{{
			Timestamp:    time.Now().UTC(),
			TotalSignals: 5,
		}},
	}

	err := SaveHistory(dir, h)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, ".stringer", "scan-history.json"))
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}

func TestSaveHistory_LoadHistory_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &ScanHistory{
		Version: "1",
		Entries: []HistoryEntry{
			{
				Timestamp:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				GitHead:         "abc123",
				TotalSignals:    10,
				CollectorCounts: map[string]int{"todos": 5, "gitlog": 5},
				KindCounts:      map[string]int{"todo": 5, "churn": 5},
			},
		},
	}

	require.NoError(t, SaveHistory(dir, original))

	loaded, err := LoadHistory(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, original.Version, loaded.Version)
	assert.Len(t, loaded.Entries, 1)
	assert.Equal(t, 10, loaded.Entries[0].TotalSignals)
	assert.Equal(t, "abc123", loaded.Entries[0].GitHead)
	assert.Equal(t, map[string]int{"todos": 5, "gitlog": 5}, loaded.Entries[0].CollectorCounts)
	assert.Equal(t, map[string]int{"todo": 5, "churn": 5}, loaded.Entries[0].KindCounts)
}

func TestLoadHistoryWorkspace(t *testing.T) {
	dir := t.TempDir()
	h := &ScanHistory{
		Version: "1",
		Entries: []HistoryEntry{{
			Timestamp:    time.Now().UTC(),
			TotalSignals: 3,
		}},
	}

	require.NoError(t, SaveHistoryWorkspace(dir, "frontend", h))

	loaded, err := LoadHistoryWorkspace(dir, "frontend")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Len(t, loaded.Entries, 1)
	assert.Equal(t, 3, loaded.Entries[0].TotalSignals)

	// Root workspace should be empty.
	rootH, err := LoadHistory(dir)
	assert.NoError(t, err)
	assert.Nil(t, rootH)
}

func TestAppendEntry_NilHistory(t *testing.T) {
	entry := HistoryEntry{TotalSignals: 5}
	h := AppendEntry(nil, entry)

	assert.NotNil(t, h)
	assert.Equal(t, historySchemaVersion, h.Version)
	assert.Len(t, h.Entries, 1)
	assert.Equal(t, 5, h.Entries[0].TotalSignals)
}

func TestAppendEntry_FIFOCap(t *testing.T) {
	h := &ScanHistory{Version: "1"}
	for i := range maxHistoryEntries + 10 {
		h = AppendEntry(h, HistoryEntry{TotalSignals: i})
	}

	assert.Len(t, h.Entries, maxHistoryEntries)
	// Oldest entry should be the 11th (index 10).
	assert.Equal(t, 10, h.Entries[0].TotalSignals)
	// Newest entry should be the last.
	assert.Equal(t, maxHistoryEntries+9, h.Entries[maxHistoryEntries-1].TotalSignals)
}

func TestAppendEntry_PreservesExisting(t *testing.T) {
	h := &ScanHistory{
		Version: "1",
		Entries: []HistoryEntry{
			{TotalSignals: 1},
			{TotalSignals: 2},
		},
	}
	h = AppendEntry(h, HistoryEntry{TotalSignals: 3})

	assert.Len(t, h.Entries, 3)
	assert.Equal(t, 1, h.Entries[0].TotalSignals)
	assert.Equal(t, 3, h.Entries[2].TotalSignals)
}

func TestBuildHistoryEntry(t *testing.T) {
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{Source: "todos", Kind: "todo"},
			{Source: "todos", Kind: "fixme"},
			{Source: "gitlog", Kind: "churn"},
		},
		Results: []signal.CollectorResult{
			{Collector: "todos", Signals: []signal.RawSignal{{}, {}}},
			{Collector: "gitlog", Signals: []signal.RawSignal{{}}},
		},
	}

	entry := BuildHistoryEntry(t.TempDir(), result)

	assert.Equal(t, 3, entry.TotalSignals)
	assert.Equal(t, map[string]int{"todos": 2, "gitlog": 1}, entry.CollectorCounts)
	assert.Equal(t, map[string]int{"todo": 1, "fixme": 1, "churn": 1}, entry.KindCounts)
	assert.False(t, entry.Timestamp.IsZero())
}

func TestHistoryFile_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	h := &ScanHistory{
		Version: "1",
		Entries: []HistoryEntry{{
			Timestamp:       time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
			TotalSignals:    42,
			CollectorCounts: map[string]int{"todos": 42},
			KindCounts:      map[string]int{"todo": 42},
		}},
	}

	require.NoError(t, SaveHistory(dir, h))

	data, err := os.ReadFile(filepath.Join(dir, ".stringer", "scan-history.json"))
	require.NoError(t, err)

	// Verify it's valid pretty-printed JSON.
	assert.True(t, json.Valid(data))
	assert.Contains(t, string(data), "scan-history.json"[:0]+"\"version\"")
}

func TestSortedKeys(t *testing.T) {
	m := map[string]int{"charlie": 3, "alpha": 1, "bravo": 2}
	keys := SortedKeys(m)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, keys)
}

func TestSortedKeys_Empty(t *testing.T) {
	keys := SortedKeys(map[string]int{})
	assert.Empty(t, keys)
}
