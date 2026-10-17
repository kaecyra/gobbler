package sharing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
)

type fakeShares struct {
	sendErr  error
	sent     []string
	accepted int64
	declined int64
	offers   []store.ShareOffer
}

func (f *fakeShares) Send(_ context.Context, _ int64, email string) error {
	f.sent = append(f.sent, email)
	return f.sendErr
}
func (f *fakeShares) Inbox(context.Context) ([]store.ShareOffer, error) { return f.offers, nil }
func (f *fakeShares) Accept(_ context.Context, id int64) (int64, error) {
	f.accepted = id
	return 77, nil
}
func (f *fakeShares) Decline(_ context.Context, id int64) error { f.declined = id; return nil }

type fakeRecipes struct {
	err error
	// onlyID, when set, is the one recipe Get knows; any other id is not found.
	onlyID  int64
	videoID string
	saved   map[string][]byte
}

func (f fakeRecipes) Get(_ context.Context, id int64) (recipe.Recipe, error) {
	if f.onlyID != 0 && id != f.onlyID {
		return recipe.Recipe{}, store.ErrNotFound
	}
	return recipe.Recipe{Name: "<b>Pie</b>", VideoID: f.videoID}, f.err
}

func (f fakeRecipes) HasThumbnail(_ context.Context, v string) (bool, error) {
	_, ok := f.saved[v]
	return ok, nil
}

func (f fakeRecipes) SaveThumbnail(_ context.Context, v string, c []byte, _ string) error {
	f.saved[v] = c
	return nil
}

// stubThumbs stands in for YouTube's image host; no test reaches the network.
type stubThumbs struct {
	asked []string
	fail  error
}

func (s *stubThumbs) Fetch(_ context.Context, id string) (video.Thumbnail, error) {
	s.asked = append(s.asked, id)
	if s.fail != nil {
		return video.Thumbnail{}, s.fail
	}
	return video.Thumbnail{Content: []byte("jpeg:" + id), ContentType: "image/jpeg"}, nil
}

func TestAcceptDownloadsTheThumbnailOfTheCopiesVideo(t *testing.T) {
	recipes := fakeRecipes{onlyID: 77, videoID: "dQw4w9WgXcQ", saved: map[string][]byte{}}
	th := &stubThumbs{}
	rec := do(Routes(Deps{Shares: &fakeShares{}, Recipes: recipes, Thumbnails: th}), http.MethodPost, "/9/accept", nil)
	if rec.Code != http.StatusSeeOther || string(recipes.saved["dQw4w9WgXcQ"]) != "jpeg:dQw4w9WgXcQ" {
		t.Errorf("accept = %d, stored %q, want 303 and the downloaded image", rec.Code, recipes.saved["dQw4w9WgXcQ"])
	}
}

func TestAFailedThumbnailDownloadDoesNotFailTheAccept(t *testing.T) {
	recipes := fakeRecipes{onlyID: 77, videoID: "dQw4w9WgXcQ", saved: map[string][]byte{}}
	th := &stubThumbs{fail: errors.New("youtube is unreachable")}
	rec := do(Routes(Deps{Shares: &fakeShares{}, Recipes: recipes, Thumbnails: th}), http.MethodPost, "/9/accept", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/recipes/77" || len(th.asked) != 1 {
		t.Errorf("accept = %d to %q after %v, want 303 to the recipe after one failed download", rec.Code, rec.Header().Get("Location"), th.asked)
	}
}

