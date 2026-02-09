package main

import "github.com/davetashner/stringer/internal/testable"

// cmdFS is the file system implementation used by CLI commands.
// Override in tests with a testable.MockFileSystem.
var cmdFS testable.FileSystem = testable.DefaultFS
