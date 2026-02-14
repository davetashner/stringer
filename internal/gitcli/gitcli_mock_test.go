// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package gitcli

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/testable"
)

func TestSetExecutor_NonNil(t *testing.T) {
	mock := &testable.MockCommandExecutor{LookPathResult: "/mock/git"}
	SetExecutor(mock)
	defer SetExecutor(nil)

	// The mock should now be active — LookPath should return mock result.
	path, err := executor.LookPath("git")
	require.NoError(t, err)
	assert.Equal(t, "/mock/git", path)
}

func TestSetExecutor_Nil(t *testing.T) {
	// Set a mock first, then restore to default.
	mock := &testable.MockCommandExecutor{LookPathResult: "/mock/git"}
	SetExecutor(mock)
	SetExecutor(nil)

	// After restoring, it should be a RealCommandExecutor (non-nil, non-mock).
	assert.NotNil(t, executor)
	// Verify it works as real executor — git should be on PATH.
	err := Available()
	assert.NoError(t, err)
}

func TestAvailable_GitNotFound(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		LookPathErr: fmt.Errorf("exec: \"git\": executable file not found in $PATH"),
	})
	defer SetExecutor(nil)

	err := Available()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git not found on PATH")
}

func TestExec_MockCommandFailure(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		DefaultError: "fatal: not a git repository",
	})
	defer SetExecutor(nil)

	_, err := Exec(context.Background(), "/tmp", "status")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git status")
}

func TestExec_MockCommandSuccess(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		CommandOutputs: map[string]string{
			"git --version": "git version 2.40.0",
		},
	})
	defer SetExecutor(nil)

	out, err := Exec(context.Background(), ".", "--version")
	require.NoError(t, err)
	assert.Contains(t, out, "git version")
}

func TestBlameSingleLine_MockExecFailure(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		DefaultError: "fatal: no such path",
	})
	defer SetExecutor(nil)

	_, err := BlameSingleLine(context.Background(), "/tmp", "missing.go", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git blame")
}

func TestBlameSingleLine_EmptyBlameOutput(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		DefaultOutput: "", // empty output from blame
	})
	defer SetExecutor(nil)

	_, err := BlameSingleLine(context.Background(), "/tmp", "file.go", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no blame output for file.go:1")
}

func TestBlameFile_MockExecFailure(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		DefaultError: "fatal: no such path",
	})
	defer SetExecutor(nil)

	_, err := BlameFile(context.Background(), "/tmp", "missing.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git blame")
}

func TestLastCommitTime_MockExecFailure(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		DefaultError: "fatal: bad default revision 'HEAD'",
	})
	defer SetExecutor(nil)

	_, err := LastCommitTime(context.Background(), "/tmp", "file.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git log")
}

func TestLastCommitTime_MockEmptyOutput(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		DefaultOutput: "",
	})
	defer SetExecutor(nil)

	ts, err := LastCommitTime(context.Background(), "/tmp", "nocommits.go")
	require.NoError(t, err)
	assert.True(t, ts.IsZero(), "empty output should return zero time")
}

func TestLastCommitTime_MockMalformedTimestamp(t *testing.T) {
	SetExecutor(&testable.MockCommandExecutor{
		DefaultOutput: "not-a-date\n",
	})
	defer SetExecutor(nil)

	_, err := LastCommitTime(context.Background(), "/tmp", "file.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing commit time")
}

func TestParsePorcelainBlame_EmptyInput(t *testing.T) {
	lines, err := parsePorcelainBlame("")
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestParsePorcelainBlame_MalformedLines(t *testing.T) {
	// Input with no valid SHA header lines — should parse but produce no results.
	input := "short\nno valid SHA here\n"
	lines, err := parsePorcelainBlame(input)
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestParsePorcelainBlame_InvalidAuthorTime(t *testing.T) {
	// Porcelain with an invalid (non-integer) author-time.
	porcelain := `abcdef0123456789abcdef0123456789abcdef01 1 1 1
author TestUser
author-time not-a-number
	line content
`
	lines, err := parsePorcelainBlame(porcelain)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, "TestUser", lines[0].AuthorName)
	// Author time should be zero since parsing failed.
	assert.True(t, lines[0].AuthorTime.IsZero(), "invalid author-time should yield zero time")
}

func TestParsePorcelainBlame_ShortFields(t *testing.T) {
	// Lines with fewer than 3 fields should be skipped as non-header lines.
	input := "ab\ncd ef\n"
	lines, err := parsePorcelainBlame(input)
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestParsePorcelainBlame_NonHexSHASkipped(t *testing.T) {
	// Lines that look like they have 3+ fields but the first is not a hex SHA.
	input := "ZZZZ0000000000000000000000000000ZZZZZZZZ 1 1 1\nauthor Test\n\tline\n"
	lines, err := parsePorcelainBlame(input)
	require.NoError(t, err)
	assert.Empty(t, lines, "non-hex SHA should be skipped")
}

func TestParsePorcelainBlame_SingleBlock(t *testing.T) {
	// Test a complete single-block porcelain output via the parser directly.
	porcelain := "abcdef0123456789abcdef0123456789abcdef01 5 5 1\n" +
		"author MockAuthor\n" +
		"author-time 1700000000\n" +
		"\tmocked content\n"

	lines, err := parsePorcelainBlame(porcelain)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, "MockAuthor", lines[0].AuthorName)
	assert.Equal(t, int64(1700000000), lines[0].AuthorTime.Unix())
}
