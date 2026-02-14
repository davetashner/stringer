// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package analysis provides LLM-powered signal clustering and bead generation.
// It groups related signals by theme, module, or intent, using an LLM provider
// for semantic understanding, and produces structured beads for backlog import.
package analysis

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/signal"
)

// DefaultSimilarityThreshold is the default Jaccard similarity threshold
// used for pre-filtering signals into groups.
const DefaultSimilarityThreshold = 0.7

// ClusterConfig controls clustering behavior.
type ClusterConfig struct {
	// SimilarityThreshold is the Jaccard similarity threshold for pre-filtering.
	// Signals with similarity above this threshold are grouped together before
	// being sent to the LLM. Default: 0.7.
	SimilarityThreshold float64

	// MinClusterSize is the minimum number of signals required to form a cluster.
	// Clusters below this size are added to the unclustered list. Default: 1.
	MinClusterSize int

	// MaxClusterSize caps the number of signals in a single cluster.
	// Larger clusters are split. Default: 20.
	MaxClusterSize int
}

// DefaultClusterConfig returns a ClusterConfig with sensible defaults.
func DefaultClusterConfig() ClusterConfig {
	return ClusterConfig{
		SimilarityThreshold: DefaultSimilarityThreshold,
		MinClusterSize:      1,
		MaxClusterSize:      20,
	}
}

// Cluster represents a group of related signals identified by the LLM.
type Cluster struct {
	// ID is a unique identifier for this cluster.
	ID string

	// Name is a short human-readable name for the cluster theme.
	Name string

	// Description summarizes what the clustered signals have in common.
	Description string

	// SignalIDs are the indices (as string keys) of signals in the input slice.
	SignalIDs []string

	// Confidence is the highest confidence among all member signals.
	Confidence float64

	// Tags are combined unique tags from all member signals.
	Tags []string
}

// ClusterResult holds the output of a clustering operation.
type ClusterResult struct {
	// Clusters are the signal groups identified by the LLM.
	Clusters []Cluster

	// Unclustered contains signal IDs that did not fit any cluster.
	Unclustered []string
}

// ClusterSignals is the main entry point for signal clustering. It pre-filters
// signals by similarity, sends them to the LLM for semantic clustering, and
// returns the structured result. On LLM failure, it falls back to treating
// each signal as its own cluster.
func ClusterSignals(ctx context.Context, signals []signal.RawSignal, provider llm.Provider, cfg ClusterConfig) (*ClusterResult, error) {
	if len(signals) == 0 {
		return &ClusterResult{}, nil
	}

	// Apply defaults for zero-valued config fields.
	if cfg.SimilarityThreshold <= 0 {
		cfg.SimilarityThreshold = DefaultSimilarityThreshold
	}
	if cfg.MinClusterSize <= 0 {
		cfg.MinClusterSize = 1
	}
	if cfg.MaxClusterSize <= 0 {
		cfg.MaxClusterSize = 20
	}

	// Step 1: Pre-filter signals into groups by similarity.
	groups := PreFilterSignals(signals, cfg.SimilarityThreshold)
	slog.Debug("pre-filter complete", "signals", len(signals), "groups", len(groups))

	// Step 2: Use LLM to form clusters from the pre-filtered groups.
	clusters, err := formClustersWithLLM(ctx, groups, provider, signals)
	if err != nil {
		slog.Warn("LLM clustering failed, falling back to ungrouped", "error", err)
		return fallbackResult(signals), nil
	}

	// Step 3: Apply size constraints and identify unclustered signals.
	result := applyConstraints(clusters, signals, cfg)

	return result, nil
}

// fallbackResult creates a ClusterResult where each signal is its own cluster.
func fallbackResult(signals []signal.RawSignal) *ClusterResult {
	clusters := make([]Cluster, len(signals))
	for i, sig := range signals {
		id := fmt.Sprintf("sig-%d", i)
		clusters[i] = Cluster{
			ID:          fmt.Sprintf("cluster-%d", i),
			Name:        sig.Title,
			Description: sig.Description,
			SignalIDs:   []string{id},
			Confidence:  sig.Confidence,
			Tags:        sig.Tags,
		}
	}
	return &ClusterResult{Clusters: clusters}
}

// applyConstraints enforces MinClusterSize and MaxClusterSize on the raw
// clusters, moving undersized clusters to the unclustered list.
func applyConstraints(clusters []Cluster, signals []signal.RawSignal, cfg ClusterConfig) *ClusterResult {
	// Track which signal IDs are claimed by valid clusters.
	claimed := make(map[string]bool)
	var validClusters []Cluster

	for _, c := range clusters {
		if len(c.SignalIDs) < cfg.MinClusterSize {
			// Too small — mark as unclustered.
			continue
		}

		// Enforce max cluster size by truncating.
		if len(c.SignalIDs) > cfg.MaxClusterSize {
			c.SignalIDs = c.SignalIDs[:cfg.MaxClusterSize]
		}

		for _, id := range c.SignalIDs {
			claimed[id] = true
		}
		validClusters = append(validClusters, c)
	}

	// Identify unclustered signals.
	allIDs := make(map[string]bool)
	for i := range signals {
		allIDs[fmt.Sprintf("sig-%d", i)] = true
	}

	var unclustered []string
	for id := range allIDs {
		if !claimed[id] {
			unclustered = append(unclustered, id)
		}
	}

	return &ClusterResult{
		Clusters:    validClusters,
		Unclustered: unclustered,
	}
}
