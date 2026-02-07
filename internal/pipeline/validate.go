// Package pipeline provides the scan orchestration engine for stringer.
// It resolves collectors, runs them sequentially, validates their output,
// and aggregates results into a ScanResult.
package pipeline

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

// ValidationError describes a single validation failure for a RawSignal.
type ValidationError struct {
	// Field is the struct field that failed validation.
	Field string

	// Message describes what went wrong.
	Message string
}

// Error implements the error interface.
func (v ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", v.Field, v.Message)
}

// ValidateSignal checks a RawSignal for validity and returns all validation
// errors found. An empty slice means the signal is valid.
func ValidateSignal(s signal.RawSignal) []ValidationError {
	var errs []ValidationError

	if strings.TrimSpace(s.Title) == "" {
		errs = append(errs, ValidationError{
			Field:   "Title",
			Message: "must not be empty",
		})
	}

	if strings.TrimSpace(s.Source) == "" {
		errs = append(errs, ValidationError{
			Field:   "Source",
			Message: "must not be empty",
		})
	}

	if filepath.IsAbs(s.FilePath) {
		errs = append(errs, ValidationError{
			Field:   "FilePath",
			Message: "must be a relative path, got absolute path",
		})
	}

	if s.Confidence < 0.0 || s.Confidence > 1.0 {
		errs = append(errs, ValidationError{
			Field:   "Confidence",
			Message: fmt.Sprintf("must be between 0.0 and 1.0, got %v", s.Confidence),
		})
	}

	return errs
}
