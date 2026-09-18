// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"hash/fnv"
	"regexp"
	"strings"
	"unicode"
)

// defaultWindowSize is the default number of consecutive normalized lines per hash window.
const defaultWindowSize = 6

// cloneLocation records where a hash window was found.
type cloneLocation struct {
	Path      string
	StartLine int // 1-based line number in original file
	EndLine   int // 1-based inclusive end line in original file
}

// span returns the number of original lines the location covers.
func (l cloneLocation) span() int {
	if l.EndLine < l.StartLine {
		return 1
	}
	return l.EndLine - l.StartLine + 1
}

// cloneGroup represents a set of identical (or near-identical) code blocks.
type cloneGroup struct {
	Lines     int             // original-line span of the largest location
	Locations []cloneLocation // 2+ distinct, non-overlapping locations
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

// hashWindow computes an FNV-64a hash of winSize consecutive normalized lines.
func hashWindow(lines []normalizedLine, start, winSize int) uint64 {
	h := fnv.New64a()
	for i := start; i < start+winSize && i < len(lines); i++ {
		_, _ = h.Write([]byte(lines[i].text))
		_, _ = h.Write([]byte{'\n'})
	}
	return h.Sum64()
}

// windowEntry stores a hash and its location for grouping.
type windowEntry struct {
	hash      uint64
	path      string
	startLine int // original 1-based line number of the first window line
	endLine   int // original 1-based line number of the last window line
	normIdx   int // index into normalized lines
}

// buildWindowHashes creates hash entries for all sliding windows in a file.
// It checks for context cancellation every 1000 windows.
func buildWindowHashes(ctx context.Context, normalized []normalizedLine, path string, winSize int) ([]windowEntry, error) {
	if len(normalized) < winSize {
		return nil, nil
	}
	entries := make([]windowEntry, 0, len(normalized)-winSize+1)
	for i := 0; i <= len(normalized)-winSize; i++ {
		if i%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		entries = append(entries, windowEntry{
			hash:      hashWindow(normalized, i, winSize),
			path:      path,
			startLine: normalized[i].origLine,
			endLine:   normalized[i+winSize-1].origLine,
			normIdx:   i,
		})
	}
	return entries, nil
}

// groupClones groups window entries by hash and returns one clone group
// per hash with 2+ distinct (path, startLine) locations. Groups are
// single-window (winSize normalized lines); mergeCloneGroups extends
// overlapping windows into larger blocks.
// It checks for context cancellation every 1000 hash buckets.
func groupClones(ctx context.Context, entries []windowEntry, winSize int) ([]cloneGroup, error) {
	// Group by hash.
	byHash := make(map[uint64][]windowEntry)
	for _, e := range entries {
		byHash[e.hash] = append(byHash[e.hash], e)
	}

	var groups []cloneGroup
	checked := 0
	for _, matches := range byHash {
		checked++
		if checked%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

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
			end := u.endLine
			if end < u.startLine {
				end = u.startLine + winSize - 1
			}
			locs[i] = cloneLocation{Path: u.path, StartLine: u.startLine, EndLine: end}
		}
		groups = append(groups, cloneGroup{
			Lines:     winSize,
			Locations: locs,
		})
	}

	return groups, nil
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
