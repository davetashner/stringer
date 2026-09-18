// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/davetashner/stringer/internal/gitcli"
	"github.com/davetashner/stringer/internal/signal"
)

// Minimum-substance thresholds for emitting a low-lottery-risk signal
// (DR-006 amendment, stringer-nxx.6). Directories below either threshold are
// still reported in metrics but never produce a signal: a directory holding
// two static fixtures owned by one person is not an organizational risk.
// These are variables rather than constants so tests can exercise tiny
// fixtures; production code treats them as fixed defaults.
var (
	// minLotteryRiskFiles is the minimum number of source files a directory
	// must contain before it can be flagged.
	minLotteryRiskFiles = 3

	// minLotteryRiskLines is the minimum number of blamed source lines a
	// directory must contain before it can be flagged.
	minLotteryRiskLines = 100
)

// shallowConfidenceCap is the maximum confidence for a low-lottery-risk signal
// produced from a shallow clone. Shallow blame attributes every line older
// than the clone boundary to the boundary commit's author, so ownership is
// over-estimated, not under-estimated.
const shallowConfidenceCap = 0.5

// nonCodeDirNames are path segments that mark a directory as holding static
// assets, fixtures, or templates rather than code. Ownership of such
// directories is not a lottery risk, regardless of file extensions inside.
var nonCodeDirNames = map[string]bool{
	"fixtures":      true,
	"fixture":       true,
	"static":        true,
	"testdata":      true,
	"test_data":     true,
	"assets":        true,
	"templates":     true,
	"fonts":         true,
	"images":        true,
	"img":           true,
	"icons":         true,
	"snapshots":     true,
	"__snapshots__": true,
	"golden":        true,
	"locales":       true,
	"i18n":          true,
}

// isNonCodeDir reports whether any segment of relPath names a static-asset,
// fixture, or template directory.
func isNonCodeDir(relPath string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if nonCodeDirNames[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// hasMinimumSubstance reports whether a directory holds enough source code
// for single-author ownership to be a meaningful risk signal.
func hasMinimumSubstance(own *dirOwnership) bool {
	if isNonCodeDir(own.Path) {
		return false
	}
	return own.SourceFiles >= minLotteryRiskFiles && own.TotalLines >= minLotteryRiskLines
}

// historyInfo describes how much git history is available for blame.
type historyInfo struct {
	// Shallow is true when the repository is a shallow clone.
	Shallow bool
	// Commits is the number of commits reachable from HEAD (0 if unknown).
	Commits int
}

// detectHistory checks whether gitRoot is a shallow clone and counts the
// commits reachable from HEAD. Any git failure degrades to "not shallow,
// unknown count" so the collector never fails on this probe.
func detectHistory(ctx context.Context, gitRoot string) historyInfo {
	var info historyInfo
	if out, err := gitcli.Exec(ctx, gitRoot, "rev-parse", "--is-shallow-repository"); err == nil {
		info.Shallow = strings.TrimSpace(out) == "true"
	}
	if out, err := gitcli.Exec(ctx, gitRoot, "rev-list", "--count", "HEAD"); err == nil {
		if n, convErr := strconv.Atoi(strings.TrimSpace(out)); convErr == nil {
			info.Commits = n
		}
	}
	return info
}

// applyShallowCaveat caps the confidence of a low-lottery-risk signal and
// explains in its description why the ownership numbers are inflated.
func applyShallowCaveat(sig *signal.RawSignal, commits int) {
	sig.Confidence = math.Min(sig.Confidence, shallowConfidenceCap)
	sig.Description += fmt.Sprintf("\n\nHistory is shallow (%d commits); ownership is over-estimated because lines older than the clone boundary are attributed to the boundary commit's author. Re-run on a full clone for accurate numbers.", commits)
	sig.Tags = append(sig.Tags, "shallow-history")
}
