package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// resetScanFlags resets all package-level scan flags to their default values.
// This must be called before each test that invokes runScan or the cobra command
// to avoid contamination from previous tests.
func resetScanFlags() {
	scanCollectors = ""
	scanFormat = "beads"
	scanOutput = ""
	scanDryRun = false
	scanDelta = false
	scanNoLLM = false
	scanJSON = false
	scanMaxIssues = 0
	scanMinConfidence = 0
	scanKind = ""
	scanStrict = false
	scanGitDepth = 0
	scanGitSince = ""
	scanExcludeCollectors = ""
	scanWorkspace = ""
	scanNoWorkspaces = false

	// Reset cobra flag "Changed" state and values to avoid test contamination.
	scanCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})

	// Reset slices AFTER VisitAll — pflag's StringSlice.Set("[]") appends a
	// literal "[]" entry rather than clearing, so we must nil out explicitly
	// after the VisitAll loop.
	scanExclude = nil
	scanPaths = nil
}

// fixtureDir returns the testdata/fixtures/sample-repo path (a small directory
// with TODOs inside the git repo). Use this instead of repoRoot for tests that
// exercise flag behavior rather than collector thoroughness — scanning the full
// repo triggers git blame on every file and can exceed the test timeout.
func fixtureDir(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	return filepath.Join(root, "testdata", "fixtures", "sample-repo")
}

// -----------------------------------------------------------------------
// exitCodeError tests
// -----------------------------------------------------------------------

func TestExitError_WithMessage(t *testing.T) {
	err := exitError(ExitInvalidArgs, "bad path %q", "/foo")
	assert.Equal(t, `bad path "/foo"`, err.Error())
	assert.Equal(t, ExitInvalidArgs, err.ExitCode())
}

func TestExitError_EmptyMessagePartialFailure(t *testing.T) {
	err := exitError(ExitPartialFailure, "")
	assert.Equal(t, "stringer: some collectors failed", err.Error())
	assert.Equal(t, ExitPartialFailure, err.ExitCode())
}

func TestExitError_EmptyMessageTotalFailure(t *testing.T) {
	err := exitError(ExitTotalFailure, "")
	assert.Equal(t, "stringer: all collectors failed", err.Error())
	assert.Equal(t, ExitTotalFailure, err.ExitCode())
}

func TestExitError_EmptyMessageUnknownCode(t *testing.T) {
	err := exitError(99, "")
	assert.Equal(t, "stringer: error", err.Error())
	assert.Equal(t, 99, err.ExitCode())
}

func TestExitError_ImplementsErrorInterface(t *testing.T) {
	var err error = exitError(1, "test")
	assert.Error(t, err)
	assert.Equal(t, "test", err.Error())
}

func TestExitCodeError_AsType(t *testing.T) {
	err := exitError(ExitPartialFailure, "partial")
	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitPartialFailure, ece.ExitCode())
	assert.Equal(t, "partial", ece.Error())
}

// -----------------------------------------------------------------------
// computeExitCode tests
// -----------------------------------------------------------------------

func TestComputeExitCode_NoResults(t *testing.T) {
	result := &signal.ScanResult{Results: nil}
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

func TestComputeExitCode_EmptyResults(t *testing.T) {
	result := &signal.ScanResult{Results: []signal.CollectorResult{}}
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

func TestComputeExitCode_AllSuccess(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: nil},
			{Collector: "gitlog", Err: nil},
		},
	}
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

func TestComputeExitCode_PartialFailure(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: nil},
			{Collector: "gitlog", Err: errors.New("git not found")},
		},
	}
	// Non-strict: partial failures are OK, exit 0.
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

func TestComputeExitCode_TotalFailure(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: errors.New("failed")},
			{Collector: "gitlog", Err: errors.New("also failed")},
		},
	}
	assert.Equal(t, ExitTotalFailure, computeExitCode(result, false))
}

func TestComputeExitCode_SingleCollectorSuccess(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: nil},
		},
	}
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

func TestComputeExitCode_SingleCollectorFailure(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: errors.New("boom")},
		},
	}
	assert.Equal(t, ExitTotalFailure, computeExitCode(result, false))
}

