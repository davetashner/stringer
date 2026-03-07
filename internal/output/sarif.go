// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/davetashner/stringer/internal/baseline"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/google/uuid"
)

func init() {
	RegisterFormatter(NewSARIFFormatter())
}

// SARIFFormatter writes signals as a SARIF v2.1.0 JSON document.
type SARIFFormatter struct {
	// Version is the stringer version to embed in the SARIF tool component.
	// If empty, "dev" is used.
	Version string

	// RepoPath is the repository root path. Used for resolving file paths
	// for code snippets and for automationDetails GUID generation.
	RepoPath string

	// GitHead is the short git HEAD commit hash (first 7 chars).
	// Used for automationDetails run correlation.
	GitHead string

	// NoSnippets disables code snippet embedding in results.
	NoSnippets bool

	// Baseline is the loaded baseline state. When set, results whose signal
	// IDs match a suppression entry receive SARIF suppressions (§3.27.22).
	// The prefix used for signal ID matching (e.g. "str-").
	Baseline       *baseline.BaselineState
	BaselinePrefix string

	// SARIFBaseline is a previously-emitted SARIF document used for
	// baseline comparison. When set, results receive baselineState
	// values (new, unchanged, absent) per SARIF §3.27.24.
	SARIFBaseline *sarifDocument
}

// Compile-time interface check.
var _ Formatter = (*SARIFFormatter)(nil)

// NewSARIFFormatter returns a new SARIFFormatter with default settings.
func NewSARIFFormatter() *SARIFFormatter {
	return &SARIFFormatter{}
}

// Name returns the format name.
func (f *SARIFFormatter) Name() string { return "sarif" }

// Format writes all signals as a SARIF v2.1.0 document to w.
func (f *SARIFFormatter) Format(signals []signal.RawSignal, w io.Writer) error {
	if signals == nil {
		signals = []signal.RawSignal{}
	}

	doc := f.buildDocument(signals)

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sarif: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	_, err = w.Write([]byte("\n"))
	if err != nil {
		return fmt.Errorf("write sarif trailing newline: %w", err)
	}
	return nil
}

// SARIF document types — only exported for JSON marshaling.

type sarifDocument struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool              sarifTool           `json:"tool"`
	AutomationDetails *sarifRunAutomation `json:"automationDetails,omitempty"`
	Results           []sarifResult       `json:"results"`
}

type sarifRunAutomation struct {
	ID   string `json:"id,omitempty"`
	GUID string `json:"guid,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string                     `json:"id"`
	ShortDescription sarifMultiformatMessage    `json:"shortDescription"`
	DefaultConfig    *sarifReportingConfig      `json:"defaultConfiguration,omitempty"`
	Properties       map[string]json.RawMessage `json:"properties,omitempty"`
}

type sarifMultiformatMessage struct {
	Text string `json:"text"`
}

type sarifReportingConfig struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID              string                     `json:"ruleId"`
	RuleIndex           int                        `json:"ruleIndex"`
	Level               string                     `json:"level"`
	Rank                float64                    `json:"rank"`
	BaselineState       string                     `json:"baselineState,omitempty"`
	Message             sarifMultiformatMessage    `json:"message"`
	Locations           []sarifLocation            `json:"locations,omitempty"`
	Suppressions        []sarifSuppression         `json:"suppressions,omitempty"`
	PartialFingerprints map[string]string          `json:"partialFingerprints,omitempty"`
	Properties          map[string]json.RawMessage `json:"properties,omitempty"`
}

// sarifSuppression represents a SARIF suppression object (§3.27.22).
type sarifSuppression struct {
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Justification string `json:"justification,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

type sarifRegion struct {
	StartLine int                   `json:"startLine"`
	Snippet   *sarifArtifactContent `json:"snippet,omitempty"`
}

type sarifArtifactContent struct {
	Text string `json:"text"`
}

func (f *SARIFFormatter) buildDocument(signals []signal.RawSignal) sarifDocument {
	rules, ruleIndex := f.buildRules(signals)
	results := f.buildResults(signals, ruleIndex)

	version := f.Version
	if version == "" {
		version = "dev"
	}

	run := sarifRun{
		Tool: sarifTool{
			Driver: sarifDriver{
				Name:           "stringer",
				Version:        version,
				InformationURI: "https://github.com/davetashner/stringer",
				Rules:          rules,
			},
		},
		AutomationDetails: f.buildAutomationDetails(),
		Results:           results,
	}

	return sarifDocument{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs:    []sarifRun{run},
	}
}

// buildAutomationDetails creates SARIF automationDetails for run correlation.
// Returns nil if GitHead is empty (backward compatible).
func (f *SARIFFormatter) buildAutomationDetails() *sarifRunAutomation {
	if f.GitHead == "" {
		return nil
	}

	head := f.GitHead
	if len(head) > 7 {
		head = head[:7]
	}

	id := "stringer/" + head

	// Deterministic UUID v5 from repo path + git head.
	seed := f.RepoPath + f.GitHead
	guid := uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed)).String()

	return &sarifRunAutomation{
		ID:   id,
		GUID: guid,
	}
}

