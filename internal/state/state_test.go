package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/signal"
)

func TestLoad_NonExistentFile(t *testing.T) {
	s, err := Load(t.TempDir())
	assert.NoError(t, err)
	assert.Nil(t, s)
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".stringer")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "last-scan.json"), []byte("not json"), 0o600))

	s, err := Load(dir)
	assert.Error(t, err)
	assert.Nil(t, s)
}

func TestSave_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	s := &ScanState{
		Version:       "1",
		ScanTimestamp: time.Now().UTC(),
		Collectors:    []string{"todos"},
		SignalHashes:  []string{"abcd1234"},
		SignalCount:   1,
	}

	err := Save(dir, s)
	require.NoError(t, err)

	// Verify directory and file exist.
	info, err := os.Stat(filepath.Join(dir, ".stringer", "last-scan.json"))
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}

func TestSave_Load_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &ScanState{
		Version:       "1",
		ScanTimestamp: time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC),
		GitHead:       "abc123",
		Collectors:    []string{"gitlog", "todos"},
		SignalHashes:  []string{"a1b2c3d4", "e5f6g7h8"},
		SignalCount:   2,
	}

	require.NoError(t, Save(dir, original))

	loaded, err := Load(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, original.Version, loaded.Version)
	assert.Equal(t, original.ScanTimestamp, loaded.ScanTimestamp)
	assert.Equal(t, original.GitHead, loaded.GitHead)
	assert.Equal(t, original.Collectors, loaded.Collectors)
	assert.Equal(t, original.SignalHashes, loaded.SignalHashes)
	assert.Equal(t, original.SignalCount, loaded.SignalCount)
}

func TestSave_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	s := &ScanState{
		Version:      "1",
		Collectors:   []string{"todos"},
		SignalHashes: []string{"deadbeef"},
		SignalCount:  1,
	}

	require.NoError(t, Save(dir, s))

	data, err := os.ReadFile(filepath.Join(dir, ".stringer", "last-scan.json")) //nolint:gosec // test
	require.NoError(t, err)
	assert.True(t, json.Valid(data), "state file is not valid JSON")
}

func TestFilterNew_AllNew(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A"},
		{Source: "todos", Kind: "todo", Title: "B"},
	}

	result := FilterNew(signals, nil)
	assert.Len(t, result, 2)
}

func TestFilterNew_AllExisting(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A"},
		{Source: "todos", Kind: "todo", Title: "B"},
	}

	hashes := make([]string, len(signals))
	for i, s := range signals {
		hashes[i] = pipeline.SignalHash(s)
	}

	prev := &ScanState{SignalHashes: hashes}
	result := FilterNew(signals, prev)
	assert.Empty(t, result)
}

func TestFilterNew_Mixed(t *testing.T) {
	existing := signal.RawSignal{Source: "todos", Kind: "todo", Title: "existing"}
	newSig := signal.RawSignal{Source: "todos", Kind: "todo", Title: "new"}

	prev := &ScanState{
		SignalHashes: []string{pipeline.SignalHash(existing)},
	}

	signals := []signal.RawSignal{existing, newSig}
	result := FilterNew(signals, prev)
	require.Len(t, result, 1)
	assert.Equal(t, "new", result[0].Title)
}

func TestFilterNew_PreservesOrder(t *testing.T) {
	existing := signal.RawSignal{Source: "todos", Kind: "todo", Title: "old"}
	sig1 := signal.RawSignal{Source: "todos", Kind: "todo", Title: "first-new"}
	sig2 := signal.RawSignal{Source: "todos", Kind: "fixme", Title: "second-new"}
	sig3 := signal.RawSignal{Source: "gitlog", Kind: "churn", Title: "third-new"}

	prev := &ScanState{
		SignalHashes: []string{pipeline.SignalHash(existing)},
	}

	signals := []signal.RawSignal{sig1, existing, sig2, sig3}
	result := FilterNew(signals, prev)
	require.Len(t, result, 3)
	assert.Equal(t, "first-new", result[0].Title)
	assert.Equal(t, "second-new", result[1].Title)
	assert.Equal(t, "third-new", result[2].Title)
}

