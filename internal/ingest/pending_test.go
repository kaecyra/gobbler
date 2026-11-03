package ingest

import (
	"testing"
	"time"
)

// A draft belongs to the household that created it (ADR-00018): another
// household holding its token can neither read, consume nor cancel it, and its
// failed attempts leave the draft for the owner.
func TestPendingDraftBelongsToItsHousehold(t *testing.T) {
	p := NewPending(time.Now)
	token, err := p.Add(1, Draft{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Get(2, token); ok {
		t.Error("household 2 read household 1's draft")
	}
	if _, ok := p.Take(2, token); ok {
		t.Error("household 2 took household 1's draft")
	}
	p.Discard(2, token)
	if _, ok := p.Get(1, token); !ok {
		t.Fatal("household 2's refused attempts removed household 1's draft")
	}
	if _, ok := p.Take(1, token); !ok {
		t.Error("the owner cannot take its own draft")
	}
	if _, ok := p.Get(1, token); ok {
		t.Error("a taken draft is still there")
	}
}

func TestPendingDraftsExpireAndAreBounded(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	p := NewPending(func() time.Time { return now })
	old, _ := p.Add(1, Draft{})
	now = now.Add(pendingTTL + time.Second)
	if _, ok := p.Get(1, old); ok {
		t.Error("a draft outlived its TTL")
	}
	first, _ := p.Add(1, Draft{})
	for i := 0; i < pendingMax; i++ {
		now = now.Add(time.Second)
		if _, err := p.Add(1, Draft{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := p.Get(1, first); ok {
		t.Error("the oldest draft was kept past the bound")
	}
	if p.Len() != pendingMax {
		t.Errorf("held %d drafts, want the bound %d", p.Len(), pendingMax)
	}
}
