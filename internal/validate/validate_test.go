// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validLine is a minimal valid JSONL line for testing.
const validLine = `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer"}`

func TestValidate_ValidLine(t *testing.T) {
	r := strings.NewReader(validLine + "\n")
	result := Validate(r)
	assert.True(t, result.Valid())
	assert.Equal(t, 1, result.TotalLines)
	assert.Empty(t, result.Errors)
}

func TestValidate_MultipleValidLines(t *testing.T) {
	input := validLine + "\n" +
		`{"id":"str-87654321","title":"Add feature","type":"feature","priority":1,"status":"open","created_by":"alice"}` + "\n" +
		`{"id":"str-abcdef12","title":"Clean up","type":"chore","priority":3,"status":"closed","created_by":"bob","closed_at":"2024-01-15T10:30:00Z"}` + "\n"
	r := strings.NewReader(input)
	result := Validate(r)
	assert.True(t, result.Valid())
	assert.Equal(t, 3, result.TotalLines)
}

func TestValidate_EmptyInput(t *testing.T) {
	r := strings.NewReader("")
	result := Validate(r)
	assert.True(t, result.Valid())
	assert.Equal(t, 0, result.TotalLines)
}

func TestValidate_EmptyLines(t *testing.T) {
	input := "\n\n" + validLine + "\n\n"
	r := strings.NewReader(input)
	result := Validate(r)
	assert.True(t, result.Valid())
	assert.Equal(t, 1, result.TotalLines)
}

func TestValidate_InvalidJSON(t *testing.T) {
	r := strings.NewReader("not json\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, 1, result.Errors[0].Line)
	assert.Contains(t, result.Errors[0].Message, "invalid JSON")
	assert.Contains(t, result.Errors[0].Suggestion, "valid JSON")
}

func TestValidate_MissingID(t *testing.T) {
	line := `{"title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "id", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "missing required field")
}

func TestValidate_MissingTitle(t *testing.T) {
	line := `{"id":"str-12345678","type":"task","priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "title", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "missing required field")
}

func TestValidate_MissingType(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "type", result.Errors[0].Field)
}

func TestValidate_MissingPriority(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "priority", result.Errors[0].Field)
}

func TestValidate_MissingStatus(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "status", result.Errors[0].Field)
}

func TestValidate_MissingCreatedBy(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "created_by", result.Errors[0].Field)
}

func TestValidate_MultipleRequiredFieldsMissing(t *testing.T) {
	line := `{"id":"str-12345678"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	assert.GreaterOrEqual(t, len(result.Errors), 4) // title, type, priority, status, created_by
}

func TestValidate_InvalidType(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"enhancement","priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "type", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "invalid type")
}

func TestValidate_InvalidType_DidYouMean(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bugg", "bug"},
		{"taks", "task"},
		{"chor", "chore"},
		{"epics", "epic"},
		{"featur", "feature"},
		{"enhancement", ""}, // too far from any valid type
		{"xyz_unknown", ""}, // no reasonable suggestion
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			line := `{"id":"str-12345678","title":"Fix bug","type":"` + tt.input + `","priority":2,"status":"open","created_by":"stringer"}`
			r := strings.NewReader(line + "\n")
			result := Validate(r)
			require.Len(t, result.Errors, 1)
			if tt.want != "" {
				assert.Contains(t, result.Errors[0].Suggestion, "did you mean")
				assert.Contains(t, result.Errors[0].Suggestion, tt.want)
			} else {
				assert.Contains(t, result.Errors[0].Suggestion, "type must be one of")
			}
		})
	}
}

func TestValidate_InvalidStatus(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"done","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "status", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "invalid status")
}

func TestValidate_InvalidStatus_DidYouMean(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"opn", "open"},
		{"close", "closed"},
		{"closd", "closed"},
		{"xyz_unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"` + tt.input + `","created_by":"stringer"}`
			r := strings.NewReader(line + "\n")
			result := Validate(r)
			require.Len(t, result.Errors, 1)
			if tt.want != "" {
				assert.Contains(t, result.Errors[0].Suggestion, "did you mean")
			} else {
				assert.Contains(t, result.Errors[0].Suggestion, "status must be one of")
			}
		})
	}
}

func TestValidate_PriorityOutOfRange(t *testing.T) {
	tests := []struct {
		name     string
		priority string
	}{
		{"too high", "7"},
		{"negative", "-1"},
		{"way too high", "100"},
		{"five", "5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":` + tt.priority + `,"status":"open","created_by":"stringer"}`
			r := strings.NewReader(line + "\n")
			result := Validate(r)
			assert.False(t, result.Valid())
			require.Len(t, result.Errors, 1)
			assert.Equal(t, "priority", result.Errors[0].Field)
			assert.Contains(t, result.Errors[0].Suggestion, "0-4")
		})
	}
}

func TestValidate_PriorityNotInteger(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":"high","status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "priority", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must be an integer")
}

func TestValidate_PriorityFloat(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2.5,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "priority", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must be an integer")
}

func TestValidate_PriorityZero(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":0,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.True(t, result.Valid())
}

func TestValidate_PriorityFour(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":4,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.True(t, result.Valid())
}

func TestValidate_InvalidCreatedAt(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","created_at":"not-a-date"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "created_at", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "not a valid ISO 8601")
}

func TestValidate_InvalidClosedAt(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"closed","created_by":"stringer","closed_at":"yesterday"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "closed_at", result.Errors[0].Field)
}

func TestValidate_ValidTimestampFormats(t *testing.T) {
	timestamps := []string{
		"2024-01-15T10:30:00Z",
		"2024-01-15T10:30:00+05:00",
		"2024-01-15T10:30:00-07:00",
		"2024-01-15T10:30:00.123456Z",
		"2024-01-15",
	}

	for _, ts := range timestamps {
		t.Run(ts, func(t *testing.T) {
			line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","created_at":"` + ts + `"}`
			r := strings.NewReader(line + "\n")
			result := Validate(r)
			assert.True(t, result.Valid(), "timestamp %q should be valid, got errors: %v", ts, result.Errors)
		})
	}
}

