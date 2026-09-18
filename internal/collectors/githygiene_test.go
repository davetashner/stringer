// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestGitHygieneCollector_Name(t *testing.T) {
	c := &GitHygieneCollector{}
	assert.Equal(t, "githygiene", c.Name())
}

func TestGitHygieneCollector_LargeBinary(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file over the threshold (1 MB).
	data := make([]byte, defaultLargeBinaryThreshold+100)
	data[0] = 0 // null byte makes it binary
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.bin"), data, 0o600))

	// Create a small binary file (under threshold).
	smallData := []byte{0, 1, 2, 3}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.bin"), smallData, 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	largeBinaries := filterByKind(signals, "large-binary")
	assert.Len(t, largeBinaries, 1)
	assert.Contains(t, largeBinaries[0].Title, "big.bin")
	assert.Contains(t, largeBinaries[0].Title, "MB")
	assert.Equal(t, 0.8, largeBinaries[0].Confidence)
	assert.Equal(t, "githygiene", largeBinaries[0].Source)
}

func TestGitHygieneCollector_LargeBinary_LFSTracked(t *testing.T) {
	dir := t.TempDir()

	// Create .gitattributes with LFS pattern.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"),
		[]byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0o600))

	// Create a large binary that matches the LFS pattern.
	data := make([]byte, defaultLargeBinaryThreshold+100)
	data[0] = 0
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.bin"), data, 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	largeBinaries := filterByKind(signals, "large-binary")
	assert.Empty(t, largeBinaries, "LFS-tracked binaries should not be flagged")
}

func TestGitHygieneCollector_MergeConflictMarkers(t *testing.T) {
	dir := t.TempDir()

	content := "line 1\n<<<<<<< HEAD\nour change\n=======\ntheir change\n>>>>>>> branch\nline 7\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "conflict.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	conflicts := filterByKind(signals, "merge-conflict-marker")
	assert.Len(t, conflicts, 1, "should report only one conflict signal per file")
	assert.Equal(t, 2, conflicts[0].Line)
	assert.Equal(t, 0.9, conflicts[0].Confidence)
}