// buildRules collects unique signal kinds into SARIF rule objects.
// Returns the rules and a map from kind to rule index.
func (f *SARIFFormatter) buildRules(signals []signal.RawSignal) ([]sarifRule, map[string]int) {
	ruleIndex := make(map[string]int)
	var rules []sarifRule

	// Collect unique kinds in stable order.
	var kinds []string
	for _, sig := range signals {
		if _, exists := ruleIndex[sig.Kind]; !exists {
			ruleIndex[sig.Kind] = -1 // placeholder
			kinds = append(kinds, sig.Kind)
		}
	}
	slices.Sort(kinds)

	// Build rules in sorted order and update index.
	for i, kind := range kinds {
		ruleIndex[kind] = i
		rules = append(rules, sarifRule{
			ID: kind,
			ShortDescription: sarifMultiformatMessage{
				Text: ruleDescription(kind),
			},
			DefaultConfig: &sarifReportingConfig{
				Level: "warning",
			},
			Properties: ruleProperties(kind),
		})
	}

	return rules, ruleIndex
}

func (f *SARIFFormatter) buildResults(signals []signal.RawSignal, ruleIndex map[string]int) []sarifResult {
	results := make([]sarifResult, 0, len(signals))

	// Build baseline lookup for suppression annotation.
	blLookup := baseline.Lookup(f.Baseline)

	// Build previous fingerprint set for baseline comparison.
	var prevFingerprints map[string]bool
	if f.SARIFBaseline != nil {
		prevFingerprints = extractFingerprints(f.SARIFBaseline)
	}

	// File line cache for snippet extraction (path → lines).
	fileCache := make(map[string][]string)

	for _, sig := range signals {
		priority := mapConfidenceToPriority(sig.Confidence)
		if sig.Priority != nil {
			priority = *sig.Priority
		}

		result := sarifResult{
			RuleID:    sig.Kind,
			RuleIndex: ruleIndex[sig.Kind],
			Level:     priorityToSARIFLevel(priority),
			Rank:      sig.Confidence * 100.0,
			Message: sarifMultiformatMessage{
				Text: sig.Title,
			},
			PartialFingerprints: map[string]string{
				"stringer/v1": SignalID(sig, ""),
			},
		}

		if sig.FilePath != "" {
			loc := sarifLocation{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{
						URI:       sig.FilePath,
						URIBaseID: "%SRCROOT%",
					},
				},
			}
			if sig.Line > 0 {
				region := &sarifRegion{
					StartLine: sig.Line,
				}
				if !f.NoSnippets && f.RepoPath != "" {
					if snippet := f.extractSnippet(sig.FilePath, sig.Line, fileCache); snippet != "" {
						region.Snippet = &sarifArtifactContent{Text: snippet}
					}
				}
				loc.PhysicalLocation.Region = region
			}
			result.Locations = []sarifLocation{loc}
		}

		props := make(map[string]json.RawMessage)
		if sig.Source != "" {
			props["collector"] = mustMarshal(sig.Source)
		}
		if sig.Author != "" {
			props["author"] = mustMarshal(sig.Author)
		}
		if len(sig.Tags) > 0 {
			props["tags"] = mustMarshal(sig.Tags)
		}
		if len(props) > 0 {
			result.Properties = props
		}

		// SA5.1: Map baseline suppressions to SARIF suppressions.
		sigID := SignalID(sig, f.BaselinePrefix)
		if sup, found := blLookup[sigID]; found && !baseline.IsExpired(sup) {
			result.Suppressions = []sarifSuppression{
				mapBaselineToSuppression(sup),
			}
		}

		// SA5.3: Baseline comparison — assign baselineState.
		if prevFingerprints != nil {
			fp := result.PartialFingerprints["stringer/v1"]
			if prevFingerprints[fp] {
				result.BaselineState = "unchanged"
			} else {
				result.BaselineState = "new"
			}
		}

		results = append(results, result)
	}

	// SA5.3: Emit absent results for fingerprints in previous but not current.
	if prevFingerprints != nil {
		currentFPs := make(map[string]bool, len(results))
		for _, r := range results {
			currentFPs[r.PartialFingerprints["stringer/v1"]] = true
		}
		absentResults := f.buildAbsentResults(prevFingerprints, currentFPs)
		results = append(results, absentResults...)
	}

	return results
}

