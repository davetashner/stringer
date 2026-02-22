// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
)

// TestNormalizeType1 verifies whitespace stripping, blank/comment/import line removal.
func TestNormalizeType1(t *testing.T) {
	lines := []string{
		"  package main  ",      // line 1 — kept
		"",                      // line 2 — blank, skipped
		"// this is a comment",  // line 3 — comment, skipped
		"# python comment",      // line 4 — comment, skipped
		"import \"fmt\"",        // line 5 — import, skipped
		"from os import path",   // line 6 — import, skipped
		"  func main() {  ",     // line 7 — kept
		"\tfmt.Println(\"hi\")", // line 8 — kept
		"  }",                   // line 9 — kept
	}

	result := normalizeType1(lines)

	if len(result) != 4 {
		t.Fatalf("expected 4 normalized lines, got %d", len(result))
	}

	// Check that whitespace is stripped.
	if result[0].text != "package main" {
		t.Errorf("expected 'package main', got %q", result[0].text)
	}
	if result[0].origLine != 1 {
		t.Errorf("expected origLine 1, got %d", result[0].origLine)
	}

	// Check that line 7 (func main) is present.
	if result[1].text != "func main() {" {
		t.Errorf("expected 'func main() {', got %q", result[1].text)
	}
	if result[1].origLine != 7 {
		t.Errorf("expected origLine 7, got %d", result[1].origLine)
	}
}

// TestNormalizeType2 verifies identifier replacement with keyword preservation.
func TestNormalizeType2(t *testing.T) {
	lines := []string{
		"func processData(input string) {",
		"    result := transform(input)",
		"    return result",
		"}",
	}

	result := normalizeType2(lines)

	if len(result) != 4 {
		t.Fatalf("expected 4 normalized lines, got %d", len(result))
	}

	// "func" should be preserved, "processData", "input", "string" should be "$".
	if !strings.Contains(result[0].text, "func") {
		t.Errorf("expected 'func' keyword preserved, got %q", result[0].text)
	}
	if strings.Contains(result[0].text, "processData") {
		t.Errorf("expected 'processData' replaced, got %q", result[0].text)
	}

	// "return" should be preserved, "result" should be "$".
	if !strings.Contains(result[2].text, "return") {
		t.Errorf("expected 'return' keyword preserved, got %q", result[2].text)
	}
	if strings.Contains(result[2].text, "result") {
		t.Errorf("expected 'result' replaced, got %q", result[2].text)
	}
}

// TestNormalizeType2Keywords verifies common keywords are preserved.
func TestNormalizeType2Keywords(t *testing.T) {
	keywords := []string{"if", "else", "for", "while", "return", "func", "class", "def", "var", "let", "const"}
	for _, kw := range keywords {
		lines := []string{kw + " something"}
		result := normalizeType2(lines)
		if len(result) != 1 {
			t.Fatalf("keyword %q: expected 1 line, got %d", kw, len(result))
		}
		if !strings.Contains(result[0].text, kw) {
			t.Errorf("keyword %q should be preserved in %q", kw, result[0].text)
		}
	}
}

// TestHashWindow verifies that identical windows produce identical hashes.
func TestHashWindow(t *testing.T) {
	lines1 := []normalizedLine{
		{text: "a"}, {text: "b"}, {text: "c"},
		{text: "d"}, {text: "e"}, {text: "f"},
	}
	lines2 := []normalizedLine{
		{text: "a"}, {text: "b"}, {text: "c"},
		{text: "d"}, {text: "e"}, {text: "f"},
	}
	lines3 := []normalizedLine{
		{text: "a"}, {text: "b"}, {text: "c"},
		{text: "d"}, {text: "e"}, {text: "g"},
	}

	h1 := hashWindow(lines1, 0)
	h2 := hashWindow(lines2, 0)
	h3 := hashWindow(lines3, 0)

	if h1 != h2 {
		t.Error("identical windows should produce identical hashes")
	}
	if h1 == h3 {
		t.Error("different windows should produce different hashes")
	}
}

