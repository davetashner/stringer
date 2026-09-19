// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// CRLF working trees are the norm on Windows (Git for Windows defaults to
// core.autocrlf=true), while CI checks the repository out with LF endings.
// These tests feed the file-walking collectors the same sources with LF and
// with CRLF endings and require identical signals, so a CRLF regression
// shows up on every OS rather than only on Windows user machines.

var crlfFixtures = map[string]string{
	"main.go": `package main

// TODO: replace the widget cache
func complexFunc(items []int) int {
	sum := 0
	for _, item := range items {
		if item > 0 {
			sum += item
		} else if item < -10 {
			sum -= item
		}
		switch {
		case item == 0:
			continue
		case item > 100:
			break
		}
		if sum > 1000 || sum < -1000 {
			return sum // FIXME: overflow handling
		}
	}
	return sum
}
`,
	"app.py": `# HACK: monkey patch until upstream fix lands
def complex_function(data):
    result = []
    for item in data:
        if item > 0:
            result.append(item)
        elif item < -10:
            result.append(-item)
        else:
            pass
        for sub in item.children:
            if sub.valid and sub.active:
                result.append(sub)
            elif sub.pending or sub.deferred:
                pass
        while len(result) > 100:
            result.pop()
    return result
`,
	"web/handler.js": `function processItems(items) {
  for (const item of items) {
    const result = transform(item);
    if (result !== null) {
      store(result);
    }
    log(item);
  }
  return items.length;
}
`,
	"web/worker.js": `function processItems(items) {
  for (const item of items) {
    const result = transform(item);
    if (result !== null) {
      store(result);
    }
    log(item);
  }
  return items.length;
}
`,
}

// writeFixtures writes files into a new temp dir, converting "\n" to eol.
func writeFixtures(t *testing.T, files map[string]string, eol string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(strings.ReplaceAll(content, "\n", eol)), 0o600))
	}
	return dir
}

// signalKeys renders the output-relevant fields of each signal, sorted.
func signalKeys(signals []signal.RawSignal) []string {
	keys := make([]string, 0, len(signals))
	for _, s := range signals {
		keys = append(keys, strings.Join([]string{
			s.Source, s.Kind, s.FilePath, strconv.Itoa(s.Line), s.Title, s.Description,
		}, "|"))
	}
	sort.Strings(keys)
	return keys
}

func TestCollectors_CRLFMatchesLF(t *testing.T) {
	tests := []struct {
		name     string
		newC     func() collector.Collector
		opts     signal.CollectorOpts
		wantKind string
	}{
		{
			name: "todos",
			newC: func() collector.Collector {
				return &TodoCollector{}
			},
			wantKind: "todo",
		},
		{
			name: "complexity",
			newC: func() collector.Collector {
				return &ComplexityCollector{}
			},
			opts:     signal.CollectorOpts{MinComplexityScore: 3.0},
			wantKind: "complex-function",
		},
		{
			name: "duplication",
			newC: func() collector.Collector {
				return &DuplicationCollector{}
			},
			wantKind: "code-clone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lfDir := writeFixtures(t, crlfFixtures, "\n")
			crlfDir := writeFixtures(t, crlfFixtures, "\r\n")

			lf, err := tt.newC().Collect(context.Background(), lfDir, tt.opts)
			require.NoError(t, err)
			crlf, err := tt.newC().Collect(context.Background(), crlfDir, tt.opts)
			require.NoError(t, err)

			require.NotEmpty(t, filterByKind(lf, tt.wantKind), "LF fixture should yield %s signals", tt.wantKind)
			assert.Equal(t, signalKeys(lf), signalKeys(crlf), "CRLF input must produce the same signals as LF")
			for _, s := range crlf {
				assert.NotContains(t, s.Title, "\r", "title leaks a carriage return: %q", s.Title)
				assert.NotContains(t, s.Description, "\r", "description leaks a carriage return")
			}
		})
	}
}
