package ingest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/store"
)

// The page as the vision model reads it, and as tesseract misreads it: the
// half cup of sugar became "4 cup", a line that still parses cleanly.
const (
	visionTranscript = "Fluffy Pancakes\n\nIngredients:\n- 2 cups all-purpose flour\n- ½ cup sugar\n- 2 eggs\n\nInstructions:\nWhisk the dry ingredients together in a large bowl.\n\nCook on a hot greased griddle until golden on both sides.\n"
	tesseractMisread = "Fluffy Pancakes\n\nIngredients:\n- 2 cups all-purpose flour\n- 4 cup sugar\n- 2 eggs\n\nInstructions:\nWhisk the dry ingredients together in a large bowl.\n\nCook on a hot greased griddle until golden on both sides.\n"
)

type logLines struct{ buf bytes.Buffer }

func (l *logLines) pipeline(t *testing.T, e *env, prov Provider) *Pipeline {
	t.Helper()
	p, err := New(testConfig(), Deps{Catalog: e.catalog, Provider: prov, Logger: slog.New(slog.NewTextHandler(&l.buf, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func lineNamed(t *testing.T, d Draft, raw string) Line {
	t.Helper()
	for _, c := range d.Components {
		for _, l := range c.Lines {
			if l.Raw == raw {
				return l
			}
		}
	}
	t.Fatalf("no line %q in %+v", raw, d.Components)
	return Line{}
}

// ADR-00053: with a provider, the transcript is the source snapshot and the
// rule parser structures it; the tesseract text is kept beside it.
func TestFromOCRWithAProviderDraftsFromTheTranscript(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{transcript: visionTranscript, resp: llmAnswer()}
	d, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, tesseractMisread, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.transcribed) != 1 || !bytes.Equal(fp.transcribed[0].Image, photoBytes) || fp.transcribed[0].ImageMIME != "image/png" {
		t.Fatalf("transcriptions = %+v, want one, of the photo", fp.transcribed)
	}
	if d.Source.Kind != store.SnapshotOCR || string(d.Source.Content) != visionTranscript {
		t.Errorf("source = %s %q, want the transcript as the ocr snapshot", d.Source.Kind, d.Source.Content)
	}
	if d.Source.CheckText != tesseractMisread {
		t.Errorf("check text = %q, want the tesseract text", d.Source.CheckText)
	}
	if !d.Source.intact() {
		t.Error("the source hash does not match the transcript")
	}
	if d.Name.Value != "Fluffy Pancakes" || len(d.Components) != 1 || len(d.Components[0].Lines) != 3 {
		t.Errorf("draft = %q %+v, want the rule parser's reading of the transcript", d.Name.Value, d.Components)
	}
	if got := lineNamed(t, d, "½ cup sugar"); got.Raw == "" {
		t.Error("the transcribed line is missing")
	}
}

// A transcribed number tesseract did not read lowers that line below the
// threshold, and gap-fill then sends exactly that line with the photo
// (ADR-00053, ADR-00020).
func TestFromOCRFlagsANumberTesseractDidNotRead(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{transcript: visionTranscript, resp: Structured{Name: "x", Components: []StructuredComponent{{Ingredients: []string{"½ cup sugar"}}}}}
	d, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, tesseractMisread, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 1 {
		t.Fatalf("gap-fill calls = %d, want 1 for the flagged line", fp.count())
	}
	req := fp.calls[0]
	if len(req.Gaps.Lines) != 1 || !strings.Contains(req.Gaps.Lines[0].Raw, "½ cup sugar") {
		t.Errorf("gap lines = %+v, want only the sugar line", req.Gaps.Lines)
	}
	if !bytes.Equal(req.Image, photoBytes) {
		t.Error("the gap-fill request did not carry the photo")
	}
	if req.Text != visionTranscript {
		t.Errorf("gap-fill text = %q, want the transcript", req.Text)
	}
	flour := lineNamed(t, d, "2 cups all-purpose flour")
	if flour.FromLLM || flour.Confidence < d.Threshold {
		t.Errorf("a line whose numbers tesseract read was touched: %+v", flour)
	}
}

// Without the gap-fill answer the flag itself is visible on the draft.
func TestFromOCRNumberFlagLowersTheLineBelowTheThreshold(t *testing.T) {
	e := newEnv(t)
	// A provider that cannot answer the gap-fill leaves the rule draft in place.
	fp := &fakeProvider{transcript: visionTranscript, err: errors.New("down")}
	d, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, tesseractMisread, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	sugar := lineNamed(t, d, "½ cup sugar")
	if sugar.Confidence >= d.Threshold {
		t.Errorf("confidence = %v, want below the %v threshold", sugar.Confidence, d.Threshold)
	}
	if len(sugar.Issues) == 0 || !strings.Contains(sugar.Issues[len(sugar.Issues)-1], "1/2") {
		t.Errorf("issues = %v, want the number named", sugar.Issues)
	}
	if flour := lineNamed(t, d, "2 cups all-purpose flour"); flour.Confidence < d.Threshold {
		t.Errorf("flour confidence = %v, want it kept", flour.Confidence)
	}
}

// When tesseract read every number, the transcript is trusted and no gap-fill runs.
func TestFromOCRAgreeingTextMakesNoGapFillCall(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{transcript: visionTranscript, resp: llmAnswer()}
	tess := strings.Replace(tesseractMisread, "4 cup", "1/2 cup", 1)
	d, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, tess, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 0 || d.UsedLLM || d.BelowThreshold {
		t.Errorf("gap-fill calls = %d, used LLM = %v, below = %v; want none", fp.count(), d.UsedLLM, d.BelowThreshold)
	}
}

// ADR-00053: replaying the stored pair gives the same draft and flags.
func TestPhotoDraftReplaysFromTranscriptAndCheckText(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{transcript: visionTranscript, err: errors.New("down")}
	first, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, tesseractMisread, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	drafts := NewDrafts(e.store)
	token, err := drafts.Add(e.ctx, first, store.Provenance{Person: auth.DevEmail})
	if err != nil {
		t.Fatal(err)
	}
	held, err := drafts.Get(e.ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if held.Source.CheckText != tesseractMisread {
		t.Fatalf("held check text = %q, want the tesseract text", held.Source.CheckText)
	}
	again, err := e.pipelineWith(t, testConfig(), nil).fromPhotoSource(e.ctx, string(held.Source.Content), held.Source.CheckText, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"½ cup sugar", "2 cups all-purpose flour", "2 eggs"} {
		a, b := lineNamed(t, first, raw), lineNamed(t, again, raw)
		if a.Confidence != b.Confidence || len(a.Issues) != len(b.Issues) {
			t.Errorf("%q replayed as %v %v, was %v %v", raw, b.Confidence, b.Issues, a.Confidence, a.Issues)
		}
	}
	if again.Overall != first.Overall {
		t.Errorf("replayed score %v, was %v", again.Overall, first.Overall)
	}
}

// Saving a photo draft stores the transcript and the tesseract text as two
// snapshots, held or direct (ADR-00053).
func TestSavePhotoDraftStoresBothSnapshots(t *testing.T) {
	e := newEnv(t)
	fp := &fakeProvider{transcript: visionTranscript, err: errors.New("down")}
	d, err := e.pipelineWith(t, testConfig(), fp).FromOCR(e.ctx, tesseractMisread, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, id int64) {
		t.Helper()
		ocr, err := e.store.GetSnapshot(e.ctx, id, store.SnapshotOCR)
		if err != nil || string(ocr.Content) != visionTranscript {
			t.Errorf("ocr snapshot = %q (%v), want the transcript", ocr.Content, err)
		}
		tess, err := e.store.GetSnapshot(e.ctx, id, store.SnapshotOCRCheck)
		if err != nil || string(tess.Content) != tesseractMisread || tess.ParserVersion != ParserVersion {
			t.Errorf("ocr_check snapshot = %q v%q (%v), want the tesseract text at the parser version", tess.Content, tess.ParserVersion, err)
		}
	}
	t.Run("direct", func(t *testing.T) {
		saved, err := e.saver().Save(e.ctx, confirm(t, d))
		if err != nil {
			t.Fatal(err)
		}
		check(t, saved.RecipeID)
	})
	t.Run("held", func(t *testing.T) {
		drafts := NewDrafts(e.store)
		token, err := drafts.Add(e.ctx, d, store.Provenance{Person: auth.DevEmail})
		if err != nil {
			t.Fatal(err)
		}
		held, err := drafts.Get(e.ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := e.saver().SaveProposed(e.ctx, token, confirm(t, held))
		if err != nil {
			t.Fatal(err)
		}
		check(t, saved.RecipeID)
	})
}

// ADR-00053: a transcription that fails is logged and the draft is made from the
// tesseract text, as with no provider.
func TestFromOCRTranscriptionFailureDraftsFromTesseract(t *testing.T) {
	for name, fp := range map[string]*fakeProvider{
		"error":   {transcribeErr: context.DeadlineExceeded},
		"timeout": {transcribeErr: errors.New("claude call: context deadline exceeded")},
		"blank":   {transcript: "  \n"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			var logs logLines
			rule, err := e.pipelineWith(t, testConfig(), nil).FromOCR(e.ctx, goodOCRText, photoBytes, "image/png")
			if err != nil {
				t.Fatal(err)
			}
			d, err := logs.pipeline(t, e, fp).FromOCR(e.ctx, goodOCRText, photoBytes, "image/png")
			if err != nil {
				t.Fatalf("a failed transcription must not fail the ingest: %v", err)
			}
			if string(d.Source.Content) != goodOCRText || d.Source.CheckText != "" {
				t.Errorf("source = %q check %q, want the tesseract text alone", d.Source.Content, d.Source.CheckText)
			}
			if d.Overall != rule.Overall || len(d.Components) != len(rule.Components) {
				t.Error("the draft differs from the one made with no provider")
			}
			if !strings.Contains(logs.buf.String(), "photo transcription failed") {
				t.Errorf("failure not logged: %q", logs.buf.String())
			}
		})
	}
}

// ADR-00053: a photo over maxVisionImageBytes is not sent; tesseract alone, logged.
func TestFromOCROversizedPhotoIsNotTranscribed(t *testing.T) {
	e := newEnv(t)
	var logs logLines
	fp := &fakeProvider{transcript: visionTranscript}
	big := make([]byte, maxVisionImageBytes+1)
	d, err := logs.pipeline(t, e, fp).FromOCR(e.ctx, goodOCRText, big, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.transcribed) != 0 {
		t.Errorf("an oversized photo was sent for transcription")
	}
	if string(d.Source.Content) != goodOCRText || d.Source.CheckText != "" {
		t.Errorf("source = %q, want the tesseract text", d.Source.Content)
	}
	if !strings.Contains(logs.buf.String(), "image_over_provider_limit") {
		t.Errorf("not logged: %q", logs.buf.String())
	}
}

// With no provider the photo path makes no provider call and behaves as before.
func TestFromOCRWithNoProviderMakesNoCall(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipelineWith(t, testConfig(), nil).FromOCR(e.ctx, goodOCRText, photoBytes, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if string(d.Source.Content) != goodOCRText || d.Source.CheckText != "" || d.UsedLLM {
		t.Errorf("draft source %q check %q used LLM %v", d.Source.Content, d.Source.CheckText, d.UsedLLM)
	}
}
