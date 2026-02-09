package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// resetReportFlags resets all package-level report flags to their default values.
func resetReportFlags() {
	reportCollectors = ""
	reportSections = ""
	reportOutput = ""
	reportGitDepth = 0
	reportGitSince = ""
	reportAnonymize = "auto"

	reportCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	if h := reportCmd.Flags().Lookup("help"); h != nil {
		_ = h.Value.Set("false")
	}

	// Reset slices AFTER VisitAll — pflag's StringSlice.Set("[]") appends a
	// literal "[]" entry rather than clearing.
	reportPaths = nil
}

func TestReportCmd_Exists(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "report" {
			found = true
			break
		}
	}
	assert.True(t, found, "report command should be registered on root")
}

func TestReportCmd_DefaultPath(t *testing.T) {
	resetReportFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Stringer Report")
	assert.Contains(t, out, "Collector Results")
	assert.Contains(t, out, "Signal Summary")
}

func TestReportCmd_ExplicitPath(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Stringer Report")
	assert.Contains(t, out, root)
}

func TestReportCmd_InvalidPath(t *testing.T) {
	resetReportFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", "/nonexistent/path/that/does/not/exist"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestReportCmd_FileNotDir(t *testing.T) {
	resetReportFlags()
	tmp := filepath.Join(t.TempDir(), "somefile.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("hello"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", tmp})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestReportCmd_OutputFile(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)
	outFile := filepath.Join(t.TempDir(), "report.txt")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "-o", outFile, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test file
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "Stringer Report")
	assert.Contains(t, content, "Signal Summary")
}

func TestReportCmd_FlagsRegistered(t *testing.T) {
	flags := []struct {
		name      string
		shorthand string
	}{
		{"collectors", "c"},
		{"sections", ""},
		{"output", "o"},
		{"git-depth", ""},
		{"git-since", ""},
		{"anonymize", ""},
		{"paths", ""},
	}

	for _, ff := range flags {
		t.Run(ff.name, func(t *testing.T) {
			f := reportCmd.Flags().Lookup(ff.name)
			require.NotNil(t, f, "flag --%s not registered", ff.name)
			if ff.shorthand != "" {
				s := reportCmd.Flags().ShorthandLookup(ff.shorthand)
				require.NotNil(t, s, "shorthand -%s not registered", ff.shorthand)
				assert.Equal(t, ff.name, s.Name)
			}
		})
	}
}

func TestReportCmd_Help(t *testing.T) {
	resetReportFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "health report")
	assert.Contains(t, out, "--output")
	assert.Contains(t, out, "--collectors")
}

func TestReportCmd_UnknownCollector(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "-c", "nonexistent"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "available")
}

func TestReportCmd_SingleCollector(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "-c", "todos", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "todos")
}

func TestReportCmd_InRootHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.True(t, strings.Contains(out, "report"), "root help should list report subcommand")
}

func TestReportCmd_SectionsFilter(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "--sections=lottery-risk", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Lottery Risk")
	assert.NotContains(t, out, "Code Churn")
	assert.NotContains(t, out, "TODO Age")
	assert.NotContains(t, out, "Test Coverage Gaps")
}

func TestReportCmd_UnknownSection(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "--sections=nonexistent", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Warning: unknown section")
	assert.Contains(t, out, "nonexistent")
}

func TestReportCmd_SectionSkipWhenCollectorNotRun(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)

	// Run only the todos collector, but request the lottery-risk section.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "-c", "todos", "--sections=lottery-risk", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "lottery-risk: skipped (collector not run)")
}

func TestReportCmd_AllSectionsDefault(t *testing.T) {
	resetReportFlags()
	root := repoRoot(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"report", root, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	// All sections should appear when no --sections filter.
	assert.Contains(t, out, "Lottery Risk")
	assert.Contains(t, out, "Code Churn")
	assert.Contains(t, out, "TODO Age Distribution")
	assert.Contains(t, out, "Test Coverage Gaps")
}

func TestRenderReport_EmptyResult(t *testing.T) {
	result := &signal.ScanResult{
		Duration: 100 * time.Millisecond,
		Metrics:  map[string]any{},
	}

	var buf bytes.Buffer
	err := renderReport(result, "/tmp/test", []string{"todos"}, nil, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Stringer Report")
	assert.Contains(t, out, "/tmp/test")
	assert.Contains(t, out, "Total signals: 0")
	// With no metrics, all sections should show "skipped".
	assert.Contains(t, out, "skipped (collector not run)")
}

func TestRenderReport_SelectedSections(t *testing.T) {
	result := &signal.ScanResult{
		Duration: 50 * time.Millisecond,
		Metrics:  map[string]any{},
	}

	var buf bytes.Buffer
	err := renderReport(result, "/tmp/test", []string{"todos"}, []string{"churn"}, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "churn: skipped")
	assert.NotContains(t, out, "Lottery Risk")
}

func TestResolveSections_UnknownPrintsWarning(t *testing.T) {
	var buf bytes.Buffer
	names := resolveSections([]string{"lottery-risk", "nonexistent"}, &buf)

	assert.Equal(t, []string{"lottery-risk"}, names)
	assert.Contains(t, buf.String(), "Warning: unknown section")
	assert.Contains(t, buf.String(), "nonexistent")
}

func TestResolveSections_EmptyReturnsAll(t *testing.T) {
	var buf bytes.Buffer
	names := resolveSections(nil, &buf)
	assert.NotEmpty(t, names)
	assert.Empty(t, buf.String())
}
