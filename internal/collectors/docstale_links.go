// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// mdLinkPattern matches markdown links: [text](target)
var mdLinkPattern = regexp.MustCompile(`\[(?:[^\]]*)\]\(([^)]+)\)`)

// uriSchemePattern matches a scheme-like prefix (person:, tel:, vscode:,
// 1914:, …) — RFC 3986 schemes plus digit-led variants used as entity-ID
// namespaces in content systems. A colon in the first path segment of a
// markdown link target essentially never denotes a relative file, so such
// targets are not checked against the working tree (stringer-rd7).
var uriSchemePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+.-]*:`)

// fencePattern matches a code-fence delimiter line (``` or ~~~).
var fencePattern = regexp.MustCompile("^\\s*(```|~~~)")

// templatePlaceholderPattern matches template syntax inside a link target:
// {version}, {{ .Site }}, ${VAR}, <placeholder>, /:param segments and
// [[wiki]] links. Such targets are rendered by a site generator or filled
// in by a reader, never resolved against the working tree (stringer-nxx.11).
var templatePlaceholderPattern = regexp.MustCompile(`\{[^}]*\}|<[^>]*>|(?:^|/):[A-Za-z_]|\[\[.*\]\]`)

// constantPlaceholderPattern matches an ALL_CAPS_IDENTIFIER link target
// (ENGLISH_VERSION_URL) that a build step substitutes.
var constantPlaceholderPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$`)

// placeholderWords are literal link targets used in authoring instructions
// ("[text](url)") rather than as paths.
var placeholderWords = map[string]bool{
	"url": true, "link": true, "path": true, "file": true, "filename": true,
}

// langDirPattern matches a translation directory name (en, de, pt-BR, zh_Hant).
var langDirPattern = regexp.MustCompile(`^[a-z]{2,3}(?:[-_][A-Za-z]{2,4})?$`)

// extensionlessFallbacks are the source files a static-site generator
// serves for a link to `dir/` or `page` (pretty URLs, directory indexes).
var extensionlessFallbacks = []string{
	".md", ".rst",
	"/index.md", "/README.md", "/_index.md", "/index.html", "/index.rst",
}

// siteConfigFile describes a static-site generator config file and the
// content directories, relative to the config's directory, that site-root
// links resolve against.
type siteConfigFile struct {
	name     string
	dirs     []string
	needsKey string // Hugo's generic config.toml is only a site config when it has baseURL
}

// siteConfigFiles lists the generators recognised at the repo root, in
// docs/-style directories and one level below them (docs/en/mkdocs.yml).
var siteConfigFiles = []siteConfigFile{
	{name: "hugo.toml", dirs: []string{"content", "static"}},
	{name: "hugo.yaml", dirs: []string{"content", "static"}},
	{name: "hugo.yml", dirs: []string{"content", "static"}},
	{name: "hugo.json", dirs: []string{"content", "static"}},
	{name: "config.toml", dirs: []string{"content", "static"}, needsKey: "baseurl"},
	{name: "config.yaml", dirs: []string{"content", "static"}, needsKey: "baseurl"},
	{name: "config.yml", dirs: []string{"content", "static"}, needsKey: "baseurl"},
	{name: "config/_default/hugo.toml", dirs: []string{"content", "static"}},
	{name: "config/_default/config.toml", dirs: []string{"content", "static"}},
	{name: "mkdocs.yml", dirs: []string{"docs"}},
	{name: "mkdocs.yaml", dirs: []string{"docs"}},
	{name: "docusaurus.config.js", dirs: []string{"docs", "static"}},
	{name: "docusaurus.config.ts", dirs: []string{"docs", "static"}},
	{name: "docusaurus.config.mjs", dirs: []string{"docs", "static"}},
	{name: "docusaurus.config.cjs", dirs: []string{"docs", "static"}},
	{name: "_config.yml", dirs: []string{"."}},
	{name: "book.toml", dirs: []string{"src"}},
	{name: "conf.py", dirs: []string{"."}},
}

// siteDirKeyPattern extracts a content-directory override from a site
// config: mkdocs `docs_dir: value`, mdBook `src = "value"`, Hugo
// `contentDir = "value"`.
var siteDirKeyPattern = regexp.MustCompile(`(?m)^\s*(?:docs_dir|src|contentDir)\s*[:=]\s*["']?([^"'\s#]+)`)

// linkResolver resolves markdown link targets against the working tree,
// taking into account the static-site layout detected in the repository.
type linkResolver struct {
	repoPath string
	// contentRoots are absolute directories that site-root links (/path)
	// and mirrored docs (a README copied from docs/index.md) resolve against.
	contentRoots []string
	// configured is true when a site generator config file was found, so
	// site URL routing is knowable; siteDetected is true on any evidence
	// of a static site, including Hugo _index.md content without a config.
	configured   bool
	siteDetected bool
}

// newLinkResolver detects the static-site layout of repoPath. docFiles are
// the repo-relative documentation files already discovered by the walk.
func newLinkResolver(repoPath string, docFiles []string) *linkResolver {
	r := &linkResolver{repoPath: repoPath}
	seen := map[string]bool{}
	addRoot := func(dir string) {
		dir = filepath.Clean(dir)
		if seen[dir] {
			return
		}
		if info, err := FS.Stat(dir); err != nil || !info.IsDir() {
			return
		}
		seen[dir] = true
		r.contentRoots = append(r.contentRoots, dir)
	}

	for _, cfgDir := range siteConfigDirs(repoPath) {
		roots := siteContentDirs(cfgDir)
		r.configured = r.configured || len(roots) > 0
		for _, root := range roots {
			addRoot(root)
		}
	}

	// Hugo content embedded in a docs tree without its config (the site
	// lives in a sibling repo): the shallowest _index.md directories.
	for _, dir := range hugoContentDirs(docFiles) {
		r.siteDetected = true
		addRoot(filepath.Join(repoPath, dir))
	}

	// Multilingual content roots (content/en, docs/de) are roots too.
	for _, root := range append([]string(nil), r.contentRoots...) {
		for _, sub := range listSubdirs(root) {
			if langDirPattern.MatchString(filepath.Base(sub)) {
				addRoot(sub)
			}
		}
	}

	r.siteDetected = r.siteDetected || r.configured
	return r
}

// hugoContentDirs returns the shallowest repo-relative directories holding
// a Hugo _index.md section file, in sorted order.
func hugoContentDirs(docFiles []string) []string {
	var indexDirs []string
	for _, rel := range docFiles {
		if filepath.Base(rel) == "_index.md" {
			indexDirs = append(indexDirs, filepath.ToSlash(filepath.Dir(rel)))
		}
	}
	sort.Strings(indexDirs)
	var roots []string
	for _, dir := range indexDirs {
		nested := false
		for _, root := range roots {
			if dir == root || strings.HasPrefix(dir, root+"/") {
				nested = true
				break
			}
		}
		if !nested {
			roots = append(roots, dir)
		}
	}
	return roots
}

// siteConfigDirs returns the directories probed for a site generator config:
// the repo root, docs-style directories and their immediate subdirectories.
func siteConfigDirs(repoPath string) []string {
	dirs := []string{repoPath}
	for _, name := range []string{"docs", "doc", "website", "site"} {
		dir := filepath.Join(repoPath, name)
		if info, err := FS.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, dir)
		dirs = append(dirs, listSubdirs(dir)...)
	}
	return dirs
}

