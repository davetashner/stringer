package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("root --help failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "codebase archaeology tool") {
		t.Errorf("root help missing description, got:\n%s", out)
	}
	if !strings.Contains(out, "scan") {
		t.Errorf("root help missing scan subcommand, got:\n%s", out)
	}
	if !strings.Contains(out, "version") {
		t.Errorf("root help missing version subcommand, got:\n%s", out)
	}
}

func TestGlobalFlags(t *testing.T) {
	tests := []struct {
		name string
		flag string
	}{
		{"verbose", "--verbose"},
		{"quiet", "--quiet"},
		{"no-color", "--no-color"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := rootCmd.PersistentFlags().Lookup(strings.TrimPrefix(tt.flag, "--"))
			if f == nil {
				t.Errorf("global flag %s not registered", tt.flag)
			}
		})
	}

	// Verify shorthand aliases.
	v := rootCmd.PersistentFlags().ShorthandLookup("v")
	if v == nil || v.Name != "verbose" {
		t.Error("-v shorthand not registered for --verbose")
	}
	q := rootCmd.PersistentFlags().ShorthandLookup("q")
	if q == nil || q.Name != "quiet" {
		t.Error("-q shorthand not registered for --quiet")
	}
}

func TestScanNotImplemented(t *testing.T) {
	// Build the binary so we can test the actual command output.
	binary := t.TempDir() + "/stringer-test"
	build := exec.Command("go", "build", //nolint:gosec // test helper with fixed args
		"-o", binary,
		".",
	)
	build.Dir, _ = os.Getwd()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	cmd := exec.Command(binary, "scan") //nolint:gosec // test helper with fixed args
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan failed: %v", err)
	}

	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, "not implemented yet") {
		t.Errorf("stringer scan = %q, want 'not implemented yet'", got)
	}
}