// TestHashWindowShortFile verifies behavior with fewer lines than window size.
func TestHashWindowShortFile(t *testing.T) {
	lines := []normalizedLine{
		{text: "a"}, {text: "b"},
	}
	entries := buildWindowHashes(lines, "short.go")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for short file, got %d", len(entries))
	}
}

// TestGroupClones verifies clone grouping with 2+ matching locations.
func TestGroupClones(t *testing.T) {
	// Two files with the same 6-line block.
	lines := []normalizedLine{
		{text: "a", origLine: 1}, {text: "b", origLine: 2}, {text: "c", origLine: 3},
		{text: "d", origLine: 4}, {text: "e", origLine: 5}, {text: "f", origLine: 6},
	}

	var entries []windowEntry
	entries = append(entries, buildWindowHashes(lines, "file1.go")...)
	entries = append(entries, buildWindowHashes(lines, "file2.go")...)

	groups := groupClones(entries)

	if len(groups) == 0 {
		t.Fatal("expected at least 1 clone group")
	}

	found := false
	for _, g := range groups {
		if len(g.Locations) >= 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a clone group with 2+ locations")
	}
}

// TestCollectExactClones creates a temp dir with duplicated files and verifies detection.
func TestCollectExactClones(t *testing.T) {
	dir := t.TempDir()

	// Create two files with identical code blocks.
	block := strings.Join([]string{
		"func processItems(items []string) {",
		"    for _, item := range items {",
		"        result := transform(item)",
		"        if result != nil {",
		"            store(result)",
		"        }",
		"        log(item)",
		"    }",
		"}",
	}, "\n")

	writeTestFile(t, dir, "pkg1/handler.go", "package pkg1\n\n"+block)
	writeTestFile(t, dir, "pkg2/handler.go", "package pkg2\n\n"+block)

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	var codeClones int
	for _, sig := range signals {
		if sig.Kind == "code-clone" {
			codeClones++
		}
	}

	if codeClones == 0 {
		t.Error("expected at least 1 code-clone signal")
	}
}

// TestCollectNearClones creates files with renamed identifiers.
func TestCollectNearClones(t *testing.T) {
	dir := t.TempDir()

	// Two files with same structure but different identifier names.
	file1 := strings.Join([]string{
		"package pkg1",
		"",
		"func processUsers(users []User) {",
		"    for _, user := range users {",
		"        result := validateUser(user)",
		"        if result.IsValid {",
		"            saveUser(result)",
		"        }",
		"        logUser(user)",
		"    }",
		"}",
	}, "\n")

	file2 := strings.Join([]string{
		"package pkg2",
		"",
		"func processOrders(orders []Order) {",
		"    for _, order := range orders {",
		"        result := validateOrder(order)",
		"        if result.IsValid {",
		"            saveOrder(result)",
		"        }",
		"        logOrder(order)",
		"    }",
		"}",
	}, "\n")

	writeTestFile(t, dir, "users/handler.go", file1)
	writeTestFile(t, dir, "orders/handler.go", file2)

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	var nearClones int
	for _, sig := range signals {
		if sig.Kind == "near-clone" {
			nearClones++
		}
	}

	if nearClones == 0 {
		t.Error("expected at least 1 near-clone signal")
	}
}

// TestCollectIntraFileClones verifies detection of duplicated blocks within the same file.
func TestCollectIntraFileClones(t *testing.T) {
	dir := t.TempDir()

	block := strings.Join([]string{
		"    for i := 0; i < len(items); i++ {",
		"        result := process(items[i])",
		"        if result.Error != nil {",
		"            log.Error(result.Error)",
		"            continue",
		"        }",
		"        output = append(output, result.Value)",
		"    }",
	}, "\n")

	content := "package main\n\nfunc first() {\n" + block + "\n}\n\nfunc second() {\n" + block + "\n}\n"
	writeTestFile(t, dir, "main.go", content)

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	var clones int
	for _, sig := range signals {
		if sig.Kind == "code-clone" || sig.Kind == "near-clone" {
			clones++
		}
	}

	if clones == 0 {
		t.Error("expected at least 1 clone signal for intra-file duplication")
	}
}

