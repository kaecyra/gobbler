package profile

import (
	"bytes"
	"context"
	"database/sql"
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

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/media"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The avatar section behind the real identity middleware and a real database
// and media directory. Package web's tests drive the same routes through the
// whole router.

type avatarSite struct {
	t       *testing.T
	h       http.Handler
	avatars *domain.Avatars
	db      *sql.DB
}

func newAvatarSite(t *testing.T) *avatarSite {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	av := domain.NewAvatars(ctx, domain.New(d), media.NewLocal(t.TempDir()), nil)
	r := chi.NewRouter()
	r.Use(a.Middleware(layout.IsPublic))
	r.Mount(Pattern, Routes(Deps{Profiles: domain.New(d), Households: fakeHouseholds{}, DB: d, Avatars: av}))
	return &avatarSite{t: t, h: r, avatars: av, db: d}
}

func (s *avatarSite) do(req *http.Request, email string) *httptest.ResponseRecorder {
	req.Header.Set(auth.DevIdentityHeader, email)
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

func (s *avatarSite) get(target, email string) *httptest.ResponseRecorder {
	return s.do(httptest.NewRequest(http.MethodGet, target, nil), email)
}

func (s *avatarSite) postForm(target, email string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(req, email)
}

func (s *avatarSite) upload(target, email, field string, data []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if field != "" {
		fw, err := mw.CreateFormFile(field, "p.jpg")
		if err != nil {
			s.t.Fatal(err)
		}
		_, _ = fw.Write(data)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return s.do(req, email)
}

// htmx marks a request as coming from the dialog; it gets fragments, not pages.
func (s *avatarSite) hx(req *http.Request, email string) *httptest.ResponseRecorder {
	req.Header.Set("HX-Request", "true")
	return s.do(req, email)
}

func (s *avatarSite) profileOf(email string) string {
	s.t.Helper()
	rec := s.get("/profile", email)
	if rec.Code != http.StatusSeeOther {
		s.t.Fatalf("GET /profile = %d", rec.Code)
	}
	return rec.Header().Get("Location")
}

func picture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 90, 60))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 10, G: 150, B: 90, A: 255}), image.Point{}, draw.Src)
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAvatarRoutesTakeAPictureThroughCropToAServedWebP(t *testing.T) {
	s := newAvatarSite(t)
	const me = "ann@example.com"
	path := s.profileOf(me)

	if rec := s.get(path+"/avatar", me); rec.Code != http.StatusNotFound {
		t.Errorf("avatar before an upload = %d, want 404", rec.Code)
	}
	if rec := s.get(path+"/avatar/crop", me); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Errorf("crop page with no upload = %d to %q, want a redirect to the profile", rec.Code, rec.Header().Get("Location"))
	}
	if rec := s.upload(path+"/avatar", me, "avatar", picture(t)); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path+"/avatar/crop" {
		t.Fatalf("upload = %d to %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	status := s.get(path+"/avatar/status", me).Body.String()
	if !strings.Contains(status, `data-state="uploaded"`) {
		t.Errorf("status after upload: %s", status)
	}
	page := s.get(path+"/avatar/crop", me)
	id := regexp.MustCompile(`name="id" value="([A-Za-z0-9_-]{22})"`).FindStringSubmatch(page.Body.String())
	if page.Code != http.StatusOK || id == nil {
		t.Fatalf("crop page = %d:\n%s", page.Code, page.Body)
	}
	if rec := s.get(path+"/avatar/preview/"+id[1], me); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("preview = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	crop := url.Values{"id": {id[1]}, "x": {"0"}, "y": {"0"}, "w": {"0.5"}, "h": {"1"}}
	if rec := s.postForm(path+"/avatar/crop", me, crop); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Fatalf("crop = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	s.avatars.Wait()

	rec := s.get(path+"/avatar", me)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/webp" || rec.Header().Get("ETag") == "" {
		t.Fatalf("avatar = %d %q etag=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("ETag"))
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes())); err != nil || format != "webp" {
		t.Errorf("served %q, %v, want webp", format, err)
	}
	// A different person sees it too, where the visibility rule allows, and
	// the profile page of either now shows the uploaded picture.
	if other := s.get(path+"/avatar", "bob@example.com"); other.Code != http.StatusOK {
		t.Errorf("another person's fetch = %d, want 200 where Visible allows", other.Code)
	}
	if body := s.get(path, "bob@example.com").Body.String(); !strings.Contains(body, `data-testid="profile-avatar-img"`) {
		t.Errorf("another person's view of the profile has no uploaded avatar:\n%s", body)
	}
	// A page drawn before the avatar existed is stale, even if the job finished
	// before its first status request; one drawn with the current avatar is not.
	if rec := s.get(path+"/avatar/status?has=", me); rec.Header().Get("HX-Refresh") != "true" {
		t.Error("a stale page was not told to reload")
	}
	own := s.get(path, me).Body.String()
	has := regexp.MustCompile(`avatar/status\?has=([^"]+)"`).FindStringSubmatch(own)
	if has == nil {
		t.Fatalf("the page's status request names no avatar:\n%s", own)
	}
	if rec := s.get(path+"/avatar/status?has="+has[1], me); rec.Header().Get("HX-Refresh") != "" {
		t.Error("an up-to-date page was told to reload")
	}
	// Revalidation.
	req := httptest.NewRequest(http.MethodGet, path+"/avatar", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	if again := s.do(req, me); again.Code != http.StatusNotModified {
		t.Errorf("revalidation = %d, want 304", again.Code)
	}
}

func TestAvatarRoutesRefuseWhatThePersonMustFix(t *testing.T) {
	s := newAvatarSite(t)
	const me = "ann@example.com"
	path := s.profileOf(me)
	for name, tc := range map[string]struct {
		field string
		data  []byte
		code  int
		want  string
	}{
		"over 5 MB":     {"avatar", bytes.Repeat([]byte{9}, media.MaxUploadBytes+multipartOverhead+10), http.StatusBadRequest, "5 MB"},
		"not a picture": {"avatar", []byte("text"), http.StatusBadRequest, "JPEG, PNG, GIF, WebP, HEIC or AVIF"},
		"no file":       {"", nil, http.StatusBadRequest, "Choose a picture"},
		"wrong field":   {"photo", picture(t), http.StatusBadRequest, "Choose a picture"},
	} {
		rec := s.upload(path+"/avatar", me, tc.field, tc.data)
		if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: %d, want %d mentioning %q:\n%.200s", name, rec.Code, tc.code, tc.want, rec.Body)
		}
	}
	if rec := s.postForm(path+"/avatar", me, url.Values{"avatar": {"x"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("a form that is not multipart = %d, want 400", rec.Code)
	}

	// A crop with text, and one outside the picture, are refused.
	if rec := s.upload(path+"/avatar", me, "avatar", picture(t)); rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	j, _, _ := s.avatars.Job(context.Background(), me)
	if rec := s.postForm(path+"/avatar/crop", me, url.Values{"id": {j.ID}, "x": {"left"}, "y": {"0"}, "w": {"1"}, "h": {"1"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("crop with text = %d, want 400", rec.Code)
	}
	if rec := s.postForm(path+"/avatar/crop", me, url.Values{"id": {j.ID}, "x": {"0.9"}, "y": {"0"}, "w": {"0.5"}, "h": {"1"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("crop outside the picture = %d, want 400", rec.Code)
	}
	if rec := s.postForm(path+"/avatar/crop", me, url.Values{"id": {strings.Repeat("A", 22)}, "x": {"0"}, "y": {"0"}, "w": {"1"}, "h": {"1"}}); rec.Code != http.StatusNotFound {
		t.Errorf("crop of an upload that is not theirs = %d, want 404", rec.Code)
	}
}

func TestAvatarSectionIsClosedToEveryoneButThePerson(t *testing.T) {
	s := newAvatarSite(t)
	path := s.profileOf("ann@example.com")
	if rec := s.upload(path+"/avatar", "ann@example.com", "avatar", picture(t)); rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	j, _, _ := s.avatars.Job(context.Background(), "ann@example.com")
	const bob = "bob@example.com"
	for _, target := range []string{path + "/avatar/crop", path + "/avatar/status", path + "/avatar/preview/" + j.ID} {
		if rec := s.get(target, bob); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s as another person = %d, want 404", target, rec.Code)
		}
	}
	if rec := s.upload(path+"/avatar", bob, "avatar", picture(t)); rec.Code != http.StatusNotFound {
		t.Errorf("upload as another person = %d, want 404", rec.Code)
	}
	if rec := s.postForm(path+"/avatar/crop", bob, url.Values{"id": {j.ID}, "x": {"0"}, "y": {"0"}, "w": {"1"}, "h": {"1"}}); rec.Code != http.StatusNotFound {
		t.Errorf("crop as another person = %d, want 404", rec.Code)
	}
	if body := s.get(path, bob).Body.String(); strings.Contains(body, `name="avatar"`) {
		t.Error("another person's view of the profile carries the upload form")
	}
}

func TestStatusFragmentShowsEachState(t *testing.T) {
	for state, want := range map[string]string{
		"": `data-state="none"`, domain.JobUploaded: `data-state="uploaded"`,
		domain.JobQueued: `hx-trigger="every 2s"`, domain.JobFailed: "Try the crop again",
	} {
		var body bytes.Buffer
		v := newStatusView("addr", domain.Job{State: state, Error: "boom"}, state != "", "")
		if err := statusFragment(v).Render(context.Background(), &body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.String(), want) {
			t.Errorf("state %q: want %q in:\n%s", state, want, body.String())
		}
	}
}

func TestAvatarSrcChangesWithTheAvatar(t *testing.T) {
	a := AvatarSrc("addr22", "avatars/one.webp")
	b := AvatarSrc("addr22", "avatars/two.webp")
	if a == b || !strings.HasPrefix(a, "/profile/addr22/avatar?v=") || strings.Contains(a, "webp") {
		t.Errorf("AvatarSrc = %q, %q", a, b)
	}
}

// The serving route goes through the visibility rule: a relation the rule does
// not know sees no avatar, even when the profile has one.
func TestServeAvatarAsksTheVisibilityRule(t *testing.T) {
	s := newAvatarSite(t)
	const me = "ann@example.com"
	path := s.profileOf(me)
	s.upload(path+"/avatar", me, "avatar", picture(t))
	j, _, _ := s.avatars.Job(context.Background(), me)
	if rec := s.postForm(path+"/avatar/crop", me, url.Values{"id": {j.ID}, "x": {"0"}, "y": {"0"}, "w": {"1"}, "h": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	s.avatars.Wait()
	p, err := domain.New(s.db).ByEmail(context.Background(), me)
	if err != nil || p.Avatar == "" {
		t.Fatalf("profile = %+v, %v", p, err)
	}
	h := &handler{Deps{Avatars: s.avatars}}
	for rel, want := range map[domain.Relation]int{domain.Self: http.StatusOK, domain.Other: http.StatusOK, "unknown": http.StatusNotFound} {
		req := httptest.NewRequest(http.MethodGet, "/avatar", nil)
		req = req.WithContext(context.WithValue(req.Context(), subjectKey{}, subject{Profile: p, Viewer: "x@example.com", Relation: rel}))
		rec := httptest.NewRecorder()
		h.serveAvatar(rec, req)
		if rec.Code != want {
			t.Errorf("relation %q = %d, want %d", rel, rec.Code, want)
		}
	}
}

// The dialog's requests carry HX-Request and stay inside the dialog: the upload
// answers with the crop step, the crop with the status, and a refusal with a
// message in the first step.
func TestAvatarDialogStepsAreFragments(t *testing.T) {
	s := newAvatarSite(t)
	const me = "dia@example.com"
	path := s.profileOf(me)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("avatar", "p.jpg")
	_, _ = fw.Write(picture(t))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path+"/avatar", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	up := s.hx(req, me)
	id := regexp.MustCompile(`name="id" value="([A-Za-z0-9_-]{22})"`).FindStringSubmatch(up.Body.String())
	if up.Code != http.StatusOK || id == nil || strings.Contains(up.Body.String(), "<html") || !strings.Contains(up.Body.String(), `data-step="crop"`) {
		t.Fatalf("upload from the dialog = %d, want the crop step as a fragment:\n%s", up.Code, up.Body)
	}

	// A refused crop shows its message in the first step and saves nothing.
	bad := httptest.NewRequest(http.MethodPost, path+"/avatar/crop", strings.NewReader(url.Values{"id": {id[1]}, "x": {"0.9"}, "y": {"0"}, "w": {"0.5"}, "h": {"1"}}.Encode()))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := s.hx(bad, me); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-testid="avatar-message"`) {
		t.Errorf("refused crop from the dialog = %d:\n%s", rec.Code, rec.Body)
	}

	good := httptest.NewRequest(http.MethodPost, path+"/avatar/crop", strings.NewReader(url.Values{"id": {id[1]}, "x": {"0"}, "y": {"0"}, "w": {"0.5"}, "h": {"1"}}.Encode()))
	good.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := s.hx(good, me)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-step="saved"`) || !strings.Contains(rec.Body.String(), `every 2s`) {
		t.Errorf("crop from the dialog = %d, want the status step polling:\n%s", rec.Code, rec.Body)
	}
	s.avatars.Wait()
	if rec := s.get(path+"/avatar", me); rec.Code != http.StatusOK {
		t.Errorf("avatar after the dialog's crop = %d, want 200", rec.Code)
	}

	// A non-picture is refused in the dialog with the same message as before.
	var junk bytes.Buffer
	jw := multipart.NewWriter(&junk)
	jf, _ := jw.CreateFormFile("avatar", "n.jpg")
	_, _ = jf.Write([]byte("not a picture"))
	_ = jw.Close()
	bad2 := httptest.NewRequest(http.MethodPost, path+"/avatar", &junk)
	bad2.Header.Set("Content-Type", jw.FormDataContentType())
	if rec := s.hx(bad2, me); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "not a JPEG") || !strings.Contains(rec.Body.String(), `data-step="upload"`) {
		t.Errorf("refused upload from the dialog = %d:\n%s", rec.Code, rec.Body)
	}
}

// A failure that is not a refusal is not turned into a message the dialog would
// swap in as if it had worked: it keeps its status, so the dialog's error
// handling sees it.
func TestAvatarDialogErrorsKeepTheirStatus(t *testing.T) {
	s := newAvatarSite(t)
	const me = "err@example.com"
	path := s.profileOf(me)

	unknown := httptest.NewRequest(http.MethodPost, path+"/avatar/crop", strings.NewReader(url.Values{"id": {strings.Repeat("A", 22)}, "x": {"0"}, "y": {"0"}, "w": {"1"}, "h": {"1"}}.Encode()))
	unknown.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := s.hx(unknown, me); rec.Code != http.StatusNotFound {
		t.Errorf("crop of an unknown upload from the dialog = %d, want 404", rec.Code)
	}
	if rec := s.hx(httptest.NewRequest(http.MethodGet, path+"/avatar/preview/"+strings.Repeat("A", 22), nil), me); rec.Code != http.StatusNotFound {
		t.Errorf("preview of an unknown upload from the dialog = %d, want 404", rec.Code)
	}
}

// The upload and crop steps answer differently to htmx and to a browser, so a
// cache must keep the two apart.
func TestAvatarStepsVaryOnHTMX(t *testing.T) {
	s := newAvatarSite(t)
	const me = "vary@example.com"
	path := s.profileOf(me)
	for _, target := range []string{path + "/avatar/upload", path + "/avatar/crop"} {
		if got := s.get(target, me).Header().Get("Vary"); !strings.Contains(got, "HX-Request") {
			t.Errorf("%s Vary = %q, want HX-Request", target, got)
		}
	}
	if rec := s.get(path+"/avatar/upload", me); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="avatar"`) || !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("upload page = %d, want a page with the upload form", rec.Code)
	}
	if rec := s.hx(httptest.NewRequest(http.MethodGet, path+"/avatar/upload", nil), me); strings.Contains(rec.Body.String(), "<html") || !strings.Contains(rec.Body.String(), `data-step="upload"`) {
		t.Errorf("upload step for htmx is not a bare fragment:\n%s", rec.Body)
	}
}
