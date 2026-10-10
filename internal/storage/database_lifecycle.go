package storage

import (
	"context"
	"sync"
)

type databaseLifecycleLock struct {
	token      chan struct{}
	references int
}

var databaseLifecycleLocks = struct {
	sync.Mutex
	entries map[string]*databaseLifecycleLock
}{entries: make(map[string]*databaseLifecycleLock)}

// LockDatabaseLifecycle serializes snapshots with source deletion in this process.
// Waiting respects cancellation, and unused lock entries do not accumulate.
func LockDatabaseLifecycle(ctx context.Context, owner, name string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := owner + "\x00" + name
	databaseLifecycleLocks.Lock()
	entry := databaseLifecycleLocks.entries[key]
	if entry == nil {
		entry = &databaseLifecycleLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		databaseLifecycleLocks.entries[key] = entry
	}
	entry.references++
	databaseLifecycleLocks.Unlock()
	dropReference := func() {
		databaseLifecycleLocks.Lock()
		entry.references--
		if entry.references == 0 {
			delete(databaseLifecycleLocks.entries, key)
		}
		databaseLifecycleLocks.Unlock()
	}
	select {
	case <-ctx.Done():
		dropReference()
		return nil, ctx.Err()
	case <-entry.token:
	}
	var once sync.Once
	release := func() { once.Do(func() { entry.token <- struct{}{}; dropReference() }) }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