// siteContentDirs returns the absolute content directories declared by any
// site generator config found directly in cfgDir.
func siteContentDirs(cfgDir string) []string {
	var out []string
	for _, cfg := range siteConfigFiles {
		path := filepath.Join(cfgDir, cfg.name)
		if info, err := FS.Stat(path); err != nil || info.IsDir() {
			continue
		}
		data, _ := FS.ReadFile(path)
		if cfg.needsKey != "" && !strings.Contains(strings.ToLower(string(data)), cfg.needsKey) {
			continue
		}
		dirs := cfg.dirs
		if m := siteDirKeyPattern.FindSubmatch(data); m != nil {
			dirs = append([]string{string(m[1])}, dirs...)
		}
		for _, d := range dirs {
			out = append(out, filepath.Join(cfgDir, d))
		}
	}
	return out
}

// listSubdirs returns the non-hidden immediate subdirectories of dir.
func listSubdirs(dir string) []string {
	var out []string
	_ = FS.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil
		}
		if d.IsDir() {
			if !strings.HasPrefix(d.Name(), ".") {
				out = append(out, path)
			}
			return filepath.SkipDir
		}
		return nil
	})
	return out
}

// brokenLink describes a broken internal link in a markdown file.
type brokenLink struct {
	target string
	line   int
}

