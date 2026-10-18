package layout

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

// centralEquipmentNames reads the central equipment catalog as the migrations
// leave it, so an item a later migration adds is covered without editing this
// test.
func centralEquipmentNames(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	rows, err := d.QueryContext(ctx, `SELECT name FROM equipment ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

// ADR-00024: every central catalog equipment item has its own icon.
func TestEveryCentralEquipmentItemHasAnIcon(t *testing.T) {
	names := centralEquipmentNames(t)
	if len(names) == 0 {
		t.Fatal("the migrations seed no equipment")
	}
	for _, n := range names {
		if !equipHasIcon(n) {
			t.Errorf("central equipment %q has no icon; add it to equipIcons", n)
		}
	}
}

// An icon keyed by a name the catalog does not hold is dead weight that hides
// a rename.
func TestEveryEquipIconNamesACentralItem(t *testing.T) {
	have := map[string]bool{}
	for _, n := range centralEquipmentNames(t) {
		have[n] = true
	}
	for n := range equipIcons {
		if !have[n] {
			t.Errorf("equipIcons has %q, which is not central equipment", n)
		}
	}
}

func renderIcon(t *testing.T, name string) string {
	t.Helper()
	var b bytes.Buffer
	if err := EquipIcon(name).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestEquipIconIsADecorativeLineIcon(t *testing.T) {
	got := renderIcon(t, "Dutch Oven")
	for _, want := range []string{`viewBox="0 0 24 24"`, `stroke="currentColor"`, `aria-hidden="true"`, `fill="none"`, "<path"} {
		if !strings.Contains(got, want) {
			t.Errorf("icon lacks %q: %s", want, got)
		}
	}
}

func TestEquipIconFallsBackForUnknownEquipment(t *testing.T) {
	got := renderIcon(t, "my grandmother's strange gadget")
	if !strings.Contains(got, "<svg") || !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("no fallback icon: %s", got)
	}
	if got != renderIcon(t, "") {
		t.Error("unknown names and the empty name should share the generic icon")
	}
}
