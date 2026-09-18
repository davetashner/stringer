// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import "regexp"

// fileOcc records how many times a token occurs in one file.
type fileOcc struct {
	file  int // index into symbolIndex.files
	count int
}

// symbolIndex is an inverted index from identifier tokens to per-file
// occurrence counts. It is built once per Collect so that each symbol
// lookup is a map access instead of a regexp scan over every file.
//
// Tokens are maximal runs of ASCII word bytes ([0-9A-Za-z_]), which is
// exactly the notion of "word" that Go's regexp `\b` uses. A symbol name
// made only of word bytes matches `\bname\b` in a file iff one of that
// file's tokens equals the name, so the count of equal tokens is the same
// as the count of regexp matches.
//
// Only tokens that are (segments of) declared symbol names are recorded,
// which keeps memory proportional to the number of symbols rather than the
// number of distinct identifiers in the repository.
type symbolIndex struct {
	files []fileContents
	occ   map[string][]fileOcc // token -> occurrences, ordered by file index
}

// isWordByte reports whether b is an ASCII word byte as defined by regexp's
// `\b` and `\w`: [0-9A-Za-z_]. All bytes >= 0x80 (non-ASCII) are non-word.
func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

// isWordOnly reports whether name consists solely of ASCII word bytes.
func isWordOnly(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isWordByte(name[i]) {
			return false
		}
	}
	return true
}

// wordSegments splits s into its maximal runs of ASCII word bytes, calling
// fn with the byte offsets of each run. It is the single tokenizer shared by
// index construction and symbol-name segmentation.
func wordSegments(s string, fn func(start, end int)) {
	start := -1
	for i := 0; i < len(s); i++ {
		if isWordByte(s[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			fn(start, i)
			start = -1
		}
	}
	if start >= 0 {
		fn(start, len(s))
	}
}

// nameSegments returns the word-byte segments of a symbol name. Names such
// as Elixir's "Foo.Bar" or Ruby's "valid?" contain non-word bytes; any
// regexp match of `\bFoo\.Bar\b` in a file necessarily contains "Foo" and
// "Bar" as whole tokens, so the segments bound the candidate files.
func nameSegments(name string) []string {
	var segs []string
	wordSegments(name, func(start, end int) {
		segs = append(segs, name[start:end])
	})
	return segs
}

// buildSymbolIndex tokenizes every file once and records per-file counts
// for every token that is a declared symbol name (or a segment of one).
func buildSymbolIndex(files []fileContents, symbols []symbolDef) *symbolIndex {
	occ := make(map[string][]fileOcc, len(symbols))
	for i := range symbols {
		for _, seg := range nameSegments(symbols[i].Name) {
			if _, ok := occ[seg]; !ok {
				occ[seg] = nil
			}
		}
	}

	for fi := range files {
		content := files[fi].content
		wordSegments(content, func(start, end int) {
			// Substring lookup: no allocation, the key is a view of content.
			s, ok := occ[content[start:end]]
			if !ok {
				return
			}
			if n := len(s); n > 0 && s[n-1].file == fi {
				s[n-1].count++
				return
			}
			occ[content[start:end]] = append(s, fileOcc{file: fi, count: 1})
		})
	}

	return &symbolIndex{files: files, occ: occ}
}

// lookupToken resolves a word-only symbol name against the index.
// Returns (dead, testOnly) with the same semantics as isDeadSymbol. When
// defInTest is set (the symbol itself lives in a test file) references from
// test files count as real references.
func (idx *symbolIndex) lookupToken(name, filePath string, defInTest bool) (dead bool, testOnly bool) {
	foundInTest := false
	for _, o := range idx.occ[name] {
		fc := &idx.files[o.file]
		if fc.relPath == filePath {
			// Same file: more than one occurrence means it is used locally.
			if o.count > 1 {
				return false, false
			}
			continue
		}
		// Different file: any occurrence means it is referenced.
		if !fc.isTest || defInTest {
			return false, false
		}
		foundInTest = true
	}
	if foundInTest {
		return false, true
	}
	return true, false
}

// candidateFiles returns the indices of files that contain every word
// segment of name as a whole token, in ascending order. A name with no word
// segments yields every file. This is the pre-filter for names that need
// the regexp path because they contain non-word bytes.
func (idx *symbolIndex) candidateFiles(name string) []int {
	segs := nameSegments(name)
	if len(segs) == 0 {
		all := make([]int, len(idx.files))
		for i := range all {
			all[i] = i
		}
		return all
	}

	// Start from the rarest segment and intersect with the rest.
	base := idx.occ[segs[0]]
	for _, seg := range segs[1:] {
		if s := idx.occ[seg]; len(s) < len(base) {
			base = s
		}
	}

	var out []int
	for _, o := range base {
		inAll := true
		for _, seg := range segs {
			if !hasFile(idx.occ[seg], o.file) {
				inAll = false
				break
			}
		}
		if inAll {
			out = append(out, o.file)
		}
	}
	return out
}

// hasFile reports whether the file-ordered occurrence list contains file.
func hasFile(occ []fileOcc, file int) bool {
	lo, hi := 0, len(occ)
	for lo < hi {
		mid := (lo + hi) / 2
		switch {
		case occ[mid].file < file:
			lo = mid + 1
		case occ[mid].file > file:
			hi = mid
		default:
			return true
		}
	}
	return false
}

// lookupRegex resolves a symbol name containing non-word bytes by running
// the word-boundary regexp over the candidate files only.
func (idx *symbolIndex) lookupRegex(pat *regexp.Regexp, name, filePath string, defInTest bool) (dead bool, testOnly bool) {
	foundInTest := false
	for _, fi := range idx.candidateFiles(name) {
		fc := &idx.files[fi]
		if fc.relPath == filePath {
			if len(pat.FindAllStringIndex(fc.content, -1)) > 1 {
				return false, false
			}
			continue
		}
		if !pat.MatchString(fc.content) {
			continue
		}
		if !fc.isTest || defInTest {
			return false, false
		}
		foundInTest = true
	}
	if foundInTest {
		return false, true
	}
	return true, false
}
