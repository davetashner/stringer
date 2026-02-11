// Package validate provides JSONL validation against the bd import schema.
// It checks each line of a JSONL file for required fields, valid types,
// and correct formats, producing detailed error messages with fix suggestions.
package validate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ValidTypes are the allowed values for the "type" field in a bead record.
var ValidTypes = []string{"bug", "task", "chore", "epic", "feature"}

// ValidStatuses are the allowed values for the "status" field in a bead record.
var ValidStatuses = []string{"open", "closed"}

// ValidationError represents a single validation issue on a specific line.
type ValidationError struct {
	Line       int    // 1-based line number
	Field      string // field name (empty if line-level error)
	Message    string // what's wrong
	Suggestion string // how to fix it
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("line %d: %s", e.Line, e.Message)
	}
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

// Result contains the outcome of validating a JSONL file.
type Result struct {
	TotalLines int
	Errors     []ValidationError
}

// Valid returns true if no errors were found.
func (r *Result) Valid() bool {
	return len(r.Errors) == 0
}

// Validate reads JSONL from r and validates each line against the bd import schema.
// It returns a Result with all validation errors found.
func Validate(r io.Reader) *Result {
	result := &Result{}
	scanner := bufio.NewScanner(r)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines.
		if line == "" {
			continue
		}

		result.TotalLines++
		validateLine(line, lineNum, result)
	}

	return result
}

// validateLine parses a single JSON line and checks all fields.
func validateLine(line string, lineNum int, result *Result) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Message:    fmt.Sprintf("invalid JSON: %v", err),
			Suggestion: "ensure each line is a valid JSON object",
		})
		return
	}

	// Check required fields.
	checkRequiredString(raw, "id", lineNum, result)
	checkRequiredString(raw, "title", lineNum, result)
	checkType(raw, lineNum, result)
	checkPriority(raw, lineNum, result)
	checkStatus(raw, lineNum, result)
	checkRequiredString(raw, "created_by", lineNum, result)

	// Check optional fields if present.
	checkOptionalTimestamp(raw, "created_at", lineNum, result)
	checkOptionalTimestamp(raw, "closed_at", lineNum, result)
	checkOptionalLabels(raw, lineNum, result)
	checkOptionalString(raw, "description", lineNum, result)
	checkOptionalString(raw, "close_reason", lineNum, result)
}

// checkRequiredString validates that a required string field is present and non-empty.
func checkRequiredString(raw map[string]json.RawMessage, field string, lineNum int, result *Result) {
	val, ok := raw[field]
	if !ok {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      field,
			Message:    fmt.Sprintf("missing required field %q", field),
			Suggestion: fmt.Sprintf("add a %q field with a descriptive string", field),
		})
		return
	}

	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      field,
			Message:    fmt.Sprintf("field %q must be a string", field),
			Suggestion: fmt.Sprintf("set %q to a JSON string value", field),
		})
		return
	}

	if s == "" {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      field,
			Message:    fmt.Sprintf("field %q must not be empty", field),
			Suggestion: fmt.Sprintf("provide a non-empty value for %q", field),
		})
	}
}

// checkType validates the "type" field.
func checkType(raw map[string]json.RawMessage, lineNum int, result *Result) {
	val, ok := raw["type"]
	if !ok {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "type",
			Message:    "missing required field \"type\"",
			Suggestion: fmt.Sprintf("add a \"type\" field with one of: %s", strings.Join(ValidTypes, ", ")),
		})
		return
	}

	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "type",
			Message:    "field \"type\" must be a string",
			Suggestion: fmt.Sprintf("set \"type\" to one of: %s", strings.Join(ValidTypes, ", ")),
		})
		return
	}

	for _, valid := range ValidTypes {
		if s == valid {
			return
		}
	}

	suggestion := fmt.Sprintf("type must be one of: %s", strings.Join(ValidTypes, ", "))
	if hint := suggestType(s); hint != "" {
		suggestion = fmt.Sprintf("did you mean %q?", hint)
	}

	result.Errors = append(result.Errors, ValidationError{
		Line:       lineNum,
		Field:      "type",
		Message:    fmt.Sprintf("invalid type %q", s),
		Suggestion: suggestion,
	})
}

// checkPriority validates the "priority" field.
func checkPriority(raw map[string]json.RawMessage, lineNum int, result *Result) {
	val, ok := raw["priority"]
	if !ok {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "priority",
			Message:    "missing required field \"priority\"",
			Suggestion: "add a \"priority\" field with an integer 0-4",
		})
		return
	}

	var f float64
	if err := json.Unmarshal(val, &f); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "priority",
			Message:    "field \"priority\" must be an integer",
			Suggestion: "set \"priority\" to an integer 0-4",
		})
		return
	}

	p := int(f)
	if float64(p) != f {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "priority",
			Message:    fmt.Sprintf("priority must be an integer, got %v", f),
			Suggestion: "priority must be 0-4",
		})
		return
	}

	if p < 0 || p > 4 {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "priority",
			Message:    fmt.Sprintf("invalid priority %d", p),
			Suggestion: "priority must be 0-4",
		})
	}
}

