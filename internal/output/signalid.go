// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"crypto/sha256"
	"fmt"

	"github.com/davetashner/stringer/internal/signal"
)

// SignalID produces a deterministic ID from signal content.
//
// It hashes these fields, in this order, separated by NUL bytes:
//
//	Source | Kind | FilePath | Line | Title
//
// using SHA-256, takes the first 4 bytes, hex-encodes them (8 lowercase hex
// chars), and prepends the given prefix.
//
// # Stability contract
//
// Signal IDs are the join key that links a scanned signal to the beads issue
// tracking it. Changing any of the following breaks existing beads silently:
//
//   - the set or order of hashed fields
//   - the separator (NUL byte)
//   - the hash algorithm or truncation length
//   - the hex encoding case or prefix format
//
// Treat the composition above as a fixed contract. If it ever needs to change,
// ship both old and new IDs for a transition window and migrate callers that
// persist IDs (the beads JSONL, baselines, report output). The regression
// tests in signalid_test.go pin specific hash outputs — they will fail loudly
// on any change here.
func SignalID(sig signal.RawSignal, prefix string) string {
	h := sha256.New()
	// Write each field separated by null bytes to avoid collisions
	// from field concatenation (e.g., "ab"+"c" vs "a"+"bc").
	// sha256.Hash.Write never returns an error per the hash.Hash contract.
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%s", sig.Source, sig.Kind, sig.FilePath, sig.Line, sig.Title)
	sum := h.Sum(nil)
	return fmt.Sprintf("%s%x", prefix, sum[:4])
}
