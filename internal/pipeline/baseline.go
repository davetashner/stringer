// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import (
	"log/slog"

	"github.com/davetashner/stringer/internal/baseline"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

// FilterSuppressed removes signals whose exact ID or stable key (DR-027)
// appears in the baseline.
// Expired suppressions are NOT filtered (signal reappears after TTL).
// Returns the filtered signals and count of suppressed signals.
func FilterSuppressed(signals []signal.RawSignal, state *baseline.BaselineState, prefix string) ([]signal.RawSignal, int) {
	lookup := baseline.Lookup(state)
	if len(lookup) == 0 {
		return signals, 0
	}

	suppressed := 0
	result := make([]signal.RawSignal, 0, len(signals))

	for _, sig := range signals {
		sup, found := output.LookupSuppression(lookup, sig, prefix)
		if found && !baseline.IsExpired(sup) {
			suppressed++
			slog.Debug("suppressed signal", "id", sup.SignalID, "reason", sup.Reason)
			continue
		}
		result = append(result, sig)
	}

	return result, suppressed
}
