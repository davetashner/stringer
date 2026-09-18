// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"io/fs"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// maxGradleBuildFiles caps build files (root + subprojects) parsed per scan.
const maxGradleBuildFiles = 100

// gradleCatalog maps dependency aliases to Maven coordinates, fed by Gradle
// version catalogs (gradle/*.versions.toml) and Groovy ext maps (dependencies.gradle).
type gradleCatalog struct {
	versions map[string]string // normalized version alias -> version
	libs     map[string]string // normalized library alias -> "group:artifact[:version]"
}

func newGradleCatalog() *gradleCatalog {
	return &gradleCatalog{versions: map[string]string{}, libs: map[string]string{}}
}

// gradleCatalogs is keyed by accessor name ("libs" by default).
type gradleCatalogs map[string]*gradleCatalog

// normalizeGradleAlias lowercases an alias and folds '-' and '_' to '.', so
// the TOML alias "commons-validator" meets the accessor "libs.commons.validator".
func normalizeGradleAlias(alias string) string {
	return strings.ToLower(strings.NewReplacer("-", ".", "_", ".").Replace(alias))
}

// resolve returns the coordinates behind an accessor such as
// "libs.commons.validator", or "" for unknown or versions/bundles/plugins refs.
func (cs gradleCatalogs) resolve(ref string) string {
	name, alias, ok := strings.Cut(ref, ".")
	if !ok {
		return ""
	}
	cat := cs[strings.ToLower(name)]
	if cat == nil {
		return ""
	}
	key := normalizeGradleAlias(alias)
	for _, sub := range []string{"versions.", "bundles.", "plugins."} {
		if strings.HasPrefix(key, sub) {
			return ""
		}
	}
	return cat.libs[key]
}

// interpolate expands $versions.x / ${x} in a literal coordinate string via
// the default catalog; strings that cannot be fully resolved return "".
func (cs gradleCatalogs) interpolate(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	cat := cs["libs"]
	if cat == nil {
		return ""
	}
	out, ok := cat.interpolate(s)
	if !ok {
		return ""
	}
	return out
}

// tomlVersionCatalog is the subset of a Gradle version catalog we read.
type tomlVersionCatalog struct {
	Versions  map[string]any `toml:"versions"`
	Libraries map[string]any `toml:"libraries"`
}

// parseTomlCatalog parses a gradle/*.versions.toml file. Libraries may be
// "g:a:v" strings, { module = "g:a", version.ref = "x" } or
// { group = "g", name = "a", version = "v" }; rich versions use require/prefer/strictly.
func parseTomlCatalog(data []byte) (*gradleCatalog, error) {
	var raw tomlVersionCatalog
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	cat := newGradleCatalog()
	for k, v := range raw.Versions {
		if s := tomlVersionString(v); s != "" {
			cat.versions[normalizeGradleAlias(k)] = s
		}
	}
	for k, v := range raw.Libraries {
		if coords := cat.tomlLibraryCoords(v); coords != "" {
			cat.libs[normalizeGradleAlias(k)] = coords
		}
	}
	return cat, nil
}

// tomlVersionString extracts a version from a string or rich version table.
func tomlVersionString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		for _, key := range []string{"require", "prefer", "strictly"} {
			if s, ok := t[key].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// tomlLibraryCoords resolves a [libraries] entry to "group:artifact[:version]";
// BOM-managed entries carry no version and are later skipped by parseCoordinates.
func (c *gradleCatalog) tomlLibraryCoords(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		module, _ := t["module"].(string)
		if module == "" {
			group, _ := t["group"].(string)
			name, _ := t["name"].(string)
			if group == "" || name == "" {
				return ""
			}
			module = group + ":" + name
		}
		if version := c.tomlLibraryVersion(t["version"]); version != "" {
			return module + ":" + version
		}
		return module
	}
	return ""
}

// tomlLibraryVersion handles version = "1.0", version.ref = "alias" and rich tables.
func (c *gradleCatalog) tomlLibraryVersion(v any) string {
	if m, ok := v.(map[string]any); ok {
		if ref, ok := m["ref"].(string); ok {
			return c.versions[normalizeGradleAlias(ref)]
		}
	}
	return tomlVersionString(v)
}

var (
	// versions.foo = "1.0" / libs.foo = "g:a:$versions.foo" / versions["foo"] = "1.0"
	reGroovyAssign = regexp.MustCompile(`^(?:ext\.)?(versions|libs)\s*(?:\.\s*([A-Za-z_]\w*)|\[\s*['"]([^'"]+)['"]\s*\])\s*=\s*(.+)$`)
	// versions += [ / versions = [ / ext.versions = [ / libs += [
	reGroovyMapOpen = regexp.MustCompile(`^(?:ext\.)?(versions|libs)\s*\+?=\s*\[\s*$`)
	// key: expr,
	reGroovyMapEntry = regexp.MustCompile(`^([A-Za-z_]\w*)\s*:\s*(.+?),?$`)
	reGroovyString   = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)
	// $versions.foo, ${versions.foo}, $foo, ${foo}
	reGroovyInterp = regexp.MustCompile(`\$\{([\w.]+)\}|\$([\w.]+)`)
)