func TestComputeExitCode_MostFailed(t *testing.T) {
	// 2 of 3 failed = partial (not all); non-strict returns OK.
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "a", Err: errors.New("fail")},
			{Collector: "b", Err: errors.New("fail")},
			{Collector: "c", Err: nil},
		},
	}
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

// -----------------------------------------------------------------------
// printDryRun tests
// -----------------------------------------------------------------------

func TestPrintDryRun_TextMode(t *testing.T) {
	resetScanFlags()
	scanDryRun = true
	scanJSON = false

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{Title: "fix this", Source: "todos"},
			{Title: "fix that", Source: "todos"},
		},
		Results: []signal.CollectorResult{
			{
				Collector: "todos",
				Signals: []signal.RawSignal{
					{Title: "fix this"},
					{Title: "fix that"},
				},
				Duration: 42 * time.Millisecond,
			},
		},
		Duration: 50 * time.Millisecond,
	}

	err := printDryRun(cmd, result, ExitOK)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "2 signal(s) found")
	assert.Contains(t, out, "todos")
	assert.Contains(t, out, "2 signals")
}

func TestPrintDryRun_TextModeWithError(t *testing.T) {
	resetScanFlags()
	scanDryRun = true
	scanJSON = false

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	result := &signal.ScanResult{
		Signals: nil,
		Results: []signal.CollectorResult{
			{
				Collector: "todos",
				Signals:   nil,
				Duration:  10 * time.Millisecond,
				Err:       errors.New("read error"),
			},
		},
		Duration: 10 * time.Millisecond,
	}

	err := printDryRun(cmd, result, ExitTotalFailure)
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitTotalFailure, ece.ExitCode())

	out := buf.String()
	assert.Contains(t, out, "0 signal(s) found")
	assert.Contains(t, out, "error: read error")
}

func TestPrintDryRun_JSONMode(t *testing.T) {
	resetScanFlags()
	scanDryRun = true
	scanJSON = true

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{Title: "sig1"},
			{Title: "sig2"},
			{Title: "sig3"},
		},
		Results: []signal.CollectorResult{
			{
				Collector: "todos",
				Signals: []signal.RawSignal{
					{Title: "sig1"},
					{Title: "sig2"},
				},
				Duration: 100 * time.Millisecond,
			},
			{
				Collector: "gitlog",
				Signals:   []signal.RawSignal{{Title: "sig3"}},
				Duration:  200 * time.Millisecond,
			},
		},
		Duration: 250 * time.Millisecond,
	}

	err := printDryRun(cmd, result, ExitOK)
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
		Collectors   []struct {
			Name     string `json:"name"`
			Signals  int    `json:"signals"`
			Duration string `json:"duration"`
			Error    string `json:"error,omitempty"`
		} `json:"collectors"`
		Duration string `json:"duration"`
		ExitCode int    `json:"exit_code"`
	}

	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, 3, parsed.TotalSignals)
	assert.Equal(t, 0, parsed.ExitCode)
	require.Len(t, parsed.Collectors, 2)
	assert.Equal(t, "todos", parsed.Collectors[0].Name)
	assert.Equal(t, 2, parsed.Collectors[0].Signals)
	assert.Equal(t, "", parsed.Collectors[0].Error)
	assert.Equal(t, "gitlog", parsed.Collectors[1].Name)
	assert.Equal(t, 1, parsed.Collectors[1].Signals)
}

