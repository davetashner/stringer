// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/davetashner/stringer/internal/signal"
)

// SignalGroup represents a set of signals that are similar enough to be
// considered together for LLM clustering. The representative signal is the
// first signal added to the group.
type SignalGroup struct {
	// Representative is the first signal in the group, used as the exemplar.
	Representative signal.RawSignal

	// Members contains all signals in the group, including the representative.
	Members []signal.RawSignal

	// MemberIndices tracks the original indices of member signals in the
	// input slice, used for mapping back to signal IDs.
	MemberIndices []int
}

// PreFilterSignals groups signals by similarity to reduce the number of items
// sent to the LLM. Signals are grouped when they share a common collector
// source AND either their file paths share a directory prefix OR their titles
// have a Jaccard similarity above the given threshold.
func PreFilterSignals(signals []signal.RawSignal, threshold float64) []SignalGroup {
	if len(signals) == 0 {
		return nil
	}

	// assigned tracks which signal indices have been placed into a group.
	assigned := make([]bool, len(signals))
	var groups []SignalGroup

	for i := range signals {
		if assigned[i] {
			continue
		}

		group := SignalGroup{
			Representative: signals[i],
			Members:        []signal.RawSignal{signals[i]},
			MemberIndices:  []int{i},
		}
		assigned[i] = true

		for j := i + 1; j < len(signals); j++ {
			if assigned[j] {
				continue
			}

			if areSimilar(signals[i], signals[j], threshold) {
				group.Members = append(group.Members, signals[j])
				group.MemberIndices = append(group.MemberIndices, j)
				assigned[j] = true
			}
		}

		groups = append(groups, group)
	}

	return groups
}

// areSimilar returns true if two signals should be grouped together.
// Signals must share the same collector source, and either have similar
// file paths (same directory) or similar titles (Jaccard above threshold).
func areSimilar(a, b signal.RawSignal, threshold float64) bool {
	// Must come from the same collector.
	if a.Source != b.Source {
		return false
	}

	// Check directory proximity.
	if pathSimilar(a.FilePath, b.FilePath) {
		return true
	}

	// Check title similarity.
	return jaccardSimilarity(a.Title, b.Title) >= threshold
}

// pathSimilar returns true if two file paths share the same parent directory.
func pathSimilar(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	dirA := filepath.Dir(a)
	dirB := filepath.Dir(b)
	return dirA == dirB && dirA != "."
}

// jaccardSimilarity computes the Jaccard index between two strings based on
// their word tokens. Returns 0.0 for empty inputs and 1.0 for identical inputs.
func jaccardSimilarity(a, b string) float64 {
	wordsA := normalizeTitle(a)
	wordsB := normalizeTitle(b)

	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 0.0
	}

	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		setA[w] = true
	}

	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		setB[w] = true
	}

	// Compute intersection and union sizes.
	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	// Union = |A| + |B| - |intersection|
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// normalizeTitle tokenizes a string into lowercase words, removing
// punctuation and common stop words.
func normalizeTitle(s string) []string {
	s = strings.ToLower(s)

	// Split on non-letter, non-digit characters.
	words := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	// Filter out very short words and common stop words.
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "it": true,
		"in": true, "of": true, "to": true, "and": true, "or": true,
		"for": true, "on": true, "at": true, "by": true, "with": true,
		"this": true, "that": true, "from": true, "as": true, "be": true,
	}

	var result []string
	for _, w := range words {
		if len(w) < 2 {
			continue
		}
		if stopWords[w] {
			continue
		}
		result = append(result, w)
	}

	return result
}
