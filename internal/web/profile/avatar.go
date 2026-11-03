package profile

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/media"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The avatar section (avatar.templ): upload, crop, and the app serving the
// processed picture (ADR-00033). Nothing here points the browser at the media
// store; every byte goes through these handlers.

const (
	// uploadField is the multipart field the picture arrives in.
	uploadField = "avatar"
	// multipartOverhead is what a multipart body may add to the picture itself.
	multipartOverhead = 64 << 10
)

// AvatarSrc is the address of a profile's uploaded avatar. The key is the
// avatar's reference from the profile; it is in the query so that a new upload
// is a new address and no cache keeps showing the old one.
func AvatarSrc(address, key string) string {
	v := strings.TrimSuffix(strings.TrimPrefix(key, "avatars/"), ".webp")
	return Pattern + "/" + address + "/avatar?v=" + url.QueryEscape(v)
}

// avatarServeRoutes registers GET /{address}/avatar, the picture itself. It is
// not self-only: anyone who may reach the profile may fetch it, if the
// visibility rule shows them the avatar.
func (h *handler) avatarServeRoutes(r chi.Router) {
	r.Get("/avatar", h.serveAvatar)
}

// avatarRoutes registers the self-only routes of the section.
//
//	POST /{address}/avatar              the picture, as multipart
//	GET  /{address}/avatar/crop         the page to choose the crop on
//	POST /{address}/avatar/crop         the chosen rectangle
//	GET  /{address}/avatar/preview/{id} the picture to crop against
//	GET  /{address}/avatar/status       the section's status fragment
func (h *handler) avatarRoutes(r chi.Router) {
	r.Post("/avatar", h.uploadAvatar)
	r.Get("/avatar/crop", h.cropPage)
	r.Post("/avatar/crop", h.cropAvatar)
	r.Get("/avatar/preview/{id}", h.avatarPreview)
	r.Get("/avatar/status", h.avatarStatus)
}

func (h *handler) serveAvatar(w http.ResponseWriter, r *http.Request) {
	s, ok := subjectFrom(r.Context())
	if !ok {
		layout.WriteError(w, r, fmt.Errorf("no profile on the request: %w", layout.ErrNotFound))
		return
	}
	// As asks the visibility rule; a viewer who may not see the avatar gets
	// the same 404 as a profile with none.
	key := s.Profile.As(s.Relation).Avatar
	if key == "" {
		layout.WriteError(w, r, fmt.Errorf("profile has no visible avatar: %w", layout.ErrNotFound))
		return
	}
	etag := `"` + key + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	b, err := h.Avatars.Open(r.Context(), key)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	writeImage(w, "image/webp", b)
}

func writeImage(w http.ResponseWriter, contentType string, b []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b) // the client went away if this fails; there is nobody to tell
}

// self returns the subject, who selfOnly has shown to be the viewer.
func self(w http.ResponseWriter, r *http.Request) (subject, bool) {
	s, ok := subjectFrom(r.Context())
	if !ok || !s.Self() {
		layout.WriteError(w, r, fmt.Errorf("avatar without the profile's person: %w", layout.ErrNotFound))
		return subject{}, false
	}
	return s, true
}

// refused answers an upload or crop the person must fix, with the message the
// media package wrote for them.
func refused(w http.ResponseWriter, r *http.Request, err error) bool {
	if msg, ok := media.RefusalText(err); ok {
		layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, msg))
		return true
	}
	if errors.Is(err, domain.ErrBusy) {
		layout.Render(w, r, http.StatusConflict, layout.ErrorPage(http.StatusConflict,
			"Your last picture is still being processed. Wait for it to finish, then upload again."))
		return true
	}
	return false
}

func (h *handler) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	data, err := readUpload(w, r)
	if err != nil {
		if !refused(w, r, err) {
			layout.WriteError(w, r, err)
		}
		return
	}
	if _, err := h.Avatars.Upload(r.Context(), s.Profile.Email, data); err != nil {
		if !refused(w, r, err) {
			layout.WriteError(w, r, err)
		}
		return
	}
	http.Redirect(w, r, Pattern+"/"+s.Profile.Address+"/avatar/crop", http.StatusSeeOther)
}

// readUpload reads the picture from the multipart body, stopping past the limit
// rather than buffering what the server will refuse.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+multipartOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, &media.Refusal{Msg: "Choose a picture to upload."}
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, &media.Refusal{Msg: "Choose a picture to upload."}
		}
		if err != nil {
			return nil, tooLargeOr(err, &media.Refusal{Msg: "The upload could not be read. Try again."})
		}
		if part.FormName() != uploadField {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, media.MaxUploadBytes+1))
		if err != nil {
			return nil, tooLargeOr(err, &media.Refusal{Msg: "The upload could not be read. Try again."})
		}
		return data, nil
	}
}

func tooLargeOr(err error, otherwise error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return &media.Refusal{Msg: "That file is over 5 MB. Choose a smaller picture."}
	}
	return otherwise
}

func (h *handler) cropPage(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	j, found, err := h.Avatars.Job(r.Context(), s.Profile.Email)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if !found || j.State == domain.JobQueued {
		redirectTo(w, r, s.Profile.Address)
		return
	}
	layout.Render(w, r, http.StatusOK, cropScreen(newCropView(s.Profile.Address, j)))
}

func (h *handler) cropAvatar(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	var c media.Crop
	for name, dst := range map[string]*float64{"x": &c.X, "y": &c.Y, "w": &c.W, "h": &c.H} {
		v, err := strconv.ParseFloat(r.PostFormValue(name), 64)
		if err != nil {
			layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest,
				"The crop is not valid. Choose the part of the picture again."))
			return
		}
		*dst = v
	}
	if err := h.Avatars.Crop(r.Context(), s.Profile.Email, r.PostFormValue("id"), c); err != nil {
		if !refused(w, r, err) {
			layout.WriteError(w, r, err)
		}
		return
	}
	redirectTo(w, r, s.Profile.Address)
}

func (h *handler) avatarPreview(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	b, err := h.Avatars.Preview(r.Context(), s.Profile.Email, chi.URLParam(r, "id"))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeImage(w, "image/jpeg", b)
}

// avatarStatus is the fragment the section loads and, while a picture is being
// processed, polls. The page says which avatar it was drawn with (has, the key
// or empty); when the profile's has moved on, the page is stale and the
// response asks htmx to reload it. Comparing, rather than watching for a job to
// finish, covers a job that finished before the page's first request.
func (h *handler) avatarStatus(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	j, found, err := h.Avatars.Job(r.Context(), s.Profile.Email)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	has := r.URL.Query().Get("has")
	if has != s.Profile.Avatar {
		w.Header().Set("HX-Refresh", "true")
	}
	layout.Render(w, r, http.StatusOK, statusFragment(newStatusView(s.Profile.Address, j, found, has)))
}