// findBrokenLinks scans a markdown file for internal links that point to
// non-existent files.
func (r *linkResolver) findBrokenLinks(relPath string) []brokenLink {
	absPath := filepath.Join(r.repoPath, relPath)
	f, err := FS.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	var broken []brokenLink

	scanner := bufio.NewScanner(f)
	lineNo := 0
	inFence := false
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		// Link-shaped text inside fenced code blocks is sample code, not a
		// link (stringer-rd7).
		if fencePattern.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		for _, m := range mdLinkPattern.FindAllStringSubmatch(line, -1) {
			target, ok := normalizeLinkTarget(m[1])
			if !ok {
				continue
			}
			if r.resolves(absPath, target) {
				continue
			}
			// Unresolved targets shaped like placeholders (including
			// <angle-wrapped> ones) are filler, not broken links; site-root
			// links on a site whose routing is not in this repo cannot be
			// checked at all.
			if isPlaceholderLinkTarget(target) || strings.HasPrefix(strings.TrimSpace(m[1]), "<") {
				continue
			}
			if strings.HasPrefix(target, "/") && r.siteDetected && !r.configured {
				continue
			}
			broken = append(broken, brokenLink{target: target, line: lineNo})
		}
	}

	return broken
}

// normalizeLinkTarget strips the parts of a markdown link target that do
// not select a file (a title, <> wrapping, #fragment, ?query) and reports
// false when nothing checkable remains (pure anchors, URI schemes).
func normalizeLinkTarget(raw string) (string, bool) {
	target := strings.TrimSpace(raw)
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
	}
	// [text](path "Title") — drop the optional title.
	if idx := strings.IndexAny(target, " \t"); idx >= 0 {
		rest := strings.TrimSpace(target[idx:])
		if strings.HasPrefix(rest, `"`) || strings.HasPrefix(rest, "'") || strings.HasPrefix(rest, "(") {
			target = target[:idx]
		}
	}
	// Skip any target with a URI scheme (http:, mailto:, but also
	// person:, tel:, vscode:, …) — schemes are not paths.
	if uriSchemePattern.MatchString(strings.TrimPrefix(target, "<")) {
		return "", false
	}
	// A quoted target is a string literal in indented sample code
	// (builder.stream[String, String]("TextLinesTopic")), not a path.
	if strings.HasPrefix(target, `"`) || strings.HasPrefix(target, "'") {
		return "", false
	}
	if idx := strings.IndexAny(target, "#?"); idx >= 0 {
		target = target[:idx]
	}
	if target == "" {
		return "", false
	}
	return target, true
}

