// Package picture owns recipe pictures (ADR-00039): fetching candidates from a
// page's image addresses, keeping them as media objects while a draft waits,
// promoting the chosen one to the recipe, and the set-picture operation (upload,
// re-find, remove). Handlers parse the request, call this package and render;
// none of this logic lives in them.
//
// The picture's key is a row of its own, never part of the recipe document, so
// a revision restore leaves it alone. Every call is scoped to the household on
// the request context (ADR-00018).
package picture

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture/picturedb"
	"github.com/kaecyra/gobbler/internal/store"
)

// Snapshots reads a recipe's saved source. *store.Store satisfies it.
type Snapshots interface {
	GetSnapshot(ctx context.Context, id int64, kind store.SnapshotKind) (store.Snapshot, error)
}

// Service runs the picture operations. It is safe for concurrent use.
type Service struct {
	db        *sql.DB
	q         *picturedb.Queries
	media     media.Store
	snapshots Snapshots
	fetch     Fetcher
	log       *slog.Logger
	now       func() time.Time
	rand      io.Reader
	// proposals serialises the candidate operations on one proposal: their
	// object keys are fixed (drafts/<proposal_id>/<n>.webp), so two runs on
	// one proposal would otherwise delete each other's committed objects.
	proposals keyedLock
}

// New returns a Service over a migrated database. f downloads candidate
// images and must be a guarded client (NewFetcher); snapshots reads the saved
// page for a re-find. log may be nil.
func New(d *sql.DB, m media.Store, snapshots Snapshots, f Fetcher, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{db: d, q: picturedb.New(d), media: m, snapshots: snapshots, fetch: f, log: log, now: time.Now, rand: rand.Reader}
}

func scope(ctx context.Context) (int64, error) {
	h, ok := household.FromContext(ctx)
	if !ok {
		return 0, store.ErrNoHousehold
	}
	return h.ID, nil
}

// Media keys (ADR-00039). The version is random and new each time a picture
// changes, so a served picture can be cached as immutable.

func recipeKey(recipeID int64, version string) string {
	return "recipes/" + strconv.FormatInt(recipeID, 10) + "/" + version + ".webp"
}

func candidateKey(proposalID int64, n int) string {
	return "drafts/" + strconv.FormatInt(proposalID, 10) + "/" + strconv.Itoa(n) + ".webp"
}

func (s *Service) newVersion() (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return "", fmt.Errorf("draw picture version: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// source returns the page address recipe id was imported from, or
// store.ErrNotFound when the recipe is not the household's.
func (s *Service) source(ctx context.Context, hid, id int64) (string, error) {
	u, err := s.q.GetRecipeSource(ctx, picturedb.GetRecipeSourceParams{ID: id, HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: recipe %d", store.ErrNotFound, id)
	}
	if err != nil {
		return "", fmt.Errorf("read recipe %d: %w", id, err)
	}
	return u, nil
}

// Key returns the object key of recipe id's picture, and whether it has one.
func (s *Service) Key(ctx context.Context, id int64) (string, bool, error) {
	hid, err := scope(ctx)
	if err != nil {
		return "", false, err
	}
	if _, err := s.source(ctx, hid, id); err != nil {
		return "", false, err
	}
	k, err := s.q.GetPictureKey(ctx, picturedb.GetPictureKeyParams{RecipeID: id, HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read picture of recipe %d: %w", id, err)
	}
	return k, true, nil
}

// setProcessed stores an already processed WebP as recipe id's picture under a
// new key, then deletes the object it replaces. A failed delete is logged: what
// remains is unreachable bytes.
func (s *Service) setProcessed(ctx context.Context, id int64, webp []byte) (string, error) {
	hid, err := scope(ctx)
	if err != nil {
		return "", err
	}
	v, err := s.newVersion()
	if err != nil {
		return "", err
	}
	key := recipeKey(id, v)
	if err := s.media.Put(ctx, key, webp, "image/webp"); err != nil {
		return "", fmt.Errorf("store picture of recipe %d: %w", id, err)
	}
	var previous string
	// The ownership check is in the transaction that writes the row, so a
	// recipe deleted meanwhile is ErrNotFound, not a foreign key error.
	err = db.WithTx(ctx, s.db, s.q, func(q *picturedb.Queries, _ *sql.Tx) error {
		if _, err := q.GetRecipeSource(ctx, picturedb.GetRecipeSourceParams{ID: id, HouseholdID: hid}); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: recipe %d", store.ErrNotFound, id)
		} else if err != nil {
			return err
		}
		p, err := q.GetPictureKey(ctx, picturedb.GetPictureKeyParams{RecipeID: id, HouseholdID: hid})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		previous = p
		return q.SetPictureKey(ctx, picturedb.SetPictureKeyParams{RecipeID: id, HouseholdID: hid, Key: key, UpdatedAt: s.stamp()})
	})
	if err != nil {
		s.deleteObject(ctx, key, "unrecorded picture")
		if errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		return "", fmt.Errorf("record picture of recipe %d: %w", id, err)
	}
	if previous != "" {
		s.deleteObject(ctx, previous, "replaced picture")
	}
	return key, nil
}

func (s *Service) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

// deleteObject removes an object and logs, rather than returns, a failure: the
// caller has either succeeded or is returning a more important error.
func (s *Service) deleteObject(ctx context.Context, key, what string) {
	if err := s.media.Delete(ctx, key); err != nil {
		s.log.WarnContext(ctx, "delete picture object", "what", what, "key", key, "error", err.Error())
	}
}

// Upload sets file as recipe id's picture, with the limits and refusals of an
// avatar (ADR-00033): a file the person must fix is a *media.Refusal. The
// previous picture's object is deleted. It returns the new key.
func (s *Service) Upload(ctx context.Context, id int64, data []byte) (string, error) {
	hid, err := scope(ctx)
	if err != nil {
		return "", err
	}
	if _, err := s.source(ctx, hid, id); err != nil {
		return "", err
	}
	d, err := media.Decode(data)
	if err != nil {
		return "", err
	}
	webp, err := media.Picture(d.Image)
	if err != nil {
		return "", err
	}
	return s.setProcessed(ctx, id, webp)
}

// Remove clears recipe id's picture and deletes its object. A recipe without a
// picture is left as it is.
func (s *Service) Remove(ctx context.Context, id int64) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	if _, err := s.source(ctx, hid, id); err != nil {
		return err
	}
	var key string
	err = db.WithTx(ctx, s.db, s.q, func(q *picturedb.Queries, _ *sql.Tx) error {
		k, err := q.GetPictureKey(ctx, picturedb.GetPictureKeyParams{RecipeID: id, HouseholdID: hid})
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		key = k
		_, err = q.DeletePictureKey(ctx, picturedb.DeletePictureKeyParams{RecipeID: id, HouseholdID: hid})
		return err
	})
	if err != nil {
		return fmt.Errorf("remove picture of recipe %d: %w", id, err)
	}
	if key != "" {
		s.deleteObject(ctx, key, "removed picture")
	}
	return nil
}
