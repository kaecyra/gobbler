package llmusage_test

import (
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/llmusage"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustWindow(t *testing.T, key string) llmusage.Window {
	t.Helper()
	w, ok := llmusage.WindowByKey(key)
	if !ok {
		t.Fatalf("no window %q", key)
	}
	return w
}

func isValidUTF8(s string) bool { return utf8.ValidString(s) }

func isInvalid(err error) bool { return errors.Is(err, llmusage.ErrInvalid) }
