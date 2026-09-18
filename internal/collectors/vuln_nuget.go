// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// csprojProject represents the top-level structure of a .csproj file.
type csprojProject struct {
	XMLName    xml.Name          `xml:"Project"`
	ItemGroups []csprojItemGroup `xml:"ItemGroup"`
}

// csprojItemGroup represents an <ItemGroup> element containing package references.
type csprojItemGroup struct {
	PackageRefs []csprojPackageRef `xml:"PackageReference"`
}

// csprojPackageRef represents a <PackageReference> element in a .csproj file.
// Version can be specified as an attribute or a child element; under Central
// Package Management it is omitted and VersionOverride may pin a project.
type csprojPackageRef struct {
	Include         string `xml:"Include,attr"`
	Version         string `xml:"Version,attr"`
	VersionElem     string `xml:"Version"`
	VersionOverride string `xml:"VersionOverride,attr"`
}

// parseCsprojDeps parses a .csproj file and returns PackageQuery entries for OSV lookup.
// It handles both version styles:
//   - Attribute: <PackageReference Include="Foo" Version="1.0" />
//   - Child element: <PackageReference Include="Foo"><Version>1.0</Version></PackageReference>
//
// Entries with empty Include or Version are skipped. Duplicates are deduplicated by package name.
func parseCsprojDeps(data []byte) ([]PackageQuery, error) {
	return resolveCsprojDeps(data, nil)
}

// resolveCsprojDeps is parseCsprojDeps with versions filled in from Central
// Package Management props and packages.lock.json (see nugetCentralVersions).
func resolveCsprojDeps(data []byte, central *nugetCentralVersions) ([]PackageQuery, error) {
	var project csprojProject
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil, err
	}

	var refs []csprojPackageRef
	for _, ig := range project.ItemGroups {
		refs = append(refs, ig.PackageRefs...)
	}
	if central != nil {
		refs = append(refs, central.global...)
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for _, ref := range refs {
		if ref.Include == "" || seen[ref.Include] {
			continue
		}
		version := central.versionFor(ref)
		if version == "" {
			continue
		}
		seen[ref.Include] = true

		// Floating ("1.0.*") and bracket ("[1.0,2.0)") versions query
		// their lower bound as a floor; a plain version resolves to
		// itself under NuGet's lowest-applicable rule and counts as exact.
		if q := mavenStyleQuery("NuGet", ref.Include, version); q != nil {
			queries = append(queries, *q)
		}
	}

	return queries, nil
}

// nugetCentralVersions holds versions declared outside a project file:
// <PackageVersion> entries from Directory.Packages.props (Central Package
// Management), implicit references from <GlobalPackageReference> and
// Directory.Build.props, and resolved versions from packages.lock.json.
type nugetCentralVersions struct {
	central map[string]string // lowercased id -> version
	global  []csprojPackageRef
	locked  map[string]string // lowercased id -> resolved version
}

// versionFor picks the version for a reference: a lockfile-resolved version
// wins, then the project's own Version, then VersionOverride, then the
// central <PackageVersion>. Safe to call on a nil receiver.
func (n *nugetCentralVersions) versionFor(ref csprojPackageRef) string {
	declared := ref.Version
	if declared == "" {
		declared = ref.VersionElem
	}
	if n == nil {
		return declared
	}
	id := strings.ToLower(ref.Include)
	if v := n.locked[id]; v != "" {
		return v
	}
	if declared != "" {
		return declared
	}
	if ref.VersionOverride != "" {
		return ref.VersionOverride
	}
	return n.central[id]
}

// msbuildProps is the subset of Directory.Packages.props / Directory.Build.props we read.
type msbuildProps struct {
	XMLName        xml.Name `xml:"Project"`
	PropertyGroups []struct {
		Properties []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	} `xml:"PropertyGroup"`
	ItemGroups []struct {
		PackageVersions []csprojPackageRef `xml:"PackageVersion"`
		GlobalRefs      []csprojPackageRef `xml:"GlobalPackageReference"`
		PackageRefs     []csprojPackageRef `xml:"PackageReference"`
	} `xml:"ItemGroup"`
}

