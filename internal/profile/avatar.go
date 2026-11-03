package profile

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/profile/profiledb"
)

// Avatar upload, crop and background processing (ADR-00033). The bytes live in
// the media store; this file keeps the state of the one upload a person has in
// flight (the avatar_jobs table) and the key of the processed picture
// (profiles.avatar).

// ErrBusy is returned (wrapped) when a new upload arrives while the person's
// last one is still being processed.
var ErrBusy = errors.New("avatar is being processed")

// Job states.
const (
	JobUploaded = "uploaded"
	JobQueued   = "queued"
	JobFailed   = "failed"
)

// Job is a person's avatar upload in flight.
type Job struct {
	// ID names the upload; the crop and preview requests quote it.
	ID    string
	State string
	Crop  media.Crop
	// Error is why processing failed, written for the person; empty unless
	// State is JobFailed.
	Error string
}

// Avatars runs avatar uploads: it stores them, takes the crop, and processes
// them in the background. It is safe for concurrent use.
type Avatars struct {
	s     *Store
	media media.Store
	log   *slog.Logger
	// ctx bounds the background jobs: when it ends they stop, and the job
	// stays queued for the next start.
	ctx     context.Context //nolint:containedctx // owned by the type on purpose: it is the lifetime of the jobs it starts
	wg      sync.WaitGroup
	mu      sync.Mutex
	running map[string]bool
}

// NewAvatars returns the avatar service over the profile store and media store.
// Jobs it starts end when ctx does. log may be nil.
func NewAvatars(ctx context.Context, s *Store, m media.Store, log *slog.Logger) *Avatars {
	if log == nil {
		log = slog.Default()
	}
	return &Avatars{s: s, media: m, log: log, ctx: ctx, running: map[string]bool{}}
}

// Wait blocks until the background jobs started so far have ended.
func (a *Avatars) Wait() { a.wg.Wait() }

func uploadKey(id, suffix string) string { return "avatars/uploads/" + id + suffix }

const (
	originalSuffix = ".orig"
	previewSuffix  = ".preview.jpg"
)