func TestGitHygieneCollector_CommittedSecrets_AWSKey(t *testing.T) {
	dir := t.TempDir()

	content := `package main
const awsKey = "AKIAIOSFODNN7EXAMPLE"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	require.Len(t, secrets, 1)
	assert.Contains(t, secrets[0].Title, "AWS access key")
	assert.Equal(t, 0.7, secrets[0].Confidence)
}

func TestGitHygieneCollector_CommittedSecrets_GitHubToken(t *testing.T) {
	dir := t.TempDir()

	// Generate a fake token of sufficient length (36+ chars).
	token := "ghp_" + strings.Repeat("A", 36)
	content := "TOKEN=" + token + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "env.sh"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	require.Len(t, secrets, 1)
	assert.Contains(t, secrets[0].Title, "GitHub token")
}

func TestGitHygieneCollector_CommittedSecrets_GenericKey(t *testing.T) {
	dir := t.TempDir()

	content := `api_key = "supersecretvalue123456"` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	require.Len(t, secrets, 1)
	assert.Contains(t, secrets[0].Title, "generic secret")
	assert.Equal(t, 0.6, secrets[0].Confidence)
}

func TestGitHygieneCollector_MixedLineEndings(t *testing.T) {
	dir := t.TempDir()

	// Create a file with mixed line endings: some CRLF, some LF.
	// bufio.Scanner strips \n but leaves \r on CRLF lines.
	content := "line1\r\nline2\r\nline3\r\nline4\nline5\nline6\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mixed.txt"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	mixed := filterByKind(signals, "mixed-line-endings")
	require.Len(t, mixed, 1)
	assert.Contains(t, mixed[0].Title, "CRLF")
	assert.Contains(t, mixed[0].Title, "LF")
	assert.Equal(t, 0.7, mixed[0].Confidence)
}

func TestGitHygieneCollector_NoMixedEndings_AllLF(t *testing.T) {
	dir := t.TempDir()

	content := "line1\nline2\nline3\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.txt"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	mixed := filterByKind(signals, "mixed-line-endings")
	assert.Empty(t, mixed)
}

func TestGitHygieneCollector_MinConfidenceFilter(t *testing.T) {
	dir := t.TempDir()

	// Create a generic secret (confidence 0.6) — should be filtered at 0.7.
	content := `api_key = "supersecretvalue123456"` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinConfidence: 0.7,
	})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	assert.Empty(t, secrets, "generic secrets should be filtered at min_confidence 0.7")
}

func TestGitHygieneCollector_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.go"), []byte("package main\n"), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := &GitHygieneCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err)
}

func TestGitHygieneCollector_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	// Put a conflict marker in a vendor file (should be excluded).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor"), 0o750))
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor", "lib.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	conflicts := filterByKind(signals, "merge-conflict-marker")
	assert.Empty(t, conflicts, "vendor files should be excluded by default")
}

func TestGitHygieneCollector_Metrics(t *testing.T) {
	dir := t.TempDir()

	// Set up files that trigger each signal type.
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> branch\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "conflict.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m, ok := c.Metrics().(*GitHygieneMetrics)
	require.True(t, ok)
	assert.Greater(t, m.FilesScanned, 0)
	assert.Equal(t, 1, m.MergeConflictMarkers)
}

func TestParseLFSPatterns(t *testing.T) {
	dir := t.TempDir()

	attrs := `# Git LFS
*.bin filter=lfs diff=lfs merge=lfs -text
*.png filter=lfs diff=lfs merge=lfs -text

# Not LFS
*.go text=auto
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte(attrs), 0o600))

	patterns := parseLFSPatterns(dir)
	assert.Equal(t, []string{"*.bin", "*.png"}, patterns)
}

func TestParseLFSPatterns_NoFile(t *testing.T) {
	dir := t.TempDir()
	patterns := parseLFSPatterns(dir)
	assert.Nil(t, patterns)
}

func TestIsLFSTracked(t *testing.T) {
	patterns := []string{"*.bin", "*.png", "assets/*.psd"}

	assert.True(t, isLFSTracked("foo.bin", patterns))
	assert.True(t, isLFSTracked("dir/bar.png", patterns))
	assert.False(t, isLFSTracked("main.go", patterns))
}

func TestHumanSize(t *testing.T) {
	assert.Equal(t, "500 B", humanSize(500))
	assert.Equal(t, "1.5 KB", humanSize(1500))
	assert.Equal(t, "2.5 MB", humanSize(2_500_000))
	assert.Equal(t, "1.0 GB", humanSize(1_000_000_000))
}

func TestGitHygieneCollector_BinarySkipsTextChecks(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file with conflict markers in it — should NOT trigger
	// merge-conflict-marker since it's binary.
	data := []byte("<<<<<<< HEAD\n\x00binary content\n>>>>>>> branch\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "binary.dat"), data, 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	conflicts := filterByKind(signals, "merge-conflict-marker")
	assert.Empty(t, conflicts, "binary files should not be checked for text patterns")
}

func TestGitHygieneCollector_OneSecretPerLine(t *testing.T) {
	dir := t.TempDir()

	// A line that matches both AWS key and generic secret patterns.
	content := `api_key = "AKIAIOSFODNN7EXAMPLE"` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "both.go"), []byte(content), 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	secrets := filterByKind(signals, "committed-secret")
	assert.Len(t, secrets, 1, "should only report one secret per line")
}

