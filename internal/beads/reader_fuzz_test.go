// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package beads

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzBeadParse(f *testing.F) {
	f.Add([]byte(`{"id":"str-abc","title":"test","status":"open","priority":1}`))
	f.Add([]byte(""))
	f.Add([]byte("{invalid json}"))
	f.Add([]byte("{}\n{}\n{}"))
	f.Add([]byte(`{"id":"a","title":"b","status":"c","priority":0,"labels":["x","y"]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Cap input size to avoid pathological JSON (deeply nested
		// structures) that cause json.Unmarshal to exceed the test deadline.
		if len(data) > 4096 {
			return
		}
		lines := bytes.Split(data, []byte("\n"))
		for _, line := range lines {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var b Bead
			if err := json.Unmarshal(line, &b); err != nil {
				continue
			}
			// Round-trip: if parse succeeded, marshal should not panic.
			json.Marshal(&b) //nolint:errcheck,gosec // fuzz: testing crash-freedom
		}
	})
}
