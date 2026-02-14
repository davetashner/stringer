// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/signal"
)

// formClustersWithLLM sends pre-filtered signal groups to the LLM for semantic
// clustering. It builds a prompt, sends it to the provider, parses the response,
// and validates that all returned signal IDs exist in the input. On any LLM or
// parsing error, it returns an error so the caller can fall back.
func formClustersWithLLM(ctx context.Context, groups []SignalGroup, provider llm.Provider, signals []signal.RawSignal) ([]Cluster, error) {
	prompt := buildClusteringPrompt(groups)

	resp, err := provider.Complete(ctx, llm.Request{
		SystemPrompt: "You are a software engineering assistant that analyzes code signals and groups related work items. Always respond with valid JSON only.",
		Prompt:       prompt,
		MaxTokens:    4096,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	items, err := parseClusterResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse cluster response: %w", err)
	}

	// Build a set of valid signal IDs for validation.
	validIDs := make(map[string]bool, len(signals))
	for i := range signals {
		validIDs[fmt.Sprintf("sig-%d", i)] = true
	}

	// Convert parsed items to Cluster structs, validating signal IDs.
	clusters := make([]Cluster, 0, len(items))
	for i, item := range items {
		var validSignalIDs []string
		for _, id := range item.SignalIDs {
			if validIDs[id] {
				validSignalIDs = append(validSignalIDs, id)
			} else {
				slog.Debug("ignoring unknown signal ID from LLM", "id", id, "cluster", item.Name)
			}
		}

		if len(validSignalIDs) == 0 {
			continue
		}

		cluster := Cluster{
			ID:          fmt.Sprintf("cluster-%d", i),
			Name:        item.Name,
			Description: item.Description,
			SignalIDs:   validSignalIDs,
			Confidence:  computeClusterConfidence(validSignalIDs, signals),
			Tags:        computeClusterTags(validSignalIDs, signals),
		}
		clusters = append(clusters, cluster)
	}

	return clusters, nil
}

// computeClusterConfidence returns the highest confidence among the signals
// referenced by the given IDs.
func computeClusterConfidence(signalIDs []string, signals []signal.RawSignal) float64 {
	var maxConf float64
	for _, id := range signalIDs {
		sig := findSignalInSlice(id, signals)
		if sig != nil && sig.Confidence > maxConf {
			maxConf = sig.Confidence
		}
	}
	return maxConf
}

// computeClusterTags returns the deduplicated union of tags from all signals
// referenced by the given IDs.
func computeClusterTags(signalIDs []string, signals []signal.RawSignal) []string {
	seen := make(map[string]bool)
	var tags []string
	for _, id := range signalIDs {
		sig := findSignalInSlice(id, signals)
		if sig == nil {
			continue
		}
		for _, tag := range sig.Tags {
			if !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

// findSignalInSlice looks up a signal by its "sig-N" ID string in the signals
// slice. Returns nil if the ID is invalid or out of range.
func findSignalInSlice(id string, signals []signal.RawSignal) *signal.RawSignal {
	if !strings.HasPrefix(id, "sig-") {
		return nil
	}
	idxStr := strings.TrimPrefix(id, "sig-")
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx >= len(signals) {
		return nil
	}
	return &signals[idx]
}
