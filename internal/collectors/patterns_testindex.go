// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"path/filepath"
	"strings"
	"unicode"
)

// minSuffixSubjectLen is the minimum length of a source stem before a
// prefixed test name (e.g. NotificationDatabaseChannelTest for
// DatabaseChannel) is accepted as a match. Very short stems such as "Log"
// would otherwise be covered by almost any test file.
const minSuffixSubjectLen = 4

// testIndex is a repo-wide index of test files keyed by the source-file stem
// each test basename implies under common naming conventions. It is built
// once per scan so that a source file counts as tested when a matching test
// file exists anywhere in the repository — sibling test projects
// (tests/<Project>.Tests/), Maven trees (src/test/java/<pkg>/), and
// framework test roots (tests/Integration/...) all resolve by basename.
type testIndex struct {
	// exact holds subject stems implied by test basenames, e.g. "KafkaRaftLog"
	// for KafkaRaftLogTest.java, "handler" for handler_test.go or test_handler.py.
	exact map[string]struct{}
	// suffixes holds CamelCase tails of subject stems taken from <Name>Test(s)
	// files, so that NotificationDatabaseChannelTest.php covers
	// DatabaseChannel.php (prefixed test-class names).
	suffixes map[string]struct{}
}

// newTestIndex returns an empty index.
func newTestIndex() *testIndex {
	return &testIndex{
		exact:    make(map[string]struct{}),
		suffixes: make(map[string]struct{}),
	}
}

// add records the test file at relPath in the index. Files whose basename
// carries no recognised test affix are ignored.
func (ti *testIndex) add(relPath string) {
	stem, camel := testSubjectStem(filepath.Base(relPath))
	if stem == "" {
		return
	}
	ti.exact[stem] = struct{}{}
	if !camel {
		return
	}
	for _, tail := range camelTails(stem) {
		ti.suffixes[tail] = struct{}{}
	}
}

// covers reports whether the index holds a test file for the source file
// with the given basename (extension included).
func (ti *testIndex) covers(sourceBase string) bool {
	stem := strings.TrimSuffix(sourceBase, filepath.Ext(sourceBase))
	if stem == "" {
		return false
	}
	if _, ok := ti.exact[stem]; ok {
		return true
	}
	if len(stem) < minSuffixSubjectLen {
		return false
	}
	_, ok := ti.suffixes[stem]
	return ok
}

// testSubjectStem strips the test affix from a test-file basename and returns
// the implied source stem. The second result is true when the affix was a
// CamelCase class suffix (<Name>Test, <Name>Tests, <Name>Spec, <Name>Suite),
// which is the only form where prefixed names are conventional.
//
// Recognised forms:
//
//	<Name>Test, <Name>Tests, <Name>Spec, <Name>Suite   (Java, Kotlin, Scala, C#, PHP, Swift)
//	Test<Name>                                        (JUnit-style prefix)
//	<name>_test, <name>_spec, test_<name>             (Go, Python, Ruby, Rust, Elixir, PHP)
//	<Name>.test, <Name>.spec                          (JS/TS)
func testSubjectStem(base string) (string, bool) {
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		return "", false
	}

	// JS/TS: foo.test.ts, foo.spec.tsx → foo
	for _, suf := range []string{".test", ".spec"} {
		if s, ok := strings.CutSuffix(stem, suf); ok && s != "" {
			return s, false
		}
	}
	// snake_case: foo_test, foo_spec, test_foo
	for _, suf := range []string{"_test", "_spec"} {
		if s, ok := strings.CutSuffix(stem, suf); ok && s != "" {
			return s, false
		}
	}
	if s, ok := strings.CutPrefix(stem, "test_"); ok && s != "" {
		return s, false
	}
	// CamelCase class suffixes: FooTests before FooTest so the longer form wins.
	for _, suf := range []string{"Tests", "Test", "Spec", "Suite"} {
		if s, ok := strings.CutSuffix(stem, suf); ok && s != "" {
			return s, true
		}
	}
	// JUnit-style prefix: TestFoo → Foo (only when followed by an upper-case
	// letter so that "Testing" or "Tester" are not treated as test files).
	if s, ok := strings.CutPrefix(stem, "Test"); ok && s != "" && unicode.IsUpper(rune(s[0])) {
		return s, true
	}
	return "", false
}

// camelTails returns every proper suffix of stem that starts at an upper-case
// letter, e.g. "NotificationDatabaseChannel" → ["DatabaseChannel", "Channel"].
func camelTails(stem string) []string {
	var tails []string
	for i, r := range stem {
		if i == 0 || !unicode.IsUpper(r) {
			continue
		}
		tails = append(tails, stem[i:])
	}
	return tails
}