// parseGroovyCatalog reads Groovy ext-map version tables (gradle/dependencies.gradle
// or a root build.gradle) into cat: "versions.x = ..." assignments and
// "versions += [ x: ... ]" map literals; libs entries interpolate $versions.x.
func parseGroovyCatalog(data []byte, cat *gradleCatalog) {
	section := "" // "versions" or "libs" while inside a map literal
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}
		if section != "" {
			if strings.HasPrefix(trimmed, "]") {
				section = ""
			} else if m := reGroovyMapEntry.FindStringSubmatch(trimmed); m != nil {
				cat.setGroovy(section, m[1], m[2])
			}
			continue
		}
		if m := reGroovyMapOpen.FindStringSubmatch(trimmed); m != nil {
			section = m[1]
			continue
		}
		if m := reGroovyAssign.FindStringSubmatch(trimmed); m != nil {
			key := m[2]
			if key == "" {
				key = m[3]
			}
			cat.setGroovy(m[1], key, m[4])
		}
	}
}

// setGroovy records one versions/libs entry from a Groovy expression. The last
// string literal wins so ternary fallbacks resolve; entries whose interpolation
// cannot be resolved are dropped rather than recorded with a literal "$versions.x".
func (c *gradleCatalog) setGroovy(section, key, expr string) {
	lits := reGroovyString.FindAllStringSubmatch(expr, -1)
	if len(lits) == 0 {
		return
	}
	resolved, ok := c.interpolate(lits[len(lits)-1][1] + lits[len(lits)-1][2])
	if !ok {
		return
	}
	target := c.libs
	if section == "versions" {
		target = c.versions
	}
	target[normalizeGradleAlias(key)] = resolved
}

// interpolate expands $versions.x / ${x} from the versions map; false if any is unknown.
func (c *gradleCatalog) interpolate(s string) (string, bool) {
	ok := true
	out := reGroovyInterp.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.TrimPrefix(strings.Trim(m, "${}"), "versions.")
		if v, found := c.versions[normalizeGradleAlias(name)]; found {
			return v
		}
		ok = false
		return m
	})
	return out, ok
}

// loadGradleCatalogs discovers catalogs under <repo>/gradle: each *.versions.toml
// is a catalog named after its prefix ("libs"), and each *.gradle script there
// (dependencies.gradle by convention) layers Groovy maps onto the "libs" catalog.
func loadGradleCatalogs(repoPath string) gradleCatalogs {
	cats := gradleCatalogs{}
	dir := filepath.Join(repoPath, "gradle")
	var groovy []string
	_ = FS.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // missing gradle/ dir is the common case
		}
		if d.IsDir() {
			if path != dir {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, ".versions.toml"):
			data, readErr := FS.ReadFile(path)
			if readErr != nil {
				slog.Warn("vuln: reading version catalog", "file", name, "error", readErr)
				return nil
			}
			cat, parseErr := parseTomlCatalog(data)
			if parseErr != nil {
				slog.Warn("vuln: parsing version catalog", "file", name, "error", parseErr)
				return nil
			}
			cats[strings.ToLower(strings.TrimSuffix(name, ".versions.toml"))] = cat
		case strings.HasSuffix(name, ".gradle"):
			groovy = append(groovy, path)
		}
		return nil
	})
	for _, path := range groovy {
		data, err := FS.ReadFile(path)
		if err != nil {
			slog.Warn("vuln: reading gradle script", "file", filepath.Base(path), "error", err)
			continue
		}
		cats.addGroovy(data)
	}
	return cats
}

// addGroovy layers Groovy versions/libs maps found in data onto the "libs" catalog.
func (cs gradleCatalogs) addGroovy(data []byte) {
	if cs["libs"] == nil {
		cs["libs"] = newGradleCatalog()
	}
	parseGroovyCatalog(data, cs["libs"])
}

// findGradleBuildFiles returns the root build file followed by the existing build
// files of subprojects included from settings.gradle(.kts), relative to repoPath.
func findGradleBuildFiles(repoPath string) []string {
	var files []string
	find := func(dir string) {
		for _, name := range []string{"build.gradle", "build.gradle.kts"} {
			rel := filepath.Join(dir, name)
			if _, err := FS.Stat(filepath.Join(repoPath, rel)); err == nil {
				files = append(files, rel)
				return
			}
		}
	}
	find(".")
	for _, dir := range gradleSubprojectDirs(repoPath) {
		if len(files) >= maxGradleBuildFiles {
			break
		}
		find(dir)
	}
	return files
}

// gradleSubprojectDirs parses include statements from settings.gradle(.kts):
// include ':a', ':b:c' / include("a") / comma-continued lists; ':b:c' maps to "b/c".
func gradleSubprojectDirs(repoPath string) []string {
	var data []byte
	for _, name := range []string{"settings.gradle", "settings.gradle.kts"} {
		if d, err := FS.ReadFile(filepath.Join(repoPath, name)); err == nil {
			data = d
			break
		}
	}
	if data == nil {
		return nil
	}
	var dirs []string
	seen := map[string]bool{}
	collecting := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !collecting {
			if !strings.HasPrefix(trimmed, "include ") && !strings.HasPrefix(trimmed, "include(") {
				continue
			}
			trimmed = strings.TrimPrefix(trimmed, "include")
		}
		for _, m := range reGroovyString.FindAllStringSubmatch(trimmed, -1) {
			dir := strings.ReplaceAll(strings.Trim(m[1]+m[2], ":"), ":", "/")
			if dir != "" && !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
		collecting = strings.HasSuffix(trimmed, ",")
	}
	return dirs
}