func TestBuild_CapturesGitHead(t *testing.T) {
	// Create a git repo to test HEAD resolution.
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	// Create an initial commit so HEAD exists.
	wt, err := repo.Worktree()
	require.NoError(t, err)
	f, err := os.Create(filepath.Join(dir, "test.txt")) //nolint:gosec // test helper
	require.NoError(t, err)
	require.NoError(t, f.Close())
	_, err = wt.Add("test.txt")
	require.NoError(t, err)
	_, err = wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test"},
	}

	s := Build(dir, []string{"todos"}, signals)
	assert.Equal(t, schemaVersion, s.Version)
	assert.NotEmpty(t, s.GitHead, "should capture git HEAD")
	assert.Len(t, s.GitHead, 40, "git HEAD should be 40-char hex")
	assert.Equal(t, []string{"todos"}, s.Collectors)
	assert.Len(t, s.SignalHashes, 1)
	assert.Equal(t, 1, s.SignalCount)
}

func TestBuild_NonGitRepo(t *testing.T) {
	dir := t.TempDir()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test"},
	}

	s := Build(dir, []string{"todos"}, signals)
	assert.Empty(t, s.GitHead, "non-git repo should have empty HEAD")
	assert.NoError(t, nil) // no error expected
}

func TestBuild_SortsCollectors(t *testing.T) {
	dir := t.TempDir()
	s := Build(dir, []string{"patterns", "todos", "gitlog"}, nil)
	assert.Equal(t, []string{"gitlog", "patterns", "todos"}, s.Collectors)
}

func TestCollectorsMatch_Same(t *testing.T) {
	prev := &ScanState{Collectors: []string{"gitlog", "todos"}}
	assert.True(t, CollectorsMatch(prev, []string{"todos", "gitlog"}))
}

func TestCollectorsMatch_Different(t *testing.T) {
	prev := &ScanState{Collectors: []string{"todos"}}
	assert.False(t, CollectorsMatch(prev, []string{"todos", "gitlog"}))
}

func TestCollectorsMatch_NilPrev(t *testing.T) {
	assert.True(t, CollectorsMatch(nil, []string{"todos"}))
}

func TestFilterNew_EmptyPrevHashes(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A"},
	}

	prev := &ScanState{SignalHashes: nil}
	result := FilterNew(signals, prev)
	assert.Len(t, result, 1, "empty hash list should treat all as new")
}

func TestBuild_PopulatesSignalMetas(t *testing.T) {
	dir := t.TempDir()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "main.go", Line: 42, Title: "implement caching"},
		{Source: "gitlog", Kind: "churn", FilePath: "config.go", Line: 0, Title: "High churn: config.go"},
	}

	s := Build(dir, []string{"todos", "gitlog"}, signals)
	require.Len(t, s.SignalMetas, 2)
	require.Len(t, s.SignalHashes, 2)

	// Verify metas match signal content.
	assert.Equal(t, "todos", s.SignalMetas[0].Source)
	assert.Equal(t, "todo", s.SignalMetas[0].Kind)
	assert.Equal(t, "main.go", s.SignalMetas[0].FilePath)
	assert.Equal(t, 42, s.SignalMetas[0].Line)
	assert.Equal(t, "implement caching", s.SignalMetas[0].Title)
	assert.Equal(t, s.SignalHashes[0], s.SignalMetas[0].Hash)

	assert.Equal(t, "gitlog", s.SignalMetas[1].Source)
	assert.Equal(t, "churn", s.SignalMetas[1].Kind)
	assert.Equal(t, "config.go", s.SignalMetas[1].FilePath)
	assert.Equal(t, 0, s.SignalMetas[1].Line)
	assert.Equal(t, "High churn: config.go", s.SignalMetas[1].Title)
	assert.Equal(t, s.SignalHashes[1], s.SignalMetas[1].Hash)
}

func TestComputeDiff_AllNew(t *testing.T) {
	prev := &ScanState{}
	current := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "aaa", Source: "todos", Kind: "todo", FilePath: "main.go", Line: 10, Title: "A"},
			{Hash: "bbb", Source: "todos", Kind: "todo", FilePath: "main.go", Line: 20, Title: "B"},
		},
	}

	diff := ComputeDiff(prev, current)
	assert.Len(t, diff.Added, 2)
	assert.Empty(t, diff.Removed)
	assert.Empty(t, diff.Moved)
}

