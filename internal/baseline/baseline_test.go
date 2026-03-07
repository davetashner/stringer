// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package baseline

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/testable"
)

// --- Load tests ---

func TestLoad_MissingFile(t *testing.T) {
	state, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if state != nil {
		t.Fatal("expected nil state for missing file")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	bs := BaselineState{
		Version: "1",
		Suppressions: []Suppression{
			{
				SignalID:     "abc123",
				Reason:       ReasonAcknowledged,
				Comment:      "reviewed",
				SuppressedBy: "alice",
				SuppressedAt: now,
			},
		},
	}
	data, _ := json.MarshalIndent(bs, "", "  ")
	stateDir := filepath.Join(dir, ".stringer")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "baseline.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.Version != "1" {
		t.Errorf("version = %q, want %q", state.Version, "1")
	}
	if len(state.Suppressions) != 1 {
		t.Fatalf("got %d suppressions, want 1", len(state.Suppressions))
	}
	if state.Suppressions[0].SignalID != "abc123" {
		t.Errorf("signal_id = %q, want %q", state.Suppressions[0].SignalID, "abc123")
	}
}

func TestLoad_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".stringer")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "baseline.json"), []byte("{invalid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if state != nil {
		t.Fatal("expected nil state on error")
	}
}

func TestLoad_PermissionError(t *testing.T) {
	// Use mock to simulate permission error without OS-level permission changes.
	origFS := FS
	FS = &testable.MockFileSystem{
		ReadFileFn: func(string) ([]byte, error) {
			return nil, errors.New("permission denied")
		},
	}
	defer func() { FS = origFS }()

	state, err := Load("/repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %v, want to contain 'permission denied'", err)
	}
	if state != nil {
		t.Fatal("expected nil state on error")
	}
}

func TestLoad_MockNotExist(t *testing.T) {
	// Verify the fs.ErrNotExist check works through mock.
	origFS := FS
	FS = &testable.MockFileSystem{
		ReadFileFn: func(string) ([]byte, error) {
			return nil, fs.ErrNotExist
		},
	}
	defer func() { FS = origFS }()

	state, err := Load("/repo")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if state != nil {
		t.Fatal("expected nil state for missing file")
	}
}

// --- Save tests ---