func TestPrintDryRun_JSONModeWithErrors(t *testing.T) {
	resetScanFlags()
	scanDryRun = true
	scanJSON = true

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	result := &signal.ScanResult{
		Signals: nil,
		Results: []signal.CollectorResult{
			{
				Collector: "todos",
				Signals:   []signal.RawSignal{{Title: "found"}},
				Duration:  50 * time.Millisecond,
			},
			{
				Collector: "gitlog",
				Duration:  30 * time.Millisecond,
				Err:       errors.New("permission denied"),
			},
		},
		Duration: 60 * time.Millisecond,
	}

	err := printDryRun(cmd, result, ExitPartialFailure)
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitPartialFailure, ece.ExitCode())

	var parsed struct {
		Collectors []struct {
			Name  string `json:"name"`
			Error string `json:"error,omitempty"`
		} `json:"collectors"`
		ExitCode int `json:"exit_code"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, ExitPartialFailure, parsed.ExitCode)
	require.Len(t, parsed.Collectors, 2)
	assert.Equal(t, "", parsed.Collectors[0].Error)
	assert.Equal(t, "permission denied", parsed.Collectors[1].Error)
}

func TestPrintDryRun_ExitOKReturnsNil(t *testing.T) {
	resetScanFlags()
	scanDryRun = true
	scanJSON = false

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	result := &signal.ScanResult{
		Signals: nil,
		Results: []signal.CollectorResult{
			{Collector: "todos", Duration: 1 * time.Millisecond},
		},
		Duration: 1 * time.Millisecond,
	}

	err := printDryRun(cmd, result, ExitOK)
	assert.NoError(t, err)
}

func TestPrintDryRun_TextModeMultipleCollectors(t *testing.T) {
	resetScanFlags()
	scanDryRun = true
	scanJSON = false

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{Title: "s1"}, {Title: "s2"}, {Title: "s3"},
		},
		Results: []signal.CollectorResult{
			{
				Collector: "todos",
				Signals:   []signal.RawSignal{{Title: "s1"}, {Title: "s2"}},
				Duration:  100 * time.Millisecond,
			},
			{
				Collector: "gitlog",
				Signals:   []signal.RawSignal{{Title: "s3"}},
				Duration:  200 * time.Millisecond,
			},
		},
		Duration: 250 * time.Millisecond,
	}

	err := printDryRun(cmd, result, ExitOK)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "3 signal(s) found")
	assert.Contains(t, out, "todos: 2 signals")
	assert.Contains(t, out, "gitlog: 1 signals")
}

func TestPrintDryRun_JSONModeZeroSignals(t *testing.T) {
	resetScanFlags()
	scanDryRun = true
	scanJSON = true

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	result := &signal.ScanResult{
		Signals:  nil,
		Results:  nil,
		Duration: 5 * time.Millisecond,
	}

	err := printDryRun(cmd, result, ExitOK)
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
		ExitCode     int `json:"exit_code"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, 0, parsed.TotalSignals)
	assert.Equal(t, 0, parsed.ExitCode)
}

// -----------------------------------------------------------------------
// runScan tests (in-process via cobra)
// -----------------------------------------------------------------------

// newTestCmd creates a fresh root command with scan attached, using isolated
// buffers for stdout and stderr. The returned command and buffers let tests
// verify output without touching the real rootCmd global.
func newTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	// We reuse the global rootCmd because scanCmd is wired to it via init().
	// But we redirect its I/O.
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	return rootCmd, stdout, stderr
}

func TestRunScan_InvalidPath(t *testing.T) {
	resetScanFlags()
	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", "/nonexistent/path/that/does/not/exist"})

	err := cmd.Execute()
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
	assert.Contains(t, ece.Error(), "cannot resolve path")
}

func TestRunScan_PathIsFile(t *testing.T) {
	resetScanFlags()

	// Create a temporary file (not a directory).
	tmp := filepath.Join(t.TempDir(), "somefile.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("hello"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", tmp})

	err := cmd.Execute()
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
	assert.Contains(t, ece.Error(), "not a directory")
}

func TestRunScan_UnknownFormat(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=xml"})

	err := cmd.Execute()
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
	assert.Contains(t, ece.Error(), "unknown format")
}

func TestRunScan_UnknownCollector(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=nonexistent"})

	err := cmd.Execute()
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
	assert.Contains(t, ece.Error(), "nonexistent")
	assert.Contains(t, ece.Error(), "available")
}

