// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	RegisterFormatter(NewSARIFFormatter())
}

// SARIFFormatter writes signals as a SARIF v2.1.0 JSON document.
type SARIFFormatter struct {
	// Version is the stringer version to embed in the SARIF tool component.
	// If empty, "dev" is used.
	Version string
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
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
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
	Message             sarifMultiformatMessage    `json:"message"`
	Locations           []sarifLocation            `json:"locations,omitempty"`
	PartialFingerprints map[string]string          `json:"partialFingerprints,omitempty"`
	Properties          map[string]json.RawMessage `json:"properties,omitempty"`
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
	StartLine int `json:"startLine"`
}

func (f *SARIFFormatter) buildDocument(signals []signal.RawSignal) sarifDocument {
	rules, ruleIndex := f.buildRules(signals)
	results := f.buildResults(signals, ruleIndex)

	version := f.Version
	if version == "" {
		version = "dev"
	}

	return sarifDocument{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "stringer",
						Version:        version,
						InformationURI: "https://github.com/davetashner/stringer",
						Rules:          rules,
					},
				},
				Results: results,
			},
		},
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
				loc.PhysicalLocation.Region = &sarifRegion{
					StartLine: sig.Line,
				}
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

		results = append(results, result)
	}

	return results
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

func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return data
}
