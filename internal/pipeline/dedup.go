// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import (
	"crypto/sha256"
	"fmt"

	"github.com/davetashner/stringer/internal/signal"
)

// SignalHash computes a content-based hash for a signal.
// The hash key is: Source + Kind + FilePath + Line + Title.
// It uses SHA-256 truncated to 8 hex characters (4 bytes).
func SignalHash(s signal.RawSignal) string {
	h := sha256.New()
	// Use null-byte separators to avoid collisions from field concatenation.
	// sha256.Hash.Write never returns an error per the hash.Hash contract.
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%s",
		s.Source, s.Kind, s.FilePath, s.Line, s.Title)
	sum := h.Sum(nil)
	return fmt.Sprintf("%x", sum[:4])
}

// DeduplicateSignals removes duplicate signals based on content hashing.
// When duplicates are found, the first occurrence is kept. If a later
// duplicate has a higher Confidence score, the kept signal's Confidence
// is updated to the higher value.
func DeduplicateSignals(signals []signal.RawSignal) []signal.RawSignal {
	if len(signals) == 0 {
		return signals
	}

	seen := make(map[string]int) // hash -> index in result slice
	result := make([]signal.RawSignal, 0, len(signals))

	for _, s := range signals {
		hash := SignalHash(s)
		if idx, exists := seen[hash]; exists {
			// Duplicate found — update confidence if the new one is higher.
			if s.Confidence > result[idx].Confidence {
				result[idx].Confidence = s.Confidence
			}
			continue
		}
		seen[hash] = len(result)
		result = append(result, s)
	}

	return result
}
