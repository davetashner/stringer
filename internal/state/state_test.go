package state

import (
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
