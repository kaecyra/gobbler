package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/store"
)

// restart is the same handler as after a restart of the process: the proposals
// table is the database's, and nothing held in memory survives.
func (f *fixture) restart() {
	f.h.drafts = ingestcore.NewDrafts(store.New(f.db))
	f.h.photoCache = ingestcore.NewPhotos(time.Now)
}

// ADR-00035 obligation 14, form half: a household holds 20 drafts waiting for
// review, the 21st is refused on the form with a message saying so, none is
// dropped, and another household is unaffected.
func TestTheTwentyFirstDraftIsRefusedOnTheForm(t *testing.T) {
	f := newFixture(t)
	var tokens []string
	for i := 0; i < store.MaxPendingDrafts; i++ {
		tokens = append(tokens, f.ingestText(t))
	}
	rec := f.do(http.MethodPost, "/", url.Values{"text": {"One more pie"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("21st draft status = %d, want 409; body: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "drafts are waiting for review") || !strings.Contains(body, "One more pie") {
		t.Errorf("the refusal lacks its message or loses the input: %s", body)
	}
	if strings.Contains(body, "Review recipe") {
		t.Error("a draft was shown for a refused ingest")
	}
	if n := f.pendingDrafts(t); n != store.MaxPendingDrafts {
		t.Errorf("%d drafts are pending, want %d: none dropped, none added", n, store.MaxPendingDrafts)
	}
	for i, tok := range tokens {
		if f.do(http.MethodGet, "/"+tok, nil).Code != http.StatusOK {
			t.Errorf("draft %d can no longer be reviewed", i+1)
		}
	}
	// Another household's request goes through.
	asB := asMember(householdB)(f.h.routes())
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{"text": {"Their pie"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out := httptest.NewRecorder()
	asB.ServeHTTP(out, req)
	if out.Code != http.StatusSeeOther {
		t.Errorf("household B's first draft = %d, want 303", out.Code)
	}
	// Deciding makes room.
	if rec := f.do(http.MethodPost, "/"+tokens[0]+"/cancel", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("cancel = %d", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/", url.Values{"text": {"Room again"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("a draft after one was discarded = %d, want 303", rec.Code)
	}
}

func TestAPhotoDraftPastTheLimitIsRefusedOnThePhotoForm(t *testing.T) {
	f := newPhotoFixture(t, &fakeReader{text: "Apple pie\n2 tbsp butter"})
	for i := 0; i < store.MaxPendingDrafts; i++ {
		if rec := f.upload(t, "page.png", pngBytes); rec.Code != http.StatusSeeOther {
			t.Fatalf("photo %d = %d", i+1, rec.Code)
		}
	}
	rec := f.upload(t, "page.png", pngBytes)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "drafts are waiting for review") {
		t.Errorf("21st photo = %d, body: %s", rec.Code, rec.Body)
	}
}

// A draft is in the database, not in the process: after a restart the review
// screen still has it, and confirming it saves the recipe.
func TestADraftSurvivesARestart(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	f.restart()
	rec := f.do(http.MethodGet, "/"+token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Apple pie") {
		t.Fatalf("review after a restart = %d: %s", rec.Code, rec.Body)
	}
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm()); rec.Code != http.StatusSeeOther {
		t.Fatalf("confirm after a restart = %d: %s", rec.Code, rec.Body)
	}
	if len(f.store.recipes) != 1 {
		t.Errorf("%d recipes saved, want 1", len(f.store.recipes))
	}
}

// ocrDraft is a draft read from a photo: its source is the OCR text.
func ocrDraft() ingestcore.Draft {
	d := sampleDraft()
	content := []byte("Apple pie\n2 tbsp butter")
	sum := sha256.Sum256(content)
	d.Source = ingestcore.Source{Kind: store.SnapshotOCR, Content: content, SHA256: hex.EncodeToString(sum[:])}
	return d
}

// ADR-00035 obligation 15. A photo is held in memory only. After it has left
// memory the draft is whole: it can be confirmed, asking the LLM again reads the
// recognised text alone, and the review screen says the photo is not available.
func TestADraftFromAPhotoWhoseImageHasLeftMemory(t *testing.T) {
	f := newPhotoFixture(t, &fakeReader{text: "Apple pie\n2 tbsp butter"})
	f.drafter.draft = ocrDraft()
	out := llmDraft()
	out.Source = ocrDraft().Source
	retrier := &fakeRetrier{configured: true, out: out}
	f.h.retrier = retrier

	rec := f.upload(t, "page.png", pngBytes)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body)
	}
	token := strings.TrimPrefix(rec.Header().Get("Location"), "/ingest/")

	// While the photo is in memory the LLM is shown it, and nothing is said.
	if body := f.do(http.MethodGet, "/"+token, nil).Body.String(); strings.Contains(body, "photo-gone") {
		t.Error("the review screen says the photo is gone while it is still in memory")
	}
	if rec := f.do(http.MethodPost, "/"+token+"/retry", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("retry = %d: %s", rec.Code, rec.Body)
	}
	if got := retrier.got[0]; !bytes.Equal(got.Source.Image, pngBytes) || got.Source.ImageMIME != "image/png" {
		t.Errorf("retry saw image %d bytes (%q), want the uploaded photo", len(got.Source.Image), got.Source.ImageMIME)
	}

	// The photo leaves memory.
	f.restart()
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if !strings.Contains(body, `id="photo-gone"`) || !strings.Contains(body, "no longer available") {
		t.Errorf("the review screen does not say the photo is gone: %s", body)
	}
	if rec := f.do(http.MethodPost, "/"+token+"/retry", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("retry without the photo = %d: %s", rec.Code, rec.Body)
	}
	got := retrier.got[1]
	if len(got.Source.Image) != 0 {
		t.Errorf("retry saw a photo of %d bytes after it left memory", len(got.Source.Image))
	}
	if string(got.Source.Content) != "Apple pie\n2 tbsp butter" || got.Source.Kind != store.SnapshotOCR {
		t.Errorf("retry source = %s %q, want the OCR text", got.Source.Kind, got.Source.Content)
	}

	// And it can be confirmed.
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm()); rec.Code != http.StatusSeeOther {
		t.Fatalf("confirm without the photo = %d: %s", rec.Code, rec.Body)
	}
	if len(f.store.recipes) != 1 {
		t.Errorf("%d recipes saved, want 1", len(f.store.recipes))
	}
}

// A draft of pasted text has no photo to lose, so it never says so.
func TestAPastedDraftNeverSaysThePhotoIsGone(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	if body := f.do(http.MethodGet, "/"+token, nil).Body.String(); strings.Contains(body, "photo-gone") {
		t.Error("a draft of a web page says its photo is gone")
	}
}