func TestRunScan_DryRunInProcess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	// dry-run with exit code OK returns nil
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_DryRunJSONInProcess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
		Collectors   []struct {
			Name    string `json:"name"`
			Signals int    `json:"signals"`
		} `json:"collectors"`
		ExitCode int `json:"exit_code"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed), "output: %s", stdout.String())
	assert.Greater(t, parsed.TotalSignals, 0)
	assert.Equal(t, 0, parsed.ExitCode)
	require.Len(t, parsed.Collectors, 1)
	assert.Equal(t, "todos", parsed.Collectors[0].Name)
}

func TestRunScan_OutputToFile(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	outFile := filepath.Join(t.TempDir(), "output.jsonl")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "-o", outFile, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.NotEmpty(t, lines)

	// Each line should be valid JSON.
	for i, line := range lines {
		assert.True(t, json.Valid([]byte(line)), "line %d is not valid JSON: %s", i, line)
	}
}

func TestRunScan_OutputToInvalidFile(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "-o", "/nonexistent/dir/file.jsonl", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
	assert.Contains(t, ece.Error(), "cannot create output file")
}

func TestRunScan_DefaultPathUsesCurrentDir(t *testing.T) {
	resetScanFlags()

	// We cannot actually change the working directory easily in tests,
	// but we can verify that when no args are passed, the scan uses ".".
	// Since the test runs from cmd/stringer (which is a valid directory),
	// it should not fail with a path error.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_BeadsFormatInProcess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=beads", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.NotEmpty(t, lines)

	// Verify JSONL structure.
	for i, line := range lines {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "line %d: %s", i, line)
		assert.Contains(t, rec, "id")
		assert.Contains(t, rec, "title")
		assert.Contains(t, rec, "status")
	}
}

func TestRunScan_MaxIssuesInProcess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--max-issues=2", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	assert.Equal(t, 2, len(lines), "expected exactly 2 lines with --max-issues=2")
}

func TestRunScan_TooManyArgs(t *testing.T) {
	resetScanFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", "path1", "path2"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestRunScan_CollectorsTrimWhitespace(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	// Spaces around collector name should be trimmed.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors= todos ", "--dry-run", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_NoLLMFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--no-llm", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// no-llm is currently a noop but should not cause errors.
	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_VerboseFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--verbose", "--dry-run", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_SymlinkPath(t *testing.T) {
	resetScanFlags()

	// Create a temp dir and a symlink to it.
	target := t.TempDir()
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "link")
	require.NoError(t, os.Symlink(target, link))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", link, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

// -----------------------------------------------------------------------
// Git depth/since integration tests
// -----------------------------------------------------------------------

func TestRunScan_GitDepthFlag(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--git-depth=50", "--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_GitSinceFlag(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--git-since=30d", "--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_GitDepthAndGitSinceTogether(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--git-depth=100", "--git-since=90d", "--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

// -----------------------------------------------------------------------
// Scan flags tests
// -----------------------------------------------------------------------

func TestScanCmd_FlagsRegistered(t *testing.T) {
	flags := []struct {
		name      string
		shorthand string
	}{
		{"collectors", "c"},
		{"format", "f"},
		{"output", "o"},
		{"dry-run", ""},
		{"no-llm", ""},
		{"json", ""},
		{"max-issues", ""},
		{"min-confidence", ""},
		{"kind", ""},
		{"strict", ""},
		{"git-depth", ""},
		{"git-since", ""},
		{"exclude", "e"},
		{"exclude-collectors", "x"},
		{"paths", ""},
	}

	for _, ff := range flags {
		t.Run(ff.name, func(t *testing.T) {
			f := scanCmd.Flags().Lookup(ff.name)
			require.NotNil(t, f, "flag --%s not registered", ff.name)
			if ff.shorthand != "" {
				s := scanCmd.Flags().ShorthandLookup(ff.shorthand)
				require.NotNil(t, s, "shorthand -%s not registered", ff.shorthand)
				assert.Equal(t, ff.name, s.Name)
			}
		})
	}
}

func TestScanCmd_DefaultFlagValues(t *testing.T) {
	f := scanCmd.Flags().Lookup("format")
	require.NotNil(t, f)
	assert.Equal(t, "beads", f.DefValue)

	f = scanCmd.Flags().Lookup("dry-run")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)

	f = scanCmd.Flags().Lookup("max-issues")
	require.NotNil(t, f)
	assert.Equal(t, "0", f.DefValue)
}

func TestScanCmd_Help(t *testing.T) {
	resetScanFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Scan a repository")
	assert.Contains(t, out, "--collectors")
	assert.Contains(t, out, "--format")
	assert.Contains(t, out, "--dry-run")
}

// -----------------------------------------------------------------------
// Exit code constant tests
// -----------------------------------------------------------------------

func TestExitCodeConstants(t *testing.T) {
	assert.Equal(t, 0, ExitOK)
	assert.Equal(t, 1, ExitInvalidArgs)
	assert.Equal(t, 2, ExitPartialFailure)
	assert.Equal(t, 3, ExitTotalFailure)
}

// -----------------------------------------------------------------------
// Edge cases for computeExitCode
// -----------------------------------------------------------------------

func TestComputeExitCode_LargeResultSet(t *testing.T) {
	// Many collectors, only last one fails; non-strict returns OK.
	results := make([]signal.CollectorResult, 10)
	for i := range results {
		results[i] = signal.CollectorResult{
			Collector: fmt.Sprintf("collector-%d", i),
		}
	}
	results[9].Err = errors.New("last one failed")

	result := &signal.ScanResult{Results: results}
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

func TestComputeExitCode_AllFailedMany(t *testing.T) {
	results := make([]signal.CollectorResult, 5)
	for i := range results {
		results[i] = signal.CollectorResult{
			Collector: fmt.Sprintf("c%d", i),
			Err:       fmt.Errorf("error %d", i),
		}
	}

	result := &signal.ScanResult{Results: results}
	assert.Equal(t, ExitTotalFailure, computeExitCode(result, false))
}

// -----------------------------------------------------------------------
// --strict flag tests for computeExitCode
// -----------------------------------------------------------------------

func TestComputeExitCode_PartialFailure_NotStrict(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: nil},
			{Collector: "gitlog", Err: errors.New("failed")},
		},
	}
	assert.Equal(t, ExitOK, computeExitCode(result, false))
}

func TestComputeExitCode_PartialFailure_Strict(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: nil},
			{Collector: "gitlog", Err: errors.New("failed")},
		},
	}
	assert.Equal(t, ExitPartialFailure, computeExitCode(result, true))
}

func TestComputeExitCode_TotalFailure_Strict(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: errors.New("failed")},
			{Collector: "gitlog", Err: errors.New("also failed")},
		},
	}
	// Total failure is always exit 3, regardless of strict.
	assert.Equal(t, ExitTotalFailure, computeExitCode(result, true))
}

func TestComputeExitCode_AllSuccess_Strict(t *testing.T) {
	result := &signal.ScanResult{
		Results: []signal.CollectorResult{
			{Collector: "todos", Err: nil},
			{Collector: "gitlog", Err: nil},
		},
	}
	// All success is always exit 0, regardless of strict.
	assert.Equal(t, ExitOK, computeExitCode(result, true))
}

func TestComputeExitCode_NoResults_Strict(t *testing.T) {
	result := &signal.ScanResult{Results: nil}
	assert.Equal(t, ExitOK, computeExitCode(result, true))
}

// -----------------------------------------------------------------------
// --min-confidence flag tests
// -----------------------------------------------------------------------

func TestRunScan_MinConfidenceFilter(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--min-confidence=0.9", "--dry-run", "--quiet", "--collectors=todos"})
	err := cmd.Execute()
	require.NoError(t, err)
	// With very high confidence filter, most/all signals should be filtered out.
	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_MinConfidenceTooHigh(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--min-confidence=1.5"})
	err := cmd.Execute()
	require.Error(t, err)
	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
	assert.Contains(t, ece.Error(), "--min-confidence must be between 0.0 and 1.0")
}

func TestRunScan_MinConfidenceNegative(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--min-confidence=-0.5"})
	err := cmd.Execute()
	require.Error(t, err)
	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
}

// -----------------------------------------------------------------------
// --kind flag tests
// -----------------------------------------------------------------------

func TestRunScan_KindFilter(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--kind=todo", "--dry-run", "--quiet", "--collectors=todos"})
	err := cmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_KindFilterNoMatch(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--kind=nonexistentkind", "--dry-run", "--quiet", "--collectors=todos"})
	err := cmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "0 signal(s) found")
}

// -----------------------------------------------------------------------
// New flag registration tests
// -----------------------------------------------------------------------

func TestScanCmd_MinConfidenceFlagRegistered(t *testing.T) {
	f := scanCmd.Flags().Lookup("min-confidence")
	require.NotNil(t, f, "flag --min-confidence not registered")
	assert.Equal(t, "0", f.DefValue)
}

func TestScanCmd_KindFlagRegistered(t *testing.T) {
	f := scanCmd.Flags().Lookup("kind")
	require.NotNil(t, f, "flag --kind not registered")
	assert.Equal(t, "", f.DefValue)
}

func TestScanCmd_StrictFlagRegistered(t *testing.T) {
	f := scanCmd.Flags().Lookup("strict")
	require.NotNil(t, f, "flag --strict not registered")
	assert.Equal(t, "false", f.DefValue)
}

// -----------------------------------------------------------------------
// Subdirectory scan test
// -----------------------------------------------------------------------

func TestRunScan_SubdirectoryFindsGitRoot(t *testing.T) {
	// This test verifies the git root discovery logic via the compiled binary,
	// avoiding cobra's shared rootCmd state issues in the test suite.
	binary := buildBinary(t)

	// Create a fresh git repo with a subdirectory.
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "main.go"), []byte("// TODO: test sub scan\n"), 0o600))

	// Initialize git repo at root.
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		gitCmd := exec.Command("git", args...) //nolint:gosec // test helper with controlled args
		gitCmd.Dir = dir
		out, err := gitCmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	// Run the binary pointing at the subdirectory.
	cmd := exec.Command(binary, "scan", subDir, "--dry-run", "--json", "--quiet", "--collectors=todos") //nolint:gosec // test helper
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "total_signals")
}

// -----------------------------------------------------------------------
// Git depth/since flag default value tests
// -----------------------------------------------------------------------

func TestScanCmd_GitDepthDefaultValue(t *testing.T) {
	f := scanCmd.Flags().Lookup("git-depth")
	require.NotNil(t, f)
	assert.Equal(t, "0", f.DefValue)
}

func TestScanCmd_GitSinceDefaultValue(t *testing.T) {
	f := scanCmd.Flags().Lookup("git-since")
	require.NotNil(t, f)
	assert.Equal(t, "", f.DefValue)
}

// -----------------------------------------------------------------------
// --exclude flag tests (use buildBinary to avoid cobra global state issues)
// -----------------------------------------------------------------------

func TestRunScan_ExcludeFlag(t *testing.T) {
	binary := buildBinary(t)

	// Create a temp directory with source files containing TODOs in two directories.
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	testDir := filepath.Join(dir, "tests")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main.go"),
		[]byte("package main\n// TODO: fix this\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "helper.go"),
		[]byte("package tests\n// TODO: excluded todo\n"), 0o600))

	// Scan with --exclude=tests/**: should only find the src TODO.
	cmd := exec.Command(binary, "scan", dir, "--dry-run", "--json", "--quiet", //nolint:gosec // test helper
		"--collectors=todos", "--exclude=tests/**")
	stdout, err := cmd.Output()
	require.NoError(t, err, "stderr: %s", stderr(cmd))

	var result struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout, &result), "output: %s", stdout)
	assert.Equal(t, 1, result.TotalSignals, "expected 1 signal with --exclude=tests/**")
}

func TestRunScan_ExcludeMultiplePatterns(t *testing.T) {
	binary := buildBinary(t)

	dir := t.TempDir()
	for _, sub := range []string{"src", "docs", "extra"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, sub, "file.go"),
			[]byte("package x\n// TODO: something\n"), 0o600))
	}

	cmd := exec.Command(binary, "scan", dir, "--dry-run", "--json", "--quiet", //nolint:gosec // test helper
		"--collectors=todos", "--exclude=docs/**,extra/**")
	stdout, err := cmd.Output()
	require.NoError(t, err, "stderr: %s", stderr(cmd))

	var result struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout, &result), "output: %s", stdout)
	// docs/** and extra/** are excluded. Only src/file.go should remain.
	assert.Equal(t, 1, result.TotalSignals, "expected 1 signal with multiple excludes")
}

// -----------------------------------------------------------------------
// --exclude-collectors flag tests
// -----------------------------------------------------------------------

func TestApplyCollectorExclusions_EmptyExclude(t *testing.T) {
	result := applyCollectorExclusions([]string{"todos", "gitlog"}, "")
	assert.Equal(t, []string{"todos", "gitlog"}, result)
}

func TestApplyCollectorExclusions_EmptyInclude(t *testing.T) {
	// When include is empty, starts from collector.List() and removes excluded.
	result := applyCollectorExclusions(nil, "github")
	assert.NotContains(t, result, "github")
	assert.Greater(t, len(result), 0)
}

func TestApplyCollectorExclusions_ExcludeFromInclude(t *testing.T) {
	result := applyCollectorExclusions([]string{"todos", "gitlog", "patterns"}, "gitlog")
	assert.Equal(t, []string{"todos", "patterns"}, result)
}

func TestApplyCollectorExclusions_MultipleExcludes(t *testing.T) {
	result := applyCollectorExclusions([]string{"todos", "gitlog", "patterns"}, "gitlog,patterns")
	assert.Equal(t, []string{"todos"}, result)
}

func TestApplyCollectorExclusions_WhitespaceHandling(t *testing.T) {
	result := applyCollectorExclusions([]string{"todos", "gitlog"}, " gitlog , ")
	assert.Equal(t, []string{"todos"}, result)
}

func TestScan_ExcludeCollectors(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--exclude-collectors=github", "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		Collectors []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed), "output: %s", stdout.String())

	for _, c := range parsed.Collectors {
		assert.NotEqual(t, "github", c.Name, "github collector should be excluded")
	}
}

func TestScan_ExcludeWithInclude(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos,gitlog", "-x", "gitlog", "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		Collectors []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed), "output: %s", stdout.String())

	require.Len(t, parsed.Collectors, 1)
	assert.Equal(t, "todos", parsed.Collectors[0].Name)
}

func TestScanCmd_ExcludeCollectorsFlagRegistered(t *testing.T) {
	f := scanCmd.Flags().Lookup("exclude-collectors")
	require.NotNil(t, f, "flag --exclude-collectors not registered")
	assert.Equal(t, "", f.DefValue)

	s := scanCmd.Flags().ShorthandLookup("x")
	require.NotNil(t, s, "shorthand -x not registered")
	assert.Equal(t, "exclude-collectors", s.Name)
}

// -----------------------------------------------------------------------
// --paths flag tests
// -----------------------------------------------------------------------

func TestScanCmd_PathsFlag_RegisteredAndDefaults(t *testing.T) {
	f := scanCmd.Flags().Lookup("paths")
	require.NotNil(t, f, "flag --paths not registered")
	assert.Equal(t, "[]", f.DefValue)

	// Should not have a shorthand.
	s := scanCmd.Flags().ShorthandLookup("p")
	if s != nil {
		assert.NotEqual(t, "paths", s.Name, "--paths should not use -p shorthand")
	}
}

func TestScanCmd_PathsFlag_SinglePath(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.go"),
		[]byte("package main\n// TODO: fix this\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--paths=src/main.go", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestScanCmd_PathsFlag_MultiplePaths(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()
	for _, sub := range []string{"cmd", "internal"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, sub, "file.go"),
			[]byte("package x\n// TODO: something\n"), 0o600))
	}

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--paths=cmd/**,internal/**", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestScanCmd_PathsFlag_Integration(t *testing.T) {
	binary := buildBinary(t)

	// Create a temp directory with source files containing TODOs in two directories.
	dir := t.TempDir()
	fooDir := filepath.Join(dir, "foo")
	bazDir := filepath.Join(dir, "baz")
	require.NoError(t, os.MkdirAll(fooDir, 0o750))
	require.NoError(t, os.MkdirAll(bazDir, 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(fooDir, "bar.go"),
		[]byte("package foo\n// TODO: fix this\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(bazDir, "qux.go"),
		[]byte("package baz\n// TODO: fix that\n"), 0o600))

	// Scan with --paths=foo/**: should only find the foo/bar.go TODO.
	cmd := exec.Command(binary, "scan", dir, "--dry-run", "--json", "--quiet", //nolint:gosec // test helper
		"--collectors=todos", "--paths=foo/**")
	stdout, err := cmd.Output()
	require.NoError(t, err, "stderr: %s", stderr(cmd))

	var result struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout, &result), "output: %s", stdout)
	assert.Equal(t, 1, result.TotalSignals, "expected 1 signal with --paths=foo/**")
}

// -----------------------------------------------------------------------
// --include-demo-paths flag tests
// -----------------------------------------------------------------------

func TestRunScan_IncludeDemoPathsFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--include-demo-paths", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

// -----------------------------------------------------------------------
// --collector-timeout flag tests
// -----------------------------------------------------------------------

func TestRunScan_CollectorTimeoutFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collector-timeout=5m", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

// -----------------------------------------------------------------------
// --include-closed and --history-depth flag tests
// -----------------------------------------------------------------------

func TestRunScan_IncludeClosedFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--include-closed", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestRunScan_HistoryDepthFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--history-depth=90d", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// --anonymize flag tests
// -----------------------------------------------------------------------

func TestRunScan_AnonymizeFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--anonymize=always", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Output format variation tests
// -----------------------------------------------------------------------

func TestRunScan_JSONFormatInProcess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// JSON output should be valid JSON.
	var result json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
}

func TestRunScan_MarkdownFormatInProcess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=markdown", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "#")
}

func TestRunScan_TasksFormatInProcess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=tasks", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.NotEmpty(t, stdout.String())
}

// -----------------------------------------------------------------------
// --strict flag integration tests
// -----------------------------------------------------------------------

func TestRunScan_StrictWithAllSuccess(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--strict", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// --delta flag tests
// -----------------------------------------------------------------------

func TestRunScan_DeltaFlagFirstRun(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Create a file with a TODO.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: first run\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--delta", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestRunScan_DeltaSecondRun(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: delta test\n"), 0o600))

	// First scan to establish state.
	cmd1, _, _ := newTestCmd()
	cmd1.SetArgs([]string{"scan", dir, "--delta", "--quiet", "--collectors=todos", "-o", filepath.Join(t.TempDir(), "out1.jsonl")})
	require.NoError(t, cmd1.Execute())

	// Second scan — same signals should be filtered out.
	resetScanFlags()
	cmd2, stdout2, _ := newTestCmd()
	cmd2.SetArgs([]string{"scan", dir, "--delta", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd2.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout2.String(), "0 signal(s) found")
}

// -----------------------------------------------------------------------
// --delta with state mismatch test
// -----------------------------------------------------------------------

func TestRunScan_DeltaCollectorMismatch(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: mismatch test\n"), 0o600))

	// First scan with todos only to establish state.
	cmd1, _, _ := newTestCmd()
	cmd1.SetArgs([]string{"scan", dir, "--delta", "--quiet", "--collectors=todos", "-o", filepath.Join(t.TempDir(), "out.jsonl")})
	require.NoError(t, cmd1.Execute())

	// Second scan with different collectors — should warn about mismatch.
	resetScanFlags()
	cmd2, stdout2, _ := newTestCmd()
	cmd2.SetArgs([]string{"scan", dir, "--delta", "--dry-run", "--quiet", "--collectors=todos,patterns"})

	err := cmd2.Execute()
	require.NoError(t, err)
	// With collector mismatch, all signals should be treated as new.
	assert.Contains(t, stdout2.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Beads-aware dedup path tests
// -----------------------------------------------------------------------

func TestRunScan_BeadsAwareDedup(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: beads test\n"), 0o600))

	// Create a .beads directory with an existing issue.
	beadsDir := filepath.Join(dir, ".beads")
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))
	beadsContent := `{"id":"str-12345678","title":"beads test","status":"open","type":"task"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(beadsContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Config file disabled collector tests
// -----------------------------------------------------------------------

func TestRunScan_DisabledCollectorInConfig(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Create a config with github disabled.
	configContent := `collectors:
  github:
    enabled: false
  todos:
    enabled: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"), []byte(configContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: config test\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		Collectors []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed), "output: %s", stdout.String())

	// github should not appear in collectors.
	for _, c := range parsed.Collectors {
		assert.NotEqual(t, "github", c.Name, "github collector should be disabled via config")
	}
}

// stderr is a helper that captures stderr from a failed exec.Command.
// It returns an empty string if the command has not been run yet.
func stderr(cmd *exec.Cmd) string {
	if cmd.Stderr == nil {
		return ""
	}
	if buf, ok := cmd.Stderr.(*bytes.Buffer); ok {
		return buf.String()
	}
	return ""
}
