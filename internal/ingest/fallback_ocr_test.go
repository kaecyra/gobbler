package ingest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/store"
)

var photoBytes = []byte("\x89PNG-not-really-a-photo")

const goodOCRText = "Fluffy Pancakes\n\nIngredients:\n- 2 cups all-purpose flour\n- 2 tbsp sugar\n- 2 eggs\n\nInstructions:\nWhisk the dry ingredients together in a large bowl.\n\nCook on a hot greased griddle until golden on both sides.\n"

func TestFromOCRBelowThresholdPassesTheImageToTheProvider(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{resp: llmAnswer()}
	d, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, messyText, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 1 {
		t.Fatalf("provider calls = %d, want 1", fp.count())
	}
	req := fp.calls[0]
	if !bytes.Equal(req.Image, photoBytes) || req.ImageMIME != "image/png" {
		t.Errorf("request image = %q (%s), want the photo", req.Image, req.ImageMIME)
	}
	if req.Text != messyText {
		t.Errorf("request text = %q, want the OCR text alongside the image", req.Text)
	}
	if !d.UsedLLM || d.Name.Value != "Glorp Stew" {
		t.Errorf("draft = %+v, want the provider's reading", d.Name)
	}
}

func TestFromOCRHighConfidenceMakesNoProviderCall(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{resp: llmAnswer()}
	d, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, goodOCRText, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if d.BelowThreshold || fp.count() != 0 || d.UsedLLM {
		t.Errorf("below = %v, provider calls = %d, used LLM = %v; want a rule-based draft and no call", d.BelowThreshold, fp.count(), d.UsedLLM)
	}
}

func TestFromOCRVisionFailureLeavesTheOCRDraftIntact(t *testing.T) {
	e := newEnv(t)
	rule, err := e.pipelineWith(t, testConfig(), nil).FromOCR(e.ctx, messyText, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.pipelineWith(t, testConfig(), &fakeProvider{err: context.DeadlineExceeded}).FromOCR(e.ctx, messyText, photoBytes, "image/png")
	if err != nil {
		t.Fatalf("a provider failure must not fail the ingest: %v", err)
	}
	if got.LLMError == "" || got.UsedLLM {
		t.Errorf("LLMError = %q, used LLM = %v; want the failure recorded and no LLM draft", got.LLMError, got.UsedLLM)
	}
	if got.Overall != rule.Overall || got.Name != rule.Name || len(got.Components) != len(rule.Components) {
		t.Error("the OCR-based draft changed")
	}
}

func TestRetryOnAPhotoDraftSendsTheImage(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipelineWith(t, testConfig(), nil).FromOCR(e.ctx, goodOCRText, photoBytes, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{resp: llmAnswer()}
	out, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 1 || !bytes.Equal(fp.calls[0].Image, photoBytes) || fp.calls[0].ImageMIME != "image/jpeg" {
		t.Errorf("retry request = %+v, want the photo", fp.calls)
	}
	if !bytes.Equal(out.Source.Image, photoBytes) {
		t.Error("the retried draft lost the photo, so a second retry would send none")
	}
}

func TestFromOCRSavesTheOCRTextAsTheSnapshotAndNeverTheImage(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipelineWith(t, testConfig(), nil).FromOCR(e.ctx, goodOCRText, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if d.Source.Kind != store.SnapshotOCR {
		t.Fatalf("source kind = %q, want ocr", d.Source.Kind)
	}
	saved, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	snap, err := e.store.GetSnapshot(e.ctx, saved.RecipeID, store.SnapshotOCR)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if string(snap.Content) != goodOCRText {
		t.Errorf("snapshot = %q, want the OCR text unchanged", snap.Content)
	}
	kinds, err := e.store.ListSnapshotKinds(e.ctx, saved.RecipeID)
	if err != nil || len(kinds) != 1 {
		t.Errorf("snapshot kinds = %v, %v; want only ocr", kinds, err)
	}
}

func TestFromOCREmptyTextIsAnInputError(t *testing.T) {
	e := newEnv(t)
	_, err := e.pipelineWith(t, testConfig(), nil).FromOCR(e.ctx, "  \n", photoBytes, "image/png")
	if !errors.Is(err, parse.ErrEmptyText) {
		t.Fatalf("err = %v, want ErrEmptyText", err)
	}
}

func TestVisionImageIsAttachedOnlyWithinTheProviderSizeLimit(t *testing.T) {
	cases := []struct {
		name      string
		size      int
		wantImage bool
	}{
		{"at the limit", maxVisionImageBytes, true},
		{"over the limit", maxVisionImageBytes + 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			fp := &fakeProvider{resp: llmAnswer()}
			big := bytes.Repeat([]byte{1}, tc.size)
			if _, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, messyText, big, "image/png"); err != nil {
				t.Fatal(err)
			}
			if fp.count() != 1 {
				t.Fatalf("provider calls = %d, want 1: the text still goes to the provider", fp.count())
			}
			req := fp.calls[0]
			if tc.wantImage != (req.Image != nil) {
				t.Errorf("request image = %d bytes, want attached = %v", len(req.Image), tc.wantImage)
			}
			if !tc.wantImage && req.ImageMIME != "" {
				t.Errorf("image MIME = %q with no image", req.ImageMIME)
			}
			if req.Text != messyText {
				t.Errorf("request text = %q, want the OCR text", req.Text)
			}
		})
	}
}
