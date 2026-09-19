// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"crypto/sha256"
	"fmt"
	"regexp"

	"github.com/davetashner/stringer/internal/baseline"
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

// StableIDPrefix marks a baseline key produced by StableSignalID, so a
// baseline can hold both exact (str-) and stable (sts-) entries (DR-027).
const StableIDPrefix = "sts-"

// titleNumber matches a standalone number (integer or decimal) in a title.
// Numbers glued to identifier characters (parseV2, sha256) are kept.
var titleNumber = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)

// StableSignalID produces a baseline key that survives line shifts and metric
// changes. It hashes, like SignalID but without Line and with every standalone
// number in the title replaced by "#":
//
//	Source | Kind | FilePath | normalised Title
//
// so "Complex function: Merge (cyclomatic: 88, …)" at line 14 and the same
// function at line 15 with cyclomatic 85 share a key. Signals of one kind in
// one file whose titles differ only in numbers (duplicated blocks, secrets
// whose title embeds a line number) share a key too.
//
// Stable keys are persisted in committed baselines, so the composition is a
// contract pinned by TestStableSignalID_StabilityContract, like SignalID.
func StableSignalID(sig signal.RawSignal) string {
	h := sha256.New()
	title := titleNumber.ReplaceAllString(sig.Title, "#")
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", sig.Source, sig.Kind, sig.FilePath, title)
	sum := h.Sum(nil)
	return fmt.Sprintf("%s%x", StableIDPrefix, sum[:4])
}

// LookupSuppression returns the baseline suppression covering sig: an entry
// under its exact ID (SignalID with prefix) or under its stable key. Expired
// suppressions are returned too; callers decide what expiry means.
func LookupSuppression(lookup map[string]baseline.Suppression, sig signal.RawSignal, prefix string) (baseline.Suppression, bool) {
	if sup, ok := lookup[SignalID(sig, prefix)]; ok {
		return sup, true
	}
	sup, ok := lookup[StableSignalID(sig)]
	return sup, ok
}
