// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/baseline"
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

// --- SA5.2: automationDetails tests ---

func TestSARIFFormatter_AutomationDetails_Present(t *testing.T) {
	f := NewSARIFFormatter()
	f.RepoPath = "/tmp/myrepo"
	f.GitHead = "abc1234def5678"

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	run := doc.Runs[0]
	require.NotNil(t, run.AutomationDetails)
	assert.Equal(t, "stringer/abc1234", run.AutomationDetails.ID)
	assert.NotEmpty(t, run.AutomationDetails.GUID)
}

func TestSARIFFormatter_AutomationDetails_Deterministic(t *testing.T) {
	f1 := &SARIFFormatter{RepoPath: "/tmp/repo", GitHead: "abc1234"}
	f2 := &SARIFFormatter{RepoPath: "/tmp/repo", GitHead: "abc1234"}

	ad1 := f1.buildAutomationDetails()
	ad2 := f2.buildAutomationDetails()

	require.NotNil(t, ad1)
	require.NotNil(t, ad2)
	assert.Equal(t, ad1.GUID, ad2.GUID, "same inputs should produce same GUID")
}

func TestSARIFFormatter_AutomationDetails_DifferentHead(t *testing.T) {
	f1 := &SARIFFormatter{RepoPath: "/tmp/repo", GitHead: "abc1234"}
	f2 := &SARIFFormatter{RepoPath: "/tmp/repo", GitHead: "def5678"}

	ad1 := f1.buildAutomationDetails()
	ad2 := f2.buildAutomationDetails()

	require.NotNil(t, ad1)
	require.NotNil(t, ad2)
	assert.NotEqual(t, ad1.GUID, ad2.GUID, "different heads should produce different GUIDs")
	assert.NotEqual(t, ad1.ID, ad2.ID)
}

func TestSARIFFormatter_AutomationDetails_EmptyHead(t *testing.T) {
	f := NewSARIFFormatter()
	f.RepoPath = "/tmp/repo"
	// GitHead is empty

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	assert.Nil(t, doc.Runs[0].AutomationDetails, "empty GitHead should omit automationDetails")
}

func TestSARIFFormatter_AutomationDetails_ShortHead(t *testing.T) {
	f := &SARIFFormatter{RepoPath: "/tmp/repo", GitHead: "abc"}
	ad := f.buildAutomationDetails()
	require.NotNil(t, ad)
	assert.Equal(t, "stringer/abc", ad.ID, "short head should be used as-is")
}

// --- SA5.4: code snippet tests ---

func TestSARIFFormatter_Snippet_MiddleLine(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o600))

	f := NewSARIFFormatter()
	f.RepoPath = dir

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", FilePath: "test.go", Line: 5, Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	result := doc.Runs[0].Results[0]
	require.Len(t, result.Locations, 1)
	region := result.Locations[0].PhysicalLocation.Region
	require.NotNil(t, region)
	require.NotNil(t, region.Snippet)
	assert.Equal(t, "line4\nline5\nline6", region.Snippet.Text)
}

func TestSARIFFormatter_Snippet_FirstLine(t *testing.T) {
	dir := t.TempDir()
	content := "first\nsecond\nthird\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o600))

	f := NewSARIFFormatter()
	f.RepoPath = dir

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", FilePath: "test.go", Line: 1, Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	region := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
	require.NotNil(t, region.Snippet)
	assert.Equal(t, "first\nsecond", region.Snippet.Text, "line 1 should include lines 1-2 only")
}

func TestSARIFFormatter_Snippet_LastLine(t *testing.T) {
	dir := t.TempDir()
	content := "alpha\nbeta\ngamma\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o600))

	f := NewSARIFFormatter()
	f.RepoPath = dir

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", FilePath: "test.go", Line: 3, Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	region := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
	require.NotNil(t, region.Snippet)
	assert.Equal(t, "beta\ngamma", region.Snippet.Text, "last line should include lines N-1 and N")
}

func TestSARIFFormatter_Snippet_MissingFile(t *testing.T) {
	dir := t.TempDir()

	f := NewSARIFFormatter()
	f.RepoPath = dir

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", FilePath: "nonexistent.go", Line: 5, Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	region := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
	require.NotNil(t, region)
	assert.Nil(t, region.Snippet, "missing file should not produce snippet")
}

