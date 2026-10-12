package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func at(h, m int) time.Time { return time.Date(2026, 10, 12, h, m, 0, 0, time.UTC) }

func TestSnapshotName(t *testing.T) {
	got := SnapshotName(time.Date(2026, 10, 12, 3, 15, 9, 0, time.FixedZone("x", 3600)))
	if want := "gobbler-20261012T021509Z.db"; got != want {
		t.Fatalf("SnapshotName = %q, want %q", got, want)
	}
	if !IsSnapshotName(got) {
		t.Fatalf("%q should be recognised as a snapshot name", got)
	}
}

func TestIsSnapshotName(t *testing.T) {
	for _, n := range []string{"", "notes.txt", "gobbler-2026.db", "gobbler-20261012T021509Z.db.partial", "x/gobbler-20261012T021509Z.db"} {
		if IsSnapshotName(n) {
			t.Errorf("IsSnapshotName(%q) = true", n)
		}
	}
}

func TestExpired(t *testing.T) {
	a, b, c, d := SnapshotName(at(1, 0)), SnapshotName(at(2, 0)), SnapshotName(at(3, 0)), SnapshotName(at(4, 0))
	tests := []struct {
		name  string
		names []string
		keep  int
		want  []string
	}{
		{"within policy", []string{a, b}, 2, nil},
		{"drops oldest beyond keep", []string{c, a, d, b}, 2, []string{a, b}},
		{"keep one", []string{a, b, c}, 1, []string{a, b}},
		{"never touches foreign files", []string{a, "notes.txt", b, "gobbler-bad.db"}, 1, []string{a}},
		{"empty", nil, 3, nil},
		{"non-positive keep deletes nothing", []string{a, b}, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Expired(tc.names, tc.keep)
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Expired = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNextRun(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before today's time", at(1, 0), at(3, 30)},
		{"after today's time", at(5, 0), at(3, 30).AddDate(0, 0, 1)},
		{"exactly at the time waits a day", at(3, 30), at(3, 30).AddDate(0, 0, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NextRun(tc.now, "03:30")
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("NextRun = %v, want %v", got, tc.want)
			}
		})
	}
	if _, err := NextRun(at(1, 0), "25:00"); err == nil {
		t.Fatal("want error for invalid schedule")
	}
}

func openSource(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db")+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.Exec(`CREATE TABLE recipe (id INTEGER PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSnapshotIsConsistentUnderWrites(t *testing.T) {
	d := openSource(t)
	for i := range 50 {
		if _, err := d.Exec(`INSERT INTO recipe (title) VALUES (?)`, "r"+string(rune('a'+i%26))); err != nil {
			t.Fatal(err)
		}
	}
	stop := make(chan struct{})
	var written atomic.Int64
	writeErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := d.Exec(`INSERT INTO recipe (title) VALUES ('w')`); err != nil {
				writeErr <- err
				return
			}
			written.Add(1)
		}
	}()
	waitForWrites := func(n int64) {
		deadline := time.Now().Add(10 * time.Second)
		for written.Load() < n {
			select {
			case err := <-writeErr:
				t.Fatalf("concurrent writer: %v", err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatal("concurrent writer made no progress")
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitForWrites(1) // the writer is running before the snapshot starts
	dir := t.TempDir()
	path, err := Snapshot(context.Background(), d, dir, at(3, 30))
	waitForWrites(written.Load() + 1) // and still committing after it
	close(stop)
	wg.Wait()
	select {
	case werr := <-writeErr:
		t.Fatalf("concurrent writer: %v", werr)
	default:
	}
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != SnapshotName(at(3, 30)) {
		t.Fatalf("snapshot path %q", path)
	}

	snap, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	var res string
	if err := snap.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		t.Fatal(err)
	}
	if res != "ok" {
		t.Fatalf("integrity_check = %q", res)
	}
	var n int
	if err := snap.QueryRow(`SELECT count(*) FROM recipe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 50 {
		t.Fatalf("snapshot has %d rows, want at least the 50 committed before it", n)
	}
	var live int
	if err := d.QueryRow(`SELECT count(*) FROM recipe`).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live <= n {
		t.Fatalf("live database has %d rows, snapshot %d: no write landed after the snapshot, so it was not taken under writes", live, n)
	}
}

func TestSnapshotFailureLeavesNoFile(t *testing.T) {
	d := openSource(t)
	dir := t.TempDir()
	if _, err := Snapshot(context.Background(), d, dir, at(3, 30)); err != nil {
		t.Fatal(err)
	}
	// The same timestamp again: VACUUM INTO refuses to overwrite.
	if _, err := Snapshot(context.Background(), d, dir, at(3, 30)); err == nil {
		t.Fatal("want error when the snapshot already exists")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != SnapshotName(at(3, 30)) {
		t.Fatalf("directory holds %v, want only the first snapshot", entries)
	}
}

type fakeStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	putErr  error
	listErr error
	deleted []string
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (f *fakeStore) Put(_ context.Context, key, path string) error {
	if f.putErr != nil {
		return f.putErr
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = b
	return nil
}

func (f *fakeStore) List(_ context.Context, prefix string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.objects {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func localNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if IsSnapshotName(e.Name()) { // status.json lives here too
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out
}

func TestRunUploadsAndPrunesBothSides(t *testing.T) {
	d := openSource(t)
	dir := t.TempDir()
	store := newFakeStore()
	var clock time.Time
	r := &Runner{DB: d, Dir: dir, Store: store, Prefix: "gobbler/", LocalKeep: 2, RemoteKeep: 1,
		Now: func() time.Time { return clock }}

	for h := 1; h <= 3; h++ {
		clock = at(h, 0)
		if _, err := r.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{SnapshotName(at(2, 0)), SnapshotName(at(3, 0))}
	if got := localNames(t, dir); !slices.Equal(got, want) {
		t.Fatalf("local = %v, want %v", got, want)
	}
	keys, _ := store.List(context.Background(), "")
	if want := []string{"gobbler/" + SnapshotName(at(3, 0))}; !slices.Equal(keys, want) {
		t.Fatalf("remote = %v, want %v", keys, want)
	}
	st := r.Status()
	if st.Destination != DestinationRemote {
		t.Fatalf("Destination = %q, want %q", st.Destination, DestinationRemote)
	}
	if !st.LastSuccess.Equal(at(3, 0)) || st.LastError != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestRunIgnoresForeignRemoteObjects(t *testing.T) {
	d := openSource(t)
	store := newFakeStore()
	store.objects["gobbler/other.txt"] = []byte("x")
	store.objects["elsewhere/"+SnapshotName(at(0, 1))] = []byte("x")
	r := &Runner{DB: d, Dir: t.TempDir(), Store: store, Prefix: "gobbler/", LocalKeep: 1, RemoteKeep: 1,
		Now: func() time.Time { return at(3, 0) }}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("deleted %v, want nothing", store.deleted)
	}
}

func TestRunUploadFailureKeepsSnapshotAndRemoteHistory(t *testing.T) {
	d := openSource(t)
	dir := t.TempDir()
	store := newFakeStore()
	store.objects["gobbler/"+SnapshotName(at(0, 30))] = []byte("old")
	store.objects["gobbler/"+SnapshotName(at(1, 0))] = []byte("old")
	store.putErr = errors.New("connection refused")
	r := &Runner{DB: d, Dir: dir, Store: store, Prefix: "gobbler/", LocalKeep: 5, RemoteKeep: 1,
		Now: func() time.Time { return at(3, 0) }}

	if _, err := r.Run(context.Background()); err == nil {
		t.Fatal("want upload error")
	}
	if got := localNames(t, dir); len(got) != 1 {
		t.Fatalf("local snapshot should be kept, have %v", got)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("remote retention ran after a failed upload: %v", store.deleted)
	}
	st := r.Status()
	if !st.LastSuccess.IsZero() || st.LastError == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestRunLocalOnly(t *testing.T) {
	d := openSource(t)
	dir := t.TempDir()
	r := &Runner{DB: d, Dir: dir, LocalKeep: 1, Now: func() time.Time { return at(3, 0) }}
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded {
		t.Fatal("nothing should be uploaded without a store")
	}
	if got := r.Status().Destination; got != "local only" {
		t.Fatalf("Destination = %q, want %q", got, "local only")
	}
	if got := localNames(t, dir); len(got) != 1 {
		t.Fatalf("local = %v", got)
	}
}

func TestScheduleRunsAtNextTimeAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := at(3, 29)
	var waits []time.Duration
	runs := 0
	err := Schedule(ctx, "03:30", ScheduleDeps{
		Now: func() time.Time { return now },
		Sleep: func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			now = now.Add(d)
			return ctx.Err()
		},
		Run: func(context.Context) error {
			runs++
			if runs == 2 {
				cancel()
			}
			return errors.New("a failed run must not stop the schedule")
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Schedule = %v, want context.Canceled", err)
	}
	if runs != 2 || waits[0] != time.Minute || waits[1] != 24*time.Hour {
		t.Fatalf("runs=%d waits=%v", runs, waits)
	}
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		in     string
		host   string
		secure bool
	}{
		{"s3.us-west-000.backblazeb2.com", "s3.us-west-000.backblazeb2.com", true},
		{"https://acct.r2.cloudflarestorage.com", "acct.r2.cloudflarestorage.com", true},
		{"http://minio:9000", "minio:9000", false},
	}
	for _, tc := range tests {
		host, secure, err := parseEndpoint(tc.in)
		if err != nil || host != tc.host || secure != tc.secure {
			t.Errorf("parseEndpoint(%q) = %q, %v, %v", tc.in, host, secure, err)
		}
	}
	if _, _, err := parseEndpoint("ftp://x"); err == nil {
		t.Error("want error for unsupported scheme")
	}
}

func TestStatusSurvivesRestart(t *testing.T) {
	d := openSource(t)
	dir := t.TempDir()
	store := newFakeStore()
	clock := at(1, 0)
	first := &Runner{DB: d, Dir: dir, Store: store, LocalKeep: 5, RemoteKeep: 5, Now: func() time.Time { return clock }}
	if _, err := first.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = at(2, 0)
	store.putErr = errors.New("connection refused")
	if _, err := first.Run(context.Background()); err == nil {
		t.Fatal("want upload error")
	}

	second := &Runner{DB: d, Dir: dir, Store: store}
	st := second.Status()
	if !st.LastSuccess.Equal(at(1, 0)) {
		t.Fatalf("LastSuccess = %v, want %v", st.LastSuccess, at(1, 0))
	}
	if !strings.Contains(st.LastError, "connection refused") {
		t.Fatalf("LastError = %q", st.LastError)
	}
	// The status file must not be mistaken for a snapshot by retention.
	if IsSnapshotName(statusFile) {
		t.Fatal("status file name matches the snapshot pattern")
	}
}

func TestStatusUnreadableFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, statusFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := (&Runner{Dir: dir}).Status()
	if !st.LastSuccess.IsZero() || st.LastError != "" {
		t.Fatalf("status = %+v, want empty", st)
	}
}

// slowStore reports the most Put calls that were ever in flight at once.
type slowStore struct {
	*fakeStore
	active, peak atomic.Int32
}

func (s *slowStore) Put(ctx context.Context, key, path string) error {
	n := s.active.Add(1)
	defer s.active.Add(-1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(100 * time.Millisecond)
	return s.fakeStore.Put(ctx, key, path)
}

func TestConcurrentRunsAreSerialised(t *testing.T) {
	d := openSource(t)
	store := &slowStore{fakeStore: newFakeStore()}
	var tick atomic.Int32
	r := &Runner{DB: d, Dir: t.TempDir(), Store: store, LocalKeep: 5, RemoteKeep: 5,
		Now: func() time.Time { return at(1, 0).Add(time.Duration(tick.Add(1)) * time.Minute) }}

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Run(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := store.peak.Load(); got != 1 {
		t.Fatalf("%d runs were uploading at once, want runs serialised", got)
	}
}

func TestRunLogsDurationAndSize(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	d := openSource(t)
	ok := &Runner{DB: d, Dir: t.TempDir(), LocalKeep: 1, Now: func() time.Time { return at(1, 0) }}
	if _, err := ok.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	store.putErr = errors.New("boom")
	bad := &Runner{DB: d, Dir: t.TempDir(), Store: store, LocalKeep: 1, Now: func() time.Time { return at(2, 0) }}
	if _, err := bad.Run(context.Background()); err == nil {
		t.Fatal("want error")
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	byMsg := map[string]map[string]any{}
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		byMsg[m["msg"].(string)] = m
	}
	for _, msg := range []string{"backup succeeded", "backup failed"} {
		m := byMsg[msg]
		if m == nil {
			t.Fatalf("no %q log line in %s", msg, buf.String())
		}
		if _, ok := m["duration_ms"]; !ok {
			t.Errorf("%q has no duration_ms: %v", msg, m)
		}
		if size, ok := m["size_bytes"].(float64); !ok || size <= 0 {
			t.Errorf("%q size_bytes = %v, want > 0", msg, m["size_bytes"])
		}
	}
}

func TestRunReportsUploadedWhenRetentionFails(t *testing.T) {
	d := openSource(t)
	store := newFakeStore()
	store.listErr = errors.New("list denied")
	r := &Runner{DB: d, Dir: t.TempDir(), Store: store, LocalKeep: 1, RemoteKeep: 1,
		Now: func() time.Time { return at(3, 0) }}
	res, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("want the list error")
	}
	if !res.Uploaded {
		t.Fatal("Uploaded = false although the snapshot reached the store")
	}
	if len(store.objects) != 1 {
		t.Fatalf("store holds %d objects, want the uploaded snapshot", len(store.objects))
	}
}
