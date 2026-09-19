// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"path/filepath"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

// workspaceScope is the slice of a repository that one collector run covers.
//
// In a monorepo the scan pipeline runs once per workspace with repoPath set
// to the workspace directory and GitRoot to the repository root, and then
// prefixes every signal path with the workspace's relative path. Collectors
// that read repository-wide git history (gitlog, lotteryrisk) use the scope
// to keep only the files of their own workspace, expressed relative to it,
// so that the prefixing yields each repository-relative path exactly once
// instead of once per workspace (stringer-nxx.17).
//
// The zero value scopes to the repository root of a non-monorepo scan.
type workspaceScope struct {
	// rel is the workspace path relative to the git root, "." at the root.
	rel string
	// members lists every workspace of the monorepo relative to the git
	// root, the scanned workspaces first; empty outside monorepos.
	members []string
	// nested holds the members that lie inside this workspace, relative to
	// the git root. Their files belong to them, not to this workspace.
	nested []string
}

// newWorkspaceScope derives the scope of repoPath inside gitRoot. members are
// the monorepo's workspaces relative to gitRoot (see
// signal.CollectorOpts.WorkspaceMembers). A repoPath that is not inside
// gitRoot scopes to the root.
func newWorkspaceScope(repoPath, gitRoot string, members []string) workspaceScope {
	s := workspaceScope{rel: "."}
	if gitRoot != "" && repoPath != "" {
		if rel, err := filepath.Rel(gitRoot, repoPath); err == nil {
			rel = filepath.ToSlash(rel)
			if rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel) {
				s.rel = rel
			}
		}
	}
	for _, m := range members {
		if m == "" {
			continue
		}
		m = filepath.ToSlash(filepath.Clean(m))
		s.members = append(s.members, m)
		if m != "." && m != s.rel && (s.rel == "." || strings.HasPrefix(m, s.rel+"/")) {
			s.nested = append(s.nested, m)
		}
	}
	return s
}

// atRoot reports whether the scope is the repository root.
func (s workspaceScope) atRoot() bool {
	return s.rel == "" || s.rel == "."
}

// unscoped reports whether the scope leaves repository paths untouched: the
// root of a repository with no member workspaces nested inside it.
func (s workspaceScope) unscoped() bool {
	return s.atRoot() && len(s.nested) == 0
}

// repoWide reports whether this scope emits repository-wide signals, those
// without a file path such as stale branches: always outside monorepos, and
// in a monorepo only for the first scanned workspace.
func (s workspaceScope) repoWide() bool {
	return len(s.members) == 0 || s.members[0] == s.rel
}

// relPath maps a git-root-relative file path to a workspace-relative one.
// It reports false for files outside the workspace or inside a nested
// member workspace, which reports them itself.
func (s workspaceScope) relPath(path string) (string, bool) {
	p := filepath.ToSlash(path)
	for _, n := range s.nested {
		if strings.HasPrefix(p, n+"/") {
			return "", false
		}
	}
	if s.atRoot() {
		return p, true
	}
	rest, ok := strings.CutPrefix(p, s.rel+"/")
	if !ok {
		return "", false
	}
	return rest, true
}

// nestedDir reports whether the workspace-relative directory dir is (or lies
// inside) a member workspace nested in this one.
func (s workspaceScope) nestedDir(dir string) bool {
	if dir == "." || len(s.nested) == 0 {
		return false
	}
	d := filepath.ToSlash(dir)
	if !s.atRoot() {
		d = s.rel + "/" + d
	}
	for _, n := range s.nested {
		if d == n || strings.HasPrefix(d, n+"/") {
			return true
		}
	}
	return false
}

// filterSignals keeps the signals whose git-root-relative FilePath lies in
// the workspace and rewrites it relative to the workspace. Signals without a
// file path are repository-wide and are kept only by the repoWide scope.
// An unscoped scope returns signals unchanged.
func (s workspaceScope) filterSignals(signals []signal.RawSignal) []signal.RawSignal {
	if s.unscoped() {
		return signals
	}
	out := make([]signal.RawSignal, 0, len(signals))
	for _, sig := range signals {
		if sig.FilePath == "" {
			if s.repoWide() {
				out = append(out, sig)
			}
			continue
		}
		rel, ok := s.relPath(sig.FilePath)
		if !ok {
			continue
		}
		sig.FilePath = rel
		out = append(out, sig)
	}
	return out
}
