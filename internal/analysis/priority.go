// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/signal"
)

// PriorityOverride maps a file-path glob to a fixed priority value.
type PriorityOverride struct {
	Pattern  string
	Priority int
}

// priorityResponseItem represents a single priority assignment from the LLM.
type priorityResponseItem struct {
	ID        string `json:"id"`
	Priority  int    `json:"priority"`
	Reasoning string `json:"reasoning"`
}

// priorityResponseWrapper is the top-level JSON structure expected from the LLM.
type priorityResponseWrapper struct {
	Priorities []priorityResponseItem `json:"priorities"`
}

// InferPriorities uses an LLM to assign P1-P4 priorities to signals based on
// their content and context. Overrides are applied after LLM inference. On LLM
// error, signals are returned unchanged (fallback to confidence-based mapping).
func InferPriorities(ctx context.Context, signals []signal.RawSignal, provider llm.Provider, overrides []PriorityOverride) ([]signal.RawSignal, error) {
	if len(signals) == 0 {
		return signals, nil
	}

	prompt := buildPriorityPrompt(signals)

	resp, err := provider.Complete(ctx, llm.Request{
		SystemPrompt: "You are a software engineering prioritization expert. Always respond with valid JSON only.",
		Prompt:       prompt,
		MaxTokens:    4096,
	})
	if err != nil {
		slog.Warn("LLM priority inference failed, using confidence-based mapping", "error", err)
		signals = applyOverrides(signals, overrides)
		return signals, nil
	}

	items, err := parsePriorityResponse(resp.Content)
	if err != nil {
		slog.Warn("failed to parse priority response, using confidence-based mapping", "error", err)
		signals = applyOverrides(signals, overrides)
		return signals, nil
	}

	// Apply LLM priorities to signals.
	for _, item := range items {
		sig := findSignalInSlice(item.ID, signals)
		if sig == nil {
			slog.Debug("ignoring unknown signal ID from priority response", "id", item.ID)
			continue
		}
		if item.Priority < 1 || item.Priority > 4 {
			slog.Debug("ignoring out-of-range priority", "id", item.ID, "priority", item.Priority)
			continue
		}
		p := item.Priority
		sig.Priority = &p
	}

	// Apply overrides after LLM (overrides win).
	signals = applyOverrides(signals, overrides)

	validateDistribution(signals)

	return signals, nil
}

// buildPriorityPrompt constructs the prompt sent to the LLM for priority inference.
func buildPriorityPrompt(signals []signal.RawSignal) string {
	var b strings.Builder

	b.WriteString("You are prioritizing actionable work items from a software repository.\n\n")
	b.WriteString("Priority levels:\n")
	b.WriteString("- P1 (Critical): Security vulnerabilities, data integrity issues, production outages\n")
	b.WriteString("- P2 (High): User-facing bugs, performance problems, broken functionality\n")
	b.WriteString("- P3 (Medium): Code quality, tech debt, refactoring, missing tests\n")
	b.WriteString("- P4 (Low): Cosmetic issues, minor improvements, low-impact cleanup\n\n")
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
		fmt.Fprintf(&b, "  Confidence: %.2f\n", sig.Confidence)
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
	b.WriteString(`{"priorities": [{"id": "sig-0", "priority": 2, "reasoning": "brief reason"}]}`)
	b.WriteString("\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Assign a priority (1-4) to every signal\n")
	b.WriteString("- Use the full range of priorities — avoid assigning everything the same level\n")
	b.WriteString("- Security and data-integrity issues should be P1\n")
	b.WriteString("- Keep reasoning to one sentence\n")

	return b.String()
}

// parsePriorityResponse parses the LLM's JSON response into priority items.
func parsePriorityResponse(content string) ([]priorityResponseItem, error) {
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
	var wrapper priorityResponseWrapper
	if err := json.Unmarshal([]byte(content), &wrapper); err == nil && len(wrapper.Priorities) > 0 {
		return wrapper.Priorities, nil
	}

	// Try raw array.
	var items []priorityResponseItem
	if err := json.Unmarshal([]byte(content), &items); err == nil && len(items) > 0 {
		return items, nil
	}

	return nil, fmt.Errorf("failed to parse LLM response as priority JSON: %.200s", content)
}

// applyOverrides sets priority on signals whose FilePath matches any override pattern.
// Override priorities take precedence over LLM-assigned priorities.
func applyOverrides(signals []signal.RawSignal, overrides []PriorityOverride) []signal.RawSignal {
	if len(overrides) == 0 {
		return signals
	}

	for i := range signals {
		for _, o := range overrides {
			matched, err := filepath.Match(o.Pattern, signals[i].FilePath)
			if err != nil {
				slog.Debug("invalid override pattern", "pattern", o.Pattern, "error", err)
				continue
			}
			if !matched {
				// Try matching against just the directory prefix for patterns like "auth/**".
				// filepath.Match doesn't support **, so check if the path starts with the
				// pattern prefix (before **).
				if strings.Contains(o.Pattern, "**") {
					prefix := strings.SplitN(o.Pattern, "**", 2)[0]
					if prefix != "" && strings.HasPrefix(signals[i].FilePath, prefix) {
						matched = true
					}
				}
			}
			if matched {
				p := o.Priority
				signals[i].Priority = &p
				break // First matching override wins.
			}
		}
	}

	return signals
}

// validateDistribution logs a warning if the priority distribution looks suspicious.
func validateDistribution(signals []signal.RawSignal) {
	if len(signals) == 0 {
		return
	}

	counts := make(map[int]int)
	assigned := 0
	for _, sig := range signals {
		if sig.Priority != nil {
			counts[*sig.Priority]++
			assigned++
		}
	}

	if assigned == 0 {
		return
	}

	// Warn if >50% are P1 (likely over-prioritization).
	if p1Frac := float64(counts[1]) / float64(assigned); p1Frac > 0.5 {
		slog.Warn("priority distribution skew: >50% P1",
			"p1", counts[1], "total", assigned, "fraction", fmt.Sprintf("%.0f%%", p1Frac*100))
	}

	// Warn if all assigned signals have the same priority.
	if len(counts) == 1 {
		for p := range counts {
			slog.Warn("all signals assigned same priority", "priority", p, "count", assigned)
		}
	}

	slog.Info("priority distribution",
		"P1", counts[1], "P2", counts[2], "P3", counts[3], "P4", counts[4],
		"total", assigned)
}