func TestValidate_EmptyTimestampIsValid(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","created_at":""}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.True(t, result.Valid())
}

func TestValidate_InvalidLabels_NotArray(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","labels":"bug"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "labels", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "array of strings")
}

func TestValidate_InvalidLabels_NonStringElement(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","labels":["bug",123]}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "labels", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "labels[1] must be a string")
}

func TestValidate_ValidLabels(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","labels":["bug","high-priority","stringer-generated"]}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.True(t, result.Valid())
}

func TestValidate_AllOptionalFields(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"closed","created_by":"stringer","created_at":"2024-01-15T10:30:00Z","closed_at":"2024-02-01T12:00:00Z","labels":["bug"],"description":"A detailed description","close_reason":"resolved"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.True(t, result.Valid())
}

func TestValidate_AllTypes(t *testing.T) {
	for _, typ := range ValidTypes {
		t.Run(typ, func(t *testing.T) {
			line := `{"id":"str-12345678","title":"Test","type":"` + typ + `","priority":2,"status":"open","created_by":"stringer"}`
			r := strings.NewReader(line + "\n")
			result := Validate(r)
			assert.True(t, result.Valid(), "type %q should be valid", typ)
		})
	}
}

func TestValidate_AllStatuses(t *testing.T) {
	for _, status := range ValidStatuses {
		t.Run(status, func(t *testing.T) {
			line := `{"id":"str-12345678","title":"Test","type":"task","priority":2,"status":"` + status + `","created_by":"stringer"}`
			r := strings.NewReader(line + "\n")
			result := Validate(r)
			assert.True(t, result.Valid(), "status %q should be valid", status)
		})
	}
}

func TestValidate_EmptyID(t *testing.T) {
	line := `{"id":"","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "id", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must not be empty")
}

func TestValidate_EmptyTitle(t *testing.T) {
	line := `{"id":"str-12345678","title":"","type":"task","priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "title", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must not be empty")
}

func TestValidate_NonStringID(t *testing.T) {
	line := `{"id":12345,"title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "id", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must be a string")
}

func TestValidate_NonStringType(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":1,"priority":2,"status":"open","created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "type", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must be a string")
}

func TestValidate_NonStringStatus(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":true,"created_by":"stringer"}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "status", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must be a string")
}

func TestValidate_MixedValidAndInvalidLines(t *testing.T) {
	input := validLine + "\n" +
		`{"id":"str-bad","type":"wrong","priority":10}` + "\n" +
		validLine + "\n"
	r := strings.NewReader(input)
	result := Validate(r)
	assert.False(t, result.Valid())
	assert.Equal(t, 3, result.TotalLines)
	// Line 2 should have multiple errors (missing title, invalid type, invalid priority, missing status, missing created_by).
	lineErrors := 0
	for _, e := range result.Errors {
		if e.Line == 2 {
			lineErrors++
		}
	}
	assert.GreaterOrEqual(t, lineErrors, 3, "line 2 should have multiple errors")
}

func TestValidate_DescriptionNotString(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","description":42}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "description", result.Errors[0].Field)
}

func TestValidate_CloseReasonNotString(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","close_reason":true}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "close_reason", result.Errors[0].Field)
}

func TestValidate_CreatedAtNotString(t *testing.T) {
	line := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","created_at":12345}`
	r := strings.NewReader(line + "\n")
	result := Validate(r)
	assert.False(t, result.Valid())
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "created_at", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "must be a string")
}

// -----------------------------------------------------------------------
// Levenshtein distance tests
// -----------------------------------------------------------------------

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "b", 1},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"bug", "bugg", 1},
		{"task", "taks", 2},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.want, levenshtein(tt.a, tt.b))
		})
	}
}

func TestClosestMatch(t *testing.T) {
	tests := []struct {
		input      string
		candidates []string
		maxDist    int
		want       string
	}{
		{"bugg", []string{"bug", "task", "chore"}, 3, "bug"},
		{"xyz", []string{"bug", "task", "chore"}, 1, ""},
		{"epik", []string{"epic", "feature"}, 2, "epic"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, closestMatch(tt.input, tt.candidates, tt.maxDist))
		})
	}
}

// -----------------------------------------------------------------------
// ValidationError.Error()
// -----------------------------------------------------------------------

func TestValidationError_Error(t *testing.T) {
	e := &ValidationError{
		Line:    5,
		Field:   "title",
		Message: "missing required field \"title\"",
	}
	assert.Equal(t, "line 5: missing required field \"title\"", e.Error())
}

func TestValidationError_ErrorNoField(t *testing.T) {
	e := &ValidationError{
		Line:    3,
		Message: "invalid JSON: unexpected end of JSON input",
	}
	assert.Equal(t, "line 3: invalid JSON: unexpected end of JSON input", e.Error())
}

// -----------------------------------------------------------------------
// Result.Valid()
// -----------------------------------------------------------------------

func TestResult_Valid_Empty(t *testing.T) {
	r := &Result{}
	assert.True(t, r.Valid())
}

func TestResult_Valid_WithErrors(t *testing.T) {
	r := &Result{
		Errors: []ValidationError{{Line: 1, Message: "bad"}},
	}
	assert.False(t, r.Valid())
}
