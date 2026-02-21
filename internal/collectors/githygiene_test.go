// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestGitHygieneCollector_Name(t *testing.T) {
	c := &GitHygieneCollector{}
	assert.Equal(t, "githygiene", c.Name())
}

func TestGitHygieneCollector_LargeBinary(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file over the threshold (1 MB).
	data := make([]byte, defaultLargeBinaryThreshold+100)
	data[0] = 0 // null byte makes it binary
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.bin"), data, 0o600))

	// Create a small binary file (under threshold).
	smallData := []byte{0, 1, 2, 3}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.bin"), smallData, 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	largeBinaries := filterByKind(signals, "large-binary")
	assert.Len(t, largeBinaries, 1)
	assert.Contains(t, largeBinaries[0].Title, "big.bin")
	assert.Contains(t, largeBinaries[0].Title, "MB")
	assert.Equal(t, 0.8, largeBinaries[0].Confidence)
	assert.Equal(t, "githygiene", largeBinaries[0].Source)
}

func TestGitHygieneCollector_LargeBinary_LFSTracked(t *testing.T) {
	dir := t.TempDir()

	// Create .gitattributes with LFS pattern.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"),
		[]byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0o600))

	// Create a large binary that matches the LFS pattern.
	data := make([]byte, defaultLargeBinaryThreshold+100)
	data[0] = 0
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.bin"), data, 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	largeBinaries := filterByKind(signals, "large-binary")
	assert.Empty(t, largeBinaries, "LFS-tracked binaries should not be flagged")
}

func TestGitHygieneCollector_MergeConflictMarkers(t *testing.T) {
	dir := t.TempDir()

	content := "line 1\n<<<<<<< HEAD\nour change\n=======\ntheir change\n>>>>>>> branch\nline 7\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "conflict.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	conflicts := filterByKind(signals, "merge-conflict-marker")
	assert.Len(t, conflicts, 1, "should report only one conflict signal per file")
	assert.Equal(t, 2, conflicts[0].Line)
	assert.Equal(t, 0.9, conflicts[0].Confidence)
}

func TestGitHygieneCollector_CommittedSecrets_AWSKey(t *testing.T) {
	dir := t.TempDir()

	content := `package main
const awsKey = "AKIAIOSFODNN7EXAMPLE"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	require.Len(t, secrets, 1)
	assert.Contains(t, secrets[0].Title, "AWS access key")
	assert.Equal(t, 0.7, secrets[0].Confidence)
}

func TestGitHygieneCollector_CommittedSecrets_GitHubToken(t *testing.T) {
	dir := t.TempDir()

	// Generate a fake token of sufficient length (36+ chars).
	token := "ghp_" + strings.Repeat("A", 36)
	content := "TOKEN=" + token + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "env.sh"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	require.Len(t, secrets, 1)
	assert.Contains(t, secrets[0].Title, "GitHub token")
}

func TestGitHygieneCollector_CommittedSecrets_GenericKey(t *testing.T) {
	dir := t.TempDir()

	content := `api_key = "supersecretvalue123456"` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	require.Len(t, secrets, 1)
	assert.Contains(t, secrets[0].Title, "generic secret")
	assert.Equal(t, 0.6, secrets[0].Confidence)
}

func TestGitHygieneCollector_MixedLineEndings(t *testing.T) {
	dir := t.TempDir()

	// Create a file with mixed line endings: some CRLF, some LF.
	// bufio.Scanner strips \n but leaves \r on CRLF lines.
	content := "line1\r\nline2\r\nline3\r\nline4\nline5\nline6\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mixed.txt"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	mixed := filterByKind(signals, "mixed-line-endings")
	require.Len(t, mixed, 1)
	assert.Contains(t, mixed[0].Title, "CRLF")
	assert.Contains(t, mixed[0].Title, "LF")
	assert.Equal(t, 0.7, mixed[0].Confidence)
}

func TestGitHygieneCollector_NoMixedEndings_AllLF(t *testing.T) {
	dir := t.TempDir()

	content := "line1\nline2\nline3\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.txt"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	mixed := filterByKind(signals, "mixed-line-endings")
	assert.Empty(t, mixed)
}

func TestGitHygieneCollector_MinConfidenceFilter(t *testing.T) {
	dir := t.TempDir()

	// Create a generic secret (confidence 0.6) — should be filtered at 0.7.
	content := `api_key = "supersecretvalue123456"` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinConfidence: 0.7,
	})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	assert.Empty(t, secrets, "generic secrets should be filtered at min_confidence 0.7")
}

func TestGitHygieneCollector_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.go"), []byte("package main\n"), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := &GitHygieneCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err)
}

func TestGitHygieneCollector_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	// Put a conflict marker in a vendor file (should be excluded).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor"), 0o750))
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor", "lib.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	conflicts := filterByKind(signals, "merge-conflict-marker")
	assert.Empty(t, conflicts, "vendor files should be excluded by default")
}

func TestGitHygieneCollector_Metrics(t *testing.T) {
	dir := t.TempDir()

	// Set up files that trigger each signal type.
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "conflict.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m, ok := c.Metrics().(*GitHygieneMetrics)
	require.True(t, ok)
	assert.Greater(t, m.FilesScanned, 0)
	assert.Equal(t, 1, m.MergeConflictMarkers)
}

func TestParseLFSPatterns(t *testing.T) {
	dir := t.TempDir()

	attrs := `# Git LFS
*.bin filter=lfs diff=lfs merge=lfs -text
*.png filter=lfs diff=lfs merge=lfs -text

# Not LFS
*.go text=auto
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(attrs), 0o600))

	patterns := parseLFSPatterns(dir)
	assert.Equal(t, []string{"*.bin", "*.png"}, patterns)
}

func TestParseLFSPatterns_NoFile(t *testing.T) {
	dir := t.TempDir()
	patterns := parseLFSPatterns(dir)
	assert.Nil(t, patterns)
}

func TestIsLFSTracked(t *testing.T) {
	patterns := []string{"*.bin", "*.png", "assets/*.psd"}

	assert.True(t, isLFSTracked("foo.bin", patterns))
	assert.True(t, isLFSTracked("dir/bar.png", patterns))
	assert.False(t, isLFSTracked("main.go", patterns))
}

func TestHumanSize(t *testing.T) {
	assert.Equal(t, "500 B", humanSize(500))
	assert.Equal(t, "1.5 KB", humanSize(1500))
	assert.Equal(t, "2.5 MB", humanSize(2_500_000))
	assert.Equal(t, "1.0 GB", humanSize(1_000_000_000))
}

func TestGitHygieneCollector_BinarySkipsTextChecks(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file with conflict markers in it — should NOT trigger
	// merge-conflict-marker since it's binary.
	data := []byte("<<<<<<< HEAD\n\x00binary content\n>>>>>>> branch\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "binary.dat"), data, 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	conflicts := filterByKind(signals, "merge-conflict-marker")
	assert.Empty(t, conflicts, "binary files should not be checked for text patterns")
}

func TestGitHygieneCollector_OneSecretPerLine(t *testing.T) {
	dir := t.TempDir()

	// A line that matches both AWS key and generic secret patterns.
	content := `api_key = "AKIAIOSFODNN7EXAMPLE"` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "both.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	assert.Len(t, secrets, 1, "should only report one secret per line")
}