func TestSARIFFormatter_Snippet_NoSnippetsFlag(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte(content), 0o600))

	f := NewSARIFFormatter()
	f.RepoPath = dir
	f.NoSnippets = true

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", FilePath: "test.go", Line: 2, Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	region := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
	require.NotNil(t, region)
	assert.Nil(t, region.Snippet, "NoSnippets=true should suppress snippets")
}

func TestSARIFFormatter_Snippet_SameFileCached(t *testing.T) {
	dir := t.TempDir()
	content := "aaa\nbbb\nccc\nddd\neee\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.go"), []byte(content), 0o600))

	f := NewSARIFFormatter()
	f.RepoPath = dir

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "first", FilePath: "shared.go", Line: 2, Confidence: 0.5},
		{Kind: "todo", Title: "second", FilePath: "shared.go", Line: 4, Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	// Both signals should have correct snippets.
	r0 := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
	require.NotNil(t, r0.Snippet)
	assert.Equal(t, "aaa\nbbb\nccc", r0.Snippet.Text)

	r1 := doc.Runs[0].Results[1].Locations[0].PhysicalLocation.Region
	require.NotNil(t, r1.Snippet)
	assert.Equal(t, "ccc\nddd\neee", r1.Snippet.Text)
}

func TestSARIFFormatter_Snippet_EmptyRepoPath(t *testing.T) {
	f := NewSARIFFormatter()
	// RepoPath is empty — snippets should be skipped

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", FilePath: "test.go", Line: 1, Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	region := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.Region
	require.NotNil(t, region)
	assert.Nil(t, region.Snippet, "empty RepoPath should suppress snippets")
}

// --- SA5.1: Baseline suppression → SARIF suppression tests ---

func TestSARIFFormatter_BaselineSuppressions(t *testing.T) {
	// Create 5 signals, 3 of which are in the baseline.
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "TODO: fix A", Confidence: 0.5},
		{Source: "todos", Kind: "todo", FilePath: "b.go", Line: 2, Title: "TODO: fix B", Confidence: 0.6},
		{Source: "vuln", Kind: "vuln", FilePath: "go.mod", Line: 10, Title: "CVE-2024-001", Confidence: 0.9},
		{Source: "todos", Kind: "fixme", FilePath: "c.go", Line: 3, Title: "FIXME: broken", Confidence: 0.7},
		{Source: "gitlog", Kind: "churn", FilePath: "d.go", Line: 0, Title: "High churn", Confidence: 0.4},
	}

	// Build baseline with 3 suppressions matching signals 0, 2, 3.
	prefix := "str-"
	blState := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: SignalID(signals[0], prefix), Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
			{SignalID: SignalID(signals[2], prefix), Reason: baseline.ReasonWontFix, SuppressedAt: time.Now()},
			{SignalID: SignalID(signals[3], prefix), Reason: baseline.ReasonFalsePositive, SuppressedAt: time.Now()},
		},
	}

	f := NewSARIFFormatter()
	f.Baseline = blState
	f.BaselinePrefix = prefix

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	results := doc.Runs[0].Results
	require.Len(t, results, 5, "all 5 signals should appear in output")

	// Signal 0: acknowledged → accepted, no justification.
	require.Len(t, results[0].Suppressions, 1)
	assert.Equal(t, "external", results[0].Suppressions[0].Kind)
	assert.Equal(t, "accepted", results[0].Suppressions[0].Status)
	assert.Empty(t, results[0].Suppressions[0].Justification)

	// Signal 1: not suppressed → no suppressions.
	assert.Empty(t, results[1].Suppressions)

	// Signal 2: won't-fix → accepted + "Won't fix".
	require.Len(t, results[2].Suppressions, 1)
	assert.Equal(t, "external", results[2].Suppressions[0].Kind)
	assert.Equal(t, "accepted", results[2].Suppressions[0].Status)
	assert.Equal(t, "Won't fix", results[2].Suppressions[0].Justification)

	// Signal 3: false-positive → accepted + "False positive".
	require.Len(t, results[3].Suppressions, 1)
	assert.Equal(t, "external", results[3].Suppressions[0].Kind)
	assert.Equal(t, "accepted", results[3].Suppressions[0].Status)
	assert.Equal(t, "False positive", results[3].Suppressions[0].Justification)

	// Signal 4: not suppressed → no suppressions.
	assert.Empty(t, results[4].Suppressions)
}

