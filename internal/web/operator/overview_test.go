package operator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/backup"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

func TestNewOverviewView(t *testing.T) {
	ok := time.Date(2026, 3, 9, 2, 30, 0, 0, time.UTC)
	cases := []struct {
		name                       string
		pending, suggested         int
		st                         backup.Status
		wantPending, wantSuggested string
		wantBackup                 string
		wantFailed                 bool
	}{
		{"nothing waiting", 0, 0, backup.Status{}, "No invitations are waiting.", "No ingredient is waiting for review.", "No backup has completed yet.", false},
		{"one of each", 1, 1, backup.Status{LastSuccess: ok}, "1 invitation is waiting.", "1 ingredient is waiting for review.", "Last successful backup: 2026-03-09 02:30 UTC", false},
		{"several, failed backup", 3, 2, backup.Status{LastSuccess: ok, LastError: "disk full"}, "3 invitations are waiting.", "2 ingredients are waiting for review.", "The last backup failed.", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := newOverviewView(c.pending, c.suggested, c.st)
			if v.Pending != c.wantPending || v.Suggested != c.wantSuggested || v.Backup != c.wantBackup || v.BackupFailed != c.wantFailed {
				t.Errorf("= %+v", v)
			}
		})
	}
}

type fakeHouseholds struct {
	Households
	pending []apihouseholds.Invitation
}

func (f fakeHouseholds) OperatorPending(context.Context, apihouseholds.Caller) ([]apihouseholds.Invitation, error) {
	return f.pending, nil
}

type fakeSuggestions struct {
	entries []apicatalog.Ingredient
	err     error
}

func (f fakeSuggestions) OperatorListSuggestedIngredients(context.Context) ([]apicatalog.Ingredient, error) {
	return f.entries, f.err
}

type fakeBackup backup.Status

func (f fakeBackup) Status() backup.Status { return backup.Status(f) }

func serveOverview(t *testing.T, d Deps, asOperator bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := verified(t, req)
	if asOperator {
		ctx = layout.WithOperator(ctx)
	}
	rec := httptest.NewRecorder()
	Routes(d).ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestOverviewShowsWhatIsWaiting(t *testing.T) {
	d := Deps{
		Households:  fakeHouseholds{pending: make([]apihouseholds.Invitation, 2)},
		Suggestions: fakeSuggestions{entries: make([]apicatalog.Ingredient, 1)},
		Backup:      fakeBackup{LastError: "disk full"},
	}
	rec := serveOverview(t, d, true)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("= %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{"2 invitations are waiting.", "1 ingredient is waiting for review.", "The last backup failed.", `href="/operator" aria-current="page"`, `href="/operator/equipment"`} {
		if !strings.Contains(body, want) {
			t.Errorf("overview lacks %q:\n%s", want, body)
		}
	}
}

func TestOverviewRefusesANonOperator(t *testing.T) {
	rec := serveOverview(t, Deps{Households: fakeHouseholds{}, Suggestions: fakeSuggestions{}, Backup: fakeBackup{}}, false)
	if rec.Code != http.StatusForbidden {
		t.Errorf("= %d, want 403", rec.Code)
	}
}

func TestOverviewFailsLoudWhenTheCatalogFails(t *testing.T) {
	rec := serveOverview(t, Deps{Households: fakeHouseholds{}, Suggestions: fakeSuggestions{err: errors.New("db gone")}, Backup: fakeBackup{}}, true)
	if rec.Code < http.StatusInternalServerError {
		t.Errorf("= %d, want a 5xx rather than an overview with a guessed count", rec.Code)
	}
}
