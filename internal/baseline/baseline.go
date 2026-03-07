// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package baseline manages signal suppression state for repeat scans.
//
// Suppressions are stored in .stringer/baseline.json and are intended to be
// version-controlled. Users suppress signals they have acknowledged, marked
// as won't-fix, or identified as false positives. On subsequent scans the
// baseline is loaded and matched against current signals to filter output.
package baseline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/davetashner/stringer/internal/testable"
)

// baselineDir is the directory name within a repo where baseline state is stored.
const baselineDir = ".stringer"

// baselineFile is the filename for baseline suppressions.
const baselineFile = "baseline.json"

// schemaVersion is the current baseline file schema version.
const schemaVersion = "1"

// FS is the file system implementation used by this package.
// Override in tests with a testable.MockFileSystem.
var FS testable.FileSystem = testable.DefaultFS

// Reason describes why a signal was suppressed.
type Reason string

const (
	// ReasonAcknowledged indicates the signal was reviewed and accepted.
	ReasonAcknowledged Reason = "acknowledged"

	// ReasonWontFix indicates the signal will not be addressed.
	ReasonWontFix Reason = "won't-fix"

	// ReasonFalsePositive indicates the signal is not a real issue.
	ReasonFalsePositive Reason = "false-positive"
)

// validReasons is the set of allowed suppression reasons.
var validReasons = map[Reason]bool{
	ReasonAcknowledged:  true,
	ReasonWontFix:       true,
	ReasonFalsePositive: true,
}

// Suppression records a single suppressed signal.
type Suppression struct {
	SignalID     string     `json:"signal_id"`
	Reason       Reason     `json:"reason"`
	Comment      string     `json:"comment,omitempty"`
	SuppressedBy string     `json:"suppressed_by,omitempty"`
	SuppressedAt time.Time  `json:"suppressed_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// BaselineState is the top-level structure persisted in baseline.json.
type BaselineState struct {
	Version      string        `json:"version"`
	Suppressions []Suppression `json:"suppressions"`
}

// Load reads the baseline file from <repoPath>/.stringer/baseline.json.
// If the file does not exist, it returns (nil, nil).
func Load(repoPath string) (*BaselineState, error) {
	path := filepath.Join(repoPath, baselineDir, baselineFile)
	data, err := FS.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var s BaselineState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the baseline state to <repoPath>/.stringer/baseline.json.
// It creates the .stringer directory if it does not exist.
// The write is atomic: data is first written to a temporary file in the same
// directory, then renamed to the final path.
func Save(repoPath string, state *BaselineState) error {
	dir := filepath.Join(repoPath, baselineDir)
	if err := FS.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create baseline directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	finalPath := filepath.Join(dir, baselineFile)
	tmpPath := finalPath + ".tmp"

	// Write to temp file first.
	if err := FS.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write baseline temp file: %w", err)
	}

	// Rename temp to final for atomic replacement.
	if err := rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename baseline file: %w", err)
	}

	return nil
}

// ValidateReason checks that r is one of the allowed suppression reasons.
func ValidateReason(r Reason) error {
	if validReasons[r] {
		return nil
	}
	return fmt.Errorf("invalid suppression reason %q: must be one of acknowledged, won't-fix, false-positive", r)
}

// IsExpired returns true if the suppression has an expiration time that is
// in the past.
func IsExpired(s Suppression) bool {
	return s.ExpiresAt != nil && s.ExpiresAt.Before(time.Now())
}

// Lookup builds an O(1) lookup map from SignalID to Suppression.
func Lookup(state *BaselineState) map[string]Suppression {
	if state == nil {
		return nil
	}
	m := make(map[string]Suppression, len(state.Suppressions))
	for _, s := range state.Suppressions {
		m[s.SignalID] = s
	}
	return m
}

// AddOrUpdate adds a new suppression or updates an existing one (matched by
// SignalID). When updating, the existing entry is replaced entirely.
func AddOrUpdate(state *BaselineState, s Suppression) {
	for i, existing := range state.Suppressions {
		if existing.SignalID == s.SignalID {
			state.Suppressions[i] = s
			return
		}
	}
	state.Suppressions = append(state.Suppressions, s)
}

// Remove deletes a suppression by SignalID. It returns true if the suppression
// was found and removed, false otherwise.
func Remove(state *BaselineState, signalID string) bool {
	for i, s := range state.Suppressions {
		if s.SignalID == signalID {
			state.Suppressions = append(state.Suppressions[:i], state.Suppressions[i+1:]...)
			return true
		}
	}
	return false
}
