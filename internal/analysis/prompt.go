// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

// clusterResponseItem represents a single cluster in the LLM's JSON response.
type clusterResponseItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	SignalIDs   []string `json:"signal_ids"`
}

// clusterResponseWrapper is the top-level JSON structure expected from the LLM.
type clusterResponseWrapper struct {
	Clusters []clusterResponseItem `json:"clusters"`
}

// buildClusteringPrompt constructs the prompt sent to the LLM for clustering.
// It lists the pre-filtered signal groups with their IDs, titles, kinds, paths,
// and descriptions, then instructs the LLM to group them by theme.
func buildClusteringPrompt(groups []SignalGroup) string {
	var b strings.Builder

	b.WriteString("You are analyzing signals extracted from a software repository. ")
	b.WriteString("Each signal represents an actionable work item (TODO, bug, code smell, etc.).\n\n")
	b.WriteString("Below is a list of signals. Group related signals into clusters based on:\n")
	b.WriteString("- Common theme or topic (e.g., all related to authentication)\n")
	b.WriteString("- Same module or directory (e.g., all in the database layer)\n")
	b.WriteString("- Similar intent (e.g., all are performance improvements)\n\n")
	b.WriteString("SIGNALS:\n")
	b.WriteString("--------\n")

	for _, group := range groups {
		for i, idx := range group.MemberIndices {
			sig := group.Members[i]
			fmt.Fprintf(&b, "ID: sig-%d\n", idx)
			fmt.Fprintf(&b, "  Title: %s\n", sig.Title)
			fmt.Fprintf(&b, "  Kind: %s\n", sig.Kind)
			fmt.Fprintf(&b, "  Source: %s\n", sig.Source)
			if sig.FilePath != "" {
				fmt.Fprintf(&b, "  Path: %s\n", sig.FilePath)
			}
			if sig.Description != "" {
				desc := sig.Description
				if len(desc) > 200 {
					desc = desc[:200] + "..."
				}
				fmt.Fprintf(&b, "  Description: %s\n", desc)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("--------\n\n")
	b.WriteString("Respond with ONLY a JSON object in the following format (no markdown, no explanation):\n")
	b.WriteString(`{"clusters": [{"name": "short cluster name", "description": "why these signals are related", "signal_ids": ["sig-0", "sig-1"]}]}`)
	b.WriteString("\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Every signal ID must appear in exactly one cluster\n")
	b.WriteString("- Cluster names should be short (3-6 words)\n")
	b.WriteString("- If a signal doesn't fit any group, put it in its own single-signal cluster\n")
	b.WriteString("- Prefer fewer, meaningful clusters over many small ones\n")

	return b.String()
}

// parseClusterResponse parses the LLM's JSON response into cluster items.
// It attempts to extract JSON from the response content, handling cases where
// the LLM wraps the JSON in markdown code fences.
func parseClusterResponse(content string) ([]clusterResponseItem, error) {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present.
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		content = strings.Join(jsonLines, "\n")
	}

	content = strings.TrimSpace(content)

	// Try parsing as the wrapper format first.
	var wrapper clusterResponseWrapper
	if err := json.Unmarshal([]byte(content), &wrapper); err == nil && len(wrapper.Clusters) > 0 {
		return wrapper.Clusters, nil
	}

	// Try parsing as a raw array of cluster items.
	var items []clusterResponseItem
	if err := json.Unmarshal([]byte(content), &items); err == nil && len(items) > 0 {
		return items, nil
	}

	return nil, fmt.Errorf("failed to parse LLM response as cluster JSON: %.200s", content)
}

// buildEpicPrompt constructs the prompt for generating an epic title and
// description from a large cluster of signals.
func buildEpicPrompt(cluster Cluster, signals []signal.RawSignal) string {
	var b strings.Builder

	b.WriteString("You are summarizing a group of related work items from a software repository.\n\n")
	fmt.Fprintf(&b, "Cluster name: %s\n", cluster.Name)
	fmt.Fprintf(&b, "Cluster description: %s\n\n", cluster.Description)
	b.WriteString("The cluster contains these signals:\n")

	for _, id := range cluster.SignalIDs {
		sig := findSignalInSlice(id, signals)
		if sig == nil {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s", id, sig.Title)
		if sig.FilePath != "" {
			fmt.Fprintf(&b, " (%s)", sig.FilePath)
		}
		b.WriteString("\n")
	}

	b.WriteString("\nRespond with ONLY a JSON object:\n")
	b.WriteString(`{"title": "epic title (under 80 chars)", "description": "2-3 sentence summary of the work needed"}`)
	b.WriteString("\n")

	return b.String()
}

// epicResponse is the JSON structure for the epic generation LLM response.
type epicResponse struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// parseEpicResponse parses the LLM's response for epic generation.
func parseEpicResponse(content string) (*epicResponse, error) {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present.
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		content = strings.Join(jsonLines, "\n")
	}

	content = strings.TrimSpace(content)

	var resp epicResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse epic response: %w", err)
	}
	if resp.Title == "" {
		return nil, fmt.Errorf("epic response missing title")
	}

	return &resp, nil
}
