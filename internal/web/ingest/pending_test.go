package ingest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A draft belongs to the household that created it (ADR-00018): another
// household holding its token can neither read, consume nor cancel it, and its
// failed attempts leave the draft for the owner.
func TestPendingDraftBelongsToItsHousehold(t *testing.T) {
	p := newPendingDrafts(time.Now)
	token, err := p.add(householdA.ID, sampleDraft())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.get(householdB.ID, token); ok {
		t.Error("household B read household A's draft")
	}
	if _, ok := p.take(householdB.ID, token); ok {
		t.Error("household B took household A's draft")
	}
	p.discard(householdB.ID, token)
	p.put(householdB.ID, "elsewhere", sampleDraft())
	if _, ok := p.get(householdA.ID, "elsewhere"); ok {
		t.Error("a draft put by household B is readable by household A")
	}
	if _, ok := p.get(householdA.ID, token); !ok {
		t.Fatal("household B's refused attempts removed household A's draft")
	}
	if _, ok := p.take(householdA.ID, token); !ok {
		t.Error("the owner cannot take its own draft")
	}
}

// The same holds through the routes: B's request for A's token is a 404 like
// an unknown token, saves nothing, and A can still review and confirm.
func TestRoutesDoNotServeAnotherHouseholdsDraft(t *testing.T) {
	f := newFixture(t) // acts as householdA
	token := f.ingestText(t)
	asB := asMember(householdB)(f.h.routes())
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/" + token, http.StatusNotFound},
		{http.MethodPost, "/" + token + "/confirm", http.StatusNotFound},
		{http.MethodPost, "/" + token + "/retry", http.StatusNotFound},
		// Cancel answers every caller the same way; that it left A's draft in
		// place is asserted below.
		{http.MethodPost, "/" + token + "/cancel", http.StatusSeeOther},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		asB.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s as household B = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
	if len(f.store.recipes) != 0 {
		t.Error("household B's request saved something")
	}
	if rec := f.do(http.MethodGet, "/"+token, nil); rec.Code != http.StatusOK {
		t.Errorf("household A reviewing its own draft after B's attempts = %d, want 200", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm()); rec.Code != http.StatusSeeOther {
		t.Errorf("household A confirming its own draft = %d, want 303", rec.Code)
	}
}

// A request with no household never creates or reaches a draft.
func TestIngestRoutesRefuseARequestWithNoHousehold(t *testing.T) {
	f := newFixture(t)
	bare := f.h.routes()
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/"},
		{http.MethodGet, "/sometoken"},
		{http.MethodPost, "/sometoken/cancel"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader("text=Apple+pie"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		bare.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s with no household = %d, want 500", c.method, c.path, rec.Code)
		}
	}
	if len(f.h.pending.entries) != 0 {
		t.Error("a request with no household left a pending draft")
	}
}
