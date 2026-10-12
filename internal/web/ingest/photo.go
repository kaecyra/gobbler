package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/ingest/ocr"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// maxPhotoBytes bounds an uploaded photo; the multipart envelope adds a little
// on top, covered by maxPhotoRequestBytes.
const (
	maxPhotoBytes        = 10 << 20
	maxPhotoRequestBytes = maxPhotoBytes + 1<<20
)

// TextReader reads the text out of a photo; *ocr.Runner satisfies it.
type TextReader interface {
	Text(ctx context.Context, image []byte) (string, error)
}

// photoTypes are the image types tesseract can be given, by sniffed media type.
var photoTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
}

// photoView is what the photo upload form renders.
type photoView struct {
	Error string
}

// mountPhoto attaches the photo routes to r with the tesseract binary named by
// the environment (ocr.EnvBin).
func (h *handler) mountPhoto(r chi.Router) {
	runner := ocr.FromEnv()
	h.mountPhotoWith(r, runner)
}

// mountPhotoWith attaches the photo routes, reading photos with reader.
func (h *handler) mountPhotoWith(r chi.Router, reader TextReader) {
	r.Get("/photo", func(w http.ResponseWriter, r *http.Request) {
		layout.Render(w, r, http.StatusOK, photoPage(photoView{}))
	})
	r.Post("/photo", func(w http.ResponseWriter, r *http.Request) { h.photoSubmit(w, r, reader) })
}

// photoSubmit reads the uploaded photo with OCR and feeds the text through the
// same pipeline as pasted text, so it gets the same extraction, scoring and
// fallback, and then the review screen. The photo itself is not kept: only the
// OCR text, as the draft's source snapshot, outlives the request. Nothing is
// saved here.
func (h *handler) photoSubmit(w http.ResponseWriter, r *http.Request, reader TextReader) {
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
	switch {
	case len(image) == 0:
		h.photoFormError(w, r, http.StatusBadRequest, "Choose a photo to upload.")
		return
	case len(image) > maxPhotoBytes:
		h.photoFormError(w, r, http.StatusRequestEntityTooLarge, "That photo is too large. The limit is 10 MB.")
		return
	case !photoTypes[strings.ToLower(http.DetectContentType(image))]:
		h.photoFormError(w, r, http.StatusUnsupportedMediaType, "That file is not a supported image. Upload a PNG, JPEG, GIF, WebP or BMP photo.")
		return
	}

	text, err := reader.Text(r.Context(), image)
	switch {
	case errors.Is(err, ocr.ErrNoText):
		h.photoFormError(w, r, http.StatusUnprocessableEntity, "No text could be read from that photo. Try a sharper, better lit photo of the page.")
		return
	case errors.Is(err, ocr.ErrFailed):
		h.log.WarnContext(r.Context(), "photo ocr failed", "error", err.Error())
		h.photoFormError(w, r, http.StatusUnprocessableEntity, "That photo could not be read: "+err.Error())
		return
	case err != nil:
		// The OCR engine is missing or could not start: the pipeline is
		// broken, not the photo.
		layout.WriteError(w, r, fmt.Errorf("read photo: %w", err))
		return
	}

	draft, err := h.drafter.FromText(r.Context(), text)
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
