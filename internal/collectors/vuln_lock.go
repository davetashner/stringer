// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/mod/modfile"
)

// --- Lockfiles ---
//
// A lockfile records the versions actually installed, so its entries are
// exact and take precedence over manifest ranges. Cargo.lock and
// composer.lock are parsed here; package-lock.json lives in vuln_npm.go.
// yarn.lock, pnpm-lock.yaml, poetry.lock, uv.lock, mix.lock and
// packages.lock.json are not parsed yet (stringer-nxx.2 follow-up).

// cargoLockFile is the subset of Cargo.lock needed for version resolution.
type cargoLockFile struct {
	Package []struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
		Source  string `toml:"source"`
	} `toml:"package"`
}

// parseCargoLock returns one exact query per registry package in Cargo.lock
// (deduplicated by name+version) and the set of packages without a source,
// which are workspace/path crates and never queried.
func parseCargoLock(data []byte) ([]PackageQuery, map[string]bool, error) {
	var lock cargoLockFile
	if err := toml.Unmarshal(data, &lock); err != nil {
		return nil, nil, err
	}

	local := make(map[string]bool)
	seen := make(map[string]bool)
	var queries []PackageQuery
	for _, p := range lock.Package {
		if p.Name == "" || p.Version == "" {
			continue
		}
		if p.Source == "" {
			local[p.Name] = true
			continue
		}
		key := p.Name + "|" + p.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		queries = append(queries, PackageQuery{Ecosystem: "crates.io", Name: p.Name, Version: p.Version})
	}
	return queries, local, nil
}

// composerLockFile is the subset of composer.lock needed for version resolution.
type composerLockFile struct {
	Packages    []composerLockPackage `json:"packages"`
	PackagesDev []composerLockPackage `json:"packages-dev"`
}

type composerLockPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// parseComposerLock returns one exact query per installed package in
// composer.lock. Entries in packages-dev are marked development-only.
func parseComposerLock(data []byte) ([]PackageQuery, error) {
	var lock composerLockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var queries []PackageQuery
	for _, group := range []struct {
		pkgs []composerLockPackage
		dev  bool
	}{{lock.Packages, false}, {lock.PackagesDev, true}} {
		for _, p := range group.pkgs {
			v := strings.TrimPrefix(strings.TrimSpace(p.Version), "v")
			if p.Name == "" || v == "" || strings.HasPrefix(v, "dev-") {
				continue
			}
			key := p.Name + "|" + v
			if seen[key] {
				continue
			}
			seen[key] = true
			queries = append(queries, PackageQuery{Ecosystem: "Packagist", Name: p.Name, Version: v, Dev: group.dev})
		}
	}
	return queries, nil
}

// --- Workspaces ---
//
// A dependency on another member of the same workspace is source in this
// repository, not a published artifact: querying a registry for it produces
// findings about a version that is never installed (stringer-nxx.2).

// expandMemberDirs resolves workspace member patterns (Cargo `members`, npm
// `workspaces`) to absolute directories. Only the final path segment may be
// a glob, which covers the "packages/*" convention; negations are ignored.
func expandMemberDirs(repoPath string, patterns []string) []string {
	var dirs []string
	for _, p := range patterns {
		p = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(p), "./"), "/")
		if p == "" || strings.HasPrefix(p, "!") {
			continue
		}
		if !strings.ContainsAny(p, "*?[") {
			dirs = append(dirs, filepath.Join(repoPath, filepath.FromSlash(p)))
			continue
		}
		parent, pattern := path.Split(p)
		if strings.ContainsAny(parent, "*?[") {
			continue // nested globs such as "crates/**/tools" are not supported
		}
		root := filepath.Join(repoPath, filepath.FromSlash(parent))
		_ = FS.WalkDir(root, func(entry string, d fs.DirEntry, err error) error {
			if err != nil || entry == root {
				return nil //nolint:nilerr // skip inaccessible paths
			}
			if !d.IsDir() {
				return nil
			}
			if ok, _ := path.Match(pattern, d.Name()); ok {
				dirs = append(dirs, entry)
			}
			return fs.SkipDir
		})
	}
	return dirs
}

// cargoWorkspaceMembers returns the crate names of every [workspace] member
// declared in the root Cargo.toml.
func cargoWorkspaceMembers(repoPath string) map[string]bool {
	var root struct {
		Workspace struct {
			Members []string `toml:"members"`
		} `toml:"workspace"`
	}
	data, err := FS.ReadFile(filepath.Join(repoPath, "Cargo.toml"))
	if err != nil || toml.Unmarshal(data, &root) != nil {
		return nil
	}
	members := make(map[string]bool)
	for _, dir := range expandMemberDirs(repoPath, root.Workspace.Members) {
		var pkg struct {
			Package struct {
				Name string `toml:"name"`
			} `toml:"package"`
		}
		data, err := FS.ReadFile(filepath.Join(dir, "Cargo.toml"))
		if err == nil && toml.Unmarshal(data, &pkg) == nil && pkg.Package.Name != "" {
			members[pkg.Package.Name] = true
		}
	}
	return members
}

// npmWorkspaceMembers returns the package names of every `workspaces` member
// declared in the root package.json (array or {"packages": [...]} form).
func npmWorkspaceMembers(repoPath string) map[string]bool {
	var root struct {
		Workspaces json.RawMessage `json:"workspaces"`
	}
	data, err := FS.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil || json.Unmarshal(data, &root) != nil || len(root.Workspaces) == 0 {
		return nil
	}
	var patterns []string
	if json.Unmarshal(root.Workspaces, &patterns) != nil {
		var obj struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal(root.Workspaces, &obj) != nil {
			return nil
		}
		patterns = obj.Packages
	}
	members := make(map[string]bool)
	for _, dir := range expandMemberDirs(repoPath, patterns) {
		var pkg struct {
			Name string `json:"name"`
		}
		data, err := FS.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil && json.Unmarshal(data, &pkg) == nil && pkg.Name != "" {
			members[pkg.Name] = true
		}
	}
	return members
}

// goWorkMembers returns the module paths listed by `use` directives in
// go.work, read from each member's go.mod.
func goWorkMembers(repoPath string) map[string]bool {
	data, err := FS.ReadFile(filepath.Join(repoPath, "go.work"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("vuln: reading go.work", "error", err)
		}
		return nil
	}
	wf, err := modfile.ParseWork("go.work", data, nil)
	if err != nil {
		slog.Warn("vuln: parsing go.work", "error", err)
		return nil
	}
	members := make(map[string]bool)
	for _, use := range wf.Use {
		modData, err := FS.ReadFile(filepath.Join(repoPath, filepath.FromSlash(use.Path), "go.mod"))
		if err != nil {
			continue
		}
		if mp := modfile.ModulePath(modData); mp != "" {
			members[mp] = true
		}
	}
	return members
}

// dropMembers removes queries whose package is a workspace member.
func dropMembers(queries []PackageQuery, members map[string]bool) []PackageQuery {
	if len(members) == 0 {
		return queries
	}
	kept := make([]PackageQuery, 0, len(queries))
	for _, q := range queries {
		if !members[q.Name] {
			kept = append(kept, q)
		}
	}
	return kept
}