func do(h http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSendRedirectsToOneConfirmationWhateverTheAddress(t *testing.T) {
	f := &fakeShares{}
	h := Routes(Deps{Shares: f, Recipes: fakeRecipes{}})
	rec := do(h, http.MethodPost, "/new/5", url.Values{"email": {"anyone@example.test"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/shares/sent" {
		t.Errorf("send = %d to %q, want 303 to /shares/sent", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.sent) != 1 || f.sent[0] != "anyone@example.test" {
		t.Errorf("service saw %v", f.sent)
	}
}

func TestSendShowsWhyAMalformedAddressWasRefused(t *testing.T) {
	f := &fakeShares{sendErr: household.ErrInvalid}
	rec := do(Routes(Deps{Shares: f, Recipes: fakeRecipes{}}), http.MethodPost, "/new/5", url.Values{"email": {"x"}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestSendFormEscapesTheRecipeNameAndMissingRecipeIs404(t *testing.T) {
	rec := do(Routes(Deps{Shares: &fakeShares{}, Recipes: fakeRecipes{}}), http.MethodGet, "/new/5", nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "<b>Pie</b>") || !strings.Contains(rec.Body.String(), "&lt;b&gt;Pie") {
		t.Errorf("form = %d, body must show the escaped name", rec.Code)
	}
	missing := Routes(Deps{Shares: &fakeShares{}, Recipes: fakeRecipes{err: store.ErrNotFound}})
	if rec := do(missing, http.MethodGet, "/new/5", nil); rec.Code != http.StatusNotFound {
		t.Errorf("recipe of another household = %d, want 404", rec.Code)
	}
	for _, p := range []string{"/new/x", "/new/0", "/new/-3"} {
		if rec := do(missing, http.MethodGet, p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
}

func TestAcceptGoesToTheNewRecipeAndDeclineBackToTheInbox(t *testing.T) {
	f := &fakeShares{}
	h := Routes(Deps{Shares: f, Recipes: fakeRecipes{}})
	rec := do(h, http.MethodPost, "/9/accept", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/recipes/77" || f.accepted != 9 {
		t.Errorf("accept = %d to %q (share %d)", rec.Code, rec.Header().Get("Location"), f.accepted)
	}
	rec = do(h, http.MethodPost, "/9/decline", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/shares" || f.declined != 9 {
		t.Errorf("decline = %d to %q (share %d)", rec.Code, rec.Header().Get("Location"), f.declined)
	}
	if rec := do(h, http.MethodPost, "/nope/accept", nil); rec.Code != http.StatusNotFound {
		t.Errorf("accept of a bad id = %d, want 404", rec.Code)
	}
}

func TestInboxListsOffersByRecipeAndSenderNameOnly(t *testing.T) {
	f := &fakeShares{offers: []store.ShareOffer{{ID: 4, SenderName: "Alpha Kitchen", RecipeName: "Pie", SentAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Ingredients: []string{"secret fruit"}}}}
	rec := do(Routes(Deps{Shares: f, Recipes: fakeRecipes{}}), http.MethodGet, "/", nil)
	body := rec.Body.String()
	for _, want := range []string{"Pie", "Alpha Kitchen", "2026-03-01", "/shares/4/accept", "/shares/4/decline"} {
		if !strings.Contains(body, want) {
			t.Errorf("inbox lacks %q", want)
		}
	}
	if strings.Contains(body, "secret fruit") {
		t.Error("the inbox shows the sender's ingredient names before the share is accepted")
	}
	empty := do(Routes(Deps{Shares: &fakeShares{}, Recipes: fakeRecipes{}}), http.MethodGet, "/", nil)
	if !strings.Contains(empty.Body.String(), "no-shares") {
		t.Error("an empty inbox says nothing is waiting")
	}
}

func TestAcceptStillRedirectsWhenTheAcceptedRecipeCannotBeRead(t *testing.T) {
	th := &stubThumbs{}
	rec := do(Routes(Deps{Shares: &fakeShares{}, Recipes: fakeRecipes{err: store.ErrNotFound}, Thumbnails: th}), http.MethodPost, "/9/accept", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/recipes/77" || len(th.asked) != 0 {
		t.Errorf("accept = %d to %q after %v, want 303 to the recipe and no download", rec.Code, rec.Header().Get("Location"), th.asked)
	}
}