func TestSARIFFormatter_BaselineSuppressions_NoBaseline(t *testing.T) {
	f := NewSARIFFormatter()
	// Baseline is nil → no suppressions emitted.

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	assert.Empty(t, doc.Runs[0].Results[0].Suppressions)
}

func TestSARIFFormatter_BaselineSuppressions_ExpiredSkipped(t *testing.T) {
	sig := signal.RawSignal{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "TODO", Confidence: 0.5}
	prefix := "str-"
	expired := time.Now().Add(-24 * time.Hour)

	blState := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: SignalID(sig, prefix), Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now(), ExpiresAt: &expired},
		},
	}

	f := NewSARIFFormatter()
	f.Baseline = blState
	f.BaselinePrefix = prefix

	var buf bytes.Buffer
	require.NoError(t, f.Format([]signal.RawSignal{sig}, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	assert.Empty(t, doc.Runs[0].Results[0].Suppressions, "expired suppressions should not emit SARIF suppression")
}

func TestSARIFFormatter_BaselineSuppressions_WithComment(t *testing.T) {
	sig := signal.RawSignal{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "TODO", Confidence: 0.5}
	prefix := "str-"

	blState := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: SignalID(sig, prefix), Reason: baseline.ReasonWontFix, Comment: "tracked elsewhere", SuppressedAt: time.Now()},
		},
	}

	f := NewSARIFFormatter()
	f.Baseline = blState
	f.BaselinePrefix = prefix

	var buf bytes.Buffer
	require.NoError(t, f.Format([]signal.RawSignal{sig}, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	require.Len(t, doc.Runs[0].Results[0].Suppressions, 1)
	assert.Equal(t, "Won't fix: tracked elsewhere", doc.Runs[0].Results[0].Suppressions[0].Justification)
}

func TestMapBaselineToSuppression(t *testing.T) {
	tests := []struct {
		name        string
		reason      baseline.Reason
		comment     string
		wantStatus  string
		wantJustify string
	}{
		{"acknowledged", baseline.ReasonAcknowledged, "", "accepted", ""},
		{"wont-fix", baseline.ReasonWontFix, "", "accepted", "Won't fix"},
		{"false-positive", baseline.ReasonFalsePositive, "", "accepted", "False positive"},
		{"acknowledged with comment", baseline.ReasonAcknowledged, "see ticket", "accepted", "see ticket"},
		{"wont-fix with comment", baseline.ReasonWontFix, "low priority", "accepted", "Won't fix: low priority"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sup := baseline.Suppression{
				Reason:  tt.reason,
				Comment: tt.comment,
			}
			result := mapBaselineToSuppression(sup)
			assert.Equal(t, "external", result.Kind)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantJustify, result.Justification)
		})
	}
}

// --- SA5.3: SARIF baseline comparison tests ---

