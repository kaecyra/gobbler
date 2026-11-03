package web

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/profile"
)

// The avatar through the real router: upload, crop, processing, serving and
// who may fetch it (ADR-00033).

type avatarRig struct {
	h       http.Handler
	avatars *profile.Avatars
	media   *media.Local
}

func newAvatarRig(t *testing.T) *avatarRig {
	t.Helper()
	d := testDB(t)
	m := media.NewLocal(t.TempDir())
	av := profile.NewAvatars(context.Background(), profile.New(d), m, nil)
	cfg := devConfig()
	cfg.Operator = config.Operator{Emails: []string{operatorEmail}}
	return &avatarRig{h: testRouter(t, cfg, Deps{DB: d, Avatars: av, Backup: idleBackup{}}), avatars: av, media: m}
}

func photo(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 90, 60))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 20, G: 120, B: 200, A: 255}), image.Point{}, draw.Src)
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func multipartUpload(t *testing.T, field string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if field != "" {
		fw, err := mw.CreateFormFile(field, "photo.jpg")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

func (a *avatarRig) upload(t *testing.T, email, path, field string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartUpload(t, field, data)
	req := httptest.NewRequest(http.MethodPost, path+"/avatar", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set(auth.DevIdentityHeader, email)
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return rec
}

// uploadAndCrop takes email's avatar through the whole flow and waits for it.
func (a *avatarRig) uploadAndCrop(t *testing.T, email, path string) {
	t.Helper()
	if rec := a.upload(t, email, path, "avatar", photo(t)); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path+"/avatar/crop" {
		t.Fatalf("upload = %d to %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	page := as(a.h, http.MethodGet, path+"/avatar/crop", email, nil)
	m := regexp.MustCompile(`name="id" value="([A-Za-z0-9_-]{22})"`).FindStringSubmatch(page.Body.String())
	if page.Code != http.StatusOK || m == nil {
		t.Fatalf("crop page = %d without an upload id:\n%s", page.Code, page.Body.String())
	}
	form := url.Values{"id": {m[1]}, "x": {"0"}, "y": {"0"}, "w": {"0.6667"}, "h": {"1"}}
	if rec := as(a.h, http.MethodPost, path+"/avatar/crop", email, form); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Fatalf("crop = %d to %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	a.avatars.Wait()
}

func TestAvatarFlowStoresAWebPThatTheAppServes(t *testing.T) {
	a := newAvatarRig(t)
	path := profileOf(t, a.h, auth.DevEmail)

	crop := as(a.h, http.MethodGet, path, auth.DevEmail, nil).Body.String()
	for _, want := range []string{`enctype="multipart/form-data"`, `name="avatar"`, `action="` + path + `/avatar"`} {
		if !strings.Contains(crop, want) {
			t.Errorf("profile page lacks %q", want)
		}
	}
	if strings.Contains(crop, "profile-avatar-img") {
		t.Error("the page shows an uploaded avatar before one exists")
	}
	if rec := as(a.h, http.MethodGet, path+"/avatar", auth.DevEmail, nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET avatar before any upload = %d, want 404", rec.Code)
	}

	a.uploadAndCrop(t, auth.DevEmail, path)

	rec := as(a.h, http.MethodGet, path+"/avatar", auth.DevEmail, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("GET avatar = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	cfg, format, err := image.DecodeConfig(rec.Body)
	if err != nil || format != "webp" || cfg.Width != 60 || cfg.Height != 60 {
		t.Errorf("served %q %dx%d, %v, want a 60x60 webp", format, cfg.Width, cfg.Height, err)
	}
	// Revalidation: the same ETag is not sent again.
	req := httptest.NewRequest(http.MethodGet, path+"/avatar", nil)
	req.Header.Set(auth.DevIdentityHeader, auth.DevEmail)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	again := httptest.NewRecorder()
	a.h.ServeHTTP(again, req)
	if again.Code != http.StatusNotModified {
		t.Errorf("revalidating = %d, want 304", again.Code)
	}

	// The profile page and the header of any page now show it, by the app's
	// own address.
	page := as(a.h, http.MethodGet, path, auth.DevEmail, nil).Body.String()
	if !strings.Contains(page, `data-testid="profile-avatar-img"`) || !strings.Contains(page, `src="`+path+`/avatar?v=`) {
		t.Errorf("profile page does not show the uploaded avatar:\n%s", page)
	}
	home := as(a.h, http.MethodGet, "/", auth.DevEmail, nil).Body.String()
	if !strings.Contains(home, `data-testid="header-avatar-img"`) || !strings.Contains(home, `src="`+path+`/avatar?v=`) {
		t.Errorf("header does not show the uploaded avatar:\n%s", home)
	}
	// Someone with none keeps the default avatar in the header.
	if other := as(a.h, http.MethodGet, "/", auth.DevOtherEmail, nil).Body.String(); strings.Contains(other, "header-avatar-img") {
		t.Error("a person with no upload got an uploaded avatar in the header")
	}
}

// ADR-00033 obligation 4: a member of another household fetches a person's
// avatar by its address only where the visibility rule allows it. The expected
// status comes from the rule itself, so the test says 404 the day the rule
// hides the avatar and 200 while it does not; it never skips.
func TestAnotherHouseholdsMemberFetchesTheAvatarOnlyWhereTheRuleAllows(t *testing.T) {
	a := newAvatarRig(t)
	path := profileOf(t, a.h, auth.DevEmail)
	a.uploadAndCrop(t, auth.DevEmail, path)

	for viewer, rel := range map[string]profile.Relation{auth.DevEmail: profile.Self, auth.DevOtherEmail: profile.Other} {
		want := http.StatusNotFound
		if profile.Visible(rel, profile.FieldAvatar) {
			want = http.StatusOK
		}
		if rec := as(a.h, http.MethodGet, path+"/avatar", viewer, nil); rec.Code != want {
			t.Errorf("%s (%s) GET avatar = %d, want %d from the visibility rule", viewer, rel, rec.Code, want)
		}
	}
	// By address only: a guessed one, and a storage key, give nothing.
	for _, p := range []string{"/profile/AAAAAAAAAAAAAAAAAAAAAA/avatar", "/profile/" + strings.Repeat("A", 22) + "/avatar/../../x", "/profile/not-an-address/avatar"} {
		if rec := as(a.h, http.MethodGet, p, auth.DevOtherEmail, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
}

func TestAvatarIsNotServedWithoutASignedInSession(t *testing.T) {
	d := testDB(t)
	av := profile.NewAvatars(context.Background(), profile.New(d), media.NewLocal(t.TempDir()), nil)
	h := testRouter(t, prodConfig(), Deps{DB: d, Avatars: av, Backup: idleBackup{}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/profile/AAAAAAAAAAAAAAAAAAAAAA/avatar", nil))
	if rec.Code == http.StatusOK || !strings.HasPrefix(rec.Header().Get("Location"), "/") && rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET avatar = %d %q, want a refusal", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAvatarUploadAndCropAreForTheProfilesPersonAlone(t *testing.T) {
	a := newAvatarRig(t)
	path := profileOf(t, a.h, auth.DevEmail)
	if rec := a.upload(t, auth.DevOtherEmail, path, "avatar", photo(t)); rec.Code != http.StatusNotFound {
		t.Errorf("another person's upload = %d, want 404", rec.Code)
	}
	if _, ok, _ := a.avatars.Job(context.Background(), auth.DevEmail); ok {
		t.Fatal("another person's upload was recorded")
	}
	if rec := a.upload(t, auth.DevEmail, path, "avatar", photo(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("own upload = %d", rec.Code)
	}
	j, _, _ := a.avatars.Job(context.Background(), auth.DevEmail)
	for _, tc := range []struct{ method, p string }{
		{http.MethodGet, path + "/avatar/crop"},
		{http.MethodGet, path + "/avatar/preview/" + j.ID},
		{http.MethodGet, path + "/avatar/status"},
		{http.MethodPost, path + "/avatar/crop"},
	} {
		if rec := as(a.h, tc.method, tc.p, auth.DevOtherEmail, url.Values{"id": {j.ID}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as another person = %d, want 404", tc.method, tc.p, rec.Code)
		}
	}
	if rec := as(a.h, http.MethodGet, path+"/avatar/preview/"+j.ID, auth.DevEmail, nil); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("own preview = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestAvatarUploadRefusalsTellThePersonWhatToDo(t *testing.T) {
	a := newAvatarRig(t)
	path := profileOf(t, a.h, auth.DevEmail)
	for name, tc := range map[string]struct {
		field string
		data  []byte
		want  string
	}{
		"over 5 MB":     {"avatar", bytes.Repeat([]byte{1}, media.MaxUploadBytes+10), "5 MB"},
		"not a picture": {"avatar", []byte("hello, not a picture"), "JPEG, PNG, GIF, WebP, HEIC or AVIF"},
		"damaged":       {"avatar", photo(t)[:120], "damaged"},
		"no file":       {"", nil, "Choose a picture"},
		"wrong field":   {"other", photo(t), "Choose a picture"},
	} {
		rec := a.upload(t, auth.DevEmail, path, tc.field, tc.data)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: %d, want 400 mentioning %q:\n%.300s", name, rec.Code, tc.want, rec.Body.String())
		}
	}
	if _, ok, _ := a.avatars.Job(context.Background(), auth.DevEmail); ok {
		t.Error("a refused upload left a job")
	}
	// A crop outside the picture is refused too.
	if rec := a.upload(t, auth.DevEmail, path, "avatar", photo(t)); rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	j, _, _ := a.avatars.Job(context.Background(), auth.DevEmail)
	bad := url.Values{"id": {j.ID}, "x": {"0.8"}, "y": {"0"}, "w": {"0.5"}, "h": {"1"}}
	if rec := as(a.h, http.MethodPost, path+"/avatar/crop", auth.DevEmail, bad); rec.Code != http.StatusBadRequest {
		t.Errorf("crop outside the picture = %d, want 400", rec.Code)
	}
	if rec := as(a.h, http.MethodPost, path+"/avatar/crop", auth.DevEmail, url.Values{"id": {j.ID}, "x": {"abc"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("crop with text = %d, want 400", rec.Code)
	}
}

func TestCropPageEmbedsItsLibraryAndLoadsNothingFromElsewhere(t *testing.T) {
	a := newAvatarRig(t)
	path := profileOf(t, a.h, auth.DevEmail)
	a.upload(t, auth.DevEmail, path, "avatar", photo(t))
	body := as(a.h, http.MethodGet, path+"/avatar/crop", auth.DevEmail, nil).Body.String()
	for _, want := range []string{`/static/vendor/cropper.min.js`, `/static/vendor/cropper.min.css`, `/static/avatar.js`} {
		if !strings.Contains(body, want) {
			t.Errorf("crop page lacks %q", want)
		}
	}
	for _, m := range regexp.MustCompile(`<(?:script|img|link)\b[^>]*\b(?:src|href)="(?:https?:)?//[^"]*"`).FindAllString(body, -1) {
		t.Errorf("crop page loads a resource from another origin: %s", m)
	}
	for _, f := range []string{"/static/vendor/cropper.min.js", "/static/vendor/cropper.min.css", "/static/avatar.js", "/static/avatar.css"} {
		if rec := as(a.h, http.MethodGet, f, auth.DevEmail, nil); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("GET %s = %d, want the embedded file", f, rec.Code)
		}
	}
}

func TestStatusFragmentFollowsTheJobAndRefreshesWhenItIsDone(t *testing.T) {
	a := newAvatarRig(t)
	path := profileOf(t, a.h, auth.DevEmail)
	frag := func(q string) *httptest.ResponseRecorder {
		return as(a.h, http.MethodGet, path+"/avatar/status"+q, auth.DevEmail, nil)
	}
	if body := frag("").Body.String(); !strings.Contains(body, `data-state="none"`) {
		t.Errorf("no upload: %s", body)
	}
	a.upload(t, auth.DevEmail, path, "avatar", photo(t))
	if body := frag("").Body.String(); !strings.Contains(body, `data-state="uploaded"`) || !strings.Contains(body, path+"/avatar/crop") {
		t.Errorf("uploaded: %s", body)
	}
	// The job finishes while the page, drawn without an avatar, is still
	// loading: its status request must tell it to reload, not leave it stale.
	a.uploadAndCrop(t, auth.DevEmail, path)
	if rec := frag("?has="); rec.Header().Get("HX-Refresh") != "true" {
		t.Errorf("a page drawn before the avatar was not told to reload; headers = %v", rec.Header())
	}
	page := as(a.h, http.MethodGet, path, auth.DevEmail, nil).Body.String()
	m := regexp.MustCompile(`avatar/status\?has=([^"]+)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the page's status request names no avatar:\n%s", page)
	}
	if rec := frag("?has=" + m[1]); rec.Header().Get("HX-Refresh") != "" {
		t.Error("a page drawn with the current avatar was told to reload")
	}
}
