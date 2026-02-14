// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

// BeadDependency represents a dependency relationship between two signals.
type BeadDependency struct {
	FromID     string  // Source signal's bead ID.
	ToID       string  // Target signal's bead ID.
	Type       string  // Relationship type: "blocks", "parent", "relates-to".
	Confidence float64 // LLM confidence in this relationship (0.0-1.0).
}

// depResponseItem represents a single dependency in the LLM's JSON response.
type depResponseItem struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

// depResponseWrapper is the top-level JSON structure expected from the LLM.
type depResponseWrapper struct {
	Dependencies []depResponseItem `json:"dependencies"`
}

// InferDependencies uses an LLM to identify blocking, parent-child, and
// relates-to relationships between signals. On LLM error, returns empty deps.
func InferDependencies(ctx context.Context, signals []signal.RawSignal, provider llm.Provider, idPrefix string) ([]BeadDependency, error) {
	if len(signals) < 2 {
		return nil, nil
	}

	prompt := buildDependencyPrompt(signals)

	resp, err := provider.Complete(ctx, llm.Request{
		SystemPrompt: "You are a software engineering dependency analysis expert. Always respond with valid JSON only.",
		Prompt:       prompt,
		MaxTokens:    4096,
	})
	if err != nil {
		slog.Warn("LLM dependency inference failed, skipping", "error", err)
		return nil, nil
	}

	items, err := parseDependencyResponse(resp.Content)
	if err != nil {
		slog.Warn("failed to parse dependency response, skipping", "error", err)
		return nil, nil
	}

	// Validate signal IDs and build dependencies.
	validIDs := make(map[string]bool, len(signals))
	for i := range signals {
		validIDs[fmt.Sprintf("sig-%d", i)] = true
	}

	var deps []BeadDependency
	for _, item := range items {
		if !validIDs[item.From] || !validIDs[item.To] {
			slog.Debug("ignoring dependency with unknown signal ID", "from", item.From, "to", item.To)
			continue
		}
		if item.From == item.To {
			slog.Debug("ignoring self-dependency", "id", item.From)
			continue
		}
		if !isValidDepType(item.Type) {
			slog.Debug("ignoring invalid dependency type", "type", item.Type)
			continue
		}
		deps = append(deps, BeadDependency{
			FromID:     item.From,
			ToID:       item.To,
			Type:       item.Type,
			Confidence: item.Confidence,
		})
	}

	// Validate DAG for "blocks" edges only.
	deps = validateDAG(deps)

	// Map sig-N indices to deterministic bead IDs.
	deps = mapToBeadIDs(deps, signals, idPrefix)

	return deps, nil
}

// ApplyDepsToSignals populates Blocks and DependsOn fields on signals
// based on inferred dependencies.
func ApplyDepsToSignals(signals []signal.RawSignal, deps []BeadDependency, idPrefix string) {
	// Build a map from bead ID to signal index.
	beadIDToIdx := make(map[string]int, len(signals))
	for i, sig := range signals {
		beadIDToIdx[output.SignalID(sig, idPrefix)] = i
	}

	for _, dep := range deps {
		if dep.Type != "blocks" {
			continue
		}
		// FromID blocks ToID: from.Blocks includes to, to.DependsOn includes from.
		fromIdx, fromOK := beadIDToIdx[dep.FromID]
		toIdx, toOK := beadIDToIdx[dep.ToID]
		if !fromOK || !toOK {
			continue
		}
		signals[fromIdx].Blocks = appendUnique(signals[fromIdx].Blocks, dep.ToID)
		signals[toIdx].DependsOn = appendUnique(signals[toIdx].DependsOn, dep.FromID)
	}
}

