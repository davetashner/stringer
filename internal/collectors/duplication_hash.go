// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"hash/fnv"
	"regexp"
	"strings"
	"unicode"
)

// windowSize is the number of consecutive normalized lines per hash window.
const windowSize = 6

// cloneLocation records where a hash window was found.
type cloneLocation struct {
	Path      string
	StartLine int // 1-based line number in original file
}

// cloneGroup represents a set of identical (or near-identical) code blocks.
type cloneGroup struct {
	Lines     int             // number of lines in the duplicated block
	Locations []cloneLocation // 2+ locations where this block appears
	NearClone bool            // true if detected via Type 2 normalization
}

// commentLineRe matches lines that are purely single-line comments.
var commentLineRe = regexp.MustCompile(`^\s*(?://|#|/\*|\*/|\*\s|--)\s*`)

// importLineRe matches import/include/require/using statements.
var importLineRe = regexp.MustCompile(`(?i)^\s*(?:import\s|from\s\S+\s+import|require\s*\(|include\s|using\s|#include\s|use\s)`)

// identifierRe matches identifiers (word characters) for Type 2 normalization.
var identifierRe = regexp.MustCompile(`\b[a-zA-Z_]\w*\b`)

// commonKeywords are language keywords preserved during Type 2 normalization.
// Covers Go, Python, JS/TS, Java, Rust, Ruby, PHP, Swift, Scala, Elixir, C/C++, C#.
var commonKeywords = map[string]bool{
	// Control flow
	"if": true, "else": true, "for": true, "while": true, "do": true,
	"switch": true, "case": true, "break": true, "continue": true,
	"return": true, "yield": true, "throw": true, "try": true,
	"catch": true, "finally": true, "except": true, "raise": true,
	// Declarations
	"func": true, "function": true, "def": true, "fn": true, "var": true,
	"let": true, "const": true, "val": true, "type": true, "class": true,
	"struct": true, "enum": true, "interface": true, "trait": true,
	"impl": true, "module": true, "package": true, "namespace": true,
	// Modifiers
	"public": true, "private": true, "protected": true, "static": true,
	"final": true, "abstract": true, "override": true, "virtual": true,
	"async": true, "await": true, "mut": true, "pub": true,
	// Literals / operators
	"true": true, "false": true, "nil": true, "null": true, "none": true,
	"self": true, "this": true, "super": true, "new": true,
	// Other
	"range": true, "in": true, "not": true, "and": true, "or": true,
	"is": true, "as": true, "with": true, "from": true, "select": true,
	"defer": true, "go": true, "chan": true, "map": true, "end": true,
}

// normalizedLine holds a normalized line and its original 1-based line number.
type normalizedLine struct {
	text     string
	origLine int // 1-based
}

// normalizeType1 strips whitespace, skips blank lines, comment-only lines,
// and import lines. Returns normalized lines with original line numbers.
func normalizeType1(lines []string) []normalizedLine {
	result := make([]normalizedLine, 0, len(lines)/2)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if commentLineRe.MatchString(line) {
			continue
		}
		if importLineRe.MatchString(line) {
			continue
		}
		result = append(result, normalizedLine{
			text:     trimmed,
			origLine: i + 1,
		})
	}
	return result
}

// normalizeType2 does everything normalizeType1 does, plus replaces
// non-keyword identifiers with "$".
func normalizeType2(lines []string) []normalizedLine {
	type1 := normalizeType1(lines)
	result := make([]normalizedLine, len(type1))
	for i, nl := range type1 {
		replaced := identifierRe.ReplaceAllStringFunc(nl.text, func(id string) string {
			if commonKeywords[id] {
				return id
			}
			return "$"
		})
		result[i] = normalizedLine{
			text:     replaced,
			origLine: nl.origLine,
		}
	}
	return result
}

// hashWindow computes an FNV-64a hash of windowSize consecutive normalized lines.
func hashWindow(lines []normalizedLine, start int) uint64 {
	h := fnv.New64a()
	for i := start; i < start+windowSize && i < len(lines); i++ {
		_, _ = h.Write([]byte(lines[i].text))
		_, _ = h.Write([]byte{'\n'})
	}
	return h.Sum64()
}

// windowEntry stores a hash and its location for grouping.
type windowEntry struct {
	hash      uint64
	path      string
	startLine int // original 1-based line number
	normIdx   int // index into normalized lines
}

// buildWindowHashes creates hash entries for all sliding windows in a file.
func buildWindowHashes(normalized []normalizedLine, path string) []windowEntry {
	if len(normalized) < windowSize {
		return nil
	}
	entries := make([]windowEntry, 0, len(normalized)-windowSize+1)
	for i := 0; i <= len(normalized)-windowSize; i++ {
		entries = append(entries, windowEntry{
			hash:      hashWindow(normalized, i),
			path:      path,
			startLine: normalized[i].origLine,
			normIdx:   i,
		})
	}
	return entries
}

// groupClones groups window entries by hash, then extends adjacent matching
// windows into larger blocks. Returns clone groups with 2+ locations.
func groupClones(entries []windowEntry) []cloneGroup {
	// Group by hash.
	byHash := make(map[uint64][]windowEntry)
	for _, e := range entries {
		byHash[e.hash] = append(byHash[e.hash], e)
	}

	var groups []cloneGroup
	for _, matches := range byHash {
		if len(matches) < 2 {
			continue
		}

		// Deduplicate: keep one entry per unique (path, startLine).
		type locKey struct {
			path string
			line int
		}
		seen := make(map[locKey]bool)
		var unique []windowEntry
		for _, m := range matches {
			k := locKey{m.path, m.startLine}
			if !seen[k] {
				seen[k] = true
				unique = append(unique, m)
			}
		}
		if len(unique) < 2 {
			continue
		}

		locs := make([]cloneLocation, len(unique))
		for i, u := range unique {
			locs[i] = cloneLocation{Path: u.path, StartLine: u.startLine}
		}
		groups = append(groups, cloneGroup{
			Lines:     windowSize,
			Locations: locs,
		})
	}

	return mergeAdjacentGroups(groups)
}