func (a *Avatars) newID() (string, error) {
	raw := make([]byte, addressBytes)
	if _, err := io.ReadFull(a.s.rand, raw); err != nil {
		return "", fmt.Errorf("draw upload id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Upload checks the file by decoding it, stores it and a preview to crop
// against, and records it as the person's upload in flight, replacing an
// earlier one that was never cropped or that failed. A file the person must fix
// is a *media.Refusal; one uploaded while the last is still processing is
// ErrBusy.
func (a *Avatars) Upload(ctx context.Context, email string, data []byte) (Job, error) {
	e, err := normalise(email)
	if err != nil {
		return Job{}, err
	}
	d, err := media.Decode(data)
	if err != nil {
		return Job{}, err
	}
	preview, err := media.Preview(d.Image)
	if err != nil {
		return Job{}, err
	}
	id, err := a.newID()
	if err != nil {
		return Job{}, err
	}
	old, hadOld, err := a.job(ctx, e)
	if err != nil {
		return Job{}, err
	}
	if hadOld && old.State == JobQueued {
		return Job{}, fmt.Errorf("upload for %s: %w", e, ErrBusy)
	}
	if err := a.media.Put(ctx, uploadKey(id, originalSuffix), data, "application/octet-stream"); err != nil {
		return Job{}, fmt.Errorf("store the upload: %w", err)
	}
	if err := a.media.Put(ctx, uploadKey(id, previewSuffix), preview, "image/jpeg"); err != nil {
		a.discard(ctx, id)
		return Job{}, fmt.Errorf("store the preview: %w", err)
	}
	var n int64
	err = db.WithTx(ctx, a.s.db, a.s.q, func(q *profiledb.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.UpsertAvatarJob(ctx, profiledb.UpsertAvatarJobParams{Email: e, ID: id, Now: a.s.stamp()})
		return err
	})
	if err != nil {
		a.discard(ctx, id)
		return Job{}, fmt.Errorf("record the upload for %s: %w", e, err)
	}
	if n == 0 { // queued between the read and the write
		a.discard(ctx, id)
		return Job{}, fmt.Errorf("upload for %s: %w", e, ErrBusy)
	}
	if hadOld {
		a.discard(ctx, old.ID)
	}
	return Job{ID: id, State: JobUploaded, Crop: media.Full}, nil
}

// discard removes an upload's objects. Failing to is logged, not returned: an
// orphan object costs disk and nothing else, and the caller has either
// succeeded or is already returning a more important error.
func (a *Avatars) discard(ctx context.Context, id string) {
	for _, suffix := range []string{originalSuffix, previewSuffix} {
		if err := a.media.Delete(ctx, uploadKey(id, suffix)); err != nil {
			a.log.WarnContext(ctx, "delete avatar upload object", "upload_id", id, "error", err.Error())
		}
	}
}

// Job returns the person's upload in flight, and whether they have one.
func (a *Avatars) Job(ctx context.Context, email string) (Job, bool, error) {
	e, err := normalise(email)
	if err != nil {
		return Job{}, false, err
	}
	return a.job(ctx, e)
}

func (a *Avatars) job(ctx context.Context, email string) (Job, bool, error) {
	row, err := a.s.q.GetAvatarJob(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("read avatar upload of %s: %w", email, err)
	}
	return jobOf(row), true, nil
}

func jobOf(row profiledb.AvatarJob) Job {
	return Job{ID: row.ID, State: row.State, Error: row.Error,
		Crop: media.Crop{X: row.CropX, Y: row.CropY, W: row.CropW, H: row.CropH}}
}

// Preview returns the picture to crop against for the person's upload id, or
// ErrNotFound when it is not theirs or is gone.
func (a *Avatars) Preview(ctx context.Context, email, id string) ([]byte, error) {
	j, ok, err := a.Job(ctx, email)
	if err != nil {
		return nil, err
	}
	if !ok || j.ID != id {
		return nil, fmt.Errorf("avatar upload %q: %w", id, ErrNotFound)
	}
	b, err := a.media.Get(ctx, uploadKey(id, previewSuffix))
	if errors.Is(err, media.ErrNotFound) {
		return nil, fmt.Errorf("avatar preview %q: %w", id, ErrNotFound)
	}
	return b, err
}

// Crop takes the part of upload id the person chose and starts processing it in
// the background. A rectangle outside the picture is a *media.Refusal; an
// upload that is not the person's, or is already queued, is ErrNotFound.
func (a *Avatars) Crop(ctx context.Context, email, id string, c media.Crop) error {
	e, err := normalise(email)
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	var n int64
	err = db.WithTx(ctx, a.s.db, a.s.q, func(q *profiledb.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.QueueAvatarJob(ctx, profiledb.QueueAvatarJobParams{
			CropX: c.X, CropY: c.Y, CropW: c.W, CropH: c.H, Now: a.s.stamp(), Email: e, ID: id,
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("queue avatar of %s: %w", e, err)
	}
	if n == 0 {
		return fmt.Errorf("avatar upload %q of %s: %w", id, e, ErrNotFound)
	}
	a.start(e)
	return nil
}

// Resume runs again every job that was queued when the app last stopped
// (ADR-00033). Call it once at start-up.
func (a *Avatars) Resume(ctx context.Context) error {
	rows, err := a.s.q.ListQueuedAvatarJobs(ctx)
	if err != nil {
		return fmt.Errorf("list queued avatar jobs: %w", err)
	}
	for _, r := range rows {
		a.log.InfoContext(ctx, "resume avatar job", "upload_id", r.ID)
		a.start(r.Email)
	}
	return nil
}

func (a *Avatars) start(email string) {
	a.mu.Lock()
	if a.running[email] {
		a.mu.Unlock()
		return
	}
	a.running[email] = true
	a.mu.Unlock()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer func() {
			a.mu.Lock()
			delete(a.running, email)
			a.mu.Unlock()
		}()
		a.run(a.ctx, email)
	}()
}

// genericFailure is shown when processing broke for a reason the person cannot
// act on; the real error is in the log.
const genericFailure = "Your picture could not be processed. Try uploading it again."

// run processes the person's queued job. The original stays until the processed
// picture is stored and recorded on the profile.
func (a *Avatars) run(ctx context.Context, email string) {
	row, err := a.s.q.GetAvatarJob(ctx, email)
	if err != nil {
		if ctx.Err() != nil {
			a.log.InfoContext(ctx, "avatar job not started: stopping", "error", err.Error())
			return
		}
		a.log.ErrorContext(ctx, "read avatar job", "error", err.Error())
		return
	}
	j := jobOf(row)
	if j.State != JobQueued {
		return
	}
	key, err := a.safeProcess(ctx, email, j)
	if err != nil {
		if ctx.Err() != nil {
			// Stopping, not failing: the job stays queued and runs on the next start.
			a.log.InfoContext(ctx, "avatar job interrupted", "upload_id", j.ID)
			return
		}
		msg, refused := media.RefusalText(err)
		if !refused {
			msg = genericFailure
		}
		a.log.ErrorContext(ctx, "process avatar", "upload_id", j.ID, "error", err.Error())
		if ferr := a.fail(ctx, email, j.ID, msg); ferr != nil {
			a.log.ErrorContext(ctx, "record avatar failure", "upload_id", j.ID, "error", ferr.Error())
		}
		return
	}
	a.log.InfoContext(ctx, "avatar processed", "upload_id", j.ID, "avatar", key)
}

func (a *Avatars) fail(ctx context.Context, email, id, msg string) error {
	return db.WithTx(ctx, a.s.db, a.s.q, func(q *profiledb.Queries, _ *sql.Tx) error {
		return q.FailAvatarJob(ctx, profiledb.FailAvatarJobParams{Error: msg, Now: a.s.stamp(), Email: email, ID: id})
	})
}

// safeProcess is process with a panic turned into an error. A decoder or
// encoder that panics on a hostile or odd file must fail that job, not the
// server: Resume would run the same job again at every start and the app would
// crash-loop. The original is untouched, since process only discards it after
// the result is recorded.
func (a *Avatars) safeProcess(ctx context.Context, email string, j Job) (key string, err error) {
	defer func() {
		if r := recover(); r != nil {
			a.log.ErrorContext(ctx, "avatar processing panicked", "upload_id", j.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			key, err = "", fmt.Errorf("panic while processing avatar: %v", r)
		}
	}()
	return a.process(ctx, email, j)
}

func (a *Avatars) process(ctx context.Context, email string, j Job) (string, error) {
	orig, err := a.media.Get(ctx, uploadKey(j.ID, originalSuffix))
	if err != nil {
		return "", fmt.Errorf("read the original: %w", err)
	}
	d, err := media.Decode(orig)
	if err != nil {
		return "", err
	}
	out, err := media.Avatar(d.Image, j.Crop)
	if err != nil {
		return "", err
	}
	id, err := a.newID()
	if err != nil {
		return "", err
	}
	key := "avatars/" + id + ".webp"
	if err := a.media.Put(ctx, key, out, "image/webp"); err != nil {
		return "", fmt.Errorf("store the avatar: %w", err)
	}
	var previous string
	err = db.WithTx(ctx, a.s.db, a.s.q, func(q *profiledb.Queries, _ *sql.Tx) error {
		cur, err := q.GetProfileByEmail(ctx, email)
		if err != nil {
			return err
		}
		previous = cur.Avatar
		if n, err := q.SetAvatar(ctx, profiledb.SetAvatarParams{Avatar: key, Now: a.s.stamp(), Email: email}); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("profile of %s: %w", email, ErrNotFound)
		}
		return q.DeleteAvatarJob(ctx, profiledb.DeleteAvatarJobParams{Email: email, ID: j.ID})
	})
	if err != nil {
		if derr := a.media.Delete(ctx, key); derr != nil {
			a.log.WarnContext(ctx, "delete unrecorded avatar", "avatar", key, "error", derr.Error())
		}
		return "", fmt.Errorf("record the avatar: %w", err)
	}
	// Recorded: the original and the replaced avatar are no longer needed.
	a.discard(ctx, j.ID)
	if previous != "" {
		if err := a.media.Delete(ctx, previous); err != nil {
			a.log.WarnContext(ctx, "delete replaced avatar", "avatar", previous, "error", err.Error())
		}
	}
	return key, nil
}

// Open returns the bytes of a processed avatar by its key from Profile.Avatar.
func (a *Avatars) Open(ctx context.Context, key string) ([]byte, error) {
	b, err := a.media.Get(ctx, key)
	if errors.Is(err, media.ErrNotFound) {
		return nil, fmt.Errorf("avatar %q: %w", key, ErrNotFound)
	}
	return b, err
}

func (s *Store) stamp() string { return s.now().UTC().Format(timeLayout) }
