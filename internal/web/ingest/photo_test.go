package ingest

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/ocr"
)

// pngBytes is the PNG signature plus filler: enough for content sniffing,
// which is all the handler does before handing the bytes to the OCR reader.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

type fakeReader struct {
	text  string
	err   error
	calls int
	got   []byte
}

func (f *fakeReader) Text(_ context.Context, image []byte) (string, error) {
	f.calls++
	f.got = image
	return f.text, f.err
}

// textDrafter records what it is asked to draft from a photo.
type textDrafter struct {
	draft ingestcore.Draft
	calls int
	text  string
	image []byte
	mime  string
}

func (d *textDrafter) FromOCR(_ context.Context, text string, image []byte, mime string) (ingestcore.Draft, error) {
	d.calls++
	d.text, d.image, d.mime = text, image, mime
	return d.draft, nil
}

type photoFixture struct {
	*fixture
	reader  *fakeReader
	drafter *textDrafter
	srv     http.Handler
}

func newPhotoFixture(t *testing.T, reader *fakeReader) *photoFixture {
	t.Helper()
	f := newFixture(t)
	d := &textDrafter{draft: sampleDraft()}
	f.h.photos = d
	f.h.ocr = reader
	return &photoFixture{fixture: f, reader: reader, drafter: d, srv: f.h.routes()}
}

func (f *photoFixture) upload(t *testing.T, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if filename != "" {
		fw, err := mw.CreateFormFile("photo", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(content)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/photo", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func TestPhotoFormRenders(t *testing.T) {
	f := newPhotoFixture(t, &fakeReader{})
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/photo", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `enctype="multipart/form-data"`) || !strings.Contains(body, `type="file"`) {
		t.Fatalf("status %d, body: %s", rec.Code, body)
	}
}

func TestPhotoUploadFeedsOCRTextToTheTextPipelineAndReviews(t *testing.T) {
	f := newPhotoFixture(t, &fakeReader{text: "Apple pie\n2 tbsp butter"})
	rec := f.upload(t, "page.png", pngBytes)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body: %s", rec.Code, rec.Body)
	}
	if !bytes.Equal(f.reader.got, pngBytes) {
		t.Errorf("OCR received %d bytes, want the uploaded image", len(f.reader.got))
	}
	if !bytes.Equal(f.drafter.image, pngBytes) || f.drafter.mime != "image/png" {
		t.Errorf("pipeline image = %d bytes (%s), want the photo", len(f.drafter.image), f.drafter.mime)
	}
	if f.drafter.text != "Apple pie\n2 tbsp butter" {
		t.Errorf("pipeline text = %q, want the OCR text", f.drafter.text)
	}
	loc := strings.TrimPrefix(rec.Header().Get("Location"), "/ingest") // the test router is mounted at the root
	review := httptest.NewRecorder()
	f.srv.ServeHTTP(review, httptest.NewRequest(http.MethodGet, loc, nil))
	if review.Code != http.StatusOK || !strings.Contains(review.Body.String(), "Apple pie") {
		t.Errorf("review at %s: status %d", loc, review.Code)
	}
	if len(f.store.recipes) != 0 {
		t.Error("a recipe was saved before confirmation")
	}
}

func TestPhotoUploadRejections(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  []byte
		status   int
		want     string
	}{
		{"no file", "", nil, http.StatusBadRequest, "Choose a photo"},
		{"empty file", "a.png", nil, http.StatusBadRequest, "Choose a photo"},
		{"not an image", "notes.txt", []byte("just some text"), http.StatusUnsupportedMediaType, "not a supported image"},
		{"pdf", "a.pdf", []byte("%PDF-1.4 ..."), http.StatusUnsupportedMediaType, "not a supported image"},
		{"too large", "big.png", append(append([]byte{}, pngBytes...), make([]byte, maxPhotoBytes)...), http.StatusRequestEntityTooLarge, "too large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPhotoFixture(t, &fakeReader{text: "x"})
			rec := f.upload(t, tc.filename, tc.content)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("status %d, want %d containing %q; body: %s", rec.Code, tc.status, tc.want, rec.Body)
			}
			if f.reader.calls != 0 || f.drafter.calls != 0 {
				t.Errorf("OCR ran %d times and the pipeline %d; a rejected upload must reach neither", f.reader.calls, f.drafter.calls)
			}
		})
	}
}

func TestPhotoOCRFailuresAreShownNotDrafted(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"no text", ocr.ErrNoText, http.StatusUnprocessableEntity},
		{"unreadable image", errors.Join(ocr.ErrFailed, errors.New("bad image")), http.StatusUnprocessableEntity},
		{"engine missing", errors.Join(ocr.ErrUnavailable, errors.New("not installed")), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPhotoFixture(t, &fakeReader{err: tc.err})
			rec := f.upload(t, "page.png", pngBytes)
			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tc.status, rec.Body)
			}
			if f.drafter.calls != 0 {
				t.Error("an OCR failure was turned into a draft")
			}
		})
	}
}
