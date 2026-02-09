// Package testable provides interfaces for abstracting OS-level operations,
// enabling mock injection in tests without modifying production behavior.
package testable

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FileSystem abstracts file system operations to enable mock injection in tests.
// The production implementation (OsFileSystem) delegates to the standard library.
type FileSystem interface {
	// Abs returns an absolute representation of path.
	Abs(path string) (string, error)

	// EvalSymlinks returns the path name after the evaluation of any symbolic links.
	EvalSymlinks(path string) (string, error)

	// Stat returns a FileInfo describing the named file.
	Stat(name string) (os.FileInfo, error)

	// Create creates or truncates the named file.
	Create(name string) (*os.File, error)

	// WriteFile writes data to the named file, creating it if necessary.
	WriteFile(name string, data []byte, perm os.FileMode) error

	// ReadFile reads the named file and returns the contents.
	ReadFile(name string) ([]byte, error)

	// MkdirAll creates a directory named path, along with any necessary parents.
	MkdirAll(path string, perm os.FileMode) error

	// WalkDir walks the file tree rooted at root, calling fn for each file or
	// directory in the tree, including root.
	WalkDir(root string, fn fs.WalkDirFunc) error

	// Open opens the named file for reading.
	Open(name string) (*os.File, error)
}

// OsFileSystem is the production implementation of FileSystem that delegates
// to the standard library os and filepath packages.
type OsFileSystem struct{}

// Abs wraps filepath.Abs.
func (OsFileSystem) Abs(path string) (string, error) {
	return filepath.Abs(path)
}

// EvalSymlinks wraps filepath.EvalSymlinks.
func (OsFileSystem) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// Stat wraps os.Stat.
func (OsFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Create wraps os.Create.
func (OsFileSystem) Create(name string) (*os.File, error) {
	return os.Create(name) //nolint:gosec // caller controls path
}

// WriteFile wraps os.WriteFile.
func (OsFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm) //nolint:gosec // caller controls path and perms
}

// ReadFile wraps os.ReadFile.
func (OsFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name) //nolint:gosec // caller controls path
}

// MkdirAll wraps os.MkdirAll.
func (OsFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// WalkDir wraps filepath.WalkDir.
func (OsFileSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)
}

// Open wraps os.Open.
func (OsFileSystem) Open(name string) (*os.File, error) {
	return os.Open(name) //nolint:gosec // caller controls path
}

// DefaultFS is the production FileSystem used as the default throughout
// the application. All packages should use this as their default when no
// custom FileSystem is injected.
var DefaultFS FileSystem = OsFileSystem{}