// buildDependencyPrompt constructs the prompt for dependency inference.
func buildDependencyPrompt(signals []signal.RawSignal) string {
	var b strings.Builder

	b.WriteString("You are analyzing work items from a software repository to identify dependencies.\n\n")
	b.WriteString("Dependency types:\n")
	b.WriteString("- \"blocks\": Signal A must be completed before Signal B can start\n")
	b.WriteString("- \"parent\": Signal A is a parent/epic that contains Signal B as a subtask\n")
	b.WriteString("- \"relates-to\": Signals are related but neither blocks the other\n\n")
	b.WriteString("SIGNALS:\n")
	b.WriteString("--------\n")

	for i, sig := range signals {
		fmt.Fprintf(&b, "ID: sig-%d\n", i)
		fmt.Fprintf(&b, "  Title: %s\n", sig.Title)
		fmt.Fprintf(&b, "  Kind: %s\n", sig.Kind)
		fmt.Fprintf(&b, "  Source: %s\n", sig.Source)
		if sig.FilePath != "" {
			fmt.Fprintf(&b, "  Path: %s\n", sig.FilePath)
		}
		if len(sig.Tags) > 0 {
			fmt.Fprintf(&b, "  Tags: %s\n", strings.Join(sig.Tags, ", "))
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

	b.WriteString("--------\n\n")
	b.WriteString("Respond with ONLY a JSON object in the following format (no markdown, no explanation):\n")
	b.WriteString(`{"dependencies": [{"from": "sig-0", "to": "sig-1", "type": "blocks", "confidence": 0.8}]}`)
	b.WriteString("\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Only include dependencies you are confident about (confidence >= 0.6)\n")
	b.WriteString("- \"blocks\" means the 'from' signal must be done before 'to' can start\n")
	b.WriteString("- Avoid creating circular blocking chains\n")
	b.WriteString("- If no dependencies exist, return {\"dependencies\": []}\n")
	b.WriteString("- A signal cannot depend on itself\n")

	return b.String()
}

// parseDependencyResponse parses the LLM's JSON response into dependency items.
func parseDependencyResponse(content string) ([]depResponseItem, error) {
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

	// Try wrapper format first.
	var wrapper depResponseWrapper
	if err := json.Unmarshal([]byte(content), &wrapper); err == nil {
		return wrapper.Dependencies, nil
	}

	// Try raw array.
	var items []depResponseItem
	if err := json.Unmarshal([]byte(content), &items); err == nil {
		return items, nil
	}

	return nil, fmt.Errorf("failed to parse LLM response as dependency JSON: %.200s", content)
}

// adjacencyList represents a directed graph.
type adjacencyList map[string][]string

// buildDependencyGraph constructs a directed graph from "blocks" dependencies.
func buildDependencyGraph(deps []BeadDependency) adjacencyList {
	graph := make(adjacencyList)
	for _, dep := range deps {
		if dep.Type == "blocks" {
			graph[dep.FromID] = append(graph[dep.FromID], dep.ToID)
			// Ensure ToID exists in graph even if it has no outgoing edges.
			if _, ok := graph[dep.ToID]; !ok {
				graph[dep.ToID] = nil
			}
		}
	}
	return graph
}

// validateDAG checks that "blocks" edges form a DAG. If cycles are detected,
// the lowest-confidence edge in each cycle is removed.
func validateDAG(deps []BeadDependency) []BeadDependency {
	// Extract only "blocks" edges for cycle detection.
	var blockDeps []BeadDependency
	var otherDeps []BeadDependency
	for _, d := range deps {
		if d.Type == "blocks" {
			blockDeps = append(blockDeps, d)
		} else {
			otherDeps = append(otherDeps, d)
		}
	}

	if len(blockDeps) == 0 {
		return deps
	}

	// Iteratively remove lowest-confidence edge until DAG.
	for {
		graph := buildDependencyGraph(blockDeps)
		if !hasCycle(graph) {
			break
		}

		// Find and remove the lowest-confidence "blocks" edge.
		minIdx := 0
		for i, d := range blockDeps {
			if d.Confidence < blockDeps[minIdx].Confidence {
				minIdx = i
			}
		}
		slog.Warn("breaking dependency cycle by removing lowest-confidence edge",
			"from", blockDeps[minIdx].FromID, "to", blockDeps[minIdx].ToID,
			"confidence", blockDeps[minIdx].Confidence)
		blockDeps = append(blockDeps[:minIdx], blockDeps[minIdx+1:]...)

		if len(blockDeps) == 0 {
			break
		}
	}

	return append(blockDeps, otherDeps...)
}

// hasCycle detects cycles using Kahn's algorithm (topological sort).
// Returns true if the graph contains a cycle.
func hasCycle(graph adjacencyList) bool {
	// Compute in-degrees.
	inDegree := make(map[string]int)
	for node := range graph {
		if _, ok := inDegree[node]; !ok {
			inDegree[node] = 0
		}
		for _, neighbor := range graph[node] {
			inDegree[neighbor]++
		}
	}

	// Collect nodes with in-degree 0.
	var queue []string
	for node, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++

		for _, neighbor := range graph[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	return visited < len(inDegree)
}

// mapToBeadIDs converts sig-N index references in dependencies to
// deterministic bead IDs using SignalID.
func mapToBeadIDs(deps []BeadDependency, signals []signal.RawSignal, idPrefix string) []BeadDependency {
	// Build index → bead ID map.
	idMap := make(map[string]string, len(signals))
	for i, sig := range signals {
		idMap[fmt.Sprintf("sig-%d", i)] = output.SignalID(sig, idPrefix)
	}

	result := make([]BeadDependency, 0, len(deps))
	for _, dep := range deps {
		fromBead, fromOK := idMap[dep.FromID]
		toBead, toOK := idMap[dep.ToID]
		if !fromOK || !toOK {
			continue
		}
		result = append(result, BeadDependency{
			FromID:     fromBead,
			ToID:       toBead,
			Type:       dep.Type,
			Confidence: dep.Confidence,
		})
	}
	return result
}

// isValidDepType returns true if the given type is a recognized dependency type.
func isValidDepType(t string) bool {
	switch t {
	case "blocks", "parent", "relates-to":
		return true
	default:
		return false
	}
}

// appendUnique appends s to the slice only if it's not already present.
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}
