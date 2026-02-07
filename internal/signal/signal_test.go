package signal

import (
	"testing"
	"time"
)

func TestRawSignalZeroValue(t *testing.T) {
	var s RawSignal
	if s.Source != "" {
		t.Errorf("zero-value Source = %q, want empty", s.Source)
	}
	if s.Confidence != 0.0 {
		t.Errorf("zero-value Confidence = %v, want 0.0", s.Confidence)
	}
	if s.Tags != nil {
		t.Errorf("zero-value Tags = %v, want nil", s.Tags)
	}
}

func TestCollectorResultTracksError(t *testing.T) {
	r := CollectorResult{
		Collector: "todos",
		Duration:  100 * time.Millisecond,
		Err:       nil,
	}
	if r.Err != nil {
		t.Errorf("expected nil error, got %v", r.Err)
	}
}
