package beads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBeads_Success(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, BeadsDir)
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))

	content := `{"id":"stringer-abc","title":"Fix bug","status":"open","type":"bug","priority":1,"labels":["security"]}
{"id":"stringer-def","title":"Add feature","status":"closed","type":"task","priority":2}
`
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, IssuesFile), []byte(content), 0o600))

	beads, err := LoadBeads(dir)
	require.NoError(t, err)
	require.Len(t, beads, 2)

	assert.Equal(t, "stringer-abc", beads[0].ID)
	assert.Equal(t, "Fix bug", beads[0].Title)
	assert.Equal(t, "open", beads[0].Status)
	assert.Equal(t, "bug", beads[0].Type)
	assert.Equal(t, 1, beads[0].Priority)
	assert.Equal(t, []string{"security"}, beads[0].Labels)

	assert.Equal(t, "stringer-def", beads[1].ID)
	assert.Equal(t, "Add feature", beads[1].Title)
	assert.Equal(t, "closed", beads[1].Status)
	assert.Equal(t, 2, beads[1].Priority)
}

func TestLoadBeads_NoFile(t *testing.T) {
	dir := t.TempDir()

	beads, err := LoadBeads(dir)
	assert.NoError(t, err)
	assert.Nil(t, beads)
}

func TestLoadBeads_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, BeadsDir)
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, IssuesFile), []byte(""), 0o600))

	beads, err := LoadBeads(dir)
	assert.NoError(t, err)
	assert.Nil(t, beads) // empty file returns nil slice
}

func TestLoadBeads_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, BeadsDir)
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, IssuesFile), []byte("not json\n"), 0o600))

	beads, err := LoadBeads(dir)
	assert.Error(t, err)
	assert.Nil(t, beads)
	assert.Contains(t, err.Error(), "parse bead at line 1")
}

func TestLoadBeads_LargeLines(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, BeadsDir)
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))

	// Create a bead with a very long title to test buffer handling.
	longTitle := strings.Repeat("a", 100_000)
	content := `{"id":"str-12345678","title":"` + longTitle + `","status":"open","priority":3}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, IssuesFile), []byte(content), 0o600))

	beads, err := LoadBeads(dir)
	require.NoError(t, err)
	require.Len(t, beads, 1)
	assert.Equal(t, longTitle, beads[0].Title)
}

func TestLoadBeads_BlankLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, BeadsDir)
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))

	content := `{"id":"a","title":"First","status":"open","priority":1}

{"id":"b","title":"Second","status":"open","priority":2}
`
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, IssuesFile), []byte(content), 0o600))

	beads, err := LoadBeads(dir)
	require.NoError(t, err)
	require.Len(t, beads, 2)
}

func TestLoadBeads_PermissionError(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, BeadsDir)
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))
	filePath := filepath.Join(beadsDir, IssuesFile)
	require.NoError(t, os.WriteFile(filePath, []byte(`{"id":"a","title":"Test","status":"open","priority":1}`+"\n"), 0o600))

	// Remove read permission.
	require.NoError(t, os.Chmod(filePath, 0o000))
	t.Cleanup(func() {
		_ = os.Chmod(filePath, 0o600) // restore for cleanup
	})

	beads, err := LoadBeads(dir)
	assert.Error(t, err, "should fail when file is unreadable")
	assert.Nil(t, beads)
	assert.Contains(t, err.Error(), "open beads file")
}

func TestLoadBeads_IssueTypeField(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, BeadsDir)
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))

	content := `{"id":"x","title":"Test","status":"open","issue_type":"enhancement","priority":2}
`
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, IssuesFile), []byte(content), 0o600))

	beads, err := LoadBeads(dir)
	require.NoError(t, err)
	require.Len(t, beads, 1)
	assert.Equal(t, "enhancement", beads[0].IssueType)
}
