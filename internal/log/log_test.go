package log

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetup_DefaultLevel(t *testing.T) {
	Setup(false, false)

	ctx := context.Background()
	// Default level should be INFO.
	handler := slog.Default().Handler()
	assert.True(t, handler.Enabled(ctx, slog.LevelInfo), "INFO should be enabled in default mode")
	assert.True(t, handler.Enabled(ctx, slog.LevelWarn), "WARN should be enabled in default mode")
	assert.True(t, handler.Enabled(ctx, slog.LevelError), "ERROR should be enabled in default mode")
	assert.False(t, handler.Enabled(ctx, slog.LevelDebug), "DEBUG should not be enabled in default mode")
}

func TestSetup_VerboseLevel(t *testing.T) {
	Setup(true, false)

	ctx := context.Background()
	handler := slog.Default().Handler()
	assert.True(t, handler.Enabled(ctx, slog.LevelDebug), "DEBUG should be enabled in verbose mode")
	assert.True(t, handler.Enabled(ctx, slog.LevelInfo), "INFO should be enabled in verbose mode")
	assert.True(t, handler.Enabled(ctx, slog.LevelWarn), "WARN should be enabled in verbose mode")
}

func TestSetup_QuietLevel(t *testing.T) {
	Setup(false, true)

	ctx := context.Background()
	handler := slog.Default().Handler()
	assert.False(t, handler.Enabled(ctx, slog.LevelInfo), "INFO should not be enabled in quiet mode")
	assert.False(t, handler.Enabled(ctx, slog.LevelDebug), "DEBUG should not be enabled in quiet mode")
	assert.True(t, handler.Enabled(ctx, slog.LevelWarn), "WARN should be enabled in quiet mode")
	assert.True(t, handler.Enabled(ctx, slog.LevelError), "ERROR should be enabled in quiet mode")
}

func TestSetup_QuietTakesPrecedence(t *testing.T) {
	// When both verbose and quiet are set, quiet takes precedence
	// because the switch checks quiet first.
	Setup(true, true)

	ctx := context.Background()
	handler := slog.Default().Handler()
	assert.False(t, handler.Enabled(ctx, slog.LevelDebug), "DEBUG should not be enabled when quiet takes precedence")
	assert.False(t, handler.Enabled(ctx, slog.LevelInfo), "INFO should not be enabled when quiet takes precedence")
	assert.True(t, handler.Enabled(ctx, slog.LevelWarn), "WARN should be enabled when quiet takes precedence")
}

func TestSetup_CalledMultipleTimes(t *testing.T) {
	ctx := context.Background()

	// Setup should be safe to call multiple times.
	Setup(true, false)
	handler1 := slog.Default().Handler()
	assert.True(t, handler1.Enabled(ctx, slog.LevelDebug))

	Setup(false, true)
	handler2 := slog.Default().Handler()
	assert.False(t, handler2.Enabled(ctx, slog.LevelDebug))
	assert.True(t, handler2.Enabled(ctx, slog.LevelWarn))
}
