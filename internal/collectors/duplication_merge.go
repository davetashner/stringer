// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"sort"
	"strings"
)

// exactSlackLines is how many lines a near-clone region may extend beyond
// the exact-clone region it contains and still be reported as an exact
// clone. Type 2 normalization always matches at least the lines Type 1
// matches, so every exact clone sits inside a near-clone cluster; the
// slack absorbs the renamed signature line that typically borders an
// otherwise identical block (DR-026).
const exactSlackLines = 2

// mergeCloneGroups collapses single-window clone groups into one group per
// duplicated region (DR-026):
//
//  1. Extension: groups that are the same set of files shifted by a few
//     lines (the sliding window walking down one clone) are merged, so a
//     30-line clone becomes one 30-line group. A merged group is reported
//     as exact when its exact members cover the same locations to within
//     exactSlackLines; otherwise it is a near-clone covering the union.
//     Lines is the shortest location span, the block every copy shares.
//  2. Collapse: within a group, locations in the same file whose ranges
//     overlap or touch become one location. A group left with fewer than
//     two locations (a repetitive list matching itself one line over) is
//     dropped.
//  3. Region merge: groups whose anchor (first location) ranges overlap or
//     touch in the same file are merged into one signal for that region,
//     with the union of their other locations. Only anchors chain, so
//     boilerplate shared across many files does not snowball into one
//     repo-wide cluster. Lines stays the longest member block.
//
// Output is sorted by anchor for deterministic results.
func mergeCloneGroups(groups []cloneGroup) []cloneGroup {
	extended := extendClones(groups)
	merged := mergeByAnchor(extended)
	sort.Slice(merged, func(i, j int) bool {
		a, b := merged[i].Locations[0], merged[j].Locations[0]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.StartLine < b.StartLine
	})
	return merged
}

// unionFind is a minimal disjoint-set over group indices.
type unionFind []int

func newUnionFind(n int) unionFind {
	uf := make(unionFind, n)
	for i := range uf {
		uf[i] = i
	}
	return uf
}

func (uf unionFind) find(x int) int {
	for uf[x] != x {
		uf[x] = uf[uf[x]]
		x = uf[x]
	}
	return x
}

func (uf unionFind) union(a, b int) {
	ra, rb := uf.find(a), uf.find(b)
	if ra != rb {
		uf[ra] = rb
	}
}

// clusters returns member indices grouped by root, in first-seen order.
func (uf unionFind) clusters() [][]int {
	byRoot := make(map[int]int)
	var out [][]int
	for i := range uf {
		root := uf.find(i)
		idx, ok := byRoot[root]
		if !ok {
			idx = len(out)
			byRoot[root] = idx
			out = append(out, nil)
		}
		out[idx] = append(out[idx], i)
	}
	return out
}

// sortLocations orders locations by (path, start, end) in place.
func sortLocations(locs []cloneLocation) {
	sort.Slice(locs, func(i, j int) bool {
		if locs[i].Path != locs[j].Path {
			return locs[i].Path < locs[j].Path
		}
		if locs[i].StartLine != locs[j].StartLine {
			return locs[i].StartLine < locs[j].StartLine
		}
		return locs[i].EndLine < locs[j].EndLine
	})
}

// touches reports whether two ranges overlap or are adjacent.
func touches(aStart, aEnd, bStart, bEnd int) bool {
	return bStart <= aEnd+1 && aStart <= bEnd+1
}

// shiftCompatible reports whether two groups with the same sorted path
// list are the same clone at a small offset: every location of a touches
// the corresponding location of b.
func shiftCompatible(a, b []cloneLocation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || !touches(a[i].StartLine, a[i].EndLine, b[i].StartLine, b[i].EndLine) {
			return false
		}
	}
	return true
}

