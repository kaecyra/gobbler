package settings_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/settings"
)

var (
	keyA = bytes.Repeat([]byte{1}, settings.KeyBytes)
	keyB = bytes.Repeat([]byte{2}, settings.KeyBytes)

	mailPassword = settings.Field{Integration: "mail", Name: "password"}
	mailHost     = settings.Field{Integration: "mail", Name: "host"}
	llmKey       = settings.Field{Integration: "llm", Name: "api_key"}
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return d
}

func newStore(t *testing.T, d *sql.DB, key []byte) *settings.Store {
	t.Helper()
	s, err := settings.New(d, key)
	if err != nil {
		t.Fatalf("settings.New: %v", err)
	}
	return s
}

func rawValue(t *testing.T, d *sql.DB, f settings.Field) []byte {
	t.Helper()
	var v []byte
	err := d.QueryRow(`SELECT value FROM integration_settings WHERE integration = ? AND field = ?`, f.Integration, f.Name).Scan(&v)
	if err != nil {
		t.Fatalf("read raw value: %v", err)
	}
	return v
}

func TestSecretRoundTrip(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	s := newStore(t, d, keyA)
	if err := s.SetSecret(ctx, mailPassword, "hunter2"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Secret(ctx, mailPassword)
	if err != nil || got != "hunter2" {
		t.Fatalf("Secret = %q, %v; want hunter2", got, err)
	}
	if bytes.Contains(rawValue(t, d, mailPassword), []byte("hunter2")) {
		t.Fatal("the stored value holds the secret in clear")
	}
}

func TestTwoWritesOfOneValueDiffer(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	s := newStore(t, d, keyA)
	if err := s.SetSecret(ctx, mailPassword, "same"); err != nil {
		t.Fatal(err)
	}
	first := rawValue(t, d, mailPassword)
	if err := s.SetSecret(ctx, mailPassword, "same"); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, rawValue(t, d, mailPassword)) {
		t.Fatal("two writes of one value gave the same ciphertext")
	}
}

func TestMovedCiphertextFailsToOpen(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	s := newStore(t, d, keyA)
	if err := s.SetSecret(ctx, mailPassword, "hunter2"); err != nil {
		t.Fatal(err)
	}
	for _, to := range []settings.Field{
		{Integration: "llm", Name: "password"},      // another integration
		{Integration: "mail", Name: "api_password"}, // another field
	} {
		_, err := d.Exec(`INSERT INTO integration_settings (integration, field, secret, value, updated_at)
			SELECT ?, ?, secret, value, updated_at FROM integration_settings WHERE integration = 'mail' AND field = 'password'`,
			to.Integration, to.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Secret(ctx, to); !errors.Is(err, settings.ErrUnreadable) {
			t.Fatalf("moved to %v: err = %v, want ErrUnreadable", to, err)
		}
	}
}

func TestWrongOrMissingKeyIsUnreadableNotUnset(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	if err := newStore(t, d, keyA).SetSecret(ctx, mailPassword, "hunter2"); err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string][]byte{"wrong key": keyB, "no key": nil} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t, d, key)
			v, err := s.Secret(ctx, mailPassword)
			if !errors.Is(err, settings.ErrUnreadable) || errors.Is(err, settings.ErrUnset) || v != "" {
				t.Fatalf("Secret = %q, %v; want empty and ErrUnreadable", v, err)
			}
			if st, err := s.SecretState(ctx, mailPassword); err != nil || st != settings.StateUnreadable {
				t.Fatalf("SecretState = %v, %v; want StateUnreadable", st, err)
			}
		})
	}
	// A field never stored is unset under any key.
	s := newStore(t, d, nil)
	if _, err := s.Secret(ctx, llmKey); !errors.Is(err, settings.ErrUnset) {
		t.Fatalf("Secret(never stored) err = %v, want ErrUnset", err)
	}
}