// mergeAdjacentGroups merges clone groups whose locations are adjacent
// (consecutive starting lines with windowSize offset) into larger blocks.
func mergeAdjacentGroups(groups []cloneGroup) []cloneGroup {
	if len(groups) == 0 {
		return nil
	}

	// Build a map of (path, startLine) → group index for fast adjacency lookup.
	type locKey struct {
		path string
		line int
	}

	// For each location, track which group it belongs to.
	locToGroup := make(map[locKey]int)
	for i, g := range groups {
		for _, loc := range g.Locations {
			locToGroup[locKey{loc.Path, loc.StartLine}] = i
		}
	}

	// Merge groups that share adjacent windows. We use a union-find approach.
	parent := make([]int, len(groups))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// For each group, check if there's a group with locations shifted by 1 line.
	// This handles the case where window at line N and window at line N+1 both
	// appear in the same set of files — they should be merged into a larger block.
	for i, g := range groups {
		for _, loc := range g.Locations {
			// Check if a window starting 1 line after this one (in normalized space)
			// exists in another group with the same set of paths.
			for delta := 1; delta <= windowSize; delta++ {
				nextKey := locKey{loc.Path, loc.StartLine + delta}
				if j, ok := locToGroup[nextKey]; ok && j != i {
					// Verify the other group has matching paths.
					if samePathSet(groups[i].Locations, groups[j].Locations) {
						union(i, j)
					}
				}
			}
		}
	}

	// Collect merged groups.
	merged := make(map[int]*cloneGroup)
	for i, g := range groups {
		root := find(i)
		if existing, ok := merged[root]; ok {
			// Extend: take min startLine per path, max endLine per path.
			existing.Lines = maxLines(existing, &g)
			mergeLocations(existing, g.Locations)
		} else {
			cp := g
			merged[root] = &cp
		}
	}

	result := make([]cloneGroup, 0, len(merged))
	for _, g := range merged {
		// Recalculate line count from location spans.
		g.Lines = calcGroupLines(g)
		result = append(result, *g)
	}
	return result
}

// samePathSet returns true if both location slices contain the same set of paths.
func samePathSet(a, b []cloneLocation) bool {
	if len(a) != len(b) {
		return false
	}
	paths := make(map[string]bool, len(a))
	for _, loc := range a {
		paths[loc.Path] = true
	}
	for _, loc := range b {
		if !paths[loc.Path] {
			return false
		}
	}
	return true
}

// mergeLocations merges new locations into an existing group, keeping the
// minimum start line per path.
func mergeLocations(g *cloneGroup, locs []cloneLocation) {
	byPath := make(map[string]*cloneLocation)
	for i := range g.Locations {
		byPath[g.Locations[i].Path] = &g.Locations[i]
	}
	for _, loc := range locs {
		if existing, ok := byPath[loc.Path]; ok {
			if loc.StartLine < existing.StartLine {
				existing.StartLine = loc.StartLine
			}
		} else {
			g.Locations = append(g.Locations, loc)
			byPath[loc.Path] = &g.Locations[len(g.Locations)-1]
		}
	}
}

// maxLines returns the larger line count from two groups.
func maxLines(a, b *cloneGroup) int {
	if a.Lines > b.Lines {
		return a.Lines
	}
	return b.Lines
}

// calcGroupLines estimates the line span of a merged group.
// For merged adjacent windows, the span is windowSize + (number of merged windows - 1).
func calcGroupLines(g *cloneGroup) int {
	if len(g.Locations) == 0 {
		return g.Lines
	}
	// Find max span across all paths.
	maxSpan := g.Lines
	byPath := make(map[string][]int)
	for _, loc := range g.Locations {
		byPath[loc.Path] = append(byPath[loc.Path], loc.StartLine)
	}
	for _, lines := range byPath {
		if len(lines) <= 1 {
			continue
		}
		// For intra-file clones, each entry is a separate location.
		// The span is just the window size (they don't merge across locations).
	}
	return maxSpan
}

// subtractType1Ranges removes Type 2 clone groups that overlap with
// Type 1 clone groups (exact matches take precedence).
func subtractType1Ranges(type2Groups []cloneGroup, type1Groups []cloneGroup) []cloneGroup {
	if len(type1Groups) == 0 {
		return type2Groups
	}

	// Build a set of covered ranges from Type 1.
	type lineRange struct {
		path string
		line int
	}
	covered := make(map[lineRange]bool)
	for _, g := range type1Groups {
		for _, loc := range g.Locations {
			for l := loc.StartLine; l < loc.StartLine+g.Lines; l++ {
				covered[lineRange{loc.Path, l}] = true
			}
		}
	}

	var result []cloneGroup
	for _, g := range type2Groups {
		// Check if all locations of this group overlap with Type 1.
		allCovered := true
		for _, loc := range g.Locations {
			locCovered := true
			for l := loc.StartLine; l < loc.StartLine+g.Lines; l++ {
				if !covered[lineRange{loc.Path, l}] {
					locCovered = false
					break
				}
			}
			if !locCovered {
				allCovered = false
				break
			}
		}
		if !allCovered {
			result = append(result, g)
		}
	}
	return result
}

// isBlankOrWhitespace returns true if a string is empty or only whitespace.
func isBlankOrWhitespace(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