func TestComputeDiff_AllRemoved(t *testing.T) {
	prev := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "aaa", Source: "todos", Kind: "todo", FilePath: "old.go", Line: 10, Title: "A"},
			{Hash: "bbb", Source: "todos", Kind: "todo", FilePath: "old.go", Line: 20, Title: "B"},
		},
	}
	current := &ScanState{}

	diff := ComputeDiff(prev, current)
	assert.Empty(t, diff.Added)
	assert.Len(t, diff.Removed, 2)
	assert.Empty(t, diff.Moved)
}

func TestComputeDiff_Mixed(t *testing.T) {
	prev := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "aaa", Source: "todos", Kind: "todo", FilePath: "old.go", Line: 10, Title: "kept"},
			{Hash: "bbb", Source: "todos", Kind: "fixme", FilePath: "old.go", Line: 20, Title: "removed"},
		},
	}
	current := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "aaa", Source: "todos", Kind: "todo", FilePath: "old.go", Line: 10, Title: "kept"},
			{Hash: "ccc", Source: "todos", Kind: "todo", FilePath: "new.go", Line: 5, Title: "added"},
		},
	}

	diff := ComputeDiff(prev, current)
	require.Len(t, diff.Added, 1)
	assert.Equal(t, "added", diff.Added[0].Title)
	require.Len(t, diff.Removed, 1)
	assert.Equal(t, "removed", diff.Removed[0].Title)
	assert.Empty(t, diff.Moved)
}

func TestComputeDiff_MovedSignal(t *testing.T) {
	// Same title+kind, different file path — should be detected as moved.
	prev := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "aaa", Source: "todos", Kind: "todo", FilePath: "parser/old.go", Line: 15, Title: "refactor parser"},
		},
	}
	current := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "bbb", Source: "todos", Kind: "todo", FilePath: "parser/new.go", Line: 22, Title: "refactor parser"},
		},
	}

	diff := ComputeDiff(prev, current)
	assert.Empty(t, diff.Added)
	assert.Empty(t, diff.Removed)
	require.Len(t, diff.Moved, 1)
	assert.Equal(t, "parser/old.go", diff.Moved[0].Previous.FilePath)
	assert.Equal(t, 15, diff.Moved[0].Previous.Line)
	assert.Equal(t, "parser/new.go", diff.Moved[0].Current.FilePath)
	assert.Equal(t, 22, diff.Moved[0].Current.Line)
}

func TestComputeDiff_NoChange(t *testing.T) {
	state := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "aaa", Source: "todos", Kind: "todo", FilePath: "main.go", Line: 10, Title: "A"},
			{Hash: "bbb", Source: "todos", Kind: "todo", FilePath: "main.go", Line: 20, Title: "B"},
		},
	}

	diff := ComputeDiff(state, state)
	assert.Empty(t, diff.Added)
	assert.Empty(t, diff.Removed)
	assert.Empty(t, diff.Moved)
}

func TestAnnotateRemovedSignals_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	// Do NOT create the file — it should be considered deleted.
	removed := []SignalMeta{
		{Source: "todos", Kind: "todo", FilePath: "nonexistent.go", Line: 10, Title: "fix bug"},
	}

	annotated := AnnotateRemovedSignals(dir, removed)
	require.Len(t, annotated, 1)
	assert.Equal(t, "file_deleted", annotated[0].Resolution)
	assert.Equal(t, "fix bug", annotated[0].Title)
}

func TestAnnotateRemovedSignals_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	// Create the file so it is NOT considered deleted.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "exists.go"), []byte("package main"), 0o600))

	removed := []SignalMeta{
		{Source: "todos", Kind: "todo", FilePath: "exists.go", Line: 5, Title: "cleanup"},
	}

	annotated := AnnotateRemovedSignals(dir, removed)
	require.Len(t, annotated, 1)
	assert.Equal(t, "", annotated[0].Resolution)
}

func TestAnnotateRemovedSignals_EmptyFilePath(t *testing.T) {
	dir := t.TempDir()
	removed := []SignalMeta{
		{Source: "gitlog", Kind: "churn", Title: "High churn"},
	}

	annotated := AnnotateRemovedSignals(dir, removed)
	require.Len(t, annotated, 1)
	assert.Equal(t, "", annotated[0].Resolution, "empty file path should not be marked as deleted")
}

