// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"errors"
	"sync"
)

// historyMemo is a process-wide, single-entry memo for an expensive
// repository-wide walk (git log, git log --numstat). In a monorepo scan the
// pipeline runs once per workspace and every workspace hands the git-history
// collectors the same repository root, so without a memo the same history is
// walked once per workspace. The first caller for a key performs the walk;
// concurrent and later callers for the same key reuse the result. A call with
// a different key evicts the previous entry, so at most one walk is retained
// (stringer-jfh.5).
//
// K must carry every option that changes what the walk sees (repository root,
// HEAD, depth, since window). Values are shared between callers and must be
// treated as read-only.
type historyMemo[K comparable, V any] struct {
	mu     sync.Mutex
	key    K
	entry  *memoEntry[V]
	hits   int
	misses int
}

// memoEntry is one in-flight or completed walk. once guarantees a single walk
// per entry even when several collectors race for the same key.
type memoEntry[V any] struct {
	once sync.Once
	val  V
	err  error
}

// load returns the value for key, running walk at most once per key. A walk
// that fails is not retained: the next caller retries it. When the walk that
// a caller waited on was aborted by another caller's context, the waiting
// caller retries once with its own context so an unrelated cancellation does
// not fail this collector.
func (m *historyMemo[K, V]) load(ctx context.Context, key K, walk func(context.Context) (V, error)) (V, error) {
	var zero V
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	e, hit := m.entryFor(key)
	e.once.Do(func() { e.val, e.err = walk(ctx) })

	if e.err != nil && hit && isContextError(e.err) && ctx.Err() == nil {
		m.evict(e)
		e, _ = m.entryFor(key)
		e.once.Do(func() { e.val, e.err = walk(ctx) })
	}
	if e.err != nil {
		m.evict(e)
		return zero, e.err
	}
	return e.val, nil
}

// entryFor returns the entry for key, creating it (and evicting any entry for
// another key) when absent. The second result reports whether an entry for
// key already existed.
func (m *historyMemo[K, V]) entryFor(key K) (*memoEntry[V], bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entry != nil && m.key == key {
		m.hits++
		return m.entry, true
	}
	m.misses++
	m.key = key
	m.entry = &memoEntry[V]{}
	return m.entry, false
}

// evict drops e if it is still the retained entry. Entries are compared by
// identity so a newer entry for the same key is never evicted by a stale
// caller.
func (m *historyMemo[K, V]) evict(e *memoEntry[V]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entry == e {
		m.entry = nil
	}
}

// reset drops the retained entry and zeroes the hit/miss counters. Tests use
// it to force a fresh walk.
func (m *historyMemo[K, V]) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	var zero K
	m.key = zero
	m.entry = nil
	m.hits = 0
	m.misses = 0
}

// stats returns the number of lookups that found an entry for their key and
// the number that had to create one.
func (m *historyMemo[K, V]) stats() (hits, misses int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hits, m.misses
}

// isContextError reports whether err stems from a cancelled or expired
// context.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
