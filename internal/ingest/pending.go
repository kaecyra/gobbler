package ingest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Pending drafts live only in memory, between the request that made them and
// the confirm or cancel that ends it. A draft holds the source page, which can
// be large, so the table is bounded and entries expire. A restart drops them:
// nothing was saved, and ingesting again is the recovery (ADR-00006).
//
// Every source of drafts shares one table, so the review screen finds a draft
// whoever made it: a person on the ingest form, or an agent over MCP
// (ADR-00034). A draft belongs to the household that made it (ADR-00018):
// every access names a household, and a token held by another household finds
// nothing, as if it were unknown. The refused access leaves the draft where it
// is, so a stranger cannot cancel or consume it.
const (
	pendingTTL = time.Hour
	// pendingMax bounds memory: the oldest draft is dropped to admit a new one.
	pendingMax = 20
)

type pendingEntry struct {
	draft     Draft
	created   time.Time
	household int64
}

// Pending is a token-keyed table of drafts awaiting review. It is safe for
// concurrent use.
type Pending struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]pendingEntry
}

// NewPending returns an empty table that reads the time from now.
func NewPending(now func() time.Time) *Pending {
	return &Pending{now: now, entries: map[string]pendingEntry{}}
}

// Add stores d for household hid and returns the token that retrieves it.
func (p *Pending) Add(hid int64, d Draft) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate draft token: %w", err)
	}
	token := hex.EncodeToString(raw[:])
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	for len(p.entries) >= pendingMax {
		p.dropOldest()
	}
	p.entries[token] = pendingEntry{draft: d, created: p.now(), household: hid}
	return token, nil
}

// Put stores d under an existing token, for a draft that was taken and has to
// go back after a failed confirm. The age restarts so a user fixing a draft is
// not timed out mid-edit.
func (p *Pending) Put(hid int64, token string, d Draft) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[token] = pendingEntry{draft: d, created: p.now(), household: hid}
}

// Get returns the draft for token without removing it, if household hid owns it.
func (p *Pending) Get(hid int64, token string) (Draft, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	e, ok := p.entries[token]
	if !ok || e.household != hid {
		return Draft{}, false
	}
	return e.draft, true
}

// Take removes and returns the draft for token. Confirming takes it, so two
// racing confirms cannot both save. Another household's draft is left alone.
func (p *Pending) Take(hid int64, token string) (Draft, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	e, ok := p.entries[token]
	if !ok || e.household != hid {
		return Draft{}, false
	}
	delete(p.entries, token)
	return e.draft, true
}

// Discard drops household hid's draft for token; another household's is kept.
func (p *Pending) Discard(hid int64, token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[token]; ok && e.household == hid {
		delete(p.entries, token)
	}
}

// expire drops entries older than pendingTTL; the caller holds mu.
func (p *Pending) expire() {
	cutoff := p.now().Add(-pendingTTL)
	for k, e := range p.entries {
		if e.created.Before(cutoff) {
			delete(p.entries, k)
		}
	}
}

// dropOldest removes the oldest entry; the caller holds mu.
func (p *Pending) dropOldest() {
	var oldest string
	var at time.Time
	for k, e := range p.entries {
		if oldest == "" || e.created.Before(at) {
			oldest, at = k, e.created
		}
	}
	delete(p.entries, oldest)
}

// Len is how many drafts are held, expired ones not yet dropped included.
func (p *Pending) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}
