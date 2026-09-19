// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/davetashner/stringer/internal/signal"
)

func TestNewWorkspaceScope(t *testing.T) {
	root := t.TempDir()
	members := []string{".", "pkg/a", "./pkg/b", "pkg/a/nested", ""}

	t.Run("zero value is the root", func(t *testing.T) {
		var s workspaceScope
		assert.True(t, s.atRoot())
		assert.True(t, s.unscoped())
		assert.True(t, s.repoWide())
		got, ok := s.relPath("x/y.go")
		assert.True(t, ok)
		assert.Equal(t, "x/y.go", got)
	})

	t.Run("no git root", func(t *testing.T) {
		s := newWorkspaceScope(root, "", nil)
		assert.Equal(t, ".", s.rel)
		assert.True(t, s.unscoped())
	})

	t.Run("root of a monorepo", func(t *testing.T) {
		s := newWorkspaceScope(root, root, members)
		assert.Equal(t, ".", s.rel)
		assert.Equal(t, []string{".", "pkg/a", "pkg/b", "pkg/a/nested"}, s.members)
		assert.Equal(t, []string{"pkg/a", "pkg/b", "pkg/a/nested"}, s.nested)
		assert.False(t, s.unscoped())
		assert.True(t, s.repoWide(), "root is the first member")
	})

	t.Run("member workspace", func(t *testing.T) {
		s := newWorkspaceScope(filepath.Join(root, "pkg", "a"), root, members)
		assert.Equal(t, "pkg/a", s.rel)
		assert.Equal(t, []string{"pkg/a/nested"}, s.nested, "only members inside pkg/a are nested")
		assert.False(t, s.repoWide())
	})

	t.Run("first scanned member is repo-wide", func(t *testing.T) {
		s := newWorkspaceScope(filepath.Join(root, "pkg", "b"), root, []string{"pkg/b", "pkg/a"})
		assert.True(t, s.repoWide())
		s = newWorkspaceScope(filepath.Join(root, "pkg", "a"), root, []string{"pkg/b", "pkg/a"})
		assert.False(t, s.repoWide())
	})

	t.Run("outside the git root scopes to the root", func(t *testing.T) {
		s := newWorkspaceScope(t.TempDir(), root, nil)
		assert.Equal(t, ".", s.rel)
		s = newWorkspaceScope("/tmp/nonexistent", root, nil)
		assert.Equal(t, ".", s.rel)
	})

	t.Run("dot-dot prefixed names are not parents", func(t *testing.T) {
		s := newWorkspaceScope(filepath.Join(root, "..foo"), root, nil)
		assert.Equal(t, "..foo", s.rel)
	})
}

func TestWorkspaceScope_RelPath(t *testing.T) {
	root := t.TempDir()
	members := []string{".", "pkg/a", "pkg/b", "pkg/a/nested", "pkg/ab"}

	cases := []struct {
		name string
		ws   string
		path string
		want string
		ok   bool
	}{
		{"root keeps its own file", ".", "Makefile", "Makefile", true},
		{"root keeps files outside members", ".", "pkg/x.go", "pkg/x.go", true},
		{"root leaves member files", ".", "pkg/a/a.go", "", false},
		{"root leaves nested member files", ".", "pkg/a/nested/n.go", "", false},
		{"member keeps its file relative", "pkg/a", "pkg/a/src/x.go", "src/x.go", true},
		{"member leaves nested member files", "pkg/a", "pkg/a/nested/n.go", "", false},
		{"member leaves other members", "pkg/a", "pkg/b/b.go", "", false},
		{"member leaves root files", "pkg/a", "Makefile", "", false},
		{"prefix match is per segment", "pkg/a", "pkg/ab/x.go", "", false},
		{"sibling with shared prefix", "pkg/ab", "pkg/ab/x.go", "x.go", true},
		{"nested member keeps its file", "pkg/a/nested", "pkg/a/nested/n.go", "n.go", true},
		{"parent member does not steal", "pkg/a/nested", "pkg/a/nested/deep/d.go", "deep/d.go", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkspaceScope(filepath.Join(root, filepath.FromSlash(tc.ws)), root, members)
			got, ok := s.relPath(tc.path)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWorkspaceScope_NestedDir(t *testing.T) {
	root := t.TempDir()
	members := []string{".", "pkg/a", "pkg/a/nested"}

	rootScope := newWorkspaceScope(root, root, members)
	assert.False(t, rootScope.nestedDir("."))
	assert.False(t, rootScope.nestedDir("pkg"))
	assert.True(t, rootScope.nestedDir("pkg/a"))
	assert.True(t, rootScope.nestedDir(filepath.Join("pkg", "a", "src")))
	assert.False(t, rootScope.nestedDir("pkg/ab"))

	member := newWorkspaceScope(filepath.Join(root, "pkg", "a"), root, members)
	assert.False(t, member.nestedDir("."))
	assert.False(t, member.nestedDir("src"))
	assert.True(t, member.nestedDir("nested"))
	assert.True(t, member.nestedDir("nested/deep"))

	assert.False(t, newWorkspaceScope(root, root, nil).nestedDir("pkg/a"), "no members, nothing nested")
}

func TestWorkspaceScope_FilterSignals(t *testing.T) {
	root := t.TempDir()
	members := []string{"pkg/a", "pkg/b"}
	in := []signal.RawSignal{
		{Kind: "churn", FilePath: "pkg/a/hot.go"},
		{Kind: "churn", FilePath: "pkg/b/warm.go"},
		{Kind: "churn", FilePath: "Makefile"},
		{Kind: "revert", FilePath: ""},
	}

	// Unscoped: the very same slice comes back.
	s := newWorkspaceScope(root, root, nil)
	out := s.filterSignals(in)
	assert.Equal(t, in, out)
	assert.Equal(t, &in[0], &out[0], "unscoped filtering must not copy")

	// First member: its own file, relative, plus the path-less signal.
	a := newWorkspaceScope(filepath.Join(root, "pkg", "a"), root, members).filterSignals(in)
	assert.Equal(t, []signal.RawSignal{
		{Kind: "churn", FilePath: "hot.go"},
		{Kind: "revert", FilePath: ""},
	}, a)
	assert.Equal(t, "pkg/a/hot.go", in[0].FilePath, "input must not be mutated")

	// Second member: only its own file.
	b := newWorkspaceScope(filepath.Join(root, "pkg", "b"), root, members).filterSignals(in)
	assert.Equal(t, []signal.RawSignal{{Kind: "churn", FilePath: "warm.go"}}, b)

	// A root workspace listed after the members keeps root files only.
	r := newWorkspaceScope(root, root, append(members, ".")).filterSignals(in)
	assert.Equal(t, []signal.RawSignal{{Kind: "churn", FilePath: "Makefile"}}, r)
}
