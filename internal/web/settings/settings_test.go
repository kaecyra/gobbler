package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/backup"
)

var lastOK = time.Date(2026, 3, 9, 2, 30, 0, 0, time.UTC)

// fakeBackup is the job as the page sees it. Run blocks on gate when set.
type fakeBackup struct {
	mu     sync.Mutex
	status backup.Status
	runs   int
	err    error
	// entered is closed when the first Run starts; gate holds it open.
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (f *fakeBackup) Status() backup.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeBackup) Run(context.Context) (backup.Result, error) {
	f.mu.Lock()
	f.runs++
	f.mu.Unlock()
	if f.entered != nil {
		f.once.Do(func() { close(f.entered) })
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		f.status.LastError = f.err.Error()
		return backup.Result{}, f.err
	}
	f.status.LastSuccess, f.status.LastError = lastOK, ""
	return backup.Result{}, nil
}

func render(t *testing.T, v backupView) string {
	t.Helper()
	var b bytes.Buffer
	if err := settingsPage(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestPageStates(t *testing.T) {
	tests := []struct {
		name    string
		status  backup.Status
		want    []string
		notWant []string
	}{
		{
			name:    "never backed up",
			status:  backup.Status{Destination: backup.DestinationRemote},
			want:    []string{"No backup has completed yet.", "uploaded to S3"},
			notWant: []string{"Last successful backup", "Last backup failed"},
		},
		{
			name:    "healthy",
			status:  backup.Status{LastSuccess: lastOK, Destination: backup.DestinationRemote},
			want:    []string{"Last successful backup", "2026-03-09 02:30 UTC", "uploaded to S3"},
			notWant: []string{"No backup has completed", "Last backup failed", "Local only"},
		},
		{
			name:   "last run failed",
			status: backup.Status{LastSuccess: lastOK, LastError: "upload: access denied", Destination: backup.DestinationRemote},
			want:   []string{"Last backup failed: upload: access denied", "2026-03-09 02:30 UTC"},
		},
		{
			name:    "local only",
			status:  backup.Status{LastSuccess: lastOK, Destination: backup.DestinationLocalOnly},
			want:    []string{"Local only", "snapshots stay on this machine"},
			notWant: []string{"uploaded to S3"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, newBackupView(tc.status, outcomeNone))
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("page lacks %q:\n%s", w, got)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(got, w) {
					t.Errorf("page has %q:\n%s", w, got)
				}
			}
		})
	}
}

func serve(h http.Handler, method, target string, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(url.Values{}.Encode()))
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestShowRendersPage(t *testing.T) {
	f := &fakeBackup{status: backup.Status{LastSuccess: lastOK, Destination: backup.DestinationLocalOnly}}
	rec := serve(Routes(Deps{Backup: f}), http.MethodGet, "/", false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<h1>Settings</h1>") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestBackupNowHTMXShowsOutcome(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := &fakeBackup{}
		rec := serve(Routes(Deps{Backup: f}), http.MethodPost, "/backup", true)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || f.runs != 1 {
			t.Fatalf("status %d runs %d", rec.Code, f.runs)
		}
		if !strings.Contains(body, outcomeOK) || !strings.Contains(body, "2026-03-09 02:30 UTC") || strings.Contains(body, "<html") {
			t.Fatalf("want a fragment with the new status, got %s", body)
		}
	})
	t.Run("failure is a swappable 200 showing the error", func(t *testing.T) {
		f := &fakeBackup{err: errors.New("upload: access denied")}
		rec := serve(Routes(Deps{Backup: f}), http.MethodPost, "/backup", true)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, outcomeFailed) || !strings.Contains(body, "upload: access denied") {
			t.Fatalf("status %d body %s", rec.Code, body)
		}
	})
}