// mapBaselineToSuppression converts a baseline suppression reason to a SARIF
// suppression object. Kind is always "external" (from baseline, not in-source).
func mapBaselineToSuppression(sup baseline.Suppression) sarifSuppression {
	s := sarifSuppression{
		Kind:   "external",
		Status: "accepted",
	}
	switch sup.Reason {
	case baseline.ReasonAcknowledged:
		// No justification needed, status is "accepted".
	case baseline.ReasonWontFix:
		s.Justification = "Won't fix"
	case baseline.ReasonFalsePositive:
		s.Justification = "False positive"
	default:
		// For any unrecognized reason, map to underReview.
		s.Status = "underReview"
	}

	// Append the suppression note if present.
	if sup.Comment != "" {
		if s.Justification != "" {
			s.Justification += ": " + sup.Comment
		} else {
			s.Justification = sup.Comment
		}
	}

	return s
}

// extractFingerprints collects all stringer/v1 partialFingerprints from a
// parsed SARIF document.
func extractFingerprints(doc *sarifDocument) map[string]bool {
	fps := make(map[string]bool)
	for _, run := range doc.Runs {
		for _, result := range run.Results {
			if fp, ok := result.PartialFingerprints["stringer/v1"]; ok && fp != "" {
				fps[fp] = true
			}
		}
	}
	return fps
}

// buildAbsentResults creates placeholder results for fingerprints that existed
// in the previous SARIF baseline but are absent from the current scan.
func (f *SARIFFormatter) buildAbsentResults(prevFPs map[string]bool, currentFPs map[string]bool) []sarifResult {
	if f.SARIFBaseline == nil {
		return nil
	}

	var absent []sarifResult
	for _, run := range f.SARIFBaseline.Runs {
		for _, prev := range run.Results {
			fp := prev.PartialFingerprints["stringer/v1"]
			if fp == "" || currentFPs[fp] {
				continue
			}
			// Clone the previous result and mark as absent.
			r := sarifResult{
				RuleID:        prev.RuleID,
				RuleIndex:     prev.RuleIndex,
				Level:         prev.Level,
				Rank:          prev.Rank,
				BaselineState: "absent",
				Message:       prev.Message,
				Locations:     prev.Locations,
				PartialFingerprints: map[string]string{
					"stringer/v1": fp,
				},
				Properties: prev.Properties,
			}
			absent = append(absent, r)
		}
	}
	return absent
}

