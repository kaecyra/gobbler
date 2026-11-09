package picture

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture/picturedb"
	"github.com/kaecyra/gobbler/internal/store"
)

// Pictures offered for a saved recipe on its edit page (ADR-00051): what a
// search of its source page found, and a picture uploaded there. Each has an
// opaque ID, a random token never reused, and the edit form chooses by it: a
// later search or upload adds pictures under new IDs, so a choice already on
// the page names the picture the person saw or nothing at all. They are kept
// as objects under recipes/<recipe_id>/found/<id>.webp until the edit form is
// saved (Apply) or FoundLifetime passes (Reaper.SweepFound).

const (
	// FoundLifetime is how long an offered picture is kept when the edit form
	// is never saved.
	FoundLifetime = 14 * 24 * time.Hour

	kindFound  = "found"
	kindUpload = "upload"

	// foundAtLayout has a fixed width, so found_at sorts as text in SQL; the
	// trailing zeros RFC3339Nano drops would put "05.5Z" before "05Z".
	foundAtLayout = "2006-01-02T15:04:05.000000000Z"
)

// ErrOfferGone is a choice naming an offered picture the recipe no longer
// has: a later search or upload replaced it, or it expired. Apply returns it
// before anything is written.
var ErrOfferGone = errors.New("that picture is no longer offered")

// ErrNotApplied wraps a failure to set or remove the picture after the save
// Apply was given has succeeded: the recipe is written, its picture is not.
var ErrNotApplied = errors.New("the recipe was saved, but its picture was not changed")

// offerID matches an offered picture's ID: what newVersion draws.
var offerID = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

// ValidOfferID reports whether s has the shape of an offered picture's ID, so
// a caller can refuse a malformed one as bad input before asking.
func ValidOfferID(s string) bool { return offerID.MatchString(s) }

// recipeLocks serialises the replacing, choosing and sweeping of one recipe's
// offered pictures. It is the package's, not a Service's: the sweep runs from
// a Reaper, and gobbler is one process over one SQLite file.
var recipeLocks keyedLock

// Pages fetches a recipe's source page live, the way an import does;
// *ingest.PageFetcher satisfies it. A failure is an error, after which the
// search reads the saved snapshot.
type Pages interface {
	FetchPage(ctx context.Context, rawURL string) (body []byte, finalURL string, err error)
}

// WithPages sets the live page fetch of a search (ADR-00051). It returns s for
// chaining.
func (s *Service) WithPages(p Pages) *Service {
	s.pages = p
	return s
}

// Offered is a picture offered for a saved recipe.
type Offered struct {
	// ID names the picture for a choice. It is never reused.
	ID string
	// Uploaded is the picture a person uploaded; otherwise a search found it.
	Uploaded      bool
	SourceURL     string
	Key           string
	Width, Height int
}

// Choice is what the edit form chose for the recipe's picture: an offered
// picture by its ID, the removal of the picture, or neither, which keeps it.
type Choice struct {
	// Offer is the ID of the chosen offered picture.
	Offer string
	// Remove removes the picture.
	Remove bool
}

// ChoiceKeep leaves the picture as it is; ChoiceNone removes it.
var (
	ChoiceKeep = Choice{}
	ChoiceNone = Choice{Remove: true}
)

func foundKey(recipeID int64, offer string) string {
	return "recipes/" + strconv.FormatInt(recipeID, 10) + "/found/" + offer + ".webp"
}

// foundAt is t as found_at stores it.
func foundAt(t time.Time) string { return t.UTC().Format(foundAtLayout) }

// FindOffer returns the picture of offers whose ID is id, and whether there
// is one. Apply and the picture route both look a choice up through it.
func FindOffer(offers []Offered, id string) (Offered, bool) {
	for _, o := range offers {
		if o.ID == id {
			return o, true
		}
	}
	return Offered{}, false
}

// sourcePage returns the page recipe id's pictures are searched on and the
// address its relative image addresses resolve against: the live page at
// source when there is a page fetcher and the fetch succeeds, else the newest
// saved html snapshot. ok is false when there is neither.
func (s *Service) sourcePage(ctx context.Context, id int64, source string) (content []byte, base string, ok bool, err error) {
	if s.pages != nil && source != "" {
		body, final, err := s.pages.FetchPage(ctx, source)
		if err == nil {
			return body, final, true, nil
		}
		s.log.InfoContext(ctx, "picture search reads the saved page: the live fetch failed", "recipe_id", id, "url", source, "error", err.Error())
	}
	snap, err := s.snapshots.GetSnapshot(ctx, id, store.SnapshotHTML)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("read html snapshot of recipe %d: %w", id, err)
	}
	return snap.Content, source, true, nil
}

// Search finds recipe id's pictures on its source page (Refind) and keeps them
// as its offered pictures, replacing what an earlier search found. An uploaded
// picture stays. It returns what it found, which may be nothing. The fetch
// and downloads run before the recipe's lock is taken, so a save waits only
// for the replacement.
func (s *Service) Search(ctx context.Context, id int64) ([]Offered, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	found, err := s.Refind(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]Offered, 0, len(found))
	data := make([][]byte, 0, len(found))
	for _, f := range found {
		o, err := s.newOffer(id, false, f.SourceURL, f.Width, f.Height)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
		data = append(data, f.WebP)
	}
	if err := s.replace(ctx, hid, id, kindFound, out, data); err != nil {
		return nil, err
	}
	return out, nil
}

