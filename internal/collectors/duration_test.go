package collectors

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration_Days(t *testing.T) {
	d, err := ParseDuration("90d")
	require.NoError(t, err)
	assert.Equal(t, 90*24*time.Hour, d)
}

func TestParseDuration_Weeks(t *testing.T) {
	d, err := ParseDuration("2w")
	require.NoError(t, err)
	assert.Equal(t, 14*24*time.Hour, d)
}

func TestParseDuration_Months(t *testing.T) {
	d, err := ParseDuration("6m")
	require.NoError(t, err)
	assert.Equal(t, 180*24*time.Hour, d)
}

func TestParseDuration_Years(t *testing.T) {
	d, err := ParseDuration("1y")
	require.NoError(t, err)
	assert.Equal(t, 365*24*time.Hour, d)
}

func TestParseDuration_InvalidTooShort(t *testing.T) {
	_, err := ParseDuration("d")
	assert.Error(t, err)
}

func TestParseDuration_InvalidEmpty(t *testing.T) {
	_, err := ParseDuration("")
	assert.Error(t, err)
}

func TestParseDuration_InvalidUnit(t *testing.T) {
	_, err := ParseDuration("10x")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration unit")
}

func TestParseDuration_InvalidNumber(t *testing.T) {
	_, err := ParseDuration("abcd")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration number")
}
