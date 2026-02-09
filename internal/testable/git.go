// Package testable provides interfaces for mocking external dependencies
// such as go-git operations. Production code uses the Real* implementations;
// tests can inject mock implementations to avoid hitting real git repos.
package testable

import (
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// GitOpener abstracts opening a git repository. Production code uses
// RealGitOpener; tests inject a mock to avoid filesystem dependencies.
type GitOpener interface {
	PlainOpen(path string) (GitRepository, error)
}

// GitRepository abstracts the subset of *git.Repository methods used by
// stringer. This keeps the interface minimal and easy to mock.
type GitRepository interface {
	Head() (*plumbing.Reference, error)
	Log(opts *git.LogOptions) (object.CommitIter, error)
	Remotes() ([]*git.Remote, error)
	References() (storer.ReferenceIter, error)
	CommitObject(h plumbing.Hash) (*object.Commit, error)
	TagObject(h plumbing.Hash) (*object.Tag, error)
	Tags() (storer.ReferenceIter, error)
}

// RealGitOpener is the production implementation of GitOpener.
// It delegates to git.PlainOpen.
type RealGitOpener struct{}

// PlainOpen opens a git repository at path and returns a GitRepository.
func (RealGitOpener) PlainOpen(path string) (GitRepository, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil, err
	}
	return &RealGitRepository{repo: repo}, nil
}

// RealGitRepository wraps *git.Repository to satisfy GitRepository.
type RealGitRepository struct {
	repo *git.Repository
}

// Head returns the reference where HEAD is pointing to.
func (r *RealGitRepository) Head() (*plumbing.Reference, error) {
	return r.repo.Head()
}

// Log returns the commit history from the current HEAD following the given options.
func (r *RealGitRepository) Log(opts *git.LogOptions) (object.CommitIter, error) {
	return r.repo.Log(opts)
}

// Remotes returns a list of remotes in a repository.
func (r *RealGitRepository) Remotes() ([]*git.Remote, error) {
	return r.repo.Remotes()
}

// References returns an unsorted ReferenceIter for all references.
func (r *RealGitRepository) References() (storer.ReferenceIter, error) {
	return r.repo.References()
}

// CommitObject returns the commit with the given hash.
func (r *RealGitRepository) CommitObject(h plumbing.Hash) (*object.Commit, error) {
	return r.repo.CommitObject(h)
}

// TagObject returns the tag with the given hash.
func (r *RealGitRepository) TagObject(h plumbing.Hash) (*object.Tag, error) {
	return r.repo.TagObject(h)
}

// Tags returns all tag References in a repository.
func (r *RealGitRepository) Tags() (storer.ReferenceIter, error) {
	return r.repo.Tags()
}

// DefaultGitOpener is the production GitOpener used as default.
var DefaultGitOpener GitOpener = RealGitOpener{}

// Compile-time interface checks.
var _ GitOpener = RealGitOpener{}
var _ GitRepository = (*RealGitRepository)(nil)