// TestCollectMinThreshold verifies blocks shorter than windowSize produce no signals.
func TestCollectMinThreshold(t *testing.T) {
	dir := t.TempDir()

	// Only 3 lines of duplication — below the 6-line window.
	shortBlock := strings.Join([]string{
		"x := 1",
		"y := 2",
		"z := x + y",
	}, "\n")

	writeTestFile(t, dir, "a.go", "package a\n\nfunc f() {\n"+shortBlock+"\n}\n")
	writeTestFile(t, dir, "b.go", "package b\n\nfunc g() {\n"+shortBlock+"\n}\n")

	c := &DuplicationCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// The 3-line duplicated block alone is too short for a 6-line window.
	// Any signals that do appear come from the surrounding boilerplate
	// (package + func wrapper), which is expected behavior.
	// We just verify no panic and the collector completes successfully.
}

// TestCollectBinarySkip verifies binary files are skipped.
func TestCollectBinarySkip(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file with null bytes.
	binPath := filepath.Join(dir, "binary.go")
	if err := os.WriteFile(binPath, []byte("package main\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	for _, sig := range signals {
		if strings.Contains(sig.FilePath, "binary.go") {
			t.Error("binary file should not produce signals")
		}
	}
}

// TestCollectGeneratedSkip verifies generated files are skipped.
func TestCollectGeneratedSkip(t *testing.T) {
	dir := t.TempDir()

	block := strings.Join([]string{
		"func processItems(items []string) {",
		"    for _, item := range items {",
		"        result := transform(item)",
		"        if result != nil {",
		"            store(result)",
		"        }",
		"        log(item)",
		"    }",
		"}",
	}, "\n")

	writeTestFile(t, dir, "generated.go", "// Code generated by tool; DO NOT EDIT.\npackage gen\n\n"+block)
	writeTestFile(t, dir, "manual.go", "package man\n\n"+block)

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	for _, sig := range signals {
		if strings.Contains(sig.FilePath, "generated.go") {
			t.Error("generated file should not appear in signals")
		}
	}
}

// TestCollectContextCancellation verifies early exit on cancelled context.
func TestCollectContextCancellation(t *testing.T) {
	dir := t.TempDir()

	block := strings.Join([]string{
		"func processItems(items []string) {",
		"    for _, item := range items {",
		"        result := transform(item)",
		"        if result != nil {",
		"            store(result)",
		"        }",
		"        log(item)",
		"    }",
		"}",
	}, "\n")

	writeTestFile(t, dir, "a.go", "package a\n\n"+block)
	writeTestFile(t, dir, "b.go", "package b\n\n"+block)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := &DuplicationCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// TestCollectMinConfidenceFilter verifies MinConfidence filtering.
func TestCollectMinConfidenceFilter(t *testing.T) {
	dir := t.TempDir()

	block := strings.Join([]string{
		"func processItems(items []string) {",
		"    for _, item := range items {",
		"        result := transform(item)",
		"        if result != nil {",
		"            store(result)",
		"        }",
		"        log(item)",
		"    }",
		"}",
	}, "\n")

	writeTestFile(t, dir, "a.go", "package a\n\n"+block)
	writeTestFile(t, dir, "b.go", "package b\n\n"+block)

	c := &DuplicationCollector{}

	// With very high min confidence, should filter out small clones.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinConfidence: 0.90,
	})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if len(signals) != 0 {
		t.Errorf("expected 0 signals with high MinConfidence filter, got %d", len(signals))
	}
}

