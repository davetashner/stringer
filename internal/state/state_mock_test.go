// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package state

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/testable"
)

// --- Load mock tests ---

func TestLoad_MockReadFileError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(_ string) ([]byte, error) {
			return nil, fmt.Errorf("I/O error")
		},
	}

	s, err := Load("/fake/repo")
	assert.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "I/O error")
}

func TestLoad_MockFileNotExist(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(_ string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}

	s, err := Load("/fake/repo")
	assert.NoError(t, err)
	assert.Nil(t, s, "file not found should return nil state")
}

// --- Save mock tests ---

func TestSave_MockMkdirAllFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		MkdirAllFn: func(_ string, _ os.FileMode) error {
			return fmt.Errorf("permission denied")
		},
	}

	s := &ScanState{
		Version:      "2",
		Collectors:   []string{"todos"},
		SignalHashes: []string{"abc"},
		SignalCount:  1,
	}

	err := Save("/fake/repo", s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestSave_MockWriteFileFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		MkdirAllFn: func(_ string, _ os.FileMode) error {
			return nil // directory creation succeeds
		},
		WriteFileFn: func(_ string, _ []byte, _ os.FileMode) error {
			return fmt.Errorf("disk full")
		},
	}

	s := &ScanState{
		Version:      "2",
		Collectors:   []string{"todos"},
		SignalHashes: []string{"abc"},
		SignalCount:  1,
	}

	err := Save("/fake/repo", s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

// --- resolveHead mock tests ---

func TestResolveHead_MockPlainOpenFailure(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	GitOpener = &testable.MockGitOpener{
		OpenErr: fmt.Errorf("not a git repo"),
	}

	head := resolveHead("/not/a/repo")
	assert.Empty(t, head)
}

func TestResolveHead_MockHeadFailure(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			HeadErr: fmt.Errorf("reference not found"),
		},
	}

	head := resolveHead("/some/repo")
	assert.Empty(t, head)
}

func TestResolveHead_MockSuccess(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	sha := plumbing.NewHash("abcdef0123456789abcdef0123456789abcdef01")
	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			HeadRef: plumbing.NewHashReference(plumbing.HEAD, sha),
		},
	}

	head := resolveHead("/some/repo")
	assert.Equal(t, sha.String(), head)
}

// --- AnnotateRemovedSignals mock tests ---

func TestAnnotateRemovedSignals_MockStatError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		StatFn: func(_ string) (os.FileInfo, error) {
			// Return an error that is NOT ErrNotExist — should not mark as file_deleted.
			return nil, fmt.Errorf("permission denied")
		},
	}

	removed := []SignalMeta{
		{Source: "todos", Kind: "todo", FilePath: "main.go", Line: 10, Title: "test"},
	}

	annotated := AnnotateRemovedSignals("/fake/repo", removed)
	require.Len(t, annotated, 1)
	assert.Equal(t, "", annotated[0].Resolution, "non-ErrNotExist should not be marked as file_deleted")
}

// --- FormatDiff error writer tests ---

// errorWriter is a writer that fails after a configurable number of successful writes.
type errorWriter struct {
	failAfter int
	count     int
}

func (w *errorWriter) Write(p []byte) (int, error) {
	w.count++
	if w.count > w.failAfter {
		return 0, fmt.Errorf("write error")
	}
	return len(p), nil
}

func TestFormatDiff_NoChangesWriteError(t *testing.T) {
	diff := &DiffResult{}

	w := &errorWriter{failAfter: 0}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write error")
}

