package ingest

import (
	"sync"
	"time"
)

// Photos holds, in memory only, the photos behind drafts read from a picture
// (ADR-00006, ADR-00035). A durable draft stores the OCR text, which is its
// source; the photo itself is only kept so that asking the LLM again can show
// it the page, and it is never written to disk. A photo expires after an hour,
// is dropped when the draft ends, and is lost to a restart. A draft whose photo
// is gone is still whole: asking the LLM again then reads the OCR text alone,
// and the review screen says the photo is no longer available.
const (
	photoTTL = time.Hour
	// photoMax bounds memory across every household. Past it the oldest photo
	// is dropped, which only costs that draft its vision retry.
	photoMax = 50
)

type photoEntry struct {
	image []byte
	mime  string
	at    time.Time
}

// Photos is a token-keyed table of photos. It is safe for concurrent use.
type Photos struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]photoEntry
}

// NewPhotos returns an empty table that reads the time from now.
func NewPhotos(now func() time.Time) *Photos {
	return &Photos{now: now, entries: map[string]photoEntry{}}
}

// Put keeps image for the draft with token.
func (p *Photos) Put(token string, image []byte, mime string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	for len(p.entries) >= photoMax {
		p.dropOldest()
	}
	p.entries[token] = photoEntry{image: image, mime: mime, at: p.now()}
}

// Get returns the photo for token and its media type, or false when it is gone.
// The caller has already shown the draft is the household's.
func (p *Photos) Get(token string) ([]byte, string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expire()
	e, ok := p.entries[token]
	return e.image, e.mime, ok
}

// Drop forgets the photo for token.
func (p *Photos) Drop(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, token)
}

// expire drops photos older than photoTTL; the caller holds mu.
func (p *Photos) expire() {
	cutoff := p.now().Add(-photoTTL)
	for k, e := range p.entries {
		if e.at.Before(cutoff) {
			delete(p.entries, k)
		}
	}
}

// dropOldest removes the oldest photo; the caller holds mu.
func (p *Photos) dropOldest() {
	var oldest string
	var at time.Time
	for k, e := range p.entries {
		if oldest == "" || e.at.Before(at) {
			oldest, at = k, e.at
		}
	}
	delete(p.entries, oldest)
}
