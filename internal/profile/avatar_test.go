package profile

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kaecyra/gobbler/internal/media"
)

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 200, G: 30, B: 30, A: 255}), image.Point{}, draw.Src)
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// failing wraps a media store and refuses Put for keys with the suffix while
// fail is set.
type failing struct {
	media.Store
	suffix string
	fail   atomic.Bool
}

func (f *failing) Put(ctx context.Context, key string, data []byte, ct string) error {
	if f.fail.Load() && strings.HasSuffix(key, f.suffix) {
		return errors.New("storage server down")
	}
	return f.Store.Put(ctx, key, data, ct)
}

type rig struct {
	t       *testing.T
	store   *Store
	media   media.Store
	avatars *Avatars
}

func newRig(t *testing.T) *rig {
	t.Helper()
	m := media.NewLocal(t.TempDir())
	s := newStore(t)
	return &rig{t: t, store: s, media: m, avatars: NewAvatars(context.Background(), s, m, nil)}
}

func (r *rig) uploadAndCrop(email string, c media.Crop) Job {
	r.t.Helper()
	ctx := context.Background()
	if _, err := r.store.Ensure(ctx, email); err != nil {
		r.t.Fatal(err)
	}
	j, err := r.avatars.Upload(ctx, email, testJPEG(r.t))
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.avatars.Crop(ctx, email, j.ID, c); err != nil {
		r.t.Fatal(err)
	}
	return j
}

func (r *rig) avatarKey(email string) string {
	r.t.Helper()
	p, err := r.store.Ensure(context.Background(), email)
	if err != nil {
		r.t.Fatal(err)
	}
	return p.Avatar
}

func (r *rig) has(key string) bool {
	_, err := r.media.Get(context.Background(), key)
	return err == nil
}

func TestUploadCropProcessStoresAWebPAndClearsTheOriginal(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	j := r.uploadAndCrop("ann@example.com", media.Crop{X: 0, Y: 0, W: 0.5, H: 1})
	if !r.has(uploadKey(j.ID, originalSuffix)) {
		t.Fatal("the original is not stored while the job is queued")
	}
	r.avatars.Wait()

	key := r.avatarKey("ann@example.com")
	if key == "" {
		t.Fatal("the profile has no avatar after processing")
	}
	b, err := r.avatars.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || format != "webp" {
		t.Fatalf("stored avatar is %q, %v, want webp", format, err)
	}
	if cfg.Width != 40 || cfg.Height != 60 {
		t.Errorf("size = %dx%d, want the 40x60 left half", cfg.Width, cfg.Height)
	}
	if _, ok, _ := r.avatars.Job(ctx, "ann@example.com"); ok {
		t.Error("the job row is still there after success")
	}
	if r.has(uploadKey(j.ID, originalSuffix)) || r.has(uploadKey(j.ID, previewSuffix)) {
		t.Error("the original or preview was kept after success")
	}

	// A second avatar replaces the first, which is deleted.
	r.uploadAndCrop("ann@example.com", media.Full)
	r.avatars.Wait()
	if next := r.avatarKey("ann@example.com"); next == key || next == "" {
		t.Errorf("avatar = %q, want a new key", next)
	}
	if r.has(key) {
		t.Error("the replaced avatar was not deleted")
	}
}

