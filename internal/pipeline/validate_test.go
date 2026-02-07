package pipeline

import (
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// validSignal returns a signal that passes all validation rules.
func validSignal() signal.RawSignal {
	return signal.RawSignal{
		Source:      "todos",
		Kind:        "todo",
		FilePath:    "internal/foo.go",
		Line:        42,
		Title:       "Fix the thing",
		Description: "This thing needs fixing",
		Author:      "alice",
		Timestamp:   time.Now(),
		Confidence:  0.8,
		Tags:        []string{"tech-debt"},
	}
}

func TestValidateSignal_Valid(t *testing.T) {
	errs := ValidateSignal(validSignal())
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid signal, got %v", errs)
	}
}

func TestValidateSignal_EmptyTitle(t *testing.T) {
	s := validSignal()
	s.Title = ""

	errs := ValidateSignal(s)
	assertHasFieldError(t, errs, "Title")
}

func TestValidateSignal_WhitespaceOnlyTitle(t *testing.T) {
	s := validSignal()
	s.Title = "   "

	errs := ValidateSignal(s)
	assertHasFieldError(t, errs, "Title")
}

func TestValidateSignal_EmptySource(t *testing.T) {
	s := validSignal()
	s.Source = ""

	errs := ValidateSignal(s)
	assertHasFieldError(t, errs, "Source")
}

func TestValidateSignal_WhitespaceOnlySource(t *testing.T) {
	s := validSignal()
	s.Source = "  \t "

	errs := ValidateSignal(s)
	assertHasFieldError(t, errs, "Source")
}

func TestValidateSignal_AbsoluteFilePath(t *testing.T) {
	s := validSignal()
	s.FilePath = "/home/user/repo/foo.go"

	errs := ValidateSignal(s)
	assertHasFieldError(t, errs, "FilePath")
}

func TestValidateSignal_RelativeFilePathOK(t *testing.T) {
	s := validSignal()
	s.FilePath = "src/main.go"

	errs := ValidateSignal(s)
	assertNoFieldError(t, errs, "FilePath")
}

func TestValidateSignal_EmptyFilePathOK(t *testing.T) {
	// An empty path is valid (some signals may not have a file).
	s := validSignal()
	s.FilePath = ""

	errs := ValidateSignal(s)
	assertNoFieldError(t, errs, "FilePath")
}

func TestValidateSignal_ConfidenceTooLow(t *testing.T) {
	s := validSignal()
	s.Confidence = -0.1

	errs := ValidateSignal(s)
	assertHasFieldError(t, errs, "Confidence")
}

func TestValidateSignal_ConfidenceTooHigh(t *testing.T) {
	s := validSignal()
	s.Confidence = 1.1

	errs := ValidateSignal(s)
	assertHasFieldError(t, errs, "Confidence")
}

func TestValidateSignal_ConfidenceBoundaryZero(t *testing.T) {
	s := validSignal()
	s.Confidence = 0.0

	errs := ValidateSignal(s)
	assertNoFieldError(t, errs, "Confidence")
}

func TestValidateSignal_ConfidenceBoundaryOne(t *testing.T) {
	s := validSignal()
	s.Confidence = 1.0

	errs := ValidateSignal(s)
	assertNoFieldError(t, errs, "Confidence")
}

func TestValidateSignal_MultipleErrors(t *testing.T) {
	s := signal.RawSignal{
		Source:     "",
		Title:      "",
		FilePath:   "/abs/path",
		Confidence: 2.0,
	}

	errs := ValidateSignal(s)
	if len(errs) != 4 {
		t.Errorf("expected 4 errors, got %d: %v", len(errs), errs)
	}
	assertHasFieldError(t, errs, "Title")
	assertHasFieldError(t, errs, "Source")
	assertHasFieldError(t, errs, "FilePath")
	assertHasFieldError(t, errs, "Confidence")
}

func TestValidationError_Error(t *testing.T) {
	e := ValidationError{Field: "Title", Message: "must not be empty"}
	want := "Title: must not be empty"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// assertHasFieldError checks that at least one error targets the given field.
func assertHasFieldError(t *testing.T, errs []ValidationError, field string) {
	t.Helper()
	for _, e := range errs {
		if e.Field == field {
			return
		}
	}
	t.Errorf("expected validation error for field %q, got %v", field, errs)
}

// assertNoFieldError checks that no error targets the given field.
func assertNoFieldError(t *testing.T, errs []ValidationError, field string) {
	t.Helper()
	for _, e := range errs {
		if e.Field == field {
			t.Errorf("unexpected validation error for field %q: %v", field, e)
		}
	}
}
