// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSARIFFormatter_Name(t *testing.T) {
	f := NewSARIFFormatter()
	assert.Equal(t, "sarif", f.Name())
}

func TestSARIFFormatter_EmptySignals(t *testing.T) {
	f := NewSARIFFormatter()
	var buf bytes.Buffer
	require.NoError(t, f.Format(nil, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	assert.Equal(t, "2.1.0", doc.Version)
	assert.Equal(t, "https://json.schemastore.org/sarif-2.1.0.json", doc.Schema)
	require.Len(t, doc.Runs, 1)
	assert.Equal(t, "stringer", doc.Runs[0].Tool.Driver.Name)
	assert.Empty(t, doc.Runs[0].Results)
	assert.Empty(t, doc.Runs[0].Tool.Driver.Rules)
}

func TestSARIFFormatter_BasicSignal(t *testing.T) {
	f := NewSARIFFormatter()
	f.Version = "1.0.1"

	signals := []signal.RawSignal{
		{
			Source:     "todos",
			Kind:       "todo",
			FilePath:   "internal/foo.go",
			Line:       42,
			Title:      "TODO: fix this",
			Confidence: 0.5,
			Author:     "alice",
			Tags:       []string{"debt"},
			Timestamp:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	run := doc.Runs[0]

	// Tool info
	assert.Equal(t, "1.0.1", run.Tool.Driver.Version)
	assert.Equal(t, "https://github.com/davetashner/stringer", run.Tool.Driver.InformationURI)

	// Rules
	require.Len(t, run.Tool.Driver.Rules, 1)
	rule := run.Tool.Driver.Rules[0]
	assert.Equal(t, "todo", rule.ID)
	assert.Equal(t, "Unresolved TODO comment in source code", rule.ShortDescription.Text)
	require.NotNil(t, rule.DefaultConfig)
	assert.Equal(t, "warning", rule.DefaultConfig.Level)

	// Results
	require.Len(t, run.Results, 1)
	result := run.Results[0]
	assert.Equal(t, "todo", result.RuleID)
	assert.Equal(t, 0, result.RuleIndex)
	assert.Equal(t, "note", result.Level) // 0.5 confidence -> P3 -> note
	assert.InDelta(t, 50.0, result.Rank, 0.01)
	assert.Equal(t, "TODO: fix this", result.Message.Text)

	// Location
	require.Len(t, result.Locations, 1)
	loc := result.Locations[0].PhysicalLocation
	assert.Equal(t, "internal/foo.go", loc.ArtifactLocation.URI)
	assert.Equal(t, "%SRCROOT%", loc.ArtifactLocation.URIBaseID)
	require.NotNil(t, loc.Region)
	assert.Equal(t, 42, loc.Region.StartLine)

	// Fingerprint
	assert.NotEmpty(t, result.PartialFingerprints["stringer/v1"])

	// Properties
	assert.NotNil(t, result.Properties)
}

func TestSARIFFormatter_NoLineOmitsRegion(t *testing.T) {
	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{
			Source:     "gitlog",
			Kind:       "stale-branch",
			Title:      "Branch feature/old is stale",
			Confidence: 0.3,
		},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	result := doc.Runs[0].Results[0]
	// No file path -> no locations
	assert.Empty(t, result.Locations)
}

func TestSARIFFormatter_NoFilePathOmitsLocations(t *testing.T) {
	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{
			Source:     "gitlog",
			Kind:       "stale-branch",
			FilePath:   "some/path.go",
			Line:       0,
			Title:      "Stale branch",
			Confidence: 0.4,
		},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	result := doc.Runs[0].Results[0]
	require.Len(t, result.Locations, 1)
	assert.Nil(t, result.Locations[0].PhysicalLocation.Region)
}

func TestSARIFFormatter_PriorityMapping(t *testing.T) {
	tests := []struct {
		confidence float64
		priority   *int
		wantLevel  string
		wantRank   float64
	}{
		{0.9, nil, "error", 90.0},   // P1
		{0.7, nil, "warning", 70.0}, // P2
		{0.5, nil, "note", 50.0},    // P3
		{0.2, nil, "none", 20.0},    // P4
	}

	for _, tt := range tests {
		f := NewSARIFFormatter()
		signals := []signal.RawSignal{
			{Kind: "todo", Title: "test", Confidence: tt.confidence},
		}
		if tt.priority != nil {
			signals[0].Priority = tt.priority
		}

		var buf bytes.Buffer
		require.NoError(t, f.Format(signals, &buf))

		var doc sarifDocument
		require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

		result := doc.Runs[0].Results[0]
		assert.Equal(t, tt.wantLevel, result.Level, "confidence=%v", tt.confidence)
		assert.InDelta(t, tt.wantRank, result.Rank, 0.01, "confidence=%v", tt.confidence)
	}
}

func TestSARIFFormatter_LLMPriorityOverride(t *testing.T) {
	f := NewSARIFFormatter()
	p := 1
	signals := []signal.RawSignal{
		{
			Kind:       "todo",
			Title:      "Critical fix needed",
			Confidence: 0.3, // Would be P4/none without override
			Priority:   &p,
		},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	result := doc.Runs[0].Results[0]
	assert.Equal(t, "error", result.Level)     // P1 override
	assert.InDelta(t, 30.0, result.Rank, 0.01) // Rank still reflects raw confidence
}

func TestSARIFFormatter_MultipleKindsCreateMultipleRules(t *testing.T) {
	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{Kind: "todo", Title: "first", Confidence: 0.5},
		{Kind: "vuln", Title: "CVE-2024-1234", Confidence: 0.9},
		{Kind: "todo", Title: "second", Confidence: 0.4},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	run := doc.Runs[0]

	// Two unique kinds -> two rules (sorted alphabetically).
	require.Len(t, run.Tool.Driver.Rules, 2)
	assert.Equal(t, "todo", run.Tool.Driver.Rules[0].ID)
	assert.Equal(t, "vuln", run.Tool.Driver.Rules[1].ID)

	// Three results total.
	require.Len(t, run.Results, 3)

	// Both "todo" results reference rule index 0.
	assert.Equal(t, 0, run.Results[0].RuleIndex)
	assert.Equal(t, 0, run.Results[2].RuleIndex)
	// "vuln" result references rule index 1.
	assert.Equal(t, 1, run.Results[1].RuleIndex)
}

func TestSARIFFormatter_DefaultVersion(t *testing.T) {
	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	assert.Equal(t, "dev", doc.Runs[0].Tool.Driver.Version)
}

func TestSARIFFormatter_WriteFailure(t *testing.T) {
	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", Confidence: 0.5},
	}

	w := &failWriter{failAfter: 0}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sarif")
}

func TestSARIFFormatter_WriteFailureNewline(t *testing.T) {
	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", Confidence: 0.5},
	}

	w := &failWriter{failAfter: 1}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sarif trailing newline")
}

func TestSARIFFormatter_UnknownKindFallback(t *testing.T) {
	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{Kind: "some-new-kind", Title: "test", Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	rule := doc.Runs[0].Tool.Driver.Rules[0]
	assert.Equal(t, "some-new-kind", rule.ID)
	assert.Contains(t, rule.ShortDescription.Text, "some-new-kind")
}

func TestPriorityToSARIFLevel(t *testing.T) {
	assert.Equal(t, "error", priorityToSARIFLevel(1))
	assert.Equal(t, "warning", priorityToSARIFLevel(2))
	assert.Equal(t, "note", priorityToSARIFLevel(3))
	assert.Equal(t, "none", priorityToSARIFLevel(4))
	assert.Equal(t, "none", priorityToSARIFLevel(99))
}

func TestRuleDescription_Known(t *testing.T) {
	assert.Equal(t, "Unresolved TODO comment in source code", ruleDescription("todo"))
	assert.Equal(t, "Known vulnerability in dependency", ruleDescription("vuln"))
}

func TestRuleDescription_Unknown(t *testing.T) {
	desc := ruleDescription("never-seen-before")
	assert.Contains(t, desc, "never-seen-before")
}

func TestKindToCollector(t *testing.T) {
	assert.Equal(t, "todos", kindToCollector("todo"))
	assert.Equal(t, "vuln", kindToCollector("vuln"))
	assert.Equal(t, "lotteryrisk", kindToCollector("low-lottery-risk"))
	assert.Equal(t, "", kindToCollector("unknown-kind"))
}

func TestSARIFFormatter_ValidJSON(t *testing.T) {
	f := NewSARIFFormatter()
	f.Version = "1.0.0"

	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "first", Confidence: 0.5, Tags: []string{"debt"}},
		{Source: "vuln", Kind: "vuln", FilePath: "go.mod", Line: 17, Title: "CVE-2024-1234", Confidence: 0.95, Author: "scanner"},
		{Source: "patterns", Kind: "large-file", FilePath: "big.go", Line: 0, Title: "File exceeds threshold", Confidence: 0.6},
		{Source: "gitlog", Kind: "stale-branch", Title: "Branch old/feature", Confidence: 0.3},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	// Verify valid JSON.
	assert.True(t, json.Valid(buf.Bytes()), "output should be valid JSON")

	// Verify structure roundtrips.
	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	assert.Equal(t, "2.1.0", doc.Version)
	require.Len(t, doc.Runs, 1)
	assert.Len(t, doc.Runs[0].Results, 4)
	// Rules are deduped: todo, vuln, large-file, stale-branch = 4 unique kinds.
	assert.Len(t, doc.Runs[0].Tool.Driver.Rules, 4)
}

func TestSARIFFormatter_Registration(t *testing.T) {
	f, err := GetFormatter("sarif")
	require.NoError(t, err)
	assert.Equal(t, "sarif", f.Name())
}