func TestFormatDiff_Output(t *testing.T) {
	dir := t.TempDir()
	// Create a file so the "still exists" resolved signal does not get [file deleted].
	require.NoError(t, os.WriteFile(filepath.Join(dir, "legacy.go"), []byte("package main"), 0o600))

	diff := &DiffResult{
		Added: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "main.go", Line: 42, Title: "implement caching"},
			{Source: "gitlog", Kind: "churn", FilePath: "config.go", Title: "High churn: config.go"},
		},
		Removed: []SignalMeta{
			{Source: "todos", Kind: "fixme", FilePath: "old.go", Line: 10, Title: "broken validation"},
			{Source: "patterns", Kind: "large_file", FilePath: "legacy.go", Title: "Large file: legacy.go"},
		},
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "parser/old.go", Line: 15, Title: "refactor parser"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "parser/new.go", Line: 22, Title: "refactor parser"},
			},
		},
	}

	var buf bytes.Buffer
	err := FormatDiff(diff, dir, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Delta scan summary:")
	assert.Contains(t, output, "+ 2 new signal(s)")
	assert.Contains(t, output, "- 2 resolved signal(s)")
	assert.Contains(t, output, "~ 1 moved signal(s)")
	assert.Contains(t, output, "+ [todos] implement caching (main.go:42)")
	assert.Contains(t, output, "+ [gitlog] High churn: config.go (config.go)")
	assert.Contains(t, output, "- [todos] broken validation (old.go:10) [file deleted]")
	assert.Contains(t, output, "- [patterns] Large file: legacy.go (legacy.go)")
	assert.NotContains(t, output, "Large file: legacy.go (legacy.go) [file deleted]")
	assert.Contains(t, output, "~ [todos] refactor parser")
	assert.Contains(t, output, "from: parser/old.go:15")
	assert.Contains(t, output, "to:   parser/new.go:22")
}

func TestFormatDiff_NoChanges(t *testing.T) {
	diff := &DiffResult{}

	var buf bytes.Buffer
	err := FormatDiff(diff, t.TempDir(), &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "no changes")
}

func TestFormatDiff_OnlyAdded(t *testing.T) {
	diff := &DiffResult{
		Added: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "main.go", Line: 1, Title: "new thing"},
		},
	}

	var buf bytes.Buffer
	err := FormatDiff(diff, t.TempDir(), &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "New signals:")
	assert.NotContains(t, output, "Resolved signals:")
	assert.NotContains(t, output, "Moved signals:")
}

func TestFormatDiff_OnlyRemoved(t *testing.T) {
	diff := &DiffResult{
		Removed: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "gone.go", Line: 5, Title: "old thing"},
		},
	}

	var buf bytes.Buffer
	err := FormatDiff(diff, t.TempDir(), &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "New signals:")
	assert.Contains(t, output, "Resolved signals:")
	assert.NotContains(t, output, "Moved signals:")
}

func TestBackwardCompat_V1State(t *testing.T) {
	dir := t.TempDir()
	// Write a v1 state file (no signal_metas field).
	v1State := map[string]interface{}{
		"version":        "1",
		"scan_timestamp": "2026-02-07T12:00:00Z",
		"git_head":       "abc123",
		"collectors":     []string{"todos"},
		"signal_hashes":  []string{"deadbeef", "cafebabe"},
		"signal_count":   2,
	}
	data, err := json.MarshalIndent(v1State, "", "  ")
	require.NoError(t, err)

	stateDir := filepath.Join(dir, ".stringer")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "last-scan.json"), data, 0o600))

	// Load should succeed — v1 states have no SignalMetas.
	loaded, err := Load(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "1", loaded.Version)
	assert.Equal(t, []string{"deadbeef", "cafebabe"}, loaded.SignalHashes)
	assert.Nil(t, loaded.SignalMetas, "v1 state should have nil SignalMetas")
	assert.Equal(t, 2, loaded.SignalCount)

	// FilterNew should still work with v1 state (uses SignalHashes).
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "new signal"},
	}
	filtered := FilterNew(signals, loaded)
	assert.Len(t, filtered, 1, "FilterNew should work with v1 state")

	// ComputeDiff should handle empty SignalMetas gracefully.
	current := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "newmeta", Source: "todos", Kind: "todo", Title: "new"},
		},
	}
	diff := ComputeDiff(loaded, current)
	assert.Len(t, diff.Added, 1, "all current signals should be added vs v1 state")
	assert.Empty(t, diff.Removed, "no removals since v1 has no metas")
}

