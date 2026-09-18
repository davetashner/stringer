// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
)

// distinctLines returns n lines that differ under both Type 1 and Type 2
// normalization (numbers are not identifiers, so they survive renaming).
func distinctLines(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("    %s%d := compute(%d)", prefix, i, i)
	}
	return out
}

func TestMergeCloneGroups_OverlappingWindowsBecomeOneGroup(t *testing.T) {
	// Sliding windows at consecutive offsets in two files, as groupClones
	// emits them for a 10-line clone with a 6-line window.
	var groups []cloneGroup
	for off := 0; off < 5; off++ {
		groups = append(groups, cloneGroup{Lines: 6, Locations: []cloneLocation{
			{Path: "a.go", StartLine: 10 + off, EndLine: 15 + off},
			{Path: "b.go", StartLine: 40 + off, EndLine: 45 + off},
		}})
	}

	merged := mergeCloneGroups(groups)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged group, got %d: %+v", len(merged), merged)
	}
	g := merged[0]
	if g.Lines != 10 {
		t.Errorf("expected merged span 10, got %d", g.Lines)
	}
	if g.NearClone {
		t.Error("all-exact members must stay an exact clone")
	}
	want := []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 19}, {Path: "b.go", StartLine: 40, EndLine: 49}}
	if !slices.Equal(g.Locations, want) {
		t.Errorf("expected locations %+v, got %+v", want, g.Locations)
	}
}

func TestMergeCloneGroups_AdjacentWindowsMerge(t *testing.T) {
	// Touching (not overlapping) ranges in the same file merge too, and pull
	// in the other file each window matched.
	groups := []cloneGroup{
		{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 15}, {Path: "b.go", StartLine: 1, EndLine: 6}}},
		{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 16, EndLine: 21}, {Path: "c.go", StartLine: 1, EndLine: 6}}},
	}
	merged := mergeCloneGroups(groups)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged group, got %d", len(merged))
	}
	if len(merged[0].Locations) != 3 {
		t.Errorf("expected union of 3 locations, got %+v", merged[0].Locations)
	}
	if merged[0].Locations[0] != (cloneLocation{Path: "a.go", StartLine: 10, EndLine: 21}) {
		t.Errorf("expected anchor range a.go:10-21, got %+v", merged[0].Locations[0])
	}
	// Two distinct 6-line blocks share a region; Lines stays the longest
	// member block rather than the region span.
	if merged[0].Lines != 6 {
		t.Errorf("expected Lines 6, got %d", merged[0].Lines)
	}
}

func TestMergeCloneGroups_NonAnchorOverlapDoesNotChain(t *testing.T) {
	// Boilerplate shared across files: each group's second location
	// overlaps the next group's anchor. Only anchors chain, so these stay
	// three signals instead of snowballing into one repo-wide cluster.
	groups := []cloneGroup{
		{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 15}, {Path: "b.go", StartLine: 10, EndLine: 15}}},
		{Lines: 6, Locations: []cloneLocation{{Path: "b.go", StartLine: 14, EndLine: 19}, {Path: "c.go", StartLine: 10, EndLine: 15}}},
		{Lines: 6, Locations: []cloneLocation{{Path: "c.go", StartLine: 14, EndLine: 19}, {Path: "d.go", StartLine: 10, EndLine: 15}}},
	}
	merged := mergeCloneGroups(groups)
	if len(merged) != 3 {
		t.Fatalf("expected 3 groups, got %d: %+v", len(merged), merged)
	}
	for _, g := range merged {
		if len(g.Locations) != 2 || g.Lines != 6 {
			t.Errorf("expected untouched 6-line 2-location group, got %+v", g)
		}
	}
}

