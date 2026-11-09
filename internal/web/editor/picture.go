package editor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/web/layout"
	webpicture "github.com/kaecyra/gobbler/internal/web/picture"
)

// The picture picker at the top of the edit page (ADR-00051). Its tiles are
// only choices: the current picture, what a search of the source page found,
// an uploaded picture and "No picture". The choice is a radio of the editor's
// form, so it is posted with Save and applied after the recipe is saved, like
// every other field. Searching and uploading are buttons under the tiles that
// swap the picker; neither changes the recipe.

// Pictures is what the picker needs from the recipe service
// (*apirecipes.Service satisfies it).
type Pictures interface {
	PictureKey(ctx context.Context, c apirecipes.Caller, id int64) (string, bool, error)
	OfferedPictures(ctx context.Context, c apirecipes.Caller, id int64) ([]apirecipes.Offered, error)
	FindPictures(ctx context.Context, c apirecipes.Caller, id int64) ([]apirecipes.Offered, error)
	OfferPicture(ctx context.Context, c apirecipes.Caller, id int64, data []byte) (apirecipes.Offered, error)
	SaveWithPicture(ctx context.Context, c apirecipes.Caller, id int64, choice apirecipes.PictureChoice, save func(context.Context) error) error
}

const (
	// fieldPicture is the picker's radio group. Its radios belong to the
	// editor's form (FormID), so the choice is posted with Save.
	fieldPicture = "picture"
	// pictureKeep and pictureNone are the values of "Current picture" and "No
	// picture"; an offered picture's value is its ID, so a choice names the
	// picture the person saw and a later search cannot change what it means.
	pictureKeep = "keep"
	pictureNone = "none"
	// uploadField is the multipart field of an uploaded picture.
	uploadField = "file"
	// pickerID is the picker's element, which a search or upload swaps.
	pickerID = "edit-picture"
	// checkedChoice selects the picker's chosen radio, which a search posts
	// so the picker it answers with keeps the choice. The radios belong to the
	// editor's form, so the search's own form does not post them.
	checkedChoice = "#" + pickerID + " input[name=" + fieldPicture + "]:checked"
	// searchParam carries a search's outcome to the edit page a browser
	// without scripts is sent back to: searchEmpty or searchFailed. With it
	// the page shows the outcome and starts no search of its own.
	searchParam  = "search"
	searchEmpty  = "empty"
	searchFailed = "failed"
	// offerGone is shown when the chosen offered picture is gone at Save: a
	// later search or upload replaced it, or it expired.
	offerGone = "The picture you chose is no longer offered, so nothing was saved. Choose a picture again, then save."
)

// pictureView is the picker.
type pictureView struct {
	ID int64
	// CurrentSrc is the address of the recipe's picture; empty when it has
	// none, and then "Current picture" is not offered.
	CurrentSrc string
	Offers     []offerTile
	// Choice is the value of the chosen radio.
	Choice string
	// CanSearch is whether the recipe has a source page to search.
	CanSearch bool
	// Searching starts a search as soon as the page loads (a recipe with no
	// picture and nothing offered).
	Searching bool
	// NothingFound says the search just run found no picture.
	NothingFound bool
	// SearchFailed says the search just run failed.
	SearchFailed bool
	// Refusal is why an upload was refused.
	Refusal string
}

// offerTile is an offered picture in the picker.
type offerTile struct {
	Value         string
	Src           string
	Width, Height int
	Uploaded      bool
}

func (v pictureView) searchPath() string { return editPath(v.ID) + "/picture/search" }
func (v pictureView) uploadPath() string { return editPath(v.ID) + "/picture/upload" }

func (v pictureView) class() string {
	if v.Searching {
		return "picker edit-picture searching"
	}
	return "picker edit-picture"
}

func (o offerTile) size() string { return fmt.Sprintf("%d × %d", o.Width, o.Height) }

// picture builds the picker for recipe id, whose source page is sourceURL,
// with choice chosen; an empty choice is the default, which leaves the picture
// as it is. auto lets a recipe with no picture and nothing offered start a
// search on load.
func (h *handler) picture(ctx context.Context, id int64, sourceURL, choice string, auto bool) (*pictureView, error) {
	if h.Pictures == nil {
		return nil, nil
	}
	c := caller.FromContext(ctx)
	key, has, err := h.Pictures.PictureKey(ctx, c, id)
	if err != nil {
		return nil, fmt.Errorf("read the picture of recipe %d: %w", id, err)
	}
	offers, err := h.Pictures.OfferedPictures(ctx, c, id)
	if err != nil {
		return nil, fmt.Errorf("read the pictures offered for recipe %d: %w", id, err)
	}
	v := &pictureView{ID: id, CanSearch: sourceURL != ""}
	if has {
		v.CurrentSrc = webpicture.Src(id, key)
	}
	for _, o := range offers {
		v.Offers = append(v.Offers, offerTile{
			Value: o.ID, Src: webpicture.OfferedSrc(id, o.ID),
			Width: o.Width, Height: o.Height, Uploaded: o.Uploaded,
		})
	}
	v.Choice = choice
	if !v.offers(choice) {
		v.Choice = pictureNone
		if has {
			v.Choice = pictureKeep
		}
	}
	v.Searching = auto && v.CanSearch && !has && len(offers) == 0
	return v, nil
}