func TestGitHygiene_ConfigurableLargeBinaryThreshold(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file of ~500 bytes.
	binData := make([]byte, 500)
	binData[0] = 0x00 // null byte to make it binary
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small.bin"), binData, 0o600))

	// With default threshold (1MB), this small binary should NOT trigger.
	c1 := &GitHygieneCollector{}
	sigs1, err := c1.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	largeBins1 := filterByKind(sigs1, "large-binary")
	assert.Empty(t, largeBins1, "500-byte binary should not trigger 1MB threshold")

	// With threshold of 100 bytes, it SHOULD trigger.
	c2 := &GitHygieneCollector{}
	sigs2, err := c2.Collect(context.Background(), dir, signal.CollectorOpts{
		LargeBinaryThreshold: 100,
	})
	require.NoError(t, err)
	largeBins2 := filterByKind(sigs2, "large-binary")
	assert.NotEmpty(t, largeBins2, "500-byte binary should trigger 100-byte threshold")
}

// gitHygieneRunGit runs a git command in dir, failing the test on error.
func gitHygieneRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestGitHygiene_SkipsUntrackedAndIgnoredFiles pins stringer-nw3: a large
// binary that git does not track — gitignored working files — is not a git
// hygiene problem. On a real repo this produced 48 false P1 signals for
// gitignored media assets.
func TestGitHygiene_SkipsUntrackedAndIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	gitHygieneRunGit(t, dir, "init")

	// Tracked large binary: must be flagged.
	tracked := make([]byte, 1_100_000)
	tracked[0] = 0x00 // NUL byte marks it binary
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.bin"), tracked, 0o600))

	// Gitignored large binary: must NOT be flagged.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.bin\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignored.bin"), tracked, 0o600))

	// Untracked large binary (not even ignored): must NOT be flagged.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "untracked.bin"), tracked, 0o600))

	gitHygieneRunGit(t, dir, "add", "tracked.bin", ".gitignore")
	gitHygieneRunGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@test.com",
		"commit", "-m", "add tracked binary")

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var large []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "large-binary" {
			large = append(large, s)
		}
	}
	require.Len(t, large, 1, "only the tracked binary is a git hygiene problem")
	assert.Equal(t, "tracked.bin", large[0].FilePath)
}

// TestGitHygiene_NonRepoFallsBackToFullScan verifies the collector still
// works on a bare directory export (no .git).
func TestGitHygiene_NonRepoFallsBackToFullScan(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 1_100_000)
	data[0] = 0x00
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.bin"), data, 0o600))

	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, s := range signals {
		if s.Kind == "large-binary" {
			found = true
		}
	}
	assert.True(t, found, "non-repo scan should still flag large binaries")
}

// ---------------------------------------------------------------------------
// Context-aware secret suppression (stringer-nxx.7). Fixture values are
// obviously fake; none resemble a real credential format.
// ---------------------------------------------------------------------------

// collectSecrets writes files into a temp dir and returns committed-secret
// signals from a default githygiene collector.
func collectSecrets(t *testing.T, files map[string]string, opts signal.CollectorOpts) []signal.RawSignal {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	c := &GitHygieneCollector{}
	signals, err := c.Collect(context.Background(), dir, opts)
	require.NoError(t, err)
	return filterByKind(signals, "committed-secret")
}

func TestGitHygiene_Secrets_SkipsDocumentationForGeneric(t *testing.T) {
	secrets := collectSecrets(t, map[string]string{
		"docs/config.rst":          "    SECRET_KEY = 'supersecretvalue123456'\n",
		"README.md":                "    password = \"supersecretvalue123456\"\n",
		"docs/tutorial/deploy.md":  "SECRET_KEY = 'supersecretvalue123456'\n",
		"docs/de/docs/oauth2.md":   "eyJabc.eyJdef.notarealsignature\n",
		"docs_src/security/t4.py":  "SECRET_KEY = 'supersecretvalue123456'\n",
		"examples/app/settings.py": "SECRET_KEY = 'supersecretvalue123456'\n",
		"templates/admin/login.html": `<input name="password" value="supersecretvalue123456">` + "\n" +
			`password = "supersecretvalue123456"` + "\n",
	}, signal.CollectorOpts{})
	assert.Empty(t, secrets)
}

