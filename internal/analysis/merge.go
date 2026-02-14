// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

// AnalysisBead represents a bead produced by the clustering analysis. It
// extends the basic signal with cluster metadata and parent-child relationships.
type AnalysisBead struct {
	// ID is a generated identifier for this bead.
	ID string

	// Title is the bead title.
	Title string

	// Description is the full bead description.
	Description string

	// Type is the bead type (e.g., "task", "epic").
	Type string

	// Confidence is the confidence level (0.0-1.0).
	Confidence float64

	// Tags are labels for the bead.
	Tags []string

	// ParentID links child beads to their parent epic. Empty for top-level beads.
	ParentID string

	// SourceSignals references the original signals that contributed to this bead.
	SourceSignals []signal.RawSignal
}

// MergeClusterToBeads converts a Cluster and its member signals into
// AnalysisBeads. Single-signal clusters produce one bead directly.
// Multi-signal clusters produce one bead with a rich description listing
// all member signals.
func MergeClusterToBeads(cluster Cluster, signals []signal.RawSignal) []AnalysisBead {
	memberSignals := resolveClusterSignals(cluster, signals)

	if len(memberSignals) == 0 {
		return nil
	}

	// Single-signal cluster: create one simple bead.
	if len(memberSignals) == 1 {
		sig := memberSignals[0]
		return []AnalysisBead{
			{
				ID:            cluster.ID,
				Title:         sig.Title,
				Description:   sig.Description,
				Type:          "task",
				Confidence:    sig.Confidence,
				Tags:          combineTags(cluster.Tags, sig.Tags),
				SourceSignals: memberSignals,
			},
		}
	}

	// Multi-signal cluster: create one bead with a merged description.
	description := buildMergedDescription(cluster, memberSignals)
	maxConf := cluster.Confidence
	for _, sig := range memberSignals {
		if sig.Confidence > maxConf {
			maxConf = sig.Confidence
		}
	}

	return []AnalysisBead{
		{
			ID:            cluster.ID,
			Title:         cluster.Name,
			Description:   description,
			Type:          "task",
			Confidence:    maxConf,
			Tags:          cluster.Tags,
			SourceSignals: memberSignals,
		},
	}
}

// buildMergedDescription creates a rich description that lists all member
// signals with their titles, locations, and the cluster's overall description.
func buildMergedDescription(cluster Cluster, members []signal.RawSignal) string {
	var b strings.Builder

	if cluster.Description != "" {
		b.WriteString(cluster.Description)
		b.WriteString("\n\n")
	}

	b.WriteString("Related signals:\n")
	for i, sig := range members {
		fmt.Fprintf(&b, "- %s", sig.Title)
		if sig.FilePath != "" {
			if sig.Line > 0 {
				fmt.Fprintf(&b, " (%s:%d)", sig.FilePath, sig.Line)
			} else {
				fmt.Fprintf(&b, " (%s)", sig.FilePath)
			}
		}
		if i < len(members)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// resolveClusterSignals maps cluster signal IDs back to actual RawSignals.
func resolveClusterSignals(cluster Cluster, signals []signal.RawSignal) []signal.RawSignal {
	var result []signal.RawSignal
	for _, id := range cluster.SignalIDs {
		sig := findSignalInSlice(id, signals)
		if sig != nil {
			result = append(result, *sig)
		}
	}
	return result
}

// combineTags returns the deduplicated union of two tag slices.
func combineTags(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var result []string
	for _, tag := range a {
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	for _, tag := range b {
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	return result
}

// BeadsToSignals converts AnalysisBeads back to RawSignals for output
// formatting. This bridges the analysis results back into the existing
// pipeline output flow.
func BeadsToSignals(beads []AnalysisBead) []signal.RawSignal {
	signals := make([]signal.RawSignal, len(beads))
	for i, b := range beads {
		signals[i] = signal.RawSignal{
			Source:      "cluster",
			Kind:        b.Type,
			Title:       b.Title,
			Description: b.Description,
			Confidence:  b.Confidence,
			Tags:        b.Tags,
		}
		// Inherit file path from first source signal if available.
		if len(b.SourceSignals) > 0 {
			signals[i].FilePath = b.SourceSignals[0].FilePath
			signals[i].Line = b.SourceSignals[0].Line
			signals[i].Author = b.SourceSignals[0].Author
			signals[i].Timestamp = b.SourceSignals[0].Timestamp
		}
	}
	return signals
}
