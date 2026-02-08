package beads

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

// FilterAgainstExisting removes signals that match existing beads.
// Matching is done via 3 tiers: ID match, hash match, and normalized title match.
// Both open and closed beads are matched to avoid re-opening resolved work.
func FilterAgainstExisting(signals []signal.RawSignal, existing []Bead) []signal.RawSignal {
	if len(existing) == 0 {
		return signals
	}

	// Build lookup sets for each tier.
	idSet := make(map[string]bool, len(existing))
	hashSet := make(map[string]bool, len(existing))
	titleSet := make(map[string]bool, len(existing))

	for _, b := range existing {
		idSet[b.ID] = true

		// Extract hash portion from str-XXXXXXXX IDs.
		if strings.HasPrefix(b.ID, "str-") {
			hashSet[strings.TrimPrefix(b.ID, "str-")] = true
		}

		titleSet[normalizeTitle(b.Title)] = true
	}

	var filtered []signal.RawSignal
	for _, s := range signals {
		sigID := signalToBeadID(s)
		sigHash := signalHash(s)
		sigTitle := normalizeTitle(s.Title)

		// Tier 1: ID match.
		if idSet[sigID] {
			continue
		}

		// Tier 2: Hash match (signal hash matches hash portion of any str-* bead).
		if hashSet[sigHash] {
			continue
		}

		// Tier 3: Normalized title match.
		if titleSet[sigTitle] {
			continue
		}

		filtered = append(filtered, s)
	}

	return filtered
}

// signalToBeadID produces the same ID that BeadsFormatter.generateID() would.
func signalToBeadID(s signal.RawSignal) string {
	return "str-" + signalHash(s)
}

// signalHash computes the hash portion of a signal's bead ID.
// Must match the algorithm in output/beads.go generateID().
func signalHash(s signal.RawSignal) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%s", s.Source, s.Kind, s.FilePath, s.Line, s.Title)
	sum := h.Sum(nil)
	return fmt.Sprintf("%x", sum[:4])
}

// normalizeTitle normalizes a title for comparison:
//   - lowercase
//   - strip common prefixes (TODO:, FIXME:, HACK:, XXX:, BUG:, OPTIMIZE:)
//   - trim whitespace
func normalizeTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))

	prefixes := []string{"todo:", "fixme:", "hack:", "xxx:", "bug:", "optimize:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(t, prefix) {
			t = strings.TrimSpace(strings.TrimPrefix(t, prefix))
			break
		}
	}

	return t
}
