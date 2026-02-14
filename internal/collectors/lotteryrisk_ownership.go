// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

// ownershipFraction computes a weighted ownership score from blame-line and
// commit-weight contributions. Returns a value in [0, 1].
func ownershipFraction(blameLines, totalBlameLines int, commitWeight, totalCommitWeight float64) float64 {
	var blameFrac float64
	if totalBlameLines > 0 {
		blameFrac = float64(blameLines) / float64(totalBlameLines)
	}
	var commitFrac float64
	if totalCommitWeight > 0 {
		commitFrac = commitWeight / totalCommitWeight
	}
	return blameFrac*blameWeight + commitFrac*commitWeightFraction
}

// computeLotteryRisk calculates the lottery risk for a directory: the minimum
// number of authors whose combined ownership exceeds 50%.
func computeLotteryRisk(own *dirOwnership) int {
	if len(own.Authors) == 0 {
		return 0
	}

	// Compute combined ownership per author.
	totalBlameLines := own.TotalLines
	totalCW := totalCommitWeight(own)

	type authorOwnership struct {
		Name      string
		Ownership float64
	}

	var authors []authorOwnership
	for name, stats := range own.Authors {
		ownership := ownershipFraction(stats.BlameLines, totalBlameLines, stats.CommitWeight, totalCW)
		authors = append(authors, authorOwnership{Name: name, Ownership: ownership})
	}

	// Sort by ownership descending (highest first).
	sort.Slice(authors, func(i, j int) bool {
		if authors[i].Ownership != authors[j].Ownership {
			return authors[i].Ownership > authors[j].Ownership
		}
		return authors[i].Name < authors[j].Name // deterministic tie-break
	})

	// Count how many authors are needed to exceed 50%.
	cumulative := 0.0
	for i, a := range authors {
		cumulative += a.Ownership
		if cumulative > ownershipMajority {
			return i + 1
		}
	}

	return len(authors)
}

// totalCommitWeight sums all authors' commit weights in a directory.
func totalCommitWeight(own *dirOwnership) float64 {
	var total float64
	for _, stats := range own.Authors {
		total += stats.CommitWeight
	}
	return total
}

// buildDirectoryOwnership converts internal dirOwnership into the exported
// DirectoryOwnership metrics type.
func buildDirectoryOwnership(own *dirOwnership) DirectoryOwnership {
	totalBlameLines := own.TotalLines
	totalCW := totalCommitWeight(own)

	var authors []AuthorShare
	for name, stats := range own.Authors {
		ownership := ownershipFraction(stats.BlameLines, totalBlameLines, stats.CommitWeight, totalCW)
		authors = append(authors, AuthorShare{Name: name, Ownership: ownership})
	}

	sort.Slice(authors, func(i, j int) bool {
		if authors[i].Ownership != authors[j].Ownership {
			return authors[i].Ownership > authors[j].Ownership
		}
		return authors[i].Name < authors[j].Name
	})

	return DirectoryOwnership{
		Path:        own.Path,
		LotteryRisk: own.LotteryRisk,
		Authors:     authors,
		TotalLines:  own.TotalLines,
	}
}

// buildLotteryRiskSignal constructs a RawSignal for a low-lottery-risk directory.
// If anon is non-nil, author names are anonymized.
func buildLotteryRiskSignal(own *dirOwnership, anon *nameAnonymizer) signal.RawSignal {
	// Find primary author (highest ownership).
	totalBlameLines := own.TotalLines
	totalCW := totalCommitWeight(own)

	type authorPct struct {
		Name string
		Pct  float64
	}

	var authors []authorPct
	for name, stats := range own.Authors {
		pct := ownershipFraction(stats.BlameLines, totalBlameLines, stats.CommitWeight, totalCW) * 100
		displayName := name
		if anon != nil {
			displayName = anon.anonymize(name)
		}
		authors = append(authors, authorPct{Name: displayName, Pct: pct})
	}

	// Sort by percentage descending, then by name for determinism.
	sort.Slice(authors, func(i, j int) bool {
		if authors[i].Pct != authors[j].Pct {
			return authors[i].Pct > authors[j].Pct
		}
		return authors[i].Name < authors[j].Name
	})

	primary := authors[0]

	// Build description with top authors.
	var descParts []string
	descParts = append(descParts, fmt.Sprintf("Lottery risk: %d", own.LotteryRisk))
	descParts = append(descParts, "Top authors:")
	for _, a := range authors {
		if a.Pct < 1.0 {
			break // skip negligible contributors
		}
		descParts = append(descParts, fmt.Sprintf("  - %s: %.0f%%", a.Name, a.Pct))
	}

	confidence := lotteryRiskConfidence(own.LotteryRisk)

	return signal.RawSignal{
		Source:      "lotteryrisk",
		Kind:        "low-lottery-risk",
		FilePath:    own.Path,
		Line:        0,
		Title:       fmt.Sprintf("%s: %s (lottery risk %d, primary: %s %.0f%%)", lotteryRiskLabel(own.LotteryRisk), own.Path, own.LotteryRisk, primary.Name, primary.Pct),
		Description: strings.Join(descParts, "\n"),
		Confidence:  confidence,
		Tags:        []string{"low-lottery-risk"},
	}
}

// lotteryRiskLabel returns a human-readable severity label for a lottery risk score.
func lotteryRiskLabel(riskScore int) string {
	switch {
	case riskScore <= 1:
		return "Critical lottery risk"
	case riskScore == 2:
		return "High lottery risk"
	default:
		return "Moderate lottery risk"
	}
}

// lotteryRiskConfidence maps lottery risk to confidence score per DR-006.
func lotteryRiskConfidence(riskScore int) float64 {
	switch {
	case riskScore <= 1:
		return 0.8
	case riskScore == 2:
		return 0.5
	default:
		return 0.3
	}
}

// nameAnonymizer provides stable, deterministic anonymization of author names.
// The same real name always maps to the same label within a single scan.
type nameAnonymizer struct {
	mapping map[string]string
	next    int
}

// newNameAnonymizer creates a new anonymizer.
func newNameAnonymizer() *nameAnonymizer {
	return &nameAnonymizer{mapping: make(map[string]string)}
}

// anonymize returns a stable anonymous label for the given name.
func (a *nameAnonymizer) anonymize(name string) string {
	if label, ok := a.mapping[name]; ok {
		return label
	}
	label := contributorLabel(a.next)
	a.mapping[name] = label
	a.next++
	return label
}

// contributorLabel returns "Contributor A", "Contributor B", ..., "Contributor Z",
// "Contributor AA", "Contributor AB", etc.
func contributorLabel(id int) string {
	if id < 26 {
		return "Contributor " + string(rune('A'+id))
	}
	// For id >= 26: AA=26, AB=27, ..., AZ=51, BA=52, ...
	first := (id / 26) - 1
	second := id % 26
	return "Contributor " + string(rune('A'+first)) + string(rune('A'+second))
}

// resolveAnonymize determines whether author names should be anonymized based
// on the mode ("always", "never", "auto") and the repository visibility.
func resolveAnonymize(ctx context.Context, ghCtx *githubContext, mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	case "auto", "":
		// Auto mode: anonymize if the repo is public.
		if ghCtx == nil {
			return false // no API available, default to not anonymizing
		}
		repo, _, err := ghCtx.API.GetRepository(ctx, ghCtx.Owner, ghCtx.Repo)
		if err != nil || repo == nil {
			return false // can't determine visibility, default to not anonymizing
		}
		// Public repos -> anonymize; private repos -> don't.
		return !repo.GetPrivate()
	default:
		return false
	}
}