func TestFormatDiff_SummaryHeaderWriteError(t *testing.T) {
	diff := &DiffResult{
		Added: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "A"},
		},
	}

	// Fail on the first write (summary header).
	w := &errorWriter{failAfter: 0}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_AddedCountWriteError(t *testing.T) {
	diff := &DiffResult{
		Added: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "A"},
		},
	}

	// Fail on second write (added count line).
	w := &errorWriter{failAfter: 1}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_RemovedCountWriteError(t *testing.T) {
	diff := &DiffResult{
		Removed: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "R"},
		},
	}

	// Fail on second write (removed count line, after summary header).
	w := &errorWriter{failAfter: 1}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_MovedCountWriteError(t *testing.T) {
	diff := &DiffResult{
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "old.go", Line: 5, Title: "M"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "new.go", Line: 10, Title: "M"},
			},
		},
	}

	// Fail on second write (moved count line, after summary header).
	w := &errorWriter{failAfter: 1}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_NewSignalsHeaderWriteError(t *testing.T) {
	diff := &DiffResult{
		Added: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "A"},
		},
	}

	// summary header (1) + added count (2) + "New signals:" header (3) — fail on 3rd.
	w := &errorWriter{failAfter: 2}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_NewSignalsItemWriteError(t *testing.T) {
	diff := &DiffResult{
		Added: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "A"},
		},
	}

	// summary header (1) + added count (2) + "New signals:" header (3) + item (4) — fail on 4th.
	w := &errorWriter{failAfter: 3}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_ResolvedSignalsHeaderWriteError(t *testing.T) {
	diff := &DiffResult{
		Removed: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "r.go", Line: 1, Title: "R"},
		},
	}

	// summary header (1) + removed count (2) + "Resolved signals:" header (3) — fail on 3rd.
	w := &errorWriter{failAfter: 2}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_ResolvedSignalsItemWriteError(t *testing.T) {
	diff := &DiffResult{
		Removed: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "r.go", Line: 1, Title: "R"},
		},
	}

	// summary header (1) + removed count (2) + header (3) + item (4) — fail on 4th.
	w := &errorWriter{failAfter: 3}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_MovedSignalsHeaderWriteError(t *testing.T) {
	diff := &DiffResult{
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "old.go", Line: 5, Title: "M"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "new.go", Line: 10, Title: "M"},
			},
		},
	}

	// summary header (1) + moved count (2) + "Moved signals:" header (3) — fail on 3rd.
	w := &errorWriter{failAfter: 2}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_MovedSignalsTitleWriteError(t *testing.T) {
	diff := &DiffResult{
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "old.go", Line: 5, Title: "M"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "new.go", Line: 10, Title: "M"},
			},
		},
	}

	// summary header (1) + moved count (2) + header (3) + title line (4) — fail on 4th.
	w := &errorWriter{failAfter: 3}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_MovedSignalsFromWriteError(t *testing.T) {
	diff := &DiffResult{
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "old.go", Line: 5, Title: "M"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "new.go", Line: 10, Title: "M"},
			},
		},
	}

	// summary header (1) + moved count (2) + header (3) + title (4) + from (5) — fail on 5th.
	w := &errorWriter{failAfter: 4}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_MovedSignalsToWriteError(t *testing.T) {
	diff := &DiffResult{
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "old.go", Line: 5, Title: "M"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "new.go", Line: 10, Title: "M"},
			},
		},
	}

	// summary header (1) + moved count (2) + header (3) + title (4) + from (5) + to (6) — fail on 6th.
	w := &errorWriter{failAfter: 5}
	err := FormatDiff(diff, t.TempDir(), w)
	require.Error(t, err)
}

func TestFormatDiff_AllSectionsComplete(t *testing.T) {
	// Verify all sections write successfully when no error.
	diff := &DiffResult{
		Added: []SignalMeta{
			{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "A"},
		},
		Removed: []SignalMeta{
			{Source: "todos", Kind: "fixme", FilePath: "r.go", Line: 2, Title: "R"},
		},
		Moved: []MovedSignal{
			{
				Previous: SignalMeta{Source: "todos", Kind: "todo", FilePath: "old.go", Line: 5, Title: "M"},
				Current:  SignalMeta{Source: "todos", Kind: "todo", FilePath: "new.go", Line: 10, Title: "M"},
			},
		},
	}

	var buf bytes.Buffer
	err := FormatDiff(diff, t.TempDir(), &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "+ 1 new signal(s)")
	assert.Contains(t, output, "- 1 resolved signal(s)")
	assert.Contains(t, output, "~ 1 moved signal(s)")
	assert.Contains(t, output, "New signals:")
	assert.Contains(t, output, "Resolved signals:")
	assert.Contains(t, output, "Moved signals:")
}

// --- Build mock tests ---

func TestBuild_MockResolveHead(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	sha := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")
	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			HeadRef: plumbing.NewHashReference(plumbing.HEAD, sha),
		},
	}

	s := Build("/fake/repo", []string{"todos"}, nil)
	assert.Equal(t, sha.String(), s.GitHead)
}

func TestBuild_MockResolveHeadFailure(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	GitOpener = &testable.MockGitOpener{
		OpenErr: fmt.Errorf("not a git repo"),
	}

	s := Build("/fake/repo", []string{"todos"}, nil)
	assert.Empty(t, s.GitHead, "should have empty git head when open fails")
}