// offers reports whether value is one of the picker's radios.
func (v pictureView) offers(value string) bool {
	switch value {
	case pictureNone:
		return true
	case pictureKeep:
		return v.CurrentSrc != ""
	}
	for _, o := range v.Offers {
		if o.Value == value {
			return true
		}
	}
	return false
}

// pictureChoice reads the choice posted with the form. No value, which a form
// without the picker posts, leaves the picture as it is.
func pictureChoice(form url.Values) (apirecipes.PictureChoice, error) {
	switch v := form.Get(fieldPicture); v {
	case "", pictureKeep:
		return apirecipes.KeepPicture, nil
	case pictureNone:
		return apirecipes.NoPicture, nil
	default:
		if !apirecipes.ValidOfferID(v) {
			return apirecipes.PictureChoice{}, fmt.Errorf("%w: picture %q", layout.ErrInvalid, v)
		}
		return apirecipes.PictureChoice{Offer: v}, nil
	}
}

// saveWithPicture runs write, the save of the recipe, with the form's picture
// choice (apirecipes.Service.SaveWithPicture). The choice is checked and
// applied under the recipe's picture lock with write in between, so a choice
// whose picture is no longer offered is refused before the recipe is written
// (ErrOfferGone), and the picture checked is the one applied. Catalog entries
// the form proposed are added before this, and stay when the choice is
// refused, as they do for a stale save. Without a picture service the choice
// must be to keep the picture.
func (h *handler) saveWithPicture(ctx context.Context, id int64, choice apirecipes.PictureChoice, write func(context.Context) error) error {
	if h.Pictures == nil {
		if choice != apirecipes.KeepPicture {
			return fmt.Errorf("%w: this editor has no picture picker", layout.ErrInvalid)
		}
		return write(ctx)
	}
	return h.Pictures.SaveWithPicture(ctx, caller.FromContext(ctx), id, choice, write)
}

// searchPictures searches the source page and shows the picker with what it
// found, keeping the choice the picker posts with the search unless the
// search replaced the picture chosen. A search that fails for any reason but
// the recipe is logged and said in the picker, so the picker never stays in
// its searching state. A browser without scripts lands on the edit page with
// the outcome in searchParam.
func (h *handler) searchPictures(w http.ResponseWriter, r *http.Request) {
	id, ok := recipeID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("%w: read search: %w", layout.ErrInvalid, err))
		return
	}
	found, err := h.Pictures.FindPictures(ctx, caller.FromContext(ctx), id)
	failed := false
	switch {
	case errors.Is(err, apirecipes.ErrNotFound), errors.Is(err, apirecipes.ErrNotMember):
		layout.WriteError(w, r, fmt.Errorf("search the source page of recipe %d: %w", id, err))
		return
	case err != nil:
		slog.ErrorContext(ctx, "picture search failed", "recipe_id", id, "error", err.Error())
		failed = true
	}
	if !FromHTMX(r) {
		to := editPath(id)
		switch {
		case failed:
			to += "?" + searchParam + "=" + searchFailed
		case len(found) == 0:
			to += "?" + searchParam + "=" + searchEmpty
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	h.showPicker(w, r, id, r.PostForm.Get(fieldPicture), func(v *pictureView) {
		v.SearchFailed = failed
		v.NothingFound = !failed && len(found) == 0
	})
}

// uploadPicture keeps an uploaded file as an offered picture and shows the
// picker with it chosen. A file the person must fix is said in the picker,
// which keeps the choice posted with the upload. The upload form carries that
// choice in a field ahead of the file, which editor.js keeps in step with the
// picker's radios, so it is read even when the file is over the size limit
// and the rest of the body is not. A browser without scripts gets the refusal
// on a page of its own, as an avatar upload does.
func (h *handler) uploadPicture(w http.ResponseWriter, r *http.Request) {
	id, ok := recipeID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	// Another household's recipe is not found before its upload is read, so a
	// bad file cannot tell it apart from one that exists.
	if _, err := h.Recipes.Info(ctx, caller.FromContext(ctx), id); err != nil {
		layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
		return
	}
	data, posted, err := layout.ReadUploadWith(w, r, uploadField, fieldPicture)
	var o apirecipes.Offered
	if err == nil {
		o, err = h.Pictures.OfferPicture(ctx, caller.FromContext(ctx), id, data)
	}
	var refusal *media.Refusal
	switch {
	case errors.As(err, &refusal) && !FromHTMX(r):
		layout.Render(w, r, http.StatusBadRequest, layout.ErrorPage(http.StatusBadRequest, refusal.Msg))
	case errors.As(err, &refusal):
		h.showPicker(w, r, id, posted[fieldPicture], func(v *pictureView) { v.Refusal = refusal.Msg })
	case err != nil:
		layout.WriteError(w, r, fmt.Errorf("upload a picture for recipe %d: %w", id, err))
	default:
		h.showPicker(w, r, id, o.ID, nil)
	}
}

// showPicker answers a search or upload: the picker for htmx, and otherwise
// the edit page, where the offered pictures now are.
func (h *handler) showPicker(w http.ResponseWriter, r *http.Request, id int64, choice string, adjust func(*pictureView)) {
	if !FromHTMX(r) {
		http.Redirect(w, r, editPath(id), http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	rec, err := h.Recipes.Get(ctx, caller.FromContext(ctx), id)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("load recipe %d: %w", id, err))
		return
	}
	v, err := h.picture(ctx, id, rec.SourceURL, choice, false)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if adjust != nil {
		adjust(v)
	}
	layout.Render(w, r, http.StatusOK, picturePicker(v))
}