func TestSaveSecretWithoutKeyIsRefusedNamingTheKey(t *testing.T) {
	s := newStore(t, openDB(t), nil)
	err := s.SetSecret(context.Background(), mailPassword, "x")
	if !errors.Is(err, settings.ErrNoKey) || !strings.Contains(err.Error(), config.EnvEncryptionKey) {
		t.Fatalf("SetSecret err = %v, want ErrNoKey naming %s", err, config.EnvEncryptionKey)
	}
	if st, _ := s.SecretState(context.Background(), mailPassword); st != settings.StateUnset {
		t.Fatalf("a refused save left state %v", st)
	}
}

func TestClearAndReplace(t *testing.T) {
	ctx := context.Background()
	s := newStore(t, openDB(t), keyA)
	if err := s.SetSecret(ctx, mailPassword, "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret(ctx, mailPassword, "two"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Secret(ctx, mailPassword); got != "two" {
		t.Fatalf("after replace = %q, want two", got)
	}
	if err := s.Clear(ctx, mailPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(ctx, mailPassword); err != nil {
		t.Fatalf("clearing an unset field: %v", err)
	}
	if st, _ := s.SecretState(ctx, mailPassword); st != settings.StateUnset {
		t.Fatalf("after clear state = %v, want StateUnset", st)
	}
}

func TestPlainValues(t *testing.T) {
	ctx := context.Background()
	s := newStore(t, openDB(t), nil) // plain settings need no key
	if _, err := s.Value(ctx, mailHost); !errors.Is(err, settings.ErrUnset) {
		t.Fatalf("Value(unset) err = %v, want ErrUnset", err)
	}
	if err := s.SetValue(ctx, mailHost, "smtp.example.com"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Value(ctx, mailHost); err != nil || got != "smtp.example.com" {
		t.Fatalf("Value = %q, %v", got, err)
	}
}

func TestRefusesEmptyAndOversized(t *testing.T) {
	s := newStore(t, openDB(t), keyA)
	ctx := context.Background()
	for _, v := range []string{"", strings.Repeat("x", settings.MaxValueBytes+1)} {
		if err := s.SetSecret(ctx, mailPassword, v); !errors.Is(err, settings.ErrInvalid) {
			t.Fatalf("SetSecret(len %d) err = %v, want ErrInvalid", len(v), err)
		}
	}
}

func TestLogUnreadableNamesTheFieldAndNeverTheValue(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	sealed := newStore(t, d, keyA)
	for _, f := range []settings.Field{mailPassword, llmKey} {
		if err := sealed.SetSecret(ctx, f, "topsecretvalue"); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	if n, err := sealed.LogUnreadable(ctx, log); err != nil || n != 0 || buf.Len() != 0 {
		t.Fatalf("right key: n = %d, err = %v, log = %q; want a quiet start", n, err, buf.String())
	}
	n, err := newStore(t, d, keyB).LogUnreadable(ctx, log)
	if err != nil || n != 2 {
		t.Fatalf("wrong key: n = %d, err = %v; want 2", n, err)
	}
	out := buf.String()
	for _, want := range []string{`"level":"ERROR"`, `"integration":"mail"`, `"field":"password"`, `"integration":"llm"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %s: %s", want, out)
		}
	}
	if strings.Contains(out, "topsecretvalue") {
		t.Fatal("the log holds a secret value")
	}
	buf.Reset()
	if n, _ := newStore(t, d, nil).LogUnreadable(ctx, log); n != 2 {
		t.Fatalf("no key: n = %d, want 2", n)
	}
}

func TestParseKey(t *testing.T) {
	good := "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	if k, err := settings.ParseKey(good + "\n"); err != nil || !bytes.Equal(k, keyA) {
		t.Fatalf("ParseKey(good) = %x, %v", k, err)
	}
	for _, bad := range []string{"", "not base64!", "AQEB"} {
		_, err := settings.ParseKey(bad)
		if err == nil {
			t.Errorf("ParseKey(%q) accepted", bad)
		} else if bad != "" && strings.Contains(err.Error(), bad) {
			t.Errorf("ParseKey error repeats the value: %v", err)
		}
	}
}