// resolves reports whether target, found in the markdown file docPath,
// points at something in the working tree: directly, via the source file a
// site generator would serve for it, from the pretty-URL directory a site
// serves the page at, from the English original of a translated doc, or
// relative to a detected content root. Site-root links are tried against
// the repo root (GitHub rendering) and, as before, the doc's own directory.
func (r *linkResolver) resolves(docPath, target string) bool {
	docDir := filepath.Dir(docPath)
	if strings.HasPrefix(target, "/") {
		if r.targetExists(r.repoPath, target) || r.targetExists(docDir, target) {
			return true
		}
		return r.existsUnderRoots(target)
	}
	if r.targetExists(docDir, target) {
		return true
	}
	if r.siteDetected {
		// Pretty URLs serve page.md at page/, so its relative links resolve
		// one level deeper than the source file (kafka's ../core-concepts).
		if pretty := prettyURLDir(docPath); pretty != "" && r.targetExists(pretty, target) {
			return true
		}
	}
	if alt := r.englishSibling(docDir); alt != "" && r.targetExists(alt, target) {
		return true
	}
	return r.existsUnderRoots(target)
}

// prettyURLDir returns the directory a static site serves docPath at
// (docs/page.md → docs/page), or "" for index pages, which are served at
// their own directory.
func prettyURLDir(docPath string) string {
	base := filepath.Base(docPath)
	switch strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base))) {
	case "index", "_index", "readme":
		return ""
	}
	return strings.TrimSuffix(docPath, filepath.Ext(docPath))
}

// existsUnderRoots reports whether target resolves against any content root.
func (r *linkResolver) existsUnderRoots(target string) bool {
	for _, root := range r.contentRoots {
		if r.targetExists(root, target) {
			return true
		}
	}
	return false
}

// englishSibling maps a translation directory (docs/de/docs/x) to its
// English original (docs/en/docs/x) when one exists, else "".
func (r *linkResolver) englishSibling(docDir string) string {
	rel, err := filepath.Rel(r.repoPath, docDir)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, part := range parts {
		if part == "en" || !langDirPattern.MatchString(part) {
			continue
		}
		alt := append(append([]string{}, parts[:i]...), "en")
		alt = append(alt, parts[i+1:]...)
		candidate := filepath.Join(r.repoPath, filepath.FromSlash(strings.Join(alt, "/")))
		if info, err := FS.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// targetExists reports whether target, resolved against base, names an
// existing path or one of the source files a site generator serves for it.
func (r *linkResolver) targetExists(base, target string) bool {
	if unescaped, err := url.PathUnescape(target); err == nil {
		target = unescaped
	}
	path := filepath.Join(base, target)
	if _, err := FS.Stat(path); err == nil {
		return true
	}
	// A rendered page (foo.html) or pretty URL (foo/, foo) is served from
	// foo.md, foo/index.md, foo/_index.md, …
	ext := filepath.Ext(path)
	switch strings.ToLower(ext) {
	case ".html", ".htm":
		path = strings.TrimSuffix(path, ext)
	case "":
	default:
		return false
	}
	for _, suffix := range extensionlessFallbacks {
		if _, err := FS.Stat(path + suffix); err == nil {
			return true
		}
	}
	return false
}

// isPlaceholderLinkTarget reports whether a link target is documentation
// filler rather than a checkable path: ellipses, template syntax,
// substituted constants and authoring-guide words.
func isPlaceholderLinkTarget(target string) bool {
	t := strings.TrimSpace(target)
	if t == "…" || t == "..." || t == ".." || t == "." {
		return true
	}
	if templatePlaceholderPattern.MatchString(t) || constantPlaceholderPattern.MatchString(t) {
		return true
	}
	if placeholderWords[strings.ToLower(t)] {
		return true
	}
	return hasPrintfVerb(t)
}

// hasPrintfVerb reports whether t contains a %s-style format verb, as
// distinct from a %2F percent-encoding.
func hasPrintfVerb(t string) bool {
	for i := 0; i+1 < len(t); i++ {
		if t[i] != '%' {
			continue
		}
		c := t[i+1]
		if !isASCIILetter(c) {
			continue
		}
		if i+2 >= len(t) || !isHexDigit(t[i+2]) || !isHexDigit(c) {
			return true
		}
	}
	return false
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
