package testable

import (
	"io/fs"
	"os"
)

// MockFileSystem is a test double for FileSystem. Each method has a
// corresponding function field. When the field is non-nil, the mock calls it;
// otherwise, it falls through to OsFileSystem (real OS behavior).
//
// This design lets tests override only the methods they care about while
// keeping realistic behavior for everything else.
type MockFileSystem struct {
	AbsFn          func(path string) (string, error)
	EvalSymlinksFn func(path string) (string, error)
	StatFn         func(name string) (os.FileInfo, error)
	CreateFn       func(name string) (*os.File, error)
	WriteFileFn    func(name string, data []byte, perm os.FileMode) error
	ReadFileFn     func(name string) ([]byte, error)
	MkdirAllFn     func(path string, perm os.FileMode) error
	WalkDirFn      func(root string, fn fs.WalkDirFunc) error
	OpenFn         func(name string) (*os.File, error)
}

var real OsFileSystem

// Abs calls AbsFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) Abs(path string) (string, error) {
	if m.AbsFn != nil {
		return m.AbsFn(path)
	}
	return real.Abs(path)
}

// EvalSymlinks calls EvalSymlinksFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) EvalSymlinks(path string) (string, error) {
	if m.EvalSymlinksFn != nil {
		return m.EvalSymlinksFn(path)
	}
	return real.EvalSymlinks(path)
}

// Stat calls StatFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	if m.StatFn != nil {
		return m.StatFn(name)
	}
	return real.Stat(name)
}

// Create calls CreateFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) Create(name string) (*os.File, error) {
	if m.CreateFn != nil {
		return m.CreateFn(name)
	}
	return real.Create(name)
}

// WriteFile calls WriteFileFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	if m.WriteFileFn != nil {
		return m.WriteFileFn(name, data, perm)
	}
	return real.WriteFile(name, data, perm)
}

// ReadFile calls ReadFileFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) ReadFile(name string) ([]byte, error) {
	if m.ReadFileFn != nil {
		return m.ReadFileFn(name)
	}
	return real.ReadFile(name)
}

// MkdirAll calls MkdirAllFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	if m.MkdirAllFn != nil {
		return m.MkdirAllFn(path, perm)
	}
	return real.MkdirAll(path, perm)
}

// WalkDir calls WalkDirFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	if m.WalkDirFn != nil {
		return m.WalkDirFn(root, fn)
	}
	return real.WalkDir(root, fn)
}

// Open calls OpenFn if set, otherwise delegates to OsFileSystem.
func (m *MockFileSystem) Open(name string) (*os.File, error) {
	if m.OpenFn != nil {
		return m.OpenFn(name)
	}
	return real.Open(name)
}

// Compile-time interface check.
var _ FileSystem = (*MockFileSystem)(nil)