func TestFormatDiff_OnlyMoved(t *testing.T) {
	diff := &DiffResult{
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "old.go", Line: 5, Title: "move me"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "new.go", Line: 15, Title: "move me"},
			},
		},
	}

	var buf bytes.Buffer
	err := FormatDiff(diff, t.TempDir(), &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "New signals:")
	assert.NotContains(t, output, "Resolved signals:")
	assert.Contains(t, output, "Moved signals:")
	assert.Contains(t, output, "~ 1 moved signal(s)")
	assert.Contains(t, output, "~ [todos] move me")
	assert.Contains(t, output, "from: old.go:5")
	assert.Contains(t, output, "to:   new.go:15")
}

func TestCollectorsMatch_SameLengthDifferentContent(t *testing.T) {
	prev := &ScanState{Collectors: []string{"gitlog", "todos"}}
	assert.False(t, CollectorsMatch(prev, []string{"patterns", "todos"}))
}

func TestSave_ReadOnlyDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a file where the .stringer directory should be, blocking MkdirAll.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer"), []byte("not a dir"), 0o600))

	s := &ScanState{
		Version:      "2",
		Collectors:   []string{"todos"},
		SignalHashes: []string{"abc"},
		SignalCount:  1,
	}

	err := Save(dir, s)
	assert.Error(t, err, "Save should fail when .stringer is a file, not directory")
}

func TestBuild_EmptyGitRepo(t *testing.T) {
	// Create a git repo WITHOUT an initial commit — Head() will fail.
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	s := Build(dir, []string{"todos"}, nil)
	assert.Empty(t, s.GitHead, "empty git repo should have empty HEAD")
}

func TestLoad_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".stringer")
	require.NoError(t, os.MkdirAll(stateDir, 0o750))
	statePath := filepath.Join(stateDir, "last-scan.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"version":"2"}`), 0o600))
	// Make the file unreadable.
	require.NoError(t, os.Chmod(statePath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(statePath, 0o600) })

	s, err := Load(dir)
	assert.Error(t, err)
	assert.Nil(t, s)
}

func TestFormatLocation(t *testing.T) {
	tests := []struct {
		name     string
		meta     SignalMeta
		expected string
	}{
		{
			name:     "with line number",
			meta:     SignalMeta{FilePath: "main.go", Line: 42},
			expected: "main.go:42",
		},
		{
			name:     "without line number",
			meta:     SignalMeta{FilePath: "main.go", Line: 0},
			expected: "main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatLocation(tt.meta))
		})
	}
}

func TestComputeDiff_MovedSameFileNewLine(t *testing.T) {
	// Signal moved within the same file (different line number).
	prev := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "aaa", Source: "todos", Kind: "todo", FilePath: "main.go", Line: 10, Title: "fix this"},
		},
	}
	current := &ScanState{
		SignalMetas: []SignalMeta{
			{Hash: "bbb", Source: "todos", Kind: "todo", FilePath: "main.go", Line: 50, Title: "fix this"},
		},
	}

	diff := ComputeDiff(prev, current)
	assert.Empty(t, diff.Added)
	assert.Empty(t, diff.Removed)
	require.Len(t, diff.Moved, 1)
	assert.Equal(t, 10, diff.Moved[0].Previous.Line)
	assert.Equal(t, 50, diff.Moved[0].Current.Line)
}

func TestBuildResolvedTodoSignals_FiltersToTodosOnly(t *testing.T) {
	dir := t.TempDir()
	removed := []SignalMeta{
		{Source: "todos", Kind: "todo", FilePath: "main.go", Line: 10, Title: "fix bug"},
		{Source: "gitlog", Kind: "churn", FilePath: "config.go", Title: "High churn"},
		{Source: "todos", Kind: "fixme", FilePath: "util.go", Line: 5, Title: "clean up"},
		{Source: "patterns", Kind: "large_file", FilePath: "big.go", Title: "Large file"},
	}

	signals := BuildResolvedTodoSignals(dir, removed)
	require.Len(t, signals, 2)
	assert.Equal(t, "fix bug", signals[0].Title)
	assert.Equal(t, "clean up", signals[1].Title)
	for _, s := range signals {
		assert.Equal(t, "todos", s.Source)
	}
}

