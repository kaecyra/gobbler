package picture

import "sync"

// keyedLock is a mutex per key, for one process. gobbler is one binary over one
// SQLite file, so a process-wide lock is a lock.
type keyedLock struct {
	mu    sync.Mutex
	locks map[int64]*entry
}

type entry struct {
	mu   sync.Mutex
	refs int
}

// lock blocks until key is free and returns the function that frees it.
func (k *keyedLock) lock(key int64) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[int64]*entry{}
	}
	e := k.locks[key]
	if e == nil {
		e = &entry{}
		k.locks[key] = e
	}
	e.refs++
	k.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		k.mu.Lock()
		if e.refs--; e.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
