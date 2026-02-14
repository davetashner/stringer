// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"crypto/sha256"
	"fmt"

	"github.com/davetashner/stringer/internal/signal"
)

// SignalID produces a deterministic ID from signal content.
// It hashes Source + Kind + FilePath + Line + Title using SHA-256,
// truncates to 8 hex characters, and prepends the given prefix.
func SignalID(sig signal.RawSignal, prefix string) string {
	h := sha256.New()
	// Write each field separated by null bytes to avoid collisions
	// from field concatenation (e.g., "ab"+"c" vs "a"+"bc").
	// sha256.Hash.Write never returns an error per the hash.Hash contract.
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%s", sig.Source, sig.Kind, sig.FilePath, sig.Line, sig.Title)
	sum := h.Sum(nil)
	return fmt.Sprintf("%s%x", prefix, sum[:4])
}