// Offer keeps an uploaded file as recipe id's uploaded picture, replacing an
// earlier one, with the limits and refusals of an avatar (ADR-00033): a file
// the person must fix is a *media.Refusal. The recipe's picture is unchanged
// until Apply.
func (s *Service) Offer(ctx context.Context, id int64, data []byte) (Offered, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Offered{}, err
	}
	if _, err := s.source(ctx, hid, id); err != nil {
		return Offered{}, err
	}
	d, err := media.Decode(data)
	if err != nil {
		return Offered{}, err
	}
	webp, err := media.Picture(d.Image)
	if err != nil {
		return Offered{}, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(webp))
	if err != nil {
		return Offered{}, fmt.Errorf("read size of processed picture: %w", err)
	}
	o, err := s.newOffer(id, true, "", cfg.Width, cfg.Height)
	if err != nil {
		return Offered{}, err
	}
	if err := s.replace(ctx, hid, id, kindUpload, []Offered{o}, [][]byte{webp}); err != nil {
		return Offered{}, err
	}
	return o, nil
}

func (s *Service) newOffer(id int64, uploaded bool, sourceURL string, w, h int) (Offered, error) {
	offer, err := s.newVersion()
	if err != nil {
		return Offered{}, fmt.Errorf("draw offered picture id: %w", err)
	}
	return Offered{ID: offer, Uploaded: uploaded, SourceURL: sourceURL, Key: foundKey(id, offer), Width: w, Height: h}, nil
}