func TestMergeCloneGroups_DifferentMatchSetsInOneRegion(t *testing.T) {
	// The bead's context_test.go case: consecutive windows in one region
	// match different sets of other places. One signal for the region,
	// listing every other location, exact only if all members are exact.
	groups := []cloneGroup{
		{Lines: 6, Locations: []cloneLocation{{Path: "ctx_test.go", StartLine: 2796, EndLine: 2801}, {Path: "x.go", StartLine: 10, EndLine: 15}}},
		{Lines: 6, NearClone: true, Locations: []cloneLocation{{Path: "ctx_test.go", StartLine: 2799, EndLine: 2804}, {Path: "y.go", StartLine: 10, EndLine: 15}, {Path: "z.go", StartLine: 10, EndLine: 15}}},
		{Lines: 6, Locations: []cloneLocation{{Path: "ctx_test.go", StartLine: 2800, EndLine: 2805}, {Path: "x.go", StartLine: 14, EndLine: 19}}},
	}
	merged := mergeCloneGroups(groups)
	if len(merged) != 1 {
		t.Fatalf("expected 1 region signal, got %d: %+v", len(merged), merged)
	}
	g := merged[0]
	if !g.NearClone {
		t.Error("a region with a near-clone member is reported as near-clone")
	}
	if g.Locations[0] != (cloneLocation{Path: "ctx_test.go", StartLine: 2796, EndLine: 2805}) {
		t.Errorf("expected anchor ctx_test.go:2796-2805, got %+v", g.Locations[0])
	}
	if len(g.Locations) != 4 { // ctx_test, x.go:10-19, y.go, z.go
		t.Errorf("expected 4 collapsed locations, got %+v", g.Locations)
	}
}

func TestShiftCompatible(t *testing.T) {
	a := []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 15}, {Path: "b.go", StartLine: 10, EndLine: 15}}
	if !shiftCompatible(a, []cloneLocation{{Path: "a.go", StartLine: 11, EndLine: 16}, {Path: "b.go", StartLine: 11, EndLine: 16}}) {
		t.Error("shifted copy should be compatible")
	}
	if shiftCompatible(a, []cloneLocation{{Path: "a.go", StartLine: 11, EndLine: 16}, {Path: "b.go", StartLine: 40, EndLine: 45}}) {
		t.Error("second location far away should not be compatible")
	}
	if shiftCompatible(a, []cloneLocation{{Path: "a.go", StartLine: 11, EndLine: 16}, {Path: "c.go", StartLine: 11, EndLine: 16}}) {
		t.Error("different path should not be compatible")
	}
	if shiftCompatible(a, a[:1]) {
		t.Error("different length should not be compatible")
	}
}

func TestUnionFindClusters(t *testing.T) {
	uf := newUnionFind(5)
	uf.union(0, 3)
	uf.union(3, 4)
	uf.union(4, 0) // already joined
	got := uf.clusters()
	if len(got) != 3 {
		t.Fatalf("expected 3 clusters, got %v", got)
	}
	if len(got[0]) != 3 || got[0][0] != 0 {
		t.Errorf("expected first cluster {0,3,4}, got %v", got[0])
	}
}

func TestMergeCloneGroups_SeparateRegionsStaySeparate(t *testing.T) {
	groups := []cloneGroup{
		{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 15}, {Path: "b.go", StartLine: 1, EndLine: 6}}},
		{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 100, EndLine: 105}, {Path: "b.go", StartLine: 50, EndLine: 55}}},
	}
	merged := mergeCloneGroups(groups)
	if len(merged) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(merged))
	}
	// Deterministic order by first location.
	if merged[0].Locations[0].StartLine != 10 || merged[1].Locations[0].StartLine != 100 {
		t.Errorf("expected groups sorted by first location, got %+v", merged)
	}
}

func TestMergeCloneGroups_SelfOverlapCollapsesAndDrops(t *testing.T) {
	// A repetitive list matches itself one line over: every location lies
	// in the same contiguous region of one file, so there is nothing to
	// deduplicate against.
	groups := []cloneGroup{
		{Lines: 6, NearClone: true, Locations: []cloneLocation{
			{Path: "list.py", StartLine: 671, EndLine: 676},
			{Path: "list.py", StartLine: 672, EndLine: 677},
			{Path: "list.py", StartLine: 673, EndLine: 678},
		}},
	}
	if merged := mergeCloneGroups(groups); len(merged) != 0 {
		t.Errorf("expected self-overlapping group to be dropped, got %+v", merged)
	}
}

func TestMergeCloneGroups_IntraFileDistinctRegionsKept(t *testing.T) {
	// Two genuinely separate regions of the same file, each reported by
	// two overlapping windows: one group, two locations, span 7.
	groups := []cloneGroup{
		{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 15}, {Path: "a.go", StartLine: 50, EndLine: 55}}},
		{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 11, EndLine: 16}, {Path: "a.go", StartLine: 51, EndLine: 56}}},
	}
	merged := mergeCloneGroups(groups)
	if len(merged) != 1 || len(merged[0].Locations) != 2 || merged[0].Lines != 7 {
		t.Fatalf("expected one 7-line group with 2 locations, got %+v", merged)
	}
}