// extendClones merges shift-compatible groups (step 1) and collapses each
// result (step 2), dropping groups left with fewer than two locations.
func extendClones(groups []cloneGroup) []cloneGroup {
	if len(groups) == 0 {
		return nil
	}
	sorted := make([]cloneGroup, len(groups))
	buckets := make(map[string][]int)
	for i, g := range groups {
		locs := make([]cloneLocation, len(g.Locations))
		copy(locs, g.Locations)
		sortLocations(locs)
		sorted[i] = cloneGroup{Lines: g.Lines, Locations: locs, NearClone: g.NearClone}
		paths := make([]string, len(locs))
		for j, loc := range locs {
			paths[j] = loc.Path
		}
		key := strings.Join(paths, "\x00")
		buckets[key] = append(buckets[key], i)
	}

	uf := newUnionFind(len(groups))
	for _, members := range buckets {
		sort.Slice(members, func(i, j int) bool {
			return sorted[members[i]].Locations[0].StartLine < sorted[members[j]].Locations[0].StartLine
		})
		var active []int
		for _, m := range members {
			anchor := sorted[m].Locations[0]
			kept := active[:0]
			for _, a := range active {
				if sorted[a].Locations[0].EndLine+1 >= anchor.StartLine {
					kept = append(kept, a)
					if shiftCompatible(sorted[a].Locations, sorted[m].Locations) {
						uf.union(a, m)
					}
				}
			}
			active = append(kept, m)
		}
	}

	var result []cloneGroup
	for _, members := range uf.clusters() {
		var allLocs, exactLocs []cloneLocation
		for _, m := range members {
			allLocs = append(allLocs, sorted[m].Locations...)
			if !sorted[m].NearClone {
				exactLocs = append(exactLocs, sorted[m].Locations...)
			}
		}
		all := collapseLocations(allLocs)
		if len(all) < 2 {
			continue
		}
		g := cloneGroup{Locations: all, NearClone: true}
		if exact := collapseLocations(exactLocs); len(exact) == len(all) &&
			maxSpan(all) <= maxSpan(exact)+exactSlackLines {
			g = cloneGroup{Locations: exact}
		}
		// A periodic region (several adjacent copies of one block) collapses
		// into one long location next to shorter ones; the shortest span is
		// what every location is guaranteed to share.
		g.Lines = minSpan(g.Locations)
		result = append(result, g)
	}
	return result
}

// mergeByAnchor merges groups whose anchor ranges overlap or touch in the
// same file (step 3). The merged group lists the union of all locations,
// keeps the longest member block as Lines, and is exact only if every
// member is exact.
func mergeByAnchor(groups []cloneGroup) []cloneGroup {
	if len(groups) == 0 {
		return nil
	}
	order := make([]int, len(groups))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := groups[order[i]].Locations[0], groups[order[j]].Locations[0]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.StartLine < b.StartLine
	})

	uf := newUnionFind(len(groups))
	cur := order[0]
	curEnd := groups[cur].Locations[0].EndLine
	for _, idx := range order[1:] {
		anchor := groups[idx].Locations[0]
		if anchor.Path == groups[cur].Locations[0].Path && anchor.StartLine <= curEnd+1 {
			uf.union(cur, idx)
			if anchor.EndLine > curEnd {
				curEnd = anchor.EndLine
			}
			continue
		}
		cur, curEnd = idx, anchor.EndLine
	}

	result := make([]cloneGroup, 0, len(groups))
	for _, members := range uf.clusters() {
		var locs []cloneLocation
		merged := cloneGroup{}
		for _, m := range members {
			locs = append(locs, groups[m].Locations...)
			if groups[m].Lines > merged.Lines {
				merged.Lines = groups[m].Lines
			}
			merged.NearClone = merged.NearClone || groups[m].NearClone
		}
		merged.Locations = collapseLocations(locs)
		if len(merged.Locations) < 2 {
			continue
		}
		result = append(result, merged)
	}
	return result
}

// collapseLocations sorts locations by (path, start) and merges those in
// the same file whose ranges overlap or are adjacent into one location
// covering the union range.
func collapseLocations(locs []cloneLocation) []cloneLocation {
	if len(locs) == 0 {
		return nil
	}
	sorted := make([]cloneLocation, len(locs))
	copy(sorted, locs)
	sortLocations(sorted)

	out := []cloneLocation{sorted[0]}
	for _, loc := range sorted[1:] {
		last := &out[len(out)-1]
		if loc.Path == last.Path && loc.StartLine <= last.EndLine+1 {
			if loc.EndLine > last.EndLine {
				last.EndLine = loc.EndLine
			}
			continue
		}
		out = append(out, loc)
	}
	return out
}

// minSpan returns the smallest line span among locations.
func minSpan(locs []cloneLocation) int {
	best := 0
	for i, loc := range locs {
		if s := loc.span(); i == 0 || s < best {
			best = s
		}
	}
	return best
}

// maxSpan returns the largest line span among locations.
func maxSpan(locs []cloneLocation) int {
	best := 0
	for _, loc := range locs {
		if s := loc.span(); s > best {
			best = s
		}
	}
	return best
}
