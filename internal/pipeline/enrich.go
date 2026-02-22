// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import "github.com/davetashner/stringer/internal/signal"

// boostRule maps a signal kind to the confidence boost applied when a signal
// co-locates with that kind in the same file.
type boostRule struct {
	Kind  string
	Boost float64
}

// boostRules defines which signal kinds trigger confidence boosts and by how
// much. A signal is never boosted by its own kind (no self-boost).
var boostRules = []boostRule{
	{Kind: "churn", Boost: 0.10},
	{Kind: "vulnerable-dependency", Boost: 0.05},
	{Kind: "low-lottery-risk", Boost: 0.05},
}

// BoostColocatedSignals applies cross-collector confidence boosts to signals
// that share a file with certain risk-indicator kinds. The index is built once
// before iteration so boosted signals cannot create new eligibility (no
// cascading). A signal is never boosted by its own kind.
func BoostColocatedSignals(signals []signal.RawSignal) {
	if len(signals) == 0 {
		return
	}

	// Build file → kinds index.
	fileKinds := make(map[string]map[string]bool)
	for _, s := range signals {
		if s.FilePath == "" {
			continue
		}
		if fileKinds[s.FilePath] == nil {
			fileKinds[s.FilePath] = make(map[string]bool)
		}
		fileKinds[s.FilePath][s.Kind] = true
	}

	// Apply boosts.
	for i := range signals {
		s := &signals[i]
		kinds := fileKinds[s.FilePath]
		if len(kinds) == 0 {
			continue
		}

		var totalBoost float64
		for _, rule := range boostRules {
			if s.Kind == rule.Kind {
				continue // no self-boost
			}
			if kinds[rule.Kind] {
				totalBoost += rule.Boost
			}
		}

		if totalBoost > 0 {
			s.Confidence += totalBoost
			if s.Confidence > 1.0 {
				s.Confidence = 1.0
			}
		}
	}
}