// checkStatus validates the "status" field.
func checkStatus(raw map[string]json.RawMessage, lineNum int, result *Result) {
	val, ok := raw["status"]
	if !ok {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "status",
			Message:    "missing required field \"status\"",
			Suggestion: fmt.Sprintf("add a \"status\" field with one of: %s", strings.Join(ValidStatuses, ", ")),
		})
		return
	}

	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "status",
			Message:    "field \"status\" must be a string",
			Suggestion: fmt.Sprintf("set \"status\" to one of: %s", strings.Join(ValidStatuses, ", ")),
		})
		return
	}

	for _, valid := range ValidStatuses {
		if s == valid {
			return
		}
	}

	suggestion := fmt.Sprintf("status must be one of: %s", strings.Join(ValidStatuses, ", "))
	if hint := suggestStatus(s); hint != "" {
		suggestion = fmt.Sprintf("did you mean %q?", hint)
	}

	result.Errors = append(result.Errors, ValidationError{
		Line:       lineNum,
		Field:      "status",
		Message:    fmt.Sprintf("invalid status %q", s),
		Suggestion: suggestion,
	})
}

// checkOptionalTimestamp validates an optional ISO 8601 timestamp field.
func checkOptionalTimestamp(raw map[string]json.RawMessage, field string, lineNum int, result *Result) {
	val, ok := raw[field]
	if !ok {
		return
	}

	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      field,
			Message:    fmt.Sprintf("field %q must be a string", field),
			Suggestion: fmt.Sprintf("set %q to an ISO 8601 timestamp (e.g., \"2024-01-15T10:30:00Z\")", field),
		})
		return
	}

	// Empty string is allowed for optional timestamps.
	if s == "" {
		return
	}

	if !isValidISO8601(s) {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      field,
			Message:    fmt.Sprintf("field %q is not a valid ISO 8601 timestamp: %q", field, s),
			Suggestion: fmt.Sprintf("use ISO 8601 format for %q (e.g., \"2024-01-15T10:30:00Z\")", field),
		})
	}
}

// checkOptionalLabels validates the optional "labels" field.
func checkOptionalLabels(raw map[string]json.RawMessage, lineNum int, result *Result) {
	val, ok := raw["labels"]
	if !ok {
		return
	}

	var labels []json.RawMessage
	if err := json.Unmarshal(val, &labels); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      "labels",
			Message:    "field \"labels\" must be an array of strings",
			Suggestion: "set \"labels\" to a JSON array of strings (e.g., [\"bug\", \"high-priority\"])",
		})
		return
	}

	for i, lbl := range labels {
		var s string
		if err := json.Unmarshal(lbl, &s); err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Line:       lineNum,
				Field:      "labels",
				Message:    fmt.Sprintf("labels[%d] must be a string", i),
				Suggestion: "all labels must be strings",
			})
		}
	}
}

// checkOptionalString validates an optional string field if present.
func checkOptionalString(raw map[string]json.RawMessage, field string, lineNum int, result *Result) {
	val, ok := raw[field]
	if !ok {
		return
	}

	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Line:       lineNum,
			Field:      field,
			Message:    fmt.Sprintf("field %q must be a string", field),
			Suggestion: fmt.Sprintf("set %q to a JSON string value", field),
		})
	}
	_ = s // value is not used; just checking type
}

// isValidISO8601 checks if a string is a valid ISO 8601 timestamp.
// It accepts several common ISO 8601 formats.
func isValidISO8601(s string) bool {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if _, err := time.Parse(f, s); err == nil {
			return true
		}
	}
	return false
}

// suggestType uses Levenshtein distance to suggest a valid type for a misspelled one.
func suggestType(input string) string {
	return closestMatch(strings.ToLower(input), ValidTypes, 3)
}

// suggestStatus uses Levenshtein distance to suggest a valid status for a misspelled one.
func suggestStatus(input string) string {
	return closestMatch(strings.ToLower(input), ValidStatuses, 3)
}

// closestMatch finds the closest string in candidates to input using
// Levenshtein distance. Returns empty string if no match is within maxDist.
func closestMatch(input string, candidates []string, maxDist int) string {
	best := ""
	bestDist := maxDist + 1

	for _, c := range candidates {
		d := levenshtein(input, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}

	if bestDist <= maxDist {
		return best
	}
	return ""
}

// levenshtein computes the Levenshtein edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Use a single-row DP approach.
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}
