package mail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/settings"
)

// fakeReader serves values by field name; a name in secrets is read with
// Secret, the others with Value. A missing name is ErrUnset.
type fakeReader struct {
	values  map[string]string
	secrets map[string]string
	// secretErr replaces the password read when set.
	secretErr error
}

func (f fakeReader) Value(_ context.Context, fl settings.Field) (string, error) {
	if v, ok := f.values[fl.Name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("read: %w", settings.ErrUnset)
}

func (f fakeReader) Secret(_ context.Context, fl settings.Field) (string, error) {
	if f.secretErr != nil {
		return "", f.secretErr
	}
	if v, ok := f.secrets[fl.Name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("read: %w", settings.ErrUnset)
}

func full() fakeReader {
	return fakeReader{
		values:  map[string]string{FieldHost: "smtp.example.com", FieldPort: "587", FieldUser: "u", FieldFrom: "Gobbler <g@example.com>"},
		secrets: map[string]string{FieldPassword: "pw"},
	}
}

func TestLoad(t *testing.T) {
	ctx := context.Background()
	s, err := Load(ctx, full())
	if err != nil || s.Host != "smtp.example.com" || s.Port != 587 || s.User != "u" || s.Password != "pw" || s.Timeout != DefaultTimeout {
		t.Fatalf("Load = %+v, %v", s, err)
	}
	r := full()
	r.values[FieldTimeout] = "45s"
	if s, err := Load(ctx, r); err != nil || s.Timeout.Seconds() != 45 {
		t.Errorf("with timeout = %+v, %v", s, err)
	}
}

func TestLoadWithoutAHostOrPasswordIsOff(t *testing.T) {
	ctx := context.Background()
	noHost := full()
	delete(noHost.values, FieldHost)
	noPass := full()
	delete(noPass.secrets, FieldPassword)
	unreadable := full()
	unreadable.secretErr = fmt.Errorf("read: %w", settings.ErrUnreadable)
	for name, r := range map[string]fakeReader{"no host": noHost, "no password": noPass, "unreadable password": unreadable} {
		if _, err := Load(ctx, r); !errors.Is(err, ErrNotConfigured) {
			t.Errorf("%s: err = %v, want ErrNotConfigured", name, err)
		}
	}
}

func TestLoadRefusesHalfSavedOrMalformedSettings(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ name, key, val string }{
		{"no port", FieldPort, ""}, {"bad port", FieldPort, "http"}, {"port zero", FieldPort, "0"},
		{"no user", FieldUser, ""}, {"no from", FieldFrom, ""}, {"bad timeout", FieldTimeout, "soon"},
	} {
		r := full()
		if tc.val == "" {
			delete(r.values, tc.key)
		} else {
			r.values[tc.key] = tc.val
		}
		_, err := Load(ctx, r)
		if err == nil || errors.Is(err, ErrNotConfigured) {
			t.Errorf("%s: err = %v, want a fault that is not ErrNotConfigured", tc.name, err)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := map[string]string{FieldHost: "smtp.example.com", FieldPort: "587", FieldUser: "u", FieldFrom: "g@example.com"}
	with := func(kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range ok {
			m[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	if p := Validate(ok, true); p != "" {
		t.Errorf("complete settings: %q", p)
	}
	if p := Validate(map[string]string{}, false); p != "" {
		t.Errorf("no host is mail off, not a problem: %q", p)
	}
	for _, tc := range []struct {
		name string
		v    map[string]string
		pw   bool
		want string
	}{
		{"no port", with(FieldPort, ""), true, "Port is required"},
		{"no user", with(FieldUser, " "), true, "User is required"},
		{"no from", with(FieldFrom, ""), true, "From address is required"},
		{"no password", ok, false, "Password is required"},
		{"url host", with(FieldHost, "https://smtp.example.com"), true, "Host must be"},
		{"host with port", with(FieldHost, "smtp.example.com:587"), true, "Host must be"},
		{"bad port", with(FieldPort, "99999"), true, "Port must be"},
		{"bad from", with(FieldFrom, "not an address"), true, "From address is not"},
		{"bad timeout", with(FieldTimeout, "10 minutes"), true, "Timeout must be"},
		{"huge timeout", with(FieldTimeout, "1h"), true, "Timeout must be"},
	} {
		if p := Validate(tc.v, tc.pw); !strings.HasPrefix(p, tc.want) {
			t.Errorf("%s: %q, want it to start %q", tc.name, p, tc.want)
		}
	}
}