func TestMergeCloneGroups_ExactVsNear(t *testing.T) {
	exact := cloneGroup{Lines: 6, Locations: []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 29}, {Path: "b.go", StartLine: 10, EndLine: 29}}}

	t.Run("near extending within slack reports exact", func(t *testing.T) {
		near := cloneGroup{Lines: 6, NearClone: true, Locations: []cloneLocation{{Path: "a.go", StartLine: 9, EndLine: 29}, {Path: "b.go", StartLine: 9, EndLine: 29}}}
		merged := mergeCloneGroups([]cloneGroup{exact, near})
		if len(merged) != 1 {
			t.Fatalf("expected 1 group, got %d", len(merged))
		}
		if merged[0].NearClone || merged[0].Lines != 20 || merged[0].Locations[0].StartLine != 10 {
			t.Errorf("expected 20-line exact clone at a.go:10, got %+v", merged[0])
		}
	})

	t.Run("near much larger than exact reports near with union", func(t *testing.T) {
		near := cloneGroup{Lines: 6, NearClone: true, Locations: []cloneLocation{{Path: "a.go", StartLine: 1, EndLine: 60}, {Path: "b.go", StartLine: 1, EndLine: 60}}}
		merged := mergeCloneGroups([]cloneGroup{exact, near})
		if len(merged) != 1 {
			t.Fatalf("expected 1 group, got %d", len(merged))
		}
		if !merged[0].NearClone || merged[0].Lines != 60 || merged[0].Locations[0].StartLine != 1 {
			t.Errorf("expected 60-line near clone at a.go:1, got %+v", merged[0])
		}
	})

	t.Run("near with an extra renamed copy reports near", func(t *testing.T) {
		near := cloneGroup{Lines: 6, NearClone: true, Locations: []cloneLocation{
			{Path: "a.go", StartLine: 10, EndLine: 29}, {Path: "b.go", StartLine: 10, EndLine: 29}, {Path: "c.go", StartLine: 10, EndLine: 29},
		}}
		merged := mergeCloneGroups([]cloneGroup{exact, near})
		if len(merged) != 1 || !merged[0].NearClone || len(merged[0].Locations) != 3 {
			t.Errorf("expected a 3-location near clone, got %+v", merged)
		}
	})

	t.Run("near only stays near", func(t *testing.T) {
		near := cloneGroup{Lines: 6, NearClone: true, Locations: []cloneLocation{{Path: "x.go", StartLine: 1, EndLine: 6}, {Path: "y.go", StartLine: 1, EndLine: 6}}}
		merged := mergeCloneGroups([]cloneGroup{near})
		if len(merged) != 1 || !merged[0].NearClone {
			t.Errorf("expected near clone preserved, got %+v", merged)
		}
	})
}

func TestMergeCloneGroups_Empty(t *testing.T) {
	if got := mergeCloneGroups(nil); got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
}

func TestCollapseLocations(t *testing.T) {
	if got := collapseLocations(nil); got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
	locs := []cloneLocation{
		{Path: "b.go", StartLine: 5, EndLine: 10},
		{Path: "a.go", StartLine: 20, EndLine: 25},
		{Path: "a.go", StartLine: 1, EndLine: 6},
		{Path: "a.go", StartLine: 7, EndLine: 9}, // adjacent to 1-6
		{Path: "a.go", StartLine: 3, EndLine: 4}, // inside 1-6
	}
	got := collapseLocations(locs)
	want := []cloneLocation{
		{Path: "a.go", StartLine: 1, EndLine: 9},
		{Path: "a.go", StartLine: 20, EndLine: 25},
		{Path: "b.go", StartLine: 5, EndLine: 10},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: expected %+v, got %+v", i, want[i], got[i])
		}
	}
}

func TestMergeCloneGroups_PeriodicRegionUsesSharedSpan(t *testing.T) {
	// Three adjacent copies of a 10-line block after the original. The
	// original-vs-first-copy clone is reported; the copies matching each
	// other collapse into one periodic region and are dropped.
	var groups []cloneGroup
	for off := 0; off < 5; off++ {
		groups = append(groups, cloneGroup{Lines: 6, NearClone: true, Locations: []cloneLocation{
			{Path: "a.go", StartLine: 10 + off, EndLine: 15 + off},
			{Path: "a.go", StartLine: 30 + off, EndLine: 35 + off},
		}}, cloneGroup{Lines: 6, NearClone: true, Locations: []cloneLocation{
			{Path: "a.go", StartLine: 30 + off, EndLine: 35 + off},
			{Path: "a.go", StartLine: 40 + off, EndLine: 45 + off},
		}}, cloneGroup{Lines: 6, NearClone: true, Locations: []cloneLocation{
			{Path: "a.go", StartLine: 40 + off, EndLine: 45 + off},
			{Path: "a.go", StartLine: 50 + off, EndLine: 55 + off},
		}})
	}
	merged := mergeCloneGroups(groups)
	if len(merged) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(merged), merged)
	}
	g := merged[0]
	if len(g.Locations) != 2 || g.Locations[0].EndLine != 19 || g.Locations[1] != (cloneLocation{Path: "a.go", StartLine: 30, EndLine: 39}) {
		t.Errorf("expected a.go:10-19 and a.go:30-39, got %+v", g.Locations)
	}
	if g.Lines != 10 {
		t.Errorf("expected shared span 10, got %d", g.Lines)
	}
}

