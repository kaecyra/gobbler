package ingest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/kaecyra/gobbler/internal/household"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// twoCandidates stands in for the picture service: every draft has two
// candidates, 1024 wide, named offerA and offerB.
type twoCandidates struct{}

// The offer IDs of twoCandidates, in the shape picture.ValidOfferID accepts.
const (
	offerA = "firstCandidateID000001"
	offerB = "secondCandidateID00002"
)

func (twoCandidates) FetchCandidates(context.Context, int64, []string) ([]picture.Candidate, error) {
	return nil, nil
}

func (twoCandidates) Candidates(context.Context, int64) ([]picture.Candidate, error) {
	return []picture.Candidate{{Offer: offerA, Position: 1, Width: 1024, Height: 341}, {Offer: offerB, Position: 2, Width: 1024, Height: 576}}, nil
}

func (twoCandidates) DeleteCandidates(context.Context, int64) error { return nil }

func (twoCandidates) Key(context.Context, int64) (string, bool, error) {
	return "recipes/5/abc.webp", true, nil
}

func (twoCandidates) Remove(context.Context, int64) error { return nil }

func (twoCandidates) Promote(context.Context, int64, string, int64) (string, error) { return "", nil }

// pickerFixture is a fixture whose drafts have two picture candidates.
func pickerFixture(t *testing.T) (*fixture, string, context.Context) {
	t.Helper()
	f := newFixture(t)
	f.rs.drafts = ingestcore.NewDrafts(store.New(f.db)).WithPictures(twoCandidates{})
	f.rs.pictures = twoCandidates{}
	token := f.ingestText(t)
	return f, token, household.WithContext(context.Background(), householdA)
}

func (f *fixture) chosen(ctx context.Context, t *testing.T, token string) ingestcore.PictureChoice {
	t.Helper()
	d, err := f.rs.drafts.Get(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	return d.Picture
}

func TestConfirmRefusesAPictureValueThatIsNotACandidate(t *testing.T) {
	// "1" and "2" are the candidates' positions, which no longer name them
	// (ADR-00052); the last is well formed but no candidate's ID.
	for _, value := range []string{"abc", "0", "-1", "1", "2", "1.5", "unknownCandidateID0000"} {
		t.Run(value, func(t *testing.T) {
			f, token, ctx := pickerFixture(t)
			rec := f.do(http.MethodPost, "/"+token+"/confirm", url.Values{"picture": {value}})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("picture=%q = %d, want 400", value, rec.Code)
			}
			if got := f.chosen(ctx, t, token); got != ingestcore.PictureUnchosen {
				t.Errorf("picture=%q left choice %q, want none made", value, got)
			}
		})
	}
}

func TestConfirmKeepsAChosenPictureOnADraftTheEditorRejects(t *testing.T) {
	for _, c := range []struct {
		value string
		want  ingestcore.PictureChoice
	}{{offerB, offerB}, {pictureNone, ingestcore.PictureNone}} {
		f, token, ctx := pickerFixture(t)
		// An empty form is rejected by the editor, so only the choice is kept.
		rec := f.do(http.MethodPost, "/"+token+"/confirm", url.Values{"picture": {c.value}})
		if rec.Code == http.StatusBadRequest || rec.Code == http.StatusInternalServerError {
			t.Fatalf("picture=%q = %d, want the form shown again", c.value, rec.Code)
		}
		if got := f.chosen(ctx, t, token); got != c.want {
			t.Errorf("picture=%q kept choice %q, want %q", c.value, got, c.want)
		}
	}
}

func TestChoosePictureOnADraftThatIsGone(t *testing.T) {
	f, _, ctx := pickerFixture(t)
	base := ingestcore.Draft{}
	if err := f.h.choosePicture(ctx, "no-such-token", offerA, &base); !errors.Is(err, errGone) {
		t.Errorf("choosing on an unknown draft = %v, want errGone", err)
	}
	if err := f.h.choosePicture(ctx, "no-such-token", "x", &base); !errors.Is(err, layout.ErrInvalid) {
		t.Errorf("a malformed value = %v, want ErrInvalid", err)
	}
}

func TestChoosePictureSkipsTheWriteWhenTheChoiceIsUnchanged(t *testing.T) {
	f, _, ctx := pickerFixture(t)
	// The token names no draft, so any write would be errGone.
	base := ingestcore.Draft{Picture: offerA}
	if err := f.h.choosePicture(ctx, "no-such-token", offerA, &base); err != nil {
		t.Errorf("an unchanged choice = %v, want no write and no error", err)
	}
	if err := f.h.choosePicture(ctx, "no-such-token", "", &base); err != nil || base.Picture != offerA {
		t.Errorf("no value = %v, picture %q; want the draft left alone", err, base.Picture)
	}
}

func TestPickerShowsCandidatesAndChoice(t *testing.T) {
	f, _, ctx := pickerFixture(t)
	two := []string{offerA, offerB}
	v, err := f.h.picker(ctx, ingestcore.Draft{Candidates: two, ProposalID: 7, Picture: offerB})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Options) != 2 || v.Options[0].Checked || !v.Options[1].Checked || v.NoneChecked || !v.Chosen {
		t.Errorf("picker with the second candidate chosen = %+v", v)
	}
	if o := v.Options[0]; o.Value != offerA || o.Src != "/pictures/drafts/7/"+offerA || o.Width != 1024 {
		t.Errorf("first option = %+v, want the candidate's ID, address and size", o)
	}
	if v, _ := f.h.picker(ctx, ingestcore.Draft{Candidates: two, ProposalID: 7}); v.Chosen {
		t.Errorf("an unchosen draft with candidates shows a choice: %+v", v)
	}
	if v, _ := f.h.picker(ctx, ingestcore.Draft{Candidates: two, ProposalID: 7, Picture: ingestcore.PictureNone}); !v.NoneChecked || !v.Chosen {
		t.Errorf("No picture chosen = %+v", v)
	}
	if v, _ := f.h.picker(ctx, ingestcore.Draft{}); len(v.Options) != 0 || !v.NoneChecked {
		t.Errorf("a draft with no candidates = %+v, want only No picture, chosen", v)
	}
	f.rs.pictures = nil
	if _, err := f.h.picker(ctx, ingestcore.Draft{Candidates: two}); err == nil {
		t.Error("candidates with no picture service gave no error")
	}
}