func TestBuildResolvedTodoSignals_SignalFields(t *testing.T) {
	dir := t.TempDir()
	removed := []SignalMeta{
		{Source: "todos", Kind: "todo", FilePath: "handler.go", Line: 42, Title: "implement retry logic"},
	}

	before := time.Now()
	signals := BuildResolvedTodoSignals(dir, removed)
	after := time.Now()

	require.Len(t, signals, 1)
	s := signals[0]

	assert.Equal(t, "todos", s.Source)
	assert.Equal(t, "todo", s.Kind)
	assert.Equal(t, "handler.go", s.FilePath)
	assert.Equal(t, 42, s.Line)
	assert.Equal(t, "implement retry logic", s.Title)
	assert.Contains(t, s.Description, "Module: .")
	assert.Contains(t, s.Description, "handler.go:42")
	assert.InEpsilon(t, 0.3, s.Confidence, 0.001)
	assert.Equal(t, []string{"todo", "pre-closed", "resolved", "stringer-generated"}, s.Tags)
	assert.False(t, s.ClosedAt.IsZero(), "ClosedAt should be set")
	assert.True(t, !s.ClosedAt.Before(before) && !s.ClosedAt.After(after), "ClosedAt should be approximately now")
	assert.False(t, s.Timestamp.IsZero(), "Timestamp should be set")
}

func TestBuildResolvedTodoSignals_FileDeleted(t *testing.T) {
	dir := t.TempDir()
	// Do NOT create the file — it should be detected as deleted.
	removed := []SignalMeta{
		{Source: "todos", Kind: "todo", FilePath: "deleted.go", Line: 1, Title: "old todo"},
	}

	signals := BuildResolvedTodoSignals(dir, removed)
	require.Len(t, signals, 1)
	assert.Contains(t, signals[0].Description, "file deleted")
}

func TestBuildResolvedTodoSignals_FileExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "exists.go"), []byte("package main"), 0o600))

	removed := []SignalMeta{
		{Source: "todos", Kind: "fixme", FilePath: "exists.go", Line: 5, Title: "resolved fixme"},
	}

	signals := BuildResolvedTodoSignals(dir, removed)
	require.Len(t, signals, 1)
	assert.NotContains(t, signals[0].Description, "file deleted")
	assert.Contains(t, signals[0].Description, "exists.go:5")
}

func TestBuildResolvedTodoSignals_Empty(t *testing.T) {
	dir := t.TempDir()

	// Empty input.
	signals := BuildResolvedTodoSignals(dir, nil)
	assert.Nil(t, signals)

	// No todos in removed signals.
	signals = BuildResolvedTodoSignals(dir, []SignalMeta{
		{Source: "gitlog", Kind: "churn", FilePath: "x.go", Title: "churn"},
	})
	assert.Nil(t, signals)
}

func TestBuildResolvedTodoSignals_ModuleContext(t *testing.T) {
	dir := t.TempDir()
	removed := []SignalMeta{
		{Source: "todos", Kind: "todo", FilePath: "internal/collectors/todos.go", Line: 10, Title: "deep module"},
		{Source: "todos", Kind: "fixme", FilePath: "cmd/main.go", Line: 5, Title: "shallow module"},
		{Source: "todos", Kind: "todo", FilePath: "README.md", Line: 1, Title: "root level"},
	}

	signals := BuildResolvedTodoSignals(dir, removed)
	require.Len(t, signals, 3)
	assert.Contains(t, signals[0].Description, "Module: internal/collectors")
	assert.Contains(t, signals[1].Description, "Module: cmd")
	assert.Contains(t, signals[2].Description, "Module: .")
}

func TestModuleFromFilePath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"internal/collectors/todos.go", "internal/collectors"},
		{"cmd/stringer/main.go", "cmd/stringer"},
		{"cmd/main.go", "cmd"},
		{"README.md", "."},
		{"go.mod", "."},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, moduleFromFilePath(tt.path))
		})
	}
}

func TestSave_Load_RoundTrip_V2WithMetas(t *testing.T) {
	dir := t.TempDir()
	original := &ScanState{
		Version:       "2",
		ScanTimestamp: time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC),
		GitHead:       "abc123",
		Collectors:    []string{"todos"},
		SignalHashes:  []string{"deadbeef"},
		SignalMetas: []SignalMeta{
			{Hash: "deadbeef", Source: "todos", Kind: "todo", FilePath: "main.go", Line: 42, Title: "test"},
		},
		SignalCount: 1,
	}

	require.NoError(t, Save(dir, original))

	loaded, err := Load(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, original.Version, loaded.Version)
	require.Len(t, loaded.SignalMetas, 1)
	assert.Equal(t, original.SignalMetas[0], loaded.SignalMetas[0])
}
