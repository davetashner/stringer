package mcpserver

import "testing"

func FuzzResolvePath(f *testing.F) {
	f.Add(".")
	f.Add("")
	f.Add("/")
	f.Add("../../etc/passwd")
	f.Add(string(make([]byte, 4096)))
	f.Add("path/with\x00null")

	f.Fuzz(func(t *testing.T, input string) {
		// ResolvePath should never panic on any input.
		ResolvePath(input) //nolint:errcheck // fuzz: testing crash-freedom
	})
}