func TestExtendClones_UnequalSpansUseSharedSpan(t *testing.T) {
	// One extended cluster where the second location collapsed with a
	// longer neighbouring copy: Lines is what every location shares.
	groups := []cloneGroup{
		{Lines: 6, NearClone: true, Locations: []cloneLocation{{Path: "a.go", StartLine: 10, EndLine: 19}, {Path: "b.go", StartLine: 10, EndLine: 39}}},
	}
	got := extendClones(groups)
	if len(got) != 1 || got[0].Lines != 10 {
		t.Errorf("expected Lines 10, got %+v", got)
	}
	if minSpan(nil) != 0 {
		t.Error("expected minSpan(nil) == 0")
	}
}

func TestCloneLocationSpan(t *testing.T) {
	if s := (cloneLocation{StartLine: 10, EndLine: 19}).span(); s != 10 {
		t.Errorf("expected span 10, got %d", s)
	}
	if s := (cloneLocation{StartLine: 10}).span(); s != 1 {
		t.Errorf("expected span 1 for unset end line, got %d", s)
	}
	if maxSpan(nil) != 0 {
		t.Error("expected maxSpan(nil) == 0")
	}
}

func TestCloneGroupToSignal_DescriptionListsRanges(t *testing.T) {
	g := cloneGroup{Lines: 10, Locations: []cloneLocation{
		{Path: "a.go", StartLine: 10, EndLine: 19},
		{Path: "b.go", StartLine: 40, EndLine: 49},
	}}
	sig := cloneGroupToSignal(g)
	if !strings.Contains(sig.Description, "a.go:10-19") || !strings.Contains(sig.Description, "b.go:40-49") {
		t.Errorf("expected line ranges in description, got %q", sig.Description)
	}
}

// TestCollect_OverlappingWindowsOneSignal pins stringer-nxx.4: a 20-line
// clone yields one signal covering the region, not one per window offset.
func TestCollect_OverlappingWindowsOneSignal(t *testing.T) {
	dir := t.TempDir()
	block := strings.Join(append(append([]string{"func processItems() {"}, distinctLines("v", 18)...), "}"), "\n")
	writeTestFile(t, dir, "pkg1/handler.go", "package pkg1\n\n"+block+"\n")
	writeTestFile(t, dir, "pkg2/handler.go", "package pkg2\n\n"+block+"\n")

	c := &DuplicationCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(signals) != 1 {
		for _, s := range signals {
			t.Logf("%s:%d %s", s.FilePath, s.Line, s.Title)
		}
		t.Fatalf("expected exactly 1 signal for one duplicated region, got %d", len(signals))
	}
	sig := signals[0]
	if sig.Kind != "code-clone" {
		t.Errorf("expected code-clone (near-clone only adds the package line), got %s", sig.Kind)
	}
	if sig.FilePath != "pkg1/handler.go" || sig.Line != 3 {
		t.Errorf("expected signal at pkg1/handler.go:3, got %s:%d", sig.FilePath, sig.Line)
	}
	if !strings.Contains(sig.Title, "20 lines, 2 locations") {
		t.Errorf("expected merged 20-line title, got %q", sig.Title)
	}
	if !strings.Contains(sig.Description, "pkg2/handler.go:3-22") {
		t.Errorf("expected other location range in description, got %q", sig.Description)
	}
}

