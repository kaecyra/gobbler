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
//
// A draft belongs to the household whose member created it (ADR-00018): every
// access names a household, and a token held by another household finds
// nothing, as if it were unknown. The refused access leaves the draft where it
// is, so a stranger cannot cancel or consume it.
const (
	pendingTTL = time.Hour
	// pendingMax bounds memory: the oldest draft is dropped to admit a new one.
	pendingMax = 20
)

type pendingEntry struct {
	draft     ingestcore.Draft
	created   time.Time
	household int64
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

// add stores d for household hid and returns the token that retrieves it.
func (p *pendingDrafts) add(hid int64, d ingestcore.Draft) (string, error) {
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

// put stores d under an existing token, for a draft that was taken and has to
// go back after a failed confirm. The age restarts so a user fixing a draft is
// not timed out mid-edit.
func (p *pendingDrafts) put(hid int64, token string, d ingestcore.Draft) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[token] = pendingEntry{draft: d, created: p.now(), household: hid}
}

// get returns the draft for token without removing it, if household hid owns it.
func (p *pendingDrafts) get(hid int64, token string) (ingestcore.Draft, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	e, ok := p.entries[token]
	if !ok || e.household != hid {
		return ingestcore.Draft{}, false
	}
	return e.draft, true
}

// take removes and returns the draft for token. Confirming takes it, so two
// racing confirms cannot both save. Another household's draft is left alone.
func (p *pendingDrafts) take(hid int64, token string) (ingestcore.Draft, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	e, ok := p.entries[token]
	if !ok || e.household != hid {
		return ingestcore.Draft{}, false
	}
	delete(p.entries, token)
	return e.draft, true
}

// discard drops household hid's draft for token; another household's is kept.
func (p *pendingDrafts) discard(hid int64, token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[token]; ok && e.household == hid {
		delete(p.entries, token)
	}
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
