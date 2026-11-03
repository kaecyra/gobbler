package oauth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/oauth"
)

// principal verifies the tokens as /mcp does and returns who they act as.
func (f *fixture) principal(toks oauth.Tokens) oauth.Principal {
	f.t.Helper()
	p, err := f.store.Verify(context.Background(), toks.Access, resource)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

// A grant's writes are logged with the grant's id, listed per grant, and kept
// after the grant is revoked (ADR-00031 obligation 5, ADR-00034).
func TestGrantWritesAreLoggedPerGrantAndOutliveTheGrant(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.joined("ann@example.com", "bob@example.com")
	ann, bob := f.principal(f.connect("ann@example.com")), f.principal(f.connect("bob@example.com"))

	if err := f.store.RecordWrite(ctx, ann, h.ID, "add_to_shopping_list", "list 3"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordWrite(ctx, bob, h.ID, "check_item", "milk"); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Writes(ctx, ann)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GrantID != ann.GrantID || got[0].HouseholdID != h.ID || got[0].Tool != "add_to_shopping_list" || got[0].Target != "list 3" || !got[0].At.Equal(f.now) {
		t.Errorf("writes of ann's grant = %+v", got)
	}

	if err := f.store.RevokeGrant(ctx, "ann@example.com", ann.GrantID); err != nil {
		t.Fatal(err)
	}
	got, err = f.store.Writes(ctx, ann)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("revoking the grant erased its writes: %+v", got)
	}
	if err := f.store.RecordWrite(ctx, oauth.Principal{}, h.ID, "t", ""); err == nil {
		t.Error("RecordWrite accepted no grant")
	}
	if err := f.store.RecordWrite(ctx, oauth.Principal{GrantID: ann.GrantID, Email: ann.Email}, h.ID, "t", ""); err == nil {
		t.Error("RecordWrite accepted a grant with no snapshot")
	}
	if err := f.store.RecordWrite(ctx, bob, 0, "t", ""); err == nil {
		t.Error("RecordWrite accepted no household")
	}
	if err := f.store.RecordWrite(ctx, bob, h.ID, "", ""); err == nil {
		t.Error("RecordWrite accepted an empty tool")
	}
}

// A write is logged with the grant as it was when its token was verified, so a
// grant revoked while the request ran still has its write recorded.
func TestAGrantRevokedBetweenVerifyAndRecordIsStillLogged(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.joined("ann@example.com", "bob@example.com")
	p := f.principal(f.connect("bob@example.com"))
	if err := f.store.RevokeGrant(ctx, "bob@example.com", p.GrantID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordWrite(ctx, p, h.ID, "check_item", "milk"); err != nil {
		t.Fatalf("the write of a grant revoked mid-request: %v", err)
	}
	got, err := f.store.Writes(ctx, p)
	if err != nil || len(got) != 1 || got[0].GrantID != p.GrantID || got[0].Tool != "check_item" {
		t.Errorf("writes = %+v, %v; want the one write under the original snapshot", got, err)
	}
}

// SQLite gives a revoked grant's id to the next grant. That grant must not list
// the revoked one's writes, which stay attributed to the grant that made them.
func TestAGrantGivenARevokedGrantsIDDoesNotSeeItsWrites(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.joined("ann@example.com", "bob@example.com")
	old := f.principal(f.connect("bob@example.com"))
	if err := f.store.RecordWrite(ctx, old, h.ID, "check_item", "milk"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RevokeGrant(ctx, "bob@example.com", old.GrantID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Minute)
	fresh := f.principal(f.connect("bob@example.com"))
	if fresh.GrantID != old.GrantID {
		t.Fatalf("the new grant has id %d, not the revoked grant's %d; this test no longer exercises id reuse", fresh.GrantID, old.GrantID)
	}
	if got, err := f.store.Writes(ctx, fresh); err != nil || len(got) != 0 {
		t.Errorf("the new grant lists %+v, %v", got, err)
	}
	if got, err := f.store.Writes(ctx, old); err != nil || len(got) != 1 {
		t.Errorf("the revoked grant lists %+v, %v; want its one write", got, err)
	}
}

// The write log is its own table: grant ids are not key ids. Rolling the
// migration back drops it and the three snapshot columns it added to
// api_key_writes, and keeps the rows that table already held.
func TestGrantWritesMigrationRollsBack(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.joined("ann@example.com", "bob@example.com")
	has := func(table string) bool {
		var n int
		if err := f.d.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	snapshot := func() []string {
		rows, err := f.d.QueryContext(ctx, `SELECT name FROM pragma_table_info('api_key_writes') WHERE name LIKE 'key\_%' ESCAPE '\' ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var cols []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cols = append(cols, c)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return cols
	}
	if !has("oauth_grant_writes") {
		t.Fatal("no oauth_grant_writes table")
	}
	if got := strings.Join(snapshot(), ","); got != "key_created_at,key_id,key_owner,key_prefix" {
		t.Fatalf("api_key_writes key columns = %s", got)
	}
	if _, err := f.d.ExecContext(ctx, `INSERT INTO api_key_writes (key_id, key_prefix, key_owner, key_created_at, household_id, tool, target, created_at)
		VALUES (7, 'gob_abc', 'ann@example.com', 't', ?, 'check_item', 'milk', 't')`, h.ID); err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, f.d, db.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 25); err != nil {
		t.Fatalf("rolling back: %v", err)
	}
	if has("oauth_grant_writes") || !has("oauth_grants") || !has("api_key_writes") {
		t.Error("the rollback dropped the wrong tables")
	}
	if got := strings.Join(snapshot(), ","); got != "key_id" {
		t.Errorf("api_key_writes key columns after the rollback = %s, want only key_id", got)
	}
	var tool string
	if err := f.d.QueryRowContext(ctx, `SELECT tool FROM api_key_writes WHERE key_id = 7`).Scan(&tool); err != nil || tool != "check_item" {
		t.Errorf("the existing api_key_writes row after the rollback: %q, %v", tool, err)
	}
}

// Deleting a household deletes its history (ADR-00018): the grant write log
// goes with it by the schema's cascade, as api_key_writes does.
func TestDeletingAHouseholdDeletesItsGrantWrites(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	hs := newHouseholds(f)
	h, err := hs.Create(ctx, "Solo", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	p := f.principal(f.connect("ann@example.com"))
	if err := f.store.RecordWrite(ctx, p, h.ID, "check_item", "milk"); err != nil {
		t.Fatal(err)
	}
	if err := hs.Delete(ctx, h.ID, "ann@example.com", "Solo"); err != nil {
		t.Fatal(err)
	}
	if n := f.count("oauth_grant_writes"); n != 0 {
		t.Errorf("%d grant write rows survived the household", n)
	}
}
