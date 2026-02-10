package mcpserver

import (
	"strings"
	"testing"
)

func FuzzSplitAndTrim(f *testing.F) {
	f.Add("")
	f.Add(",")
	f.Add("a,b,c")
	f.Add("  ,  ,  ")

	f.Fuzz(func(t *testing.T, input string) {
		result := splitAndTrim(input)
		for _, s := range result {
			if s == "" {
				t.Error("splitAndTrim returned empty string")
			}
			if strings.TrimSpace(s) != s {
				t.Errorf("splitAndTrim returned untrimmed string: %q", s)
			}
		}
	})
}