func TestGitHygiene_Secrets_HighPrecisionStillFiresInDocs(t *testing.T) {
	secrets := collectSecrets(t, map[string]string{
		"docs/setup.md":    "const awsKey = \"AKIAIOSFODNN7EXAMPLE\"\n",
		"examples/key.pem": "-----BEGIN RSA PRIVATE KEY-----\nexample-not-a-real-key\n-----END RSA PRIVATE KEY-----\n",
		"settings.py-tpl":  "const awsKey = \"AKIAIOSFODNN7EXAMPLE\"\n",
		"src/settings.py":  "# const awsKey = \"AKIAIOSFODNN7EXAMPLE\"\n",
	}, signal.CollectorOpts{})
	require.Len(t, secrets, 4)
	for _, s := range secrets {
		assert.NotContains(t, s.Tags, "likely-placeholder")
		assert.GreaterOrEqual(t, s.Confidence, 0.7)
	}
}

func TestGitHygiene_Secrets_SkipsTemplateFilesAndPlaceholders(t *testing.T) {
	secrets := collectSecrets(t, map[string]string{
		"project_name/settings.py-tpl": "SECRET_KEY = 'supersecretvalue123456'\n",
		"config/app.yaml.example":      "password: \"supersecretvalue123456\"\n",
		"deploy/values.tmpl":           "password: \"supersecretvalue123456\"\n",
		"ansible/vars.j2":              "password: \"supersecretvalue123456\"\n",
		"src/settings.py": "SECRET_KEY = '{{ secret_key }}'\n" +
			"API_KEY = \"${API_KEY_FROM_ENV}\"\n" +
			"PASSWORD = '<your-password-here>'\n" +
			"PASSWORD = 'changeme-now-please'\n" +
			"API_KEY = 'example-not-a-real-key'\n" +
			"API_KEY = 'placeholder-value-here'\n" +
			"API_KEY = 'xxxxxxxxxxxxxxxx'\n" +
			"API_KEY = 'abcdef...'\n" +
			"SET_PASSWORD = 'ALTER USER %(user)s IDENTIFIED BY \"%(password)s\"'\n",
	}, signal.CollectorOpts{})
	assert.Empty(t, secrets)
}

func TestGitHygiene_Secrets_SkipsCommentsAndDocstrings(t *testing.T) {
	secrets := collectSecrets(t, map[string]string{
		"src/flask/config.py": "class Config:\n" +
			"    \"\"\"Example usage::\n\n" +
			"        SECRET_KEY = 'supersecretvalue123456'\n" +
			"    \"\"\"\n\n" +
			"    # SECRET_KEY = 'supersecretvalue123456'\n" +
			"    SECRET_KEY = 'supersecretvalue999999'\n",
		"cmd/main.go": "// password = \"supersecretvalue123456\"\n" +
			"/* password = \"supersecretvalue123456\" */\n" +
			" * password = \"supersecretvalue123456\"\n" +
			"var password = \"supersecretvalue777777\"\n",
	}, signal.CollectorOpts{})
	require.Len(t, secrets, 2)
	files := map[string]int{}
	for _, s := range secrets {
		files[filepath.ToSlash(s.FilePath)] = s.Line
		assert.Equal(t, 0.6, s.Confidence)
	}
	assert.Equal(t, 8, files["src/flask/config.py"])
	assert.Equal(t, 4, files["cmd/main.go"])
}

func TestGitHygiene_Secrets_FakeValuesDownweighted(t *testing.T) {
	secrets := collectSecrets(t, map[string]string{
		"src/settings.py": "SECRET_KEY = 'development key'\n" +
			"PASSWORD = 'dummy'\n" +
			"API_KEY = 'supersecretvalue123456'\n",
	}, signal.CollectorOpts{})
	require.Len(t, secrets, 2, "'dummy' is shorter than the generic pattern minimum")
	byLine := map[int]signal.RawSignal{}
	for _, s := range secrets {
		byLine[s.Line] = s
	}
	assert.Equal(t, 0.2, byLine[1].Confidence)
	assert.Contains(t, byLine[1].Tags, "likely-placeholder")
	assert.Equal(t, 0.6, byLine[3].Confidence)
	assert.NotContains(t, byLine[3].Tags, "likely-placeholder")
}