// extractSnippet reads up to 3 lines of context around the target line.
// Returns the joined lines or empty string if the file cannot be read.
// Uses fileCache to avoid re-reading the same file.
func (f *SARIFFormatter) extractSnippet(filePath string, line int, cache map[string][]string) string {
	fullPath := filepath.Join(f.RepoPath, filePath)

	lines, ok := cache[fullPath]
	if !ok {
		var err error
		lines, err = readFileLines(fullPath)
		if err != nil {
			// Mark as attempted so we don't retry.
			cache[fullPath] = nil
			return ""
		}
		cache[fullPath] = lines
	}
	if lines == nil {
		return ""
	}

	// line is 1-indexed; convert to 0-indexed.
	idx := line - 1
	if idx < 0 || idx >= len(lines) {
		return ""
	}

	start := idx - 1
	if start < 0 {
		start = 0
	}
	end := idx + 2 // exclusive, so line after target
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

// readFileLines reads a file and returns its lines.
func readFileLines(path string) ([]string, error) {
	cleanPath := filepath.Clean(path)
	file, err := os.Open(cleanPath) //nolint:gosec // path is constructed from RepoPath + signal FilePath
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// priorityToSARIFLevel maps P1-P4 to SARIF level values.
func priorityToSARIFLevel(priority int) string {
	switch priority {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "note"
	default:
		return "none"
	}
}

// ruleDescription returns a human-readable description for a signal kind.
func ruleDescription(kind string) string {
	descriptions := map[string]string{
		"todo":                  "Unresolved TODO comment in source code",
		"fixme":                 "FIXME comment indicating a known issue",
		"hack":                  "HACK comment indicating a workaround",
		"xxx":                   "XXX comment flagging problematic code",
		"optimize":              "OPTIMIZE comment suggesting performance improvement",
		"bug":                   "BUG comment marking a known defect",
		"revert":                "Git revert commit detected",
		"churn":                 "High file churn detected in recent history",
		"stale-branch":          "Stale branch with no recent activity",
		"large-file":            "Source file exceeds size threshold",
		"missing-tests":         "Source file has no corresponding test file",
		"low-test-ratio":        "Directory has low test-to-source file ratio",
		"low-lottery-risk":      "File has concentrated code ownership",
		"review-concentration":  "Code reviews concentrated among few reviewers",
		"vuln":                  "Known vulnerability in dependency",
		"complexity":            "High cyclomatic complexity detected",
		"deadcode":              "Potentially unused code detected",
		"merge-conflict-marker": "Unresolved merge conflict marker in file",
		"committed-secret":      "Potential secret committed to repository",
		"large-binary":          "Large binary file committed to repository",
		"mixed-line-endings":    "File has inconsistent line endings",
		"stale-doc":             "Documentation may be outdated",
		"undocumented-route":    "API route without documentation",
		"unimplemented-route":   "Documented API route without implementation",
		"stale-api-version":     "API version with no recent changes",
		"env-var-drift":         "Environment variable referenced but not documented",
		"dead-config-key":       "Configuration key defined but not referenced",
		"inconsistent-defaults": "Configuration defaults differ across locations",
		"deprecated-dependency": "Dependency is deprecated by its maintainer",
		"archived-dependency":   "Dependency repository is archived",
		"stale-dependency":      "Dependency has not been updated recently",
		"yanked-dependency":     "Dependency version has been yanked",
		"local-replace":         "Go module uses a local replace directive",
		"retracted-version":     "Go module uses a retracted version",
	}
	if desc, ok := descriptions[kind]; ok {
		return desc
	}
	return fmt.Sprintf("Signal of kind %q detected", kind)
}

// ruleProperties returns SARIF properties for a rule, mapping kinds to collectors.
func ruleProperties(kind string) map[string]json.RawMessage {
	collector := kindToCollector(kind)
	if collector == "" {
		return nil
	}
	return map[string]json.RawMessage{
		"collector": mustMarshal(collector),
	}
}

func kindToCollector(kind string) string {
	collectorMap := map[string]string{
		"todo": "todos", "fixme": "todos", "hack": "todos",
		"xxx": "todos", "optimize": "todos", "bug": "todos",
		"revert": "gitlog", "churn": "gitlog", "stale-branch": "gitlog",
		"large-file": "patterns", "missing-tests": "patterns", "low-test-ratio": "patterns",
		"low-lottery-risk": "lotteryrisk", "review-concentration": "lotteryrisk",
		"vuln":                  "vuln",
		"complexity":            "complexity",
		"deadcode":              "deadcode",
		"merge-conflict-marker": "githygiene", "committed-secret": "githygiene",
		"large-binary": "githygiene", "mixed-line-endings": "githygiene",
		"stale-doc":          "docstale",
		"undocumented-route": "apidrift", "unimplemented-route": "apidrift",
		"stale-api-version": "apidrift",
		"env-var-drift":     "configdrift", "dead-config-key": "configdrift",
		"inconsistent-defaults": "configdrift",
		"deprecated-dependency": "dephealth", "archived-dependency": "dephealth",
		"stale-dependency": "dephealth", "yanked-dependency": "dephealth",
		"local-replace": "dephealth", "retracted-version": "dephealth",
	}
	return collectorMap[kind]
}

// ParseSARIFBaseline reads a SARIF file and returns its parsed document.
// This is used by --sarif-baseline to load the previous scan's output.
func ParseSARIFBaseline(path string) (*sarifDocument, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read sarif baseline: %w", err)
	}
	var doc sarifDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse sarif baseline: %w", err)
	}
	return &doc, nil
}

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return data
}