func TestSave_CreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	state := &BaselineState{
		Version: "1",
		Suppressions: []Suppression{
			{
				SignalID:     "sig1",
				Reason:       ReasonWontFix,
				SuppressedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := Save(dir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the file exists.
	path := filepath.Join(dir, ".stringer", "baseline.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not found: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected file, got directory")
	}

	// Verify JSON is readable and indented.
	data, err := os.ReadFile(path) //nolint:gosec // test code, path from t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "  ") {
		t.Error("expected indented JSON")
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("expected trailing newline")
	}

	// Verify content.
	var loaded BaselineState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if loaded.Version != "1" {
		t.Errorf("version = %q, want %q", loaded.Version, "1")
	}

	// Verify no temp file left behind.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !errors.Is(err, fs.ErrNotExist) {
		t.Error("temp file should not exist after successful save")
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	// Verify atomic write uses temp + rename by checking the rename function is called.
	dir := t.TempDir()

	var renamedFrom, renamedTo string
	origRename := rename
	rename = func(src, dst string) error {
		renamedFrom = src
		renamedTo = dst
		return os.Rename(src, dst)
	}
	defer func() { rename = origRename }()

	state := &BaselineState{Version: "1"}
	if err := Save(dir, state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantTmp := filepath.Join(dir, ".stringer", "baseline.json.tmp")
	wantFinal := filepath.Join(dir, ".stringer", "baseline.json")

	if renamedFrom != wantTmp {
		t.Errorf("rename from = %q, want %q", renamedFrom, wantTmp)
	}
	if renamedTo != wantFinal {
		t.Errorf("rename to = %q, want %q", renamedTo, wantFinal)
	}
}

func TestSave_MkdirError(t *testing.T) {
	origFS := FS
	FS = &testable.MockFileSystem{
		MkdirAllFn: func(string, os.FileMode) error {
			return errors.New("mkdir failed")
		},
	}
	defer func() { FS = origFS }()

	err := Save("/repo", &BaselineState{Version: "1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "create baseline directory") {
		t.Errorf("error = %v, want to contain 'create baseline directory'", err)
	}
}

func TestSave_WriteError(t *testing.T) {
	origFS := FS
	FS = &testable.MockFileSystem{
		MkdirAllFn: func(string, os.FileMode) error { return nil },
		WriteFileFn: func(string, []byte, os.FileMode) error {
			return errors.New("disk full")
		},
	}
	defer func() { FS = origFS }()

	err := Save("/repo", &BaselineState{Version: "1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "write baseline temp file") {
		t.Errorf("error = %v, want to contain 'write baseline temp file'", err)
	}
}

func TestSave_RenameError(t *testing.T) {
	origFS := FS
	FS = &testable.MockFileSystem{
		MkdirAllFn:  func(string, os.FileMode) error { return nil },
		WriteFileFn: func(string, []byte, os.FileMode) error { return nil },
	}
	origRename := rename
	rename = func(string, string) error {
		return errors.New("rename failed")
	}
	defer func() {
		FS = origFS
		rename = origRename
	}()

	err := Save("/repo", &BaselineState{Version: "1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rename baseline file") {
		t.Errorf("error = %v, want to contain 'rename baseline file'", err)
	}
}

// --- Save+Load round trip ---

func TestSave_Load_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	state := &BaselineState{
		Version: "1",
		Suppressions: []Suppression{
			{
				SignalID:     "sig-a",
				Reason:       ReasonAcknowledged,
				SuppressedAt: now,
			},
			{
				SignalID:     "sig-b",
				Reason:       ReasonWontFix,
				Comment:      "deferred",
				SuppressedBy: "alice",
				SuppressedAt: now,
				ExpiresAt:    &expires,
			},
		},
	}

	if err := Save(dir, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != state.Version {
		t.Errorf("version = %q, want %q", loaded.Version, state.Version)
	}
	if len(loaded.Suppressions) != len(state.Suppressions) {
		t.Fatalf("got %d suppressions, want %d", len(loaded.Suppressions), len(state.Suppressions))
	}
	for i, want := range state.Suppressions {
		got := loaded.Suppressions[i]
		if got.SignalID != want.SignalID {
			t.Errorf("[%d] signal_id = %q, want %q", i, got.SignalID, want.SignalID)
		}
		if got.Reason != want.Reason {
			t.Errorf("[%d] reason = %q, want %q", i, got.Reason, want.Reason)
		}
		if got.Comment != want.Comment {
			t.Errorf("[%d] comment = %q, want %q", i, got.Comment, want.Comment)
		}
		if got.SuppressedBy != want.SuppressedBy {
			t.Errorf("[%d] suppressed_by = %q, want %q", i, got.SuppressedBy, want.SuppressedBy)
		}
		if !got.SuppressedAt.Equal(want.SuppressedAt) {
			t.Errorf("[%d] suppressed_at = %v, want %v", i, got.SuppressedAt, want.SuppressedAt)
		}
	}

	// Verify expires_at on second suppression.
	if loaded.Suppressions[1].ExpiresAt == nil {
		t.Fatal("expected non-nil expires_at on second suppression")
	}
	if !loaded.Suppressions[1].ExpiresAt.Equal(expires) {
		t.Errorf("expires_at = %v, want %v", loaded.Suppressions[1].ExpiresAt, expires)
	}
}

// --- ValidateReason tests ---

func TestValidateReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason  Reason
		wantErr bool
	}{
		{ReasonAcknowledged, false},
		{ReasonWontFix, false},
		{ReasonFalsePositive, false},
		{"unknown", true},
		{"", true},
	}

	for _, tc := range tests {
		err := ValidateReason(tc.reason)
		if tc.wantErr && err == nil {
			t.Errorf("ValidateReason(%q) = nil, want error", tc.reason)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ValidateReason(%q) = %v, want nil", tc.reason, err)
		}
	}
}

// --- IsExpired tests ---

func TestIsExpired(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	tests := []struct {
		name string
		s    Suppression
		want bool
	}{
		{"no expiry", Suppression{}, false},
		{"expired", Suppression{ExpiresAt: &past}, true},
		{"not expired", Suppression{ExpiresAt: &future}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsExpired(tc.s); got != tc.want {
				t.Errorf("IsExpired() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- Lookup tests ---

func TestLookup(t *testing.T) {
	t.Parallel()

	t.Run("nil state", func(t *testing.T) {
		t.Parallel()
		m := Lookup(nil)
		if m != nil {
			t.Error("expected nil map for nil state")
		}
	})

	t.Run("empty suppressions", func(t *testing.T) {
		t.Parallel()
		m := Lookup(&BaselineState{Version: "1"})
		if len(m) != 0 {
			t.Errorf("expected empty map, got %d entries", len(m))
		}
	})

	t.Run("with suppressions", func(t *testing.T) {
		t.Parallel()
		state := &BaselineState{
			Version: "1",
			Suppressions: []Suppression{
				{SignalID: "a", Reason: ReasonAcknowledged},
				{SignalID: "b", Reason: ReasonWontFix},
			},
		}
		m := Lookup(state)
		if len(m) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(m))
		}
		if m["a"].Reason != ReasonAcknowledged {
			t.Errorf("a.Reason = %q, want %q", m["a"].Reason, ReasonAcknowledged)
		}
		if m["b"].Reason != ReasonWontFix {
			t.Errorf("b.Reason = %q, want %q", m["b"].Reason, ReasonWontFix)
		}
	})
}

// --- AddOrUpdate tests ---

func TestAddOrUpdate(t *testing.T) {
	t.Parallel()

	t.Run("add new", func(t *testing.T) {
		t.Parallel()
		state := &BaselineState{Version: "1"}
		s := Suppression{SignalID: "new1", Reason: ReasonAcknowledged}
		AddOrUpdate(state, s)
		if len(state.Suppressions) != 1 {
			t.Fatalf("expected 1 suppression, got %d", len(state.Suppressions))
		}
		if state.Suppressions[0].SignalID != "new1" {
			t.Error("expected signal_id = new1")
		}
	})

	t.Run("update existing", func(t *testing.T) {
		t.Parallel()
		state := &BaselineState{
			Version: "1",
			Suppressions: []Suppression{
				{SignalID: "existing", Reason: ReasonAcknowledged, Comment: "old"},
			},
		}
		updated := Suppression{SignalID: "existing", Reason: ReasonWontFix, Comment: "new"}
		AddOrUpdate(state, updated)

		if len(state.Suppressions) != 1 {
			t.Fatalf("expected 1 suppression, got %d", len(state.Suppressions))
		}
		if state.Suppressions[0].Reason != ReasonWontFix {
			t.Errorf("reason = %q, want %q", state.Suppressions[0].Reason, ReasonWontFix)
		}
		if state.Suppressions[0].Comment != "new" {
			t.Errorf("comment = %q, want %q", state.Suppressions[0].Comment, "new")
		}
	})

	t.Run("add second", func(t *testing.T) {
		t.Parallel()
		state := &BaselineState{
			Version: "1",
			Suppressions: []Suppression{
				{SignalID: "first", Reason: ReasonAcknowledged},
			},
		}
		AddOrUpdate(state, Suppression{SignalID: "second", Reason: ReasonFalsePositive})
		if len(state.Suppressions) != 2 {
			t.Fatalf("expected 2 suppressions, got %d", len(state.Suppressions))
		}
	})
}

// --- Remove tests ---

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("remove existing", func(t *testing.T) {
		t.Parallel()
		state := &BaselineState{
			Version: "1",
			Suppressions: []Suppression{
				{SignalID: "a"},
				{SignalID: "b"},
				{SignalID: "c"},
			},
		}
		found := Remove(state, "b")
		if !found {
			t.Error("expected true for removing existing suppression")
		}
		if len(state.Suppressions) != 2 {
			t.Fatalf("expected 2 suppressions, got %d", len(state.Suppressions))
		}
		if state.Suppressions[0].SignalID != "a" || state.Suppressions[1].SignalID != "c" {
			t.Errorf("unexpected order after remove: %v", state.Suppressions)
		}
	})

	t.Run("remove nonexistent", func(t *testing.T) {
		t.Parallel()
		state := &BaselineState{
			Version: "1",
			Suppressions: []Suppression{
				{SignalID: "a"},
			},
		}
		found := Remove(state, "nonexistent")
		if found {
			t.Error("expected false for removing nonexistent suppression")
		}
		if len(state.Suppressions) != 1 {
			t.Error("expected suppressions unchanged")
		}
	})

	t.Run("remove from empty", func(t *testing.T) {
		t.Parallel()
		state := &BaselineState{Version: "1"}
		found := Remove(state, "any")
		if found {
			t.Error("expected false for removing from empty list")
		}
	})
}

// --- Schema version test ---

func TestSchemaVersion(t *testing.T) {
	t.Parallel()
	if schemaVersion != "1" {
		t.Errorf("schemaVersion = %q, want %q", schemaVersion, "1")
	}
}

// --- JSON serialization tests ---

func TestSuppressionJSON(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	s := Suppression{
		SignalID:     "sig-001",
		Reason:       ReasonFalsePositive,
		Comment:      "not relevant",
		SuppressedBy: "bob",
		SuppressedAt: now,
		ExpiresAt:    &expires,
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed Suppression
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.SignalID != s.SignalID {
		t.Errorf("signal_id = %q, want %q", parsed.SignalID, s.SignalID)
	}
	if parsed.Reason != s.Reason {
		t.Errorf("reason = %q, want %q", parsed.Reason, s.Reason)
	}
	if parsed.Comment != s.Comment {
		t.Errorf("comment = %q, want %q", parsed.Comment, s.Comment)
	}
	if parsed.SuppressedBy != s.SuppressedBy {
		t.Errorf("suppressed_by = %q, want %q", parsed.SuppressedBy, s.SuppressedBy)
	}
	if !parsed.SuppressedAt.Equal(s.SuppressedAt) {
		t.Errorf("suppressed_at = %v, want %v", parsed.SuppressedAt, s.SuppressedAt)
	}
	if parsed.ExpiresAt == nil || !parsed.ExpiresAt.Equal(*s.ExpiresAt) {
		t.Errorf("expires_at = %v, want %v", parsed.ExpiresAt, s.ExpiresAt)
	}
}

func TestSuppressionJSON_OmitEmpty(t *testing.T) {
	t.Parallel()

	s := Suppression{
		SignalID:     "sig-002",
		Reason:       ReasonAcknowledged,
		SuppressedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	raw := string(data)
	if strings.Contains(raw, "comment") {
		t.Error("expected comment to be omitted when empty")
	}
	if strings.Contains(raw, "suppressed_by") {
		t.Error("expected suppressed_by to be omitted when empty")
	}
	if strings.Contains(raw, "expires_at") {
		t.Error("expected expires_at to be omitted when nil")
	}
}