func TestBackupNowWithoutHTMXRedirects(t *testing.T) {
	f := &fakeBackup{}
	rec := serve(Routes(Deps{Backup: f}), http.MethodPost, "/backup", false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != Pattern || f.runs != 1 {
		t.Fatalf("status %d location %q runs %d", rec.Code, rec.Header().Get("Location"), f.runs)
	}
}

func TestDoubleSubmitStartsOneRun(t *testing.T) {
	f := &fakeBackup{entered: make(chan struct{}), gate: make(chan struct{})}
	h := Routes(Deps{Backup: f})
	first := make(chan *httptest.ResponseRecorder)
	go func() { first <- serve(h, http.MethodPost, "/backup", true) }()
	<-f.entered

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { secondDone <- serve(h, http.MethodPost, "/backup", true) }()
	select {
	case second := <-secondDone:
		if !strings.Contains(second.Body.String(), outcomeRunning) {
			t.Fatalf("second submit should report a running backup, got %s", second.Body)
		}
	case <-time.After(2 * time.Second):
		close(f.gate) // do not leave the first run blocked
		t.Fatal("second submit waited for the running backup instead of being refused")
	}
	close(f.gate)
	if rec := <-first; !strings.Contains(rec.Body.String(), outcomeOK) {
		t.Fatalf("first submit: %s", rec.Body)
	}
	if f.runs != 1 {
		t.Fatalf("runs = %d, want 1", f.runs)
	}
	// The guard is released: a later submit runs again.
	serve(h, http.MethodPost, "/backup", true)
	if f.runs != 2 {
		t.Fatalf("runs after release = %d, want 2", f.runs)
	}
}

type staticStatus backup.Status

func (s staticStatus) Status() backup.Status { return backup.Status(s) }

func TestHealthDetails(t *testing.T) {
	tests := []struct {
		name   string
		status backup.Status
		want   map[string]string
	}{
		{"never", backup.Status{}, map[string]string{"backup": "never"}},
		{"ok", backup.Status{LastSuccess: lastOK}, map[string]string{"backup": "ok", "backup_last_success": "2026-03-09T02:30:00Z"}},
		{"failed keeps the last success", backup.Status{LastSuccess: lastOK, LastError: "x"}, map[string]string{"backup": "failed", "backup_last_success": "2026-03-09T02:30:00Z"}},
		{"failed before any success", backup.Status{LastError: "x"}, map[string]string{"backup": "failed"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := HealthDetails(staticStatus(tc.status))(context.Background())
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// The health route is unauthenticated (ADR-00003): whatever the job recorded,
// only a status word and a timestamp may come out.
func TestHealthDetailsLeakNothing(t *testing.T) {
	secrets := []string{"/data/backups/db-2026.sqlite", "my-bucket", "https://s3.example.internal:9000", "AccessDenied: invalid key"}
	st := backup.Status{
		LastSuccess: lastOK,
		LastError:   strings.Join(secrets, " "),
		Destination: backup.DestinationRemote,
	}
	b, err := json.Marshal(HealthDetails(staticStatus(st))(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if strings.Contains(string(b), s) {
			t.Errorf("health detail leaks %q: %s", s, b)
		}
	}
	var got map[string]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for k := range got {
		if k != HealthBackupKey && k != HealthBackupTimeKey {
			t.Errorf("unexpected health key %q", k)
		}
	}
}

func TestErrorShownWithSecretsRedacted(t *testing.T) {
	const keyID, secret = "AKIAE2EKEYID", "s3cr3t/Value+xyz"
	msg := `upload "snap.db": Put "http://s3.local/b/snap.db": 403 InvalidAccessKeyId ` + keyID + ` signature ` + secret + ` for ` + keyID
	// A run that fails with msg leaves it in the status the fragment shows.
	f := &fakeBackup{err: errors.New(msg), status: backup.Status{LastError: msg, Destination: backup.DestinationRemote}}
	h := Routes(Deps{Backup: f, Secrets: []string{keyID, "", secret}})
	for _, tc := range []struct {
		name, method, target string
		htmx                 bool
	}{{"page", http.MethodGet, "/", false}, {"fragment", http.MethodPost, "/backup", true}} {
		t.Run(tc.name, func(t *testing.T) {
			body := serve(h, tc.method, tc.target, tc.htmx).Body.String()
			if strings.Contains(body, keyID) || strings.Contains(body, secret) {
				t.Fatalf("response contains a secret:\n%s", body)
			}
			if !strings.Contains(body, "upload &#34;snap.db&#34;") || !strings.Contains(body, "[redacted]") {
				t.Fatalf("the rest of the error should remain, with [redacted] in place of secrets:\n%s", body)
			}
		})
	}
}