func TestSARIFFormatter_BaselineComparison(t *testing.T) {
	// Create 5 signals for the "previous" baseline SARIF.
	prevSignals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "TODO: fix A", Confidence: 0.5},
		{Source: "todos", Kind: "todo", FilePath: "b.go", Line: 2, Title: "TODO: fix B", Confidence: 0.6},
		{Source: "vuln", Kind: "vuln", FilePath: "go.mod", Line: 10, Title: "CVE-2024-001", Confidence: 0.9},
		{Source: "todos", Kind: "fixme", FilePath: "c.go", Line: 3, Title: "FIXME: broken", Confidence: 0.7},
		{Source: "gitlog", Kind: "churn", FilePath: "d.go", Line: 5, Title: "High churn", Confidence: 0.4},
	}

	// Build previous SARIF document.
	prevFormatter := NewSARIFFormatter()
	var prevBuf bytes.Buffer
	require.NoError(t, prevFormatter.Format(prevSignals, &prevBuf))
	var prevDoc sarifDocument
	require.NoError(t, json.Unmarshal(prevBuf.Bytes(), &prevDoc))

	// Current scan has 5 signals: 3 unchanged (0,1,2) + 2 new (4,5).
	// Signal 3 and 4 from previous are absent.
	currentSignals := []signal.RawSignal{
		prevSignals[0], // unchanged
		prevSignals[1], // unchanged
		prevSignals[2], // unchanged
		{Source: "todos", Kind: "hack", FilePath: "e.go", Line: 1, Title: "HACK: workaround", Confidence: 0.3}, // new
		{Source: "vuln", Kind: "vuln", FilePath: "go.mod", Line: 20, Title: "CVE-2024-999", Confidence: 0.8},   // new
	}

	f := NewSARIFFormatter()
	f.SARIFBaseline = &prevDoc

	var buf bytes.Buffer
	require.NoError(t, f.Format(currentSignals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	results := doc.Runs[0].Results

	// 5 current + 2 absent = 7 total results.
	require.Len(t, results, 7, "should have 5 current + 2 absent results")

	// First 3 should be unchanged.
	assert.Equal(t, "unchanged", results[0].BaselineState)
	assert.Equal(t, "unchanged", results[1].BaselineState)
	assert.Equal(t, "unchanged", results[2].BaselineState)

	// Next 2 should be new.
	assert.Equal(t, "new", results[3].BaselineState)
	assert.Equal(t, "new", results[4].BaselineState)

	// Last 2 should be absent.
	assert.Equal(t, "absent", results[5].BaselineState)
	assert.Equal(t, "absent", results[6].BaselineState)

	// Verify absent results have correct fingerprints from previous.
	for _, r := range results[5:] {
		assert.NotEmpty(t, r.PartialFingerprints["stringer/v1"])
	}
}

func TestSARIFFormatter_NoBaselineComparison_NoBaselineState(t *testing.T) {
	f := NewSARIFFormatter()
	// SARIFBaseline is nil → no baselineState on results.

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "test", Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	assert.Empty(t, doc.Runs[0].Results[0].BaselineState, "no baselineState when SARIFBaseline is nil")
}

func TestSARIFFormatter_BaselineComparison_AllNew(t *testing.T) {
	// Empty previous doc.
	prevDoc := sarifDocument{
		Version: "2.1.0",
		Runs:    []sarifRun{{Results: []sarifResult{}}},
	}

	f := NewSARIFFormatter()
	f.SARIFBaseline = &prevDoc

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "new signal", Confidence: 0.5},
	}

	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	require.Len(t, doc.Runs[0].Results, 1)
	assert.Equal(t, "new", doc.Runs[0].Results[0].BaselineState)
}

func TestSARIFFormatter_BaselineComparison_AllAbsent(t *testing.T) {
	prevSignals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "old signal", Confidence: 0.5},
	}

	prevFormatter := NewSARIFFormatter()
	var prevBuf bytes.Buffer
	require.NoError(t, prevFormatter.Format(prevSignals, &prevBuf))
	var prevDoc sarifDocument
	require.NoError(t, json.Unmarshal(prevBuf.Bytes(), &prevDoc))

	f := NewSARIFFormatter()
	f.SARIFBaseline = &prevDoc

	// Empty current signals → the previous signal is absent.
	var buf bytes.Buffer
	require.NoError(t, f.Format([]signal.RawSignal{}, &buf))

	var doc sarifDocument
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	require.Len(t, doc.Runs[0].Results, 1)
	assert.Equal(t, "absent", doc.Runs[0].Results[0].BaselineState)
	assert.Equal(t, "old signal", doc.Runs[0].Results[0].Message.Text)
}

func TestParseSARIFBaseline(t *testing.T) {
	// Write a valid SARIF file and parse it.
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.sarif")

	f := NewSARIFFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "test", Confidence: 0.5},
	}
	var buf bytes.Buffer
	require.NoError(t, f.Format(signals, &buf))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	doc, err := ParseSARIFBaseline(path)
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "2.1.0", doc.Version)
	require.Len(t, doc.Runs, 1)
	require.Len(t, doc.Runs[0].Results, 1)
	assert.NotEmpty(t, doc.Runs[0].Results[0].PartialFingerprints["stringer/v1"])
}

func TestParseSARIFBaseline_NotFound(t *testing.T) {
	_, err := ParseSARIFBaseline("/nonexistent/path.sarif")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read sarif baseline")
}

func TestParseSARIFBaseline_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.sarif")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, err := ParseSARIFBaseline(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse sarif baseline")
}