var reMSBuildProperty = regexp.MustCompile(`\$\((\w+)\)`)

// nugetCentralResolver locates and caches CPM props per project directory.
type nugetCentralResolver struct {
	repoPath string
	cache    map[string]*nugetCentralVersions // keyed by project dir
}

func newNuGetCentralResolver(repoPath string) *nugetCentralResolver {
	return &nugetCentralResolver{repoPath: repoPath, cache: map[string]*nugetCentralVersions{}}
}

// forProject returns the central versions that apply to the .csproj at rel:
// the nearest Directory.Packages.props and Directory.Build.props walking up
// to the repo root, plus a packages.lock.json beside the project. Returns
// nil when none of these exist.
func (r *nugetCentralResolver) forProject(rel string) *nugetCentralVersions {
	dir := filepath.Dir(rel)
	if cached, ok := r.cache[dir]; ok {
		return cached
	}
	n := &nugetCentralVersions{central: map[string]string{}, locked: map[string]string{}}
	props := map[string]string{}
	found := false
	for d := dir; ; d = filepath.Dir(d) {
		for _, name := range []string{"Directory.Build.props", "Directory.Packages.props"} {
			data, err := FS.ReadFile(filepath.Join(r.repoPath, d, name))
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					slog.Warn("vuln: reading msbuild props", "file", name, "error", err)
				}
				continue
			}
			if r.mergeProps(n, props, data, name) {
				found = true
			}
		}
		if found || d == "." || d == "/" || d == "" {
			break
		}
	}
	if data, err := FS.ReadFile(filepath.Join(r.repoPath, dir, "packages.lock.json")); err == nil {
		if parseNuGetLock(data, n.locked) {
			found = true
		}
	}
	if !found {
		n = nil
	}
	r.cache[dir] = n
	return n
}

// mergeProps merges one props file into n. Returns true when the file
// carried package versions or implicit references.
func (r *nugetCentralResolver) mergeProps(n *nugetCentralVersions, props map[string]string, data []byte, name string) bool {
	var p msbuildProps
	if err := xml.Unmarshal(data, &p); err != nil {
		slog.Warn("vuln: parsing msbuild props", "file", name, "error", err)
		return false
	}
	for _, pg := range p.PropertyGroups {
		for _, prop := range pg.Properties {
			props[prop.XMLName.Local] = strings.TrimSpace(prop.Value)
		}
	}
	expand := func(v string) string {
		return reMSBuildProperty.ReplaceAllStringFunc(v, func(m string) string {
			if val, ok := props[m[2:len(m)-1]]; ok {
				return val
			}
			return m
		})
	}
	hit := false
	for _, ig := range p.ItemGroups {
		for _, pv := range ig.PackageVersions {
			if pv.Include == "" || pv.Version == "" {
				continue
			}
			id := strings.ToLower(pv.Include)
			if _, exists := n.central[id]; !exists {
				n.central[id] = expand(pv.Version)
			}
			hit = true
		}
		for _, g := range append(ig.GlobalRefs, ig.PackageRefs...) {
			if g.Include == "" {
				continue
			}
			g.Version = expand(g.Version)
			n.global = append(n.global, g)
			hit = true
		}
	}
	return hit
}

// nugetLock is the subset of packages.lock.json we read.
type nugetLock struct {
	Dependencies map[string]map[string]struct {
		Type     string `json:"type"`
		Resolved string `json:"resolved"`
	} `json:"dependencies"`
}

// parseNuGetLock records resolved versions of direct dependencies from a
// packages.lock.json into locked (lowercased id -> version). Returns true
// when at least one entry was read.
func parseNuGetLock(data []byte, locked map[string]string) bool {
	var lock nugetLock
	if err := json.Unmarshal(data, &lock); err != nil {
		slog.Warn("vuln: parsing packages.lock.json", "error", err)
		return false
	}
	for _, framework := range lock.Dependencies {
		for id, dep := range framework {
			if dep.Type == "Direct" && dep.Resolved != "" {
				locked[strings.ToLower(id)] = dep.Resolved
			}
		}
	}
	return len(locked) > 0
}
