package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

	// Reset cobra flag "Changed" state and values to avoid test contamination.
	scanCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
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
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	// dry-run with exit code OK returns nil
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_DryRunJSONInProcess(t *testing.T) {
	resetScanFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--dry-run", "--json", "--quiet", "--collectors=todos"})

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
	root := repoRoot(t)
	outFile := filepath.Join(t.TempDir(), "output.jsonl")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "-o", outFile, "--quiet", "--collectors=todos"})

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
	root := repoRoot(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "-o", "/nonexistent/dir/file.jsonl", "--quiet", "--collectors=todos"})

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
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--format=beads", "--quiet", "--collectors=todos"})

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
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--max-issues=2", "--quiet", "--collectors=todos"})

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
	root := repoRoot(t)

	// Spaces around collector name should be trimmed.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--collectors= todos ", "--dry-run", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_NoLLMFlag(t *testing.T) {
	resetScanFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--no-llm", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// no-llm is currently a noop but should not cause errors.
	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_VerboseFlag(t *testing.T) {
	resetScanFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--verbose", "--dry-run", "--collectors=todos"})

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
	root := repoRoot(t)
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--min-confidence=0.9", "--dry-run", "--quiet", "--collectors=todos"})
	err := cmd.Execute()
	require.NoError(t, err)
	// With very high confidence filter, most/all signals should be filtered out.
	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

// -----------------------------------------------------------------------
// --kind flag tests
// -----------------------------------------------------------------------

func TestRunScan_KindFilter(t *testing.T) {
	resetScanFlags()
	root := repoRoot(t)
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--kind=todo", "--dry-run", "--quiet", "--collectors=todos"})
	err := cmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "signal(s) found")
}

func TestRunScan_KindFilterNoMatch(t *testing.T) {
	resetScanFlags()
	root := repoRoot(t)
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--kind=nonexistentkind", "--dry-run", "--quiet", "--collectors=todos"})
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