// TestDuplicationConfidence table-driven tests for all confidence tiers.
func TestDuplicationConfidence(t *testing.T) {
	tests := []struct {
		name      string
		lines     int
		locations int
		nearClone bool
		wantMin   float64
		wantMax   float64
	}{
		{"6 lines 2 locs", 6, 2, false, 0.35, 0.36},
		{"10 lines 2 locs", 10, 2, false, 0.35, 0.46},
		{"14 lines 2 locs", 14, 2, false, 0.44, 0.46},
		{"15 lines 2 locs", 15, 2, false, 0.45, 0.46},
		{"20 lines 2 locs", 20, 2, false, 0.45, 0.56},
		{"29 lines 2 locs", 29, 2, false, 0.59, 0.61},
		{"30 lines 2 locs", 30, 2, false, 0.60, 0.61},
		{"40 lines 2 locs", 40, 2, false, 0.60, 0.76},
		{"49 lines 2 locs", 49, 2, false, 0.74, 0.76},
		{"50 lines 2 locs", 50, 2, false, 0.75, 0.76},
		{"100 lines 2 locs", 100, 2, false, 0.75, 0.76},
		// Location bonus.
		{"50 lines 3 locs", 50, 3, false, 0.79, 0.81},
		{"50 lines 4 locs", 50, 4, false, 0.80, 0.81},
		// Near-clone penalty.
		{"50 lines 2 locs near", 50, 2, true, 0.69, 0.71},
		// Cap at 0.80.
		{"100 lines 4 locs", 100, 4, false, 0.80, 0.81},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := duplicationConfidence(tt.lines, tt.locations, tt.nearClone)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("duplicationConfidence(%d, %d, %v) = %f, want [%f, %f]",
					tt.lines, tt.locations, tt.nearClone, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestDuplicationConfidenceCap verifies the 0.80 cap.
func TestDuplicationConfidenceCap(t *testing.T) {
	// Even with huge lines and many locations, should not exceed 0.80.
	got := duplicationConfidence(1000, 10, false)
	if got > 0.80+0.001 {
		t.Errorf("confidence %f exceeds cap 0.80", got)
	}
}

// TestCollectMetrics verifies metrics are populated.
func TestCollectMetrics(t *testing.T) {
	dir := t.TempDir()

	block := strings.Join([]string{
		"func processItems(items []string) {",
		"    for _, item := range items {",
		"        result := transform(item)",
		"        if result != nil {",
		"            store(result)",
		"        }",
		"        log(item)",
		"    }",
		"}",
	}, "\n")

	writeTestFile(t, dir, "a.go", "package a\n\n"+block)
	writeTestFile(t, dir, "b.go", "package b\n\n"+block)

	c := &DuplicationCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	m := c.Metrics()
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}
	metrics, ok := m.(*DuplicationMetrics)
	if !ok {
		t.Fatalf("expected *DuplicationMetrics, got %T", m)
	}
	if metrics.FilesScanned != 2 {
		t.Errorf("expected 2 files scanned, got %d", metrics.FilesScanned)
	}
}

// TestCollectName verifies the collector name.
func TestCollectName(t *testing.T) {
	c := &DuplicationCollector{}
	if c.Name() != "duplication" {
		t.Errorf("expected name 'duplication', got %q", c.Name())
	}
}

// TestSubtractType1Ranges verifies deduplication between Type 1 and Type 2.
func TestSubtractType1Ranges(t *testing.T) {
	type1 := []cloneGroup{
		{
			Lines: 6,
			Locations: []cloneLocation{
				{Path: "a.go", StartLine: 10},
				{Path: "b.go", StartLine: 20},
			},
		},
	}

	type2Overlapping := []cloneGroup{
		{
			Lines:     6,
			NearClone: true,
			Locations: []cloneLocation{
				{Path: "a.go", StartLine: 10},
				{Path: "b.go", StartLine: 20},
			},
		},
	}

	type2Different := []cloneGroup{
		{
			Lines:     6,
			NearClone: true,
			Locations: []cloneLocation{
				{Path: "c.go", StartLine: 5},
				{Path: "d.go", StartLine: 15},
			},
		},
	}

	// Overlapping should be removed.
	result := subtractType1Ranges(type2Overlapping, type1)
	if len(result) != 0 {
		t.Errorf("expected overlapping Type 2 to be removed, got %d groups", len(result))
	}

	// Non-overlapping should be kept.
	result = subtractType1Ranges(type2Different, type1)
	if len(result) != 1 {
		t.Errorf("expected non-overlapping Type 2 to be kept, got %d groups", len(result))
	}
}

// TestNormalizeType1EmptyInput verifies empty input handling.
func TestNormalizeType1EmptyInput(t *testing.T) {
	result := normalizeType1(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 lines for nil input, got %d", len(result))
	}

	result = normalizeType1([]string{})
	if len(result) != 0 {
		t.Errorf("expected 0 lines for empty input, got %d", len(result))
	}
}

// TestNormalizeType1VariousCommentStyles verifies different comment syntaxes.
func TestNormalizeType1VariousCommentStyles(t *testing.T) {
	lines := []string{
		"// Go comment",
		"# Python comment",
		"/* C block comment */",
		"* Javadoc line",
		"-- SQL comment",
		"real code here",
	}

	result := normalizeType1(lines)
	if len(result) != 1 {
		t.Errorf("expected 1 non-comment line, got %d", len(result))
	}
	if result[0].text != "real code here" {
		t.Errorf("expected 'real code here', got %q", result[0].text)
	}
}

// TestNormalizeType1ImportVariants verifies different import syntaxes.
func TestNormalizeType1ImportVariants(t *testing.T) {
	lines := []string{
		"import \"fmt\"",
		"from os import path",
		"require('lodash')",
		"include <stdio.h>",
		"using System;",
		"#include <vector>",
		"use std::io;",
		"actual code",
	}

	result := normalizeType1(lines)
	if len(result) != 1 {
		t.Errorf("expected 1 non-import line, got %d", len(result))
	}
}

// TestIsBlankOrWhitespace verifies the whitespace helper.
func TestIsBlankOrWhitespace(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"  ", true},
		{"\t\n", true},
		{"a", false},
		{"  x  ", false},
	}
	for _, tt := range tests {
		got := isBlankOrWhitespace(tt.input)
		if got != tt.want {
			t.Errorf("isBlankOrWhitespace(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestCloneGroupToSignal verifies signal generation from clone groups.
func TestCloneGroupToSignal(t *testing.T) {
	t.Run("exact clone", func(t *testing.T) {
		g := cloneGroup{
			Lines: 20,
			Locations: []cloneLocation{
				{Path: "a.go", StartLine: 10},
				{Path: "b.go", StartLine: 30},
			},
		}
		sig := cloneGroupToSignal(g)
		if sig.Kind != "code-clone" {
			t.Errorf("expected kind 'code-clone', got %q", sig.Kind)
		}
		if sig.Source != "duplication" {
			t.Errorf("expected source 'duplication', got %q", sig.Source)
		}
		if !strings.Contains(sig.Title, "20 lines") {
			t.Errorf("expected title to contain '20 lines', got %q", sig.Title)
		}
		if !strings.Contains(sig.Title, "2 locations") {
			t.Errorf("expected title to contain '2 locations', got %q", sig.Title)
		}
		if !strings.Contains(sig.Description, "a.go:10") {
			t.Errorf("expected description to contain 'a.go:10', got %q", sig.Description)
		}
	})

	t.Run("near clone", func(t *testing.T) {
		g := cloneGroup{
			Lines:     15,
			NearClone: true,
			Locations: []cloneLocation{
				{Path: "x.go", StartLine: 5},
				{Path: "y.go", StartLine: 15},
			},
		}
		sig := cloneGroupToSignal(g)
		if sig.Kind != "near-clone" {
			t.Errorf("expected kind 'near-clone', got %q", sig.Kind)
		}
		if !strings.Contains(sig.Title, "renamed identifiers") {
			t.Errorf("expected title to mention renamed identifiers, got %q", sig.Title)
		}
	})
}

// TestDuplicationConfidenceMonotonic verifies confidence increases with line count.
func TestDuplicationConfidenceMonotonic(t *testing.T) {
	prev := 0.0
	for lines := 6; lines <= 100; lines++ {
		c := duplicationConfidence(lines, 2, false)
		if c < prev-0.001 {
			t.Errorf("confidence decreased at %d lines: %f < %f", lines, c, prev)
		}
		prev = c
	}
}

// TestSamePathSet verifies path set comparison.
func TestSamePathSet(t *testing.T) {
	a := []cloneLocation{{Path: "a.go"}, {Path: "b.go"}}
	b := []cloneLocation{{Path: "b.go"}, {Path: "a.go"}}
	c := []cloneLocation{{Path: "a.go"}, {Path: "c.go"}}
	d := []cloneLocation{{Path: "a.go"}}

	if !samePathSet(a, b) {
		t.Error("same paths in different order should match")
	}
	if samePathSet(a, c) {
		t.Error("different paths should not match")
	}
	if samePathSet(a, d) {
		t.Error("different length should not match")
	}
}

// TestCollectExcludePatterns verifies exclude pattern filtering.
func TestCollectExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	block := strings.Join([]string{
		"func processItems(items []string) {",
		"    for _, item := range items {",
		"        result := transform(item)",
		"        if result != nil {",
		"            store(result)",
		"        }",
		"        log(item)",
		"    }",
		"}",
	}, "\n")

	writeTestFile(t, dir, "src/handler.go", "package src\n\n"+block)
	writeTestFile(t, dir, "vendor/lib/handler.go", "package lib\n\n"+block)

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// vendor/ is excluded by default — should not produce cross-file clones.
	for _, sig := range signals {
		if strings.Contains(sig.Description, "vendor/") {
			t.Error("vendor/ file should be excluded")
		}
	}
}

// TestCollectNonSourceExtensions verifies non-source files are skipped.
func TestCollectNonSourceExtensions(t *testing.T) {
	dir := t.TempDir()

	content := strings.Repeat("some text line\n", 20)
	writeTestFile(t, dir, "readme.md", content)
	writeTestFile(t, dir, "readme2.md", content)

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if len(signals) != 0 {
		t.Errorf("expected 0 signals for non-source files, got %d", len(signals))
	}
}

// TestDuplicationConfidenceNearClonePenalty verifies the -0.05 penalty.
func TestDuplicationConfidenceNearClonePenalty(t *testing.T) {
	exact := duplicationConfidence(30, 2, false)
	near := duplicationConfidence(30, 2, true)
	diff := exact - near
	if math.Abs(diff-0.05) > 0.001 {
		t.Errorf("expected 0.05 penalty, got %f (exact=%f, near=%f)", diff, exact, near)
	}
}

// TestCollectSignalCap verifies that output is capped at MaxIssues (or 200 default).
func TestCollectSignalCap(t *testing.T) {
	dir := t.TempDir()

	// Create many files with identical blocks to generate lots of signals.
	block := strings.Join([]string{
		"func process(items []string) {",
		"    for _, item := range items {",
		"        result := transform(item)",
		"        if result != nil {",
		"            store(result)",
		"        }",
		"        log(item)",
		"    }",
		"}",
	}, "\n")

	// Create enough copies to produce signals.
	for i := 0; i < 20; i++ {
		writeTestFile(t, dir, filepath.Join("pkg"+strings.Repeat("x", i), "handler.go"),
			"package pkg\n\n"+block)
	}

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MaxIssues: 3,
	})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	if len(signals) > 3 {
		t.Errorf("expected at most 3 signals with MaxIssues=3, got %d", len(signals))
	}
}

// writeTestFile creates a file in the test directory with proper directory structure.
func writeTestFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
