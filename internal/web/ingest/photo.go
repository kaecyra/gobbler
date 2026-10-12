package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/ocr"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// maxPhotoBytes bounds an uploaded photo; the multipart envelope adds a little
// on top, covered by maxPhotoRequestBytes.
const (
	maxPhotoBytes        = 10 << 20
	maxPhotoRequestBytes = maxPhotoBytes + 1<<20
)

// TextReader reads the text out of a photo; ocr.Runner satisfies it.
type TextReader interface {
	Text(ctx context.Context, image []byte) (string, error)
}

// photoTypes are the image types accepted, by sniffed media type. The same
// bytes go to tesseract and, on fallback, to the vision provider, so this is
// the intersection of what tesseract, Anthropic (JPEG, PNG, GIF, WebP:
// https://platform.claude.com/docs/en/build-with-claude/vision) and Gemini
// (PNG, JPEG, WebP, HEIC, HEIF: https://ai.google.dev/gemini-api/docs/image-understanding)
// accept. Checked against those pages when this was written.
var photoTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// PhotoDrafter drafts a recipe from OCR text and the photo it came from;
// *ingestcore.Pipeline satisfies it.
type PhotoDrafter interface {
	FromOCR(ctx context.Context, text string, image []byte, imageMIME string) (ingestcore.Draft, error)
}

// photoView is what the photo upload form renders.
type photoView struct {
	Error string
}

func (h *handler) photoForm(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, photoPage(photoView{}))
}

// photoSubmit reads the uploaded photo with OCR and feeds the text through the
// same pipeline as pasted text, so it gets the same extraction, scoring and
// fallback, and then the review screen. The photo is never saved: only the OCR
// text is the draft's source snapshot. The photo is held in memory on the
// pending draft, for the vision fallback and retry, until the draft expires or
// is confirmed. Nothing is saved here.
func (h *handler) photoSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoRequestBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.photoFormError(w, r, http.StatusRequestEntityTooLarge, "That photo is too large. The limit is 10 MB.")
			return
		}
		h.photoFormError(w, r, http.StatusBadRequest, "Choose a photo to upload.")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, _, err := r.FormFile("photo")
	if err != nil {
		h.photoFormError(w, r, http.StatusBadRequest, "Choose a photo to upload.")
		return
	}
	defer func() { _ = file.Close() }()
	image, err := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("read uploaded photo: %w", err))
		return
	}
	mime := strings.ToLower(http.DetectContentType(image))
	switch {
	case len(image) == 0:
		h.photoFormError(w, r, http.StatusBadRequest, "Choose a photo to upload.")
		return
	case len(image) > maxPhotoBytes:
		h.photoFormError(w, r, http.StatusRequestEntityTooLarge, "That photo is too large. The limit is 10 MB.")
		return
	case !photoTypes[mime]:
		h.photoFormError(w, r, http.StatusUnsupportedMediaType, "That file is not a supported image. Upload a PNG, JPEG or WebP photo.")
		return
	}

	text, err := h.ocr.Text(r.Context(), image)
	switch {
	case errors.Is(err, ocr.ErrNoText):
		h.photoFormError(w, r, http.StatusUnprocessableEntity, "No text could be read from that photo. Try a sharper, better lit photo of the page.")
		return
	case errors.Is(err, ocr.ErrFailed):
		h.log.WarnContext(r.Context(), "photo ocr failed", "error", err.Error())
		h.photoFormError(w, r, http.StatusUnprocessableEntity, "That photo could not be read. Try a different photo, or one that is sharper and smaller.")
		return
	case err != nil:
		// The OCR engine is missing or could not start: the pipeline is
		// broken, not the photo.
		layout.WriteError(w, r, fmt.Errorf("read photo: %w", err))
		return
	}

	draft, err := h.photos.FromOCR(r.Context(), text, image, mime)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("ingest photo text: %w", err))
		return
	}
	token, err := h.pending.add(draft)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, "/ingest/"+token, http.StatusSeeOther)
}

func (h *handler) photoFormError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	layout.Render(w, r, status, photoPage(photoView{Error: msg}))
}

var _ TextReader = ocr.Runner{}