// TestCollect_AdjacentLineArtifactsDropped pins stringer-nxx.4: a
// repetitive list whose consecutive windows match each other is not a
// clone of anything.
func TestCollect_AdjacentLineArtifactsDropped(t *testing.T) {
	t.Run("identical lines", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{"package p", "", "func fill() {"}
		for i := 0; i < 20; i++ {
			lines = append(lines, "    items = append(items, item)")
		}
		lines = append(lines, "}")
		writeTestFile(t, dir, "list.go", strings.Join(lines, "\n"))

		c := &DuplicationCollector{}
		signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
		if err != nil {
			t.Fatalf("Collect() error: %v", err)
		}
		if len(signals) != 0 {
			t.Errorf("expected no signals for a self-overlapping list, got %d: %+v", len(signals), signals)
		}
	})

	t.Run("renamed lines", func(t *testing.T) {
		dir := t.TempDir()
		lines := []string{"package p", "", "var ("}
		for i := 0; i < 20; i++ {
			lines = append(lines, fmt.Sprintf("    ref%c = export%c", 'a'+i, 'a'+i))
		}
		lines = append(lines, ")")
		writeTestFile(t, dir, "refs.go", strings.Join(lines, "\n"))

		c := &DuplicationCollector{}
		signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
		if err != nil {
			t.Fatalf("Collect() error: %v", err)
		}
		if len(signals) != 0 {
			t.Errorf("expected no near-clone signals for a repetitive list, got %d: %+v", len(signals), signals)
		}
	})
}

// TestCollect_TestOnlyMinLines pins stringer-nxx.4: short test-only clones
// are dropped by default, long ones survive (down-weighted), and the same
// short clone in production code is unaffected.
func TestCollect_TestOnlyMinLines(t *testing.T) {
	short := strings.Join(distinctLines("s", 6), "\n")
	long := strings.Join(distinctLines("l", 14), "\n")

	collect := func(t *testing.T, dir string, opts signal.CollectorOpts) ([]signal.RawSignal, *DuplicationMetrics) {
		t.Helper()
		c := &DuplicationCollector{}
		signals, err := c.Collect(context.Background(), dir, opts)
		if err != nil {
			t.Fatalf("Collect() error: %v", err)
		}
		return signals, c.Metrics().(*DuplicationMetrics)
	}

	t.Run("6-line test-only clone dropped", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, dir, "a_test.go", "package a\n\nfunc TestA(t *testing.T) {\n"+short+"\n}\n")
		writeTestFile(t, dir, "b_test.go", "package b\n\nfunc TestB(t *testing.T) {\n"+short+"\n}\n")
		signals, m := collect(t, dir, signal.CollectorOpts{})
		if len(signals) != 0 {
			t.Errorf("expected short test-only clone to be dropped, got %+v", signals)
		}
		if m.TestOnlySuppressed != 1 {
			t.Errorf("expected TestOnlySuppressed == 1, got %d", m.TestOnlySuppressed)
		}
	})

	t.Run("14-line test-only clone kept and down-weighted", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, dir, "a_test.go", "package a\n\nfunc TestA(t *testing.T) {\n"+long+"\n}\n")
		writeTestFile(t, dir, "b_test.go", "package b\n\nfunc TestB(t *testing.T) {\n"+long+"\n}\n")
		signals, m := collect(t, dir, signal.CollectorOpts{})
		if len(signals) != 1 {
			t.Fatalf("expected 1 signal for a long test-only clone, got %d", len(signals))
		}
		if m.TestOnlySuppressed != 0 {
			t.Errorf("expected nothing suppressed, got %d", m.TestOnlySuppressed)
		}
		found := false
		for _, tag := range signals[0].Tags {
			if tag == "test-only" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected test-only tag, got %v", signals[0].Tags)
		}
	})

	t.Run("6-line production clone unchanged", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, dir, "a.go", "package a\n\nfunc A() {\n"+short+"\n}\n")
		writeTestFile(t, dir, "b.go", "package b\n\nfunc B() {\n"+short+"\n}\n")
		signals, _ := collect(t, dir, signal.CollectorOpts{})
		if len(signals) != 1 {
			t.Fatalf("expected 1 signal for a production clone, got %d", len(signals))
		}
	})

	t.Run("min_test_lines override keeps short test clone", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, dir, "a_test.go", "package a\n\nfunc TestA(t *testing.T) {\n"+short+"\n}\n")
		writeTestFile(t, dir, "b_test.go", "package b\n\nfunc TestB(t *testing.T) {\n"+short+"\n}\n")
		signals, _ := collect(t, dir, signal.CollectorOpts{DuplicationMinTestLines: 6})
		if len(signals) != 1 {
			t.Fatalf("expected 1 signal with duplication_min_test_lines=6, got %d", len(signals))
		}
	})
}
