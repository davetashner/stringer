// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoKey struct {
	root  string
	depth int
}

func TestHistoryMemo_HitAfterMiss(t *testing.T) {
	var m historyMemo[memoKey, string]
	var calls int32
	walk := func(context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "history", nil
	}
	key := memoKey{root: "/repo", depth: 1000}

	v, err := m.load(context.Background(), key, walk)
	require.NoError(t, err)
	assert.Equal(t, "history", v)

	v, err = m.load(context.Background(), key, walk)
	require.NoError(t, err)
	assert.Equal(t, "history", v)

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "second load must reuse the first walk")
	hits, misses := m.stats()
	assert.Equal(t, 1, hits)
	assert.Equal(t, 1, misses)
}

func TestHistoryMemo_DifferentKeyEvicts(t *testing.T) {
	var m historyMemo[memoKey, int]
	var calls int32
	walk := func(context.Context) (int, error) {
		return int(atomic.AddInt32(&calls, 1)), nil
	}
	a := memoKey{root: "/repo", depth: 1000}
	b := memoKey{root: "/repo", depth: 50}

	v, err := m.load(context.Background(), a, walk)
	require.NoError(t, err)
	assert.Equal(t, 1, v)

	v, err = m.load(context.Background(), b, walk)
	require.NoError(t, err)
	assert.Equal(t, 2, v, "a different depth must trigger its own walk")

	// The memo holds a single entry: returning to key a walks again.
	v, err = m.load(context.Background(), a, walk)
	require.NoError(t, err)
	assert.Equal(t, 3, v, "the entry for a was evicted by b")

	hits, misses := m.stats()
	assert.Equal(t, 0, hits)
	assert.Equal(t, 3, misses)
}

func TestHistoryMemo_ResetForcesWalk(t *testing.T) {
	var m historyMemo[memoKey, int]
	var calls int32
	walk := func(context.Context) (int, error) {
		return int(atomic.AddInt32(&calls, 1)), nil
	}
	key := memoKey{root: "/repo"}

	_, err := m.load(context.Background(), key, walk)
	require.NoError(t, err)
	m.reset()
	hits, misses := m.stats()
	assert.Zero(t, hits)
	assert.Zero(t, misses)

	v, err := m.load(context.Background(), key, walk)
	require.NoError(t, err)
	assert.Equal(t, 2, v)
}

func TestHistoryMemo_ErrorNotRetained(t *testing.T) {
	var m historyMemo[memoKey, int]
	var calls int32
	walk := func(context.Context) (int, error) {
		n := int(atomic.AddInt32(&calls, 1))
		if n == 1 {
			return 0, errors.New("boom")
		}
		return n, nil
	}
	key := memoKey{root: "/repo"}

	_, err := m.load(context.Background(), key, walk)
	require.EqualError(t, err, "boom")

	v, err := m.load(context.Background(), key, walk)
	require.NoError(t, err)
	assert.Equal(t, 2, v, "a failed walk must be retried by the next caller")
}

func TestHistoryMemo_CancelledContextSkipsWalk(t *testing.T) {
	var m historyMemo[memoKey, int]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.load(ctx, memoKey{root: "/repo"}, func(context.Context) (int, error) {
		t.Fatal("walk must not run with a cancelled context")
		return 0, nil
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestHistoryMemo_ConcurrentSameKeyWalksOnce(t *testing.T) {
	var m historyMemo[memoKey, int]
	var calls int32
	release := make(chan struct{})
	walk := func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return 42, nil
	}
	key := memoKey{root: "/repo"}

	const n = 16
	results := make([]int, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = m.load(context.Background(), key, walk)
		}(i)
	}
	close(release)
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		assert.Equal(t, 42, results[i])
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "concurrent callers must share one walk")
	hits, misses := m.stats()
	assert.Equal(t, 1, misses)
	assert.Equal(t, n-1, hits)
}

func TestHistoryMemo_WaiterRetriesAfterOtherCallerCancelled(t *testing.T) {
	var m historyMemo[memoKey, int]
	key := memoKey{root: "/repo"}

	started := make(chan struct{})
	proceed := make(chan struct{})
	var calls int32
	walk := func(ctx context.Context) (int, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			close(started)
			<-proceed
			return 0, ctx.Err() // first walker was cancelled mid-walk
		}
		return 7, nil
	}

	firstCtx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := m.load(firstCtx, key, walk)
		firstDone <- err
	}()
	<-started

	// Second caller finds the in-flight entry and waits on it.
	secondDone := make(chan int, 1)
	go func() {
		v, err := m.load(context.Background(), key, walk)
		assert.NoError(t, err)
		secondDone <- v
	}()

	cancel()
	close(proceed)

	assert.ErrorIs(t, <-firstDone, context.Canceled)
	assert.Equal(t, 7, <-secondDone, "waiter must retry with its own live context")
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestIsContextError(t *testing.T) {
	assert.True(t, isContextError(context.Canceled))
	assert.True(t, isContextError(context.DeadlineExceeded))
	assert.False(t, isContextError(errors.New("other")))
	assert.False(t, isContextError(nil))
}