// replace makes offers recipe id's offered pictures of kind. The new objects
// are stored first and the rows swapped in one transaction, so a failure at
// any step leaves the previous pictures as they were; the previous objects
// are deleted only after the swap commits.
func (s *Service) replace(ctx context.Context, hid, id int64, kind string, offers []Offered, data [][]byte) error {
	var put []string
	for i, o := range offers {
		if err := s.media.Put(ctx, o.Key, data[i], "image/webp"); err != nil {
			s.removeKeys(ctx, put)
			return fmt.Errorf("store offered picture %s of recipe %d: %w", o.ID, id, err)
		}
		put = append(put, o.Key)
	}
	defer recipeLocks.lock(id)()
	at := foundAt(s.now())
	var old []string
	// The ownership check is in the transaction that writes the rows, so a
	// recipe deleted meanwhile is ErrNotFound, not a foreign key error.
	err := db.WithTx(ctx, s.db, s.q, func(q *picturedb.Queries, _ *sql.Tx) error {
		if _, err := q.GetRecipeSource(ctx, picturedb.GetRecipeSourceParams{ID: id, HouseholdID: hid}); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: recipe %d", store.ErrNotFound, id)
		} else if err != nil {
			return err
		}
		var err error
		k := picturedb.ListFoundPictureKeysOfKindParams{RecipeID: id, HouseholdID: hid, Kind: kind}
		if old, err = q.ListFoundPictureKeysOfKind(ctx, k); err != nil {
			return err
		}
		if err := q.DeleteFoundPicturesOfKind(ctx, picturedb.DeleteFoundPicturesOfKindParams(k)); err != nil {
			return err
		}
		for _, o := range offers {
			if err := q.InsertFoundPicture(ctx, picturedb.InsertFoundPictureParams{
				HouseholdID: hid, RecipeID: id, Offer: o.ID, Kind: kind, SourceUrl: o.SourceURL,
				Key: o.Key, Width: int64(o.Width), Height: int64(o.Height), FoundAt: at,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.removeKeys(ctx, put)
		if errors.Is(err, store.ErrNotFound) {
			return err
		}
		return fmt.Errorf("record offered pictures of recipe %d: %w", id, err)
	}
	s.removeKeys(ctx, old)
	return nil
}

func (s *Service) removeKeys(ctx context.Context, keys []string) {
	for _, k := range keys {
		s.deleteObject(ctx, k, "offered picture")
	}
}

// Offered returns recipe id's offered pictures that have not expired, found
// ones first, in the order found. A recipe that is not the household's is
// ErrNotFound.
func (s *Service) Offered(ctx context.Context, id int64) ([]Offered, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.source(ctx, hid, id); err != nil {
		return nil, err
	}
	return s.offered(ctx, hid, id)
}

func (s *Service) offered(ctx context.Context, hid, id int64) ([]Offered, error) {
	rows, err := s.q.ListFoundPictures(ctx, picturedb.ListFoundPicturesParams{
		RecipeID: id, HouseholdID: hid, Cutoff: foundAt(s.now().Add(-FoundLifetime)),
	})
	if err != nil {
		return nil, fmt.Errorf("list offered pictures of recipe %d: %w", id, err)
	}
	out := make([]Offered, len(rows))
	for i, r := range rows {
		out[i] = Offered{
			ID: r.Offer, Uploaded: r.Kind == kindUpload, SourceURL: r.SourceUrl,
			Key: r.Key, Width: int(r.Width), Height: int(r.Height),
		}
	}
	return out, nil
}

// Apply runs save, the write of the edit form, and then carries out the form's
// picture choice: an offered picture's ID makes that picture recipe id's,
// ChoiceNone removes the picture and ChoiceKeep leaves it. Every offered
// picture is then deleted, whatever the choice. save may be nil.
//
// The recipe's lock is held from checking the choice to applying it, and every
// operation that replaces, sweeps or deletes offered pictures takes it, so the
// picture checked is the one applied. A choice the recipe no longer offers is
// ErrOfferGone and save does not run. An error from save is returned as it is,
// and the picture is left alone. A failure after save succeeded wraps
// ErrNotApplied: that is a failure of the media or the database, not a change
// of what was offered.
func (s *Service) Apply(ctx context.Context, id int64, c Choice, save func(context.Context) error) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	if _, err := s.source(ctx, hid, id); err != nil {
		return err
	}
	if c.Offer != "" && c.Remove {
		return fmt.Errorf("%w: a picture choice both chooses and removes", store.ErrInvalid)
	}
	defer recipeLocks.lock(id)()
	var chosen []byte
	if c.Offer != "" {
		offers, err := s.offered(ctx, hid, id)
		if err != nil {
			return err
		}
		o, ok := FindOffer(offers, c.Offer)
		if !ok {
			return fmt.Errorf("%w: %q on recipe %d", ErrOfferGone, c.Offer, id)
		}
		// Read now, so nothing after the save depends on the object still
		// being there.
		if chosen, err = s.media.Get(ctx, o.Key); err != nil {
			return fmt.Errorf("read offered picture %q of recipe %d: %w", c.Offer, id, err)
		}
	}
	if save != nil {
		if err := save(ctx); err != nil {
			return err
		}
	}
	switch {
	case c.Remove:
		if err := s.Remove(ctx, id); err != nil {
			return fmt.Errorf("%w: %w", ErrNotApplied, err)
		}
	case chosen != nil:
		if _, err := s.setProcessed(ctx, id, chosen); err != nil {
			return fmt.Errorf("%w: %w", ErrNotApplied, err)
		}
	}
	if err := s.deleteOffered(ctx, hid, id); err != nil {
		// The choice is made; the leftovers go with the sweep.
		s.log.WarnContext(ctx, "delete offered pictures after a save", "recipe_id", id, "error", err.Error())
	}
	return nil
}

// deleteOffered deletes all of recipe id's offered pictures, rows first and
// then objects. The caller holds the recipe's lock.
func (s *Service) deleteOffered(ctx context.Context, hid, id int64) error {
	var keys []string
	err := db.WithTx(ctx, s.db, s.q, func(q *picturedb.Queries, _ *sql.Tx) error {
		var err error
		if keys, err = q.ListFoundPictureKeys(ctx, picturedb.ListFoundPictureKeysParams{RecipeID: id, HouseholdID: hid}); err != nil {
			return err
		}
		return q.DeleteFoundPictures(ctx, picturedb.DeleteFoundPicturesParams{RecipeID: id, HouseholdID: hid})
	})
	if err != nil {
		return fmt.Errorf("delete offered pictures of recipe %d: %w", id, err)
	}
	s.removeKeys(ctx, keys)
	return nil
}

// SweepFound deletes every household's offered pictures older than
// FoundLifetime at now (ADR-00051), a recipe at a time under that recipe's
// lock: its expired rows in one transaction, then their objects. A recipe whose
// sweep fails does not stop the others: it returns how many it deleted in all
// and the failures joined. d is the database the rows are in.
func (r *Reaper) SweepFound(ctx context.Context, d *sql.DB, now time.Time) (int, error) {
	cutoff := foundAt(now.Add(-FoundLifetime))
	q := picturedb.New(d)
	recipes, err := q.ListRecipesWithExpiredFoundPictures(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("list recipes with expired offered pictures: %w", err)
	}
	total := 0
	var errs []error
	for _, id := range recipes {
		n, err := r.sweepRecipe(ctx, d, q, id, cutoff)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		total += n
	}
	return total, errors.Join(errs...)
}

func (r *Reaper) sweepRecipe(ctx context.Context, d *sql.DB, q *picturedb.Queries, id int64, cutoff string) (int, error) {
	defer recipeLocks.lock(id)()
	var keys []string
	var n int64
	err := db.WithTx(ctx, d, q, func(q *picturedb.Queries, _ *sql.Tx) error {
		var err error
		if keys, err = q.ListExpiredFoundPictureKeys(ctx, picturedb.ListExpiredFoundPictureKeysParams{RecipeID: id, FoundAt: cutoff}); err != nil {
			return err
		}
		n, err = q.DeleteExpiredFoundPictures(ctx, picturedb.DeleteExpiredFoundPicturesParams{RecipeID: id, FoundAt: cutoff})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("sweep expired offered pictures of recipe %d: %w", id, err)
	}
	r.Remove(ctx, keys)
	return int(n), nil
}