func TestOriginalIsKeptWhenProcessingFailsAndTheCropCanBeRetried(t *testing.T) {
	m := media.NewLocal(t.TempDir())
	f := &failing{Store: m, suffix: ".webp"}
	f.fail.Store(true)
	s := newStore(t)
	a := NewAvatars(context.Background(), s, f, nil)
	ctx := context.Background()
	if _, err := s.Ensure(ctx, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	j, err := a.Upload(ctx, "ann@example.com", testJPEG(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Crop(ctx, "ann@example.com", j.ID, media.Full); err != nil {
		t.Fatal(err)
	}
	a.Wait()

	got, ok, err := a.Job(ctx, "ann@example.com")
	if err != nil || !ok || got.State != JobFailed || got.Error == "" {
		t.Fatalf("Job = %+v, %v, %v, want a failed job with a message", got, ok, err)
	}
	if strings.Contains(got.Error, "storage server") {
		t.Errorf("the message %q leaks the internal error", got.Error)
	}
	if _, err := m.Get(ctx, uploadKey(j.ID, originalSuffix)); err != nil {
		t.Errorf("the original was not kept after a failure: %v", err)
	}
	if p, _ := s.Ensure(ctx, "ann@example.com"); p.Avatar != "" {
		t.Errorf("a failed job set the avatar to %q", p.Avatar)
	}

	f.fail.Store(false)
	if err := a.Crop(ctx, "ann@example.com", j.ID, media.Full); err != nil {
		t.Fatalf("crop again after a failure: %v", err)
	}
	a.Wait()
	if p, _ := s.Ensure(ctx, "ann@example.com"); p.Avatar == "" {
		t.Error("the retry did not produce an avatar")
	}
}

func TestAJobInterruptedByARestartRunsAgainAtStartUp(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	// The first process stops before its job runs: its context is already done.
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	dying := NewAvatars(stopped, r.store, r.media, nil)
	if _, err := r.store.Ensure(ctx, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	j, err := dying.Upload(ctx, "ann@example.com", testJPEG(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := dying.Crop(ctx, "ann@example.com", j.ID, media.Full); err != nil {
		t.Fatal(err)
	}
	dying.Wait()
	if got, _, _ := dying.Job(ctx, "ann@example.com"); got.State != JobQueued {
		t.Fatalf("State after the interruption = %q, want it still queued", got.State)
	}
	if r.avatarKey("ann@example.com") != "" {
		t.Fatal("the interrupted job produced an avatar")
	}

	// A new process starts and resumes it.
	next := NewAvatars(ctx, r.store, r.media, nil)
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	next.Wait()
	if r.avatarKey("ann@example.com") == "" {
		t.Error("Resume did not finish the interrupted job")
	}
}

func TestUploadRefusals(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.store.Ensure(ctx, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"over 5 MB":     bytes.Repeat([]byte{1}, media.MaxUploadBytes+1),
		"not a picture": []byte("just some text"),
		"damaged":       testJPEG(t)[:100],
	} {
		_, err := r.avatars.Upload(ctx, "ann@example.com", data)
		if msg, ok := media.RefusalText(err); !ok || msg == "" {
			t.Errorf("%s: err = %v, want a refusal with a message", name, err)
		}
	}
	if _, ok, _ := r.avatars.Job(ctx, "ann@example.com"); ok {
		t.Error("a refused upload left a job behind")
	}
}

func TestAnUploadWhileOneIsBeingProcessedIsBusy(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	held := NewAvatars(stopped, r.store, r.media, nil)
	if _, err := r.store.Ensure(ctx, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	j, err := held.Upload(ctx, "ann@example.com", testJPEG(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Crop(ctx, "ann@example.com", j.ID, media.Full); err != nil {
		t.Fatal(err)
	}
	held.Wait()
	if _, err := r.avatars.Upload(ctx, "ann@example.com", testJPEG(t)); !errors.Is(err, ErrBusy) {
		t.Errorf("Upload beside a queued job = %v, want ErrBusy", err)
	}
	if !r.has(uploadKey(j.ID, originalSuffix)) {
		t.Error("the queued job's original was removed by the refused upload")
	}
}

func TestAnUploadBelongsToItsPersonAlone(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for _, e := range []string{"ann@example.com", "bob@example.com"} {
		if _, err := r.store.Ensure(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	j, err := r.avatars.Upload(ctx, "ann@example.com", testJPEG(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.avatars.Preview(ctx, "ann@example.com", j.ID); err != nil {
		t.Errorf("own preview: %v", err)
	}
	if _, err := r.avatars.Preview(ctx, "ann@example.com", "AAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, ErrNotFound) {
		t.Errorf("preview of an id that is not the upload = %v, want ErrNotFound", err)
	}
	if _, err := r.avatars.Preview(ctx, "bob@example.com", j.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another person's preview = %v, want ErrNotFound", err)
	}
	if err := r.avatars.Crop(ctx, "bob@example.com", j.ID, media.Full); !errors.Is(err, ErrNotFound) {
		t.Errorf("another person's crop = %v, want ErrNotFound", err)
	}
	if err := r.avatars.Crop(ctx, "ann@example.com", j.ID, media.Crop{X: 0.9, Y: 0, W: 0.5, H: 1}); err == nil {
		t.Error("a crop outside the picture was accepted")
	}
}
