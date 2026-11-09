package profile

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The avatar section (avatar.templ): upload, crop, and the app serving the
// processed picture (ADR-00033). Nothing here points the browser at the media
// store; every byte goes through these handlers.

const (
	// uploadField is the multipart field the picture arrives in.
	uploadField = "avatar"
)

// AvatarSrc is the address of a profile's uploaded avatar. The key is the
// avatar's reference from the profile (people/<person>/avatar/<version>.webp);
// its version alone goes in the query, so that a new upload is a new address
// and no cache keeps showing the old one, and the key's path stays out of it.
func AvatarSrc(address, key string) string {
	v := strings.TrimSuffix(path.Base(key), ".webp")
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
//	GET  /{address}/avatar/upload       the upload step (a fragment for the dialog, a page otherwise)
//	GET  /{address}/avatar/crop         the crop step (a fragment for the dialog, a page otherwise)
//	POST /{address}/avatar/crop         the chosen rectangle
//	GET  /{address}/avatar/preview/{id} the picture to crop against
//	GET  /{address}/avatar/status       the status fragment of the page
func (h *handler) avatarRoutes(r chi.Router) {
	r.Post("/avatar", h.uploadAvatar)
	r.Get("/avatar/upload", h.uploadPage)
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
	// The visibility rule decides; a viewer who may not see the avatar gets
	// the same 404 as a profile with none.
	key := apiprofile.AvatarKey(s)
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
	b, err := h.Profile.OpenAvatar(r.Context(), callerOf(r), s.Profile.Address)
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
//
// The dialog (htmx) shows the message in its own panel and starts over at the
// first step. htmx does not swap a 4xx response, so that one answer is a 200;
// a request that is not htmx still gets the status code and the error page.
func refused(w http.ResponseWriter, r *http.Request, s subject, err error) bool {
	if msg, ok := media.RefusalText(err); ok {
		refuse(w, r, s, http.StatusBadRequest, msg)
		return true
	}
	if errors.Is(err, apiprofile.ErrBusy) {
		refuse(w, r, s, http.StatusConflict,
			"Your last picture is still being processed. Wait for it to finish, then upload again.")
		return true
	}
	return false
}

// refuse answers a request the person must correct, with msg.
func refuse(w http.ResponseWriter, r *http.Request, s subject, status int, msg string) {
	if r.Header.Get("HX-Request") == "true" {
		layout.Render(w, r, http.StatusOK, uploadPanel(newPanelView(s.Profile.Address, s.Profile.Avatar, msg)))
		return
	}
	layout.Render(w, r, status, layout.ErrorPage(status, msg))
}

func (h *handler) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	data, err := layout.ReadUpload(w, r, uploadField)
	if err != nil {
		if !refused(w, r, s, err) {
			layout.WriteError(w, r, err)
		}
		return
	}
	j, err := h.Profile.UploadAvatar(r.Context(), callerOf(r), data)
	if err != nil {
		if !refused(w, r, s, err) {
			layout.WriteError(w, r, err)
		}
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		layout.Render(w, r, http.StatusOK, cropPanel(newCropView(s.Profile.Address, s.Profile.Avatar, j)))
		return
	}
	http.Redirect(w, r, Pattern+"/"+s.Profile.Address+"/avatar/crop", http.StatusSeeOther)
}

// uploadPage is the upload step: a fragment for the dialog to start over with,
// and a page of its own for the avatar link of a browser without scripts.
func (h *handler) uploadPage(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	w.Header().Add("Vary", "HX-Request")
	v := newPanelView(s.Profile.Address, s.Profile.Avatar, "")
	if r.Header.Get("HX-Request") == "true" {
		layout.Render(w, r, http.StatusOK, uploadPanel(v))
		return
	}
	layout.Render(w, r, http.StatusOK, uploadScreen(v))
}

func (h *handler) cropPage(w http.ResponseWriter, r *http.Request) {
	s, ok := self(w, r)
	if !ok {
		return
	}
	w.Header().Add("Vary", "HX-Request")
	j, found, err := h.Profile.AvatarJob(r.Context(), callerOf(r))
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if !found || j.State == apiprofile.JobQueued {
		if r.Header.Get("HX-Request") == "true" {
			layout.Render(w, r, http.StatusOK, uploadPanel(newPanelView(s.Profile.Address, s.Profile.Avatar, "")))
			return
		}
		redirectTo(w, r, s.Profile.Address)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		layout.Render(w, r, http.StatusOK, cropPanel(newCropView(s.Profile.Address, s.Profile.Avatar, j)))
		return
	}
	layout.Render(w, r, http.StatusOK, cropScreen(newCropView(s.Profile.Address, s.Profile.Avatar, j)))
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
			refuse(w, r, s, http.StatusBadRequest, "The crop is not valid. Choose the part of the picture again.")
			return
		}
		*dst = v
	}
	if err := h.Profile.CropAvatar(r.Context(), callerOf(r), r.PostFormValue("id"), c); err != nil {
		if !refused(w, r, s, err) {
			layout.WriteError(w, r, err)
		}
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		queued := newStatusView(s.Profile.Address, apiprofile.Job{State: apiprofile.JobQueued}, true, s.Profile.Avatar)
		queued.OOB = true
		layout.Render(w, r, http.StatusOK, savedPanel(queued))
		return
	}
	redirectTo(w, r, s.Profile.Address)
}

func (h *handler) avatarPreview(w http.ResponseWriter, r *http.Request) {
	_, ok := self(w, r)
	if !ok {
		return
	}
	b, err := h.Profile.AvatarPreview(r.Context(), callerOf(r), chi.URLParam(r, "id"))
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
	j, found, err := h.Profile.AvatarJob(r.Context(), callerOf(r))
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
