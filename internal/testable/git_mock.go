package testable

import (
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// MockGitOpener is a test double for GitOpener.
// Set OpenFunc to control PlainOpen behavior. If nil, PlainOpen returns
// the Repo field (or ErrRepositoryNotExists if Repo is nil).
type MockGitOpener struct {
	// Repo is the repository returned by PlainOpen when OpenFunc is nil.
	Repo GitRepository

	// OpenErr is the error returned by PlainOpen when OpenFunc is nil.
	OpenErr error

	// OpenFunc, if set, is called instead of using Repo/OpenErr.
	OpenFunc func(path string) (GitRepository, error)

	// OpenCalls records the paths passed to PlainOpen.
	OpenCalls []string
}

// PlainOpen records the call and delegates to OpenFunc or returns Repo/OpenErr.
func (m *MockGitOpener) PlainOpen(path string) (GitRepository, error) {
	m.OpenCalls = append(m.OpenCalls, path)
	if m.OpenFunc != nil {
		return m.OpenFunc(path)
	}
	if m.OpenErr != nil {
		return nil, m.OpenErr
	}
	if m.Repo != nil {
		return m.Repo, nil
	}
	return nil, git.ErrRepositoryNotExists
}

// MockGitRepository is a test double for GitRepository.
// Each method has a corresponding field for the return value and error.
type MockGitRepository struct {
	// HeadRef is returned by Head().
	HeadRef *plumbing.Reference
	// HeadErr is the error returned by Head().
	HeadErr error

	// LogIter is returned by Log().
	LogIter object.CommitIter
	// LogErr is the error returned by Log().
	LogErr error
	// LogCalls records LogOptions passed to Log().
	LogCalls []*git.LogOptions

	// RemotesList is returned by Remotes().
	RemotesList []*git.Remote
	// RemotesErr is the error returned by Remotes().
	RemotesErr error

	// ReferencesIter is returned by References().
	ReferencesIter storer.ReferenceIter
	// ReferencesErr is the error returned by References().
	ReferencesErr error

	// CommitObjects maps hashes to commits for CommitObject().
	CommitObjects map[plumbing.Hash]*object.Commit
	// CommitObjectErr is the default error returned by CommitObject() when
	// the hash is not found in CommitObjects.
	CommitObjectErr error

	// TagObjects maps hashes to tags for TagObject().
	TagObjects map[plumbing.Hash]*object.Tag
	// TagObjectErr is the default error returned by TagObject() when
	// the hash is not found in TagObjects.
	TagObjectErr error

	// TagsIter is returned by Tags().
	TagsIter storer.ReferenceIter
	// TagsErr is the error returned by Tags().
	TagsErr error
}

// Head returns HeadRef and HeadErr.
func (m *MockGitRepository) Head() (*plumbing.Reference, error) {
	return m.HeadRef, m.HeadErr
}

// Log records the call and returns LogIter and LogErr.
func (m *MockGitRepository) Log(opts *git.LogOptions) (object.CommitIter, error) {
	m.LogCalls = append(m.LogCalls, opts)
	return m.LogIter, m.LogErr
}

// Remotes returns RemotesList and RemotesErr.
func (m *MockGitRepository) Remotes() ([]*git.Remote, error) {
	return m.RemotesList, m.RemotesErr
}

// References returns ReferencesIter and ReferencesErr.
func (m *MockGitRepository) References() (storer.ReferenceIter, error) {
	return m.ReferencesIter, m.ReferencesErr
}

// CommitObject looks up the hash in CommitObjects, falling back to CommitObjectErr.
func (m *MockGitRepository) CommitObject(h plumbing.Hash) (*object.Commit, error) {
	if m.CommitObjects != nil {
		if c, ok := m.CommitObjects[h]; ok {
			return c, nil
		}
	}
	if m.CommitObjectErr != nil {
		return nil, m.CommitObjectErr
	}
	return nil, plumbing.ErrObjectNotFound
}

// TagObject looks up the hash in TagObjects, falling back to TagObjectErr.
func (m *MockGitRepository) TagObject(h plumbing.Hash) (*object.Tag, error) {
	if m.TagObjects != nil {
		if t, ok := m.TagObjects[h]; ok {
			return t, nil
		}
	}
	if m.TagObjectErr != nil {
		return nil, m.TagObjectErr
	}
	return nil, plumbing.ErrObjectNotFound
}

// Tags returns TagsIter and TagsErr.
func (m *MockGitRepository) Tags() (storer.ReferenceIter, error) {
	return m.TagsIter, m.TagsErr
}

// Compile-time interface checks.
var _ GitOpener = (*MockGitOpener)(nil)
var _ GitRepository = (*MockGitRepository)(nil)
