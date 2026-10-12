package ingest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

// Pending drafts live only in memory, between the ingest request and the
// confirm or cancel that ends it. A draft holds the source page, which can be
// large, so the table is bounded and entries expire. A restart drops them:
// nothing was saved, and ingesting again is the recovery (ADR-00006).
const (
	pendingTTL = time.Hour
	// pendingMax bounds memory: the oldest draft is dropped to admit a new one.
	pendingMax = 20
)

type pendingEntry struct {
	draft   ingestcore.Draft
	created time.Time
}

// pendingDrafts is a token-keyed table of drafts awaiting review.
type pendingDrafts struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]pendingEntry
}

func newPendingDrafts(now func() time.Time) *pendingDrafts {
	return &pendingDrafts{now: now, entries: map[string]pendingEntry{}}
}

// add stores d and returns the token that retrieves it.
func (p *pendingDrafts) add(d ingestcore.Draft) (string, error) {
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
	p.entries[token] = pendingEntry{draft: d, created: p.now()}
	return token, nil
}

// put stores d under an existing token, for a draft that was taken and has to
// go back after a failed confirm. The age restarts so a user fixing a draft is
// not timed out mid-edit.
func (p *pendingDrafts) put(token string, d ingestcore.Draft) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[token] = pendingEntry{draft: d, created: p.now()}
}

// get returns the draft for token without removing it.
func (p *pendingDrafts) get(token string) (ingestcore.Draft, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	e, ok := p.entries[token]
	return e.draft, ok
}

// take removes and returns the draft for token. Confirming takes it, so two
// racing confirms cannot both save.
func (p *pendingDrafts) take(token string) (ingestcore.Draft, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	e, ok := p.entries[token]
	delete(p.entries, token)
	return e.draft, ok
}

func (p *pendingDrafts) discard(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, token)
}

// expire drops entries older than pendingTTL; the caller holds mu.
func (p *pendingDrafts) expire() {
	cutoff := p.now().Add(-pendingTTL)
	for k, e := range p.entries {
		if e.created.Before(cutoff) {
			delete(p.entries, k)
		}
	}
}

// dropOldest removes the oldest entry; the caller holds mu.
func (p *pendingDrafts) dropOldest() {
	var oldest string
	var at time.Time
	for k, e := range p.entries {
		if oldest == "" || e.created.Before(at) {
			oldest, at = k, e.created
		}
	}
	delete(p.entries, oldest)
}
