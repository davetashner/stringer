package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestVersionDefault(t *testing.T) {
	if Version != "dev" {
		t.Errorf("default Version = %q, want %q", Version, "dev")
	}
}

func TestVersionSubcommand(t *testing.T) {
	// Build the binary with a known version.
	binary := t.TempDir() + "/stringer-test"
	build := exec.Command("go", "build", //nolint:gosec // test helper with fixed args
		"-ldflags", `-X main.Version=v0.1.0-test`,
		"-o", binary,
		".",
	)
	build.Dir, _ = os.Getwd()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	// Run "stringer version" and check output.
	cmd := exec.Command(binary, "version") //nolint:gosec // test helper with fixed args
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer version failed: %v", err)
	}

	got := strings.TrimSpace(string(out))
	want := "stringer v0.1.0-test"
	if got != want {
		t.Errorf("stringer version = %q, want %q", got, want)
	}
}