func TestGitHygiene_Secrets_TestFilesCapped(t *testing.T) {
	secrets := collectSecrets(t, map[string]string{
		"tests/conftest.py":      "app.config.update(SECRET_KEY=\"test key\")\n",
		"tests/settings.py":      "SECRET_KEY = 'supersecretvalue123456'\n",
		"pkg/config_test.go":     "password = \"supersecretvalue123456\"\n",
		"src/__tests__/setup.js": "const apiKey = { api_key: 'supersecretvalue123456' };\n",
	}, signal.CollectorOpts{})
	require.Len(t, secrets, 4)
	for _, s := range secrets {
		assert.Contains(t, s.Tags, "test-file", s.FilePath)
		assert.LessOrEqual(t, s.Confidence, 0.3, s.FilePath)
	}
	for _, s := range secrets {
		if strings.HasSuffix(s.FilePath, "conftest.py") {
			assert.Equal(t, 0.2, s.Confidence)
			assert.Contains(t, s.Tags, "likely-placeholder")
		}
	}
}

func TestGitHygiene_Secrets_MinConfidenceDropsDownweighted(t *testing.T) {
	secrets := collectSecrets(t, map[string]string{
		"tests/conftest.py": "SECRET_KEY = \"test key\"\n",
		"src/settings.py":   "SECRET_KEY = 'supersecretvalue123456'\n",
	}, signal.CollectorOpts{MinConfidence: 0.5})
	require.Len(t, secrets, 1)
	assert.Equal(t, "src/settings.py", filepath.ToSlash(secrets[0].FilePath))
}

func TestGitHygiene_Secrets_EntropyRespectsContext(t *testing.T) {
	// Mixed-case alphanumerics with digits reach the 4.0 bit entropy floor
	// without resembling any provider key format.
	highEntropy := "aB3dE6gH9jK2mN5pQ8sT1vW4yZ7xC0fL"
	secrets := collectSecrets(t, map[string]string{
		"docs/auth.md":       "token = \"" + highEntropy + "\"\n",
		"src/auth.py":        "# token = \"" + highEntropy + "\"\n" + "token = \"" + highEntropy + "\"\n",
		"tests/test_auth.py": "token = \"" + highEntropy + "\"\n",
		"src/tpl.py":         "token = \"{{ " + highEntropy + " }}\"\n",
	}, signal.CollectorOpts{EntropyDetection: true})
	require.Len(t, secrets, 2)
	for _, s := range secrets {
		assert.Contains(t, s.Tags, "entropy-based")
		switch filepath.ToSlash(s.FilePath) {
		case "src/auth.py":
			assert.Equal(t, 2, s.Line)
			assert.Equal(t, 0.4, s.Confidence)
		case "tests/test_auth.py":
			assert.Equal(t, 0.3, s.Confidence)
			assert.Contains(t, s.Tags, "test-file")
		default:
			t.Errorf("unexpected entropy signal in %s", s.FilePath)
		}
	}
}

func TestGitHygiene_Secrets_EntropyMinConfidence(t *testing.T) {
	highEntropy := "aB3dE6gH9jK2mN5pQ8sT1vW4yZ7xC0fL"
	secrets := collectSecrets(t, map[string]string{
		"tests/test_auth.py": "token = \"" + highEntropy + "\"\n",
		"src/auth.py":        "token = \"" + highEntropy + "\"\n",
	}, signal.CollectorOpts{EntropyDetection: true, MinConfidence: 0.35})
	require.Len(t, secrets, 1, "test-file hit capped at 0.3 falls below the 0.35 floor")
	assert.Equal(t, "src/auth.py", filepath.ToSlash(secrets[0].FilePath))
}
