// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// initRootMemberMonorepo builds a go.work monorepo whose root is itself a
// member (`use .`, as in kubernetes) with a member a and a member nested
// inside a. Every workspace holds one TODO and one untested source file;
// root and a also hold one large (and therefore also untested) file. The
// root has an internal/a directory that must not be mistaken for the
// member a.
func initRootMemberMonorepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	source := func(pkg, todo string) string {
		var b strings.Builder
		b.WriteString("package " + pkg + "\n\n// TODO: " + todo + "\nfunc Run() int {\n\tn := 0\n")
		for i := 0; i < 20; i++ {
			b.WriteString("\tn++\n")
		}
		b.WriteString("\treturn n\n}\n")
		return b.String()
	}
	large := func(pkg string) string {
		var b strings.Builder
		b.WriteString("package " + pkg + "\n\nvar lines = []int{\n")
		for i := 0; i < 1600; i++ {
			b.WriteString("\t1,\n")
		}
		b.WriteString("}\n")
		return b.String()
	}

	writeTestFile(t, dir, "go.work", "go 1.24\n\nuse (\n\t.\n\t./a\n\t./a/nested\n)\n")
	writeTestFile(t, dir, "go.mod", "module root\n\ngo 1.24\n")
	writeTestFile(t, dir, "rootsvc.go", source("root", "root todo"))
	writeTestFile(t, dir, "rootbig.go", large("root"))
	writeTestFile(t, dir, "internal/a/keep.go", source("a", "internal a todo"))
	writeTestFile(t, dir, "a/go.mod", "module a\n\ngo 1.24\n")
	writeTestFile(t, dir, "a/asvc.go", source("a", "member a todo"))
	writeTestFile(t, dir, "a/abig.go", large("a"))
	writeTestFile(t, dir, "a/nested/go.mod", "module nested\n\ngo 1.24\n")
	writeTestFile(t, dir, "a/nested/nsvc.go", source("nested", "nested todo"))

	runGitCmd(t, dir, "init", "-q")
	runGitCmd(t, dir, "add", "-A")
	runGitCmd(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init")
	return dir
}

// scanSignalPaths runs scan with the todos and patterns collectors and
// returns every signal as "workspace kind path:line", sorted.
func scanSignalPaths(t *testing.T, binary, dir string, extra ...string) []string {
	t.Helper()
	args := append([]string{"scan", dir, "-f", "json", "-c", "todos,patterns", "--quiet"}, extra...)
	cmd := exec.Command(binary, args...) //nolint:gosec // test helper
	out, err := cmd.Output()
	require.NoError(t, err, "stringer scan failed")
	var result struct {
		Signals []signal.RawSignal `json:"signals"`
	}
	require.NoError(t, json.Unmarshal(out, &result))
	keys := make([]string, 0, len(result.Signals))
	for _, s := range result.Signals {
		keys = append(keys, s.Workspace+" "+s.Kind+" "+filepath.ToSlash(s.FilePath)+":"+strconv.Itoa(s.Line))
	}
	sort.Strings(keys)
	return keys
}

func TestScan_RootMemberWorkspaceReportsEachFileOnce(t *testing.T) {
	binary := buildBinary(t)
	dir := initRootMemberMonorepo(t)

	got := scanSignalPaths(t, binary, dir)

	// Each TODO, missing-tests and large-file signal appears exactly once,
	// owned by the workspace the file belongs to; internal/a stays with the
	// root even though a member is named a.
	want := []string{
		". large-file rootbig.go:0",
		". missing-tests internal/a/keep.go:0",
		". missing-tests rootbig.go:0",
		". missing-tests rootsvc.go:0",
		". todo internal/a/keep.go:3",
		". todo rootsvc.go:3",
		"a large-file a/abig.go:0",
		"a missing-tests a/abig.go:0",
		"a missing-tests a/asvc.go:0",
		"a todo a/asvc.go:3",
		"nested missing-tests a/nested/nsvc.go:0",
		"nested todo a/nested/nsvc.go:3",
	}
	assert.Equal(t, want, got)

	// A member scanned on its own still leaves its nested member alone.
	assert.Equal(t, []string{
		"a large-file a/abig.go:0",
		"a missing-tests a/abig.go:0",
		"a missing-tests a/asvc.go:0",
		"a todo a/asvc.go:3",
	}, scanSignalPaths(t, binary, dir, "--workspace", "a"))
}

func TestScan_RootMemberWorkspaceNoWorkspacesUnchanged(t *testing.T) {
	binary := buildBinary(t)
	dir := initRootMemberMonorepo(t)

	// Without workspace detection the root walk covers the whole tree once.
	assert.Equal(t, []string{
		" large-file a/abig.go:0",
		" large-file rootbig.go:0",
		" missing-tests a/abig.go:0",
		" missing-tests a/asvc.go:0",
		" missing-tests a/nested/nsvc.go:0",
		" missing-tests internal/a/keep.go:0",
		" missing-tests rootbig.go:0",
		" missing-tests rootsvc.go:0",
		" todo a/asvc.go:3",
		" todo a/nested/nsvc.go:3",
		" todo internal/a/keep.go:3",
		" todo rootsvc.go:3",
	}, scanSignalPaths(t, binary, dir, "--no-workspaces"))
}
