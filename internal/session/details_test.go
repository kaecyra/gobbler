package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateRecordsTheClient(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token, err := f.s.Create(ctx, "ann@example.com", Client{UserAgent: "UA/1", IP: "203.0.113.5"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.s.ListForEmail(ctx, "ann@example.com", token)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListForEmail = %v, %v", got, err)
	}
	g := got[0]
	if g.Client != (Client{UserAgent: "UA/1", IP: "203.0.113.5"}) || !g.Current || !g.CreatedAt.Equal(f.now) || !g.LastUsedAt.Equal(f.now) {
		t.Errorf("session = %+v", g)
	}
}

func TestListForEmailIsTheEmailsLiveSessionsNewestFirstWithTheCurrentMarked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.create("ann@example.com")
	f.now = f.now.Add(time.Minute)
	second := f.create("ann@example.com")
	f.create("bob@example.com")

	got, err := f.s.ListForEmail(ctx, "Ann@Example.com", first)
	if err != nil || len(got) != 2 {
		t.Fatalf("ListForEmail = %v, %v; want ann's two", got, err)
	}
	if got[0].Current || !got[1].Current {
		t.Errorf("current = %v, %v; want the older, which is first's", got[0].Current, got[1].Current)
	}
	none, err := f.s.ListForEmail(ctx, "ann@example.com", "")
	if err != nil || none[0].Current || none[1].Current {
		t.Errorf("without a token nothing is current: %+v, %v", none, err)
	}

	// An expired session is not listed.
	f.now = f.now.Add(lifetime)
	if got, err := f.s.ListForEmail(ctx, "ann@example.com", second); err != nil || len(got) != 0 {
		t.Errorf("after expiry: %v, %v", got, err)
	}
}

// An ended session is refused on its next request.
func TestEndRefusesThatSessionAndOnlyThat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	keep, gone := f.create("ann@example.com"), f.create("ann@example.com")
	list, _ := f.s.ListForEmail(ctx, "ann@example.com", keep)
	var id string
	for _, i := range list {
		if !i.Current {
			id = i.ID
		}
	}
	ok, err := f.s.End(ctx, "ann@example.com", id)
	if err != nil || !ok {
		t.Fatalf("End = %v, %v", ok, err)
	}
	if _, err := f.lookup(gone); !errors.Is(err, ErrNoSession) {
		t.Errorf("ended session still live: %v", err)
	}
	if _, err := f.lookup(keep); err != nil {
		t.Errorf("the other session was ended: %v", err)
	}
	if ok, err := f.s.End(ctx, "ann@example.com", id); err != nil || ok {
		t.Errorf("ending twice = %v, %v; want false, nil", ok, err)
	}
}

// The id comes from the request and the email from the identity, so naming
// another person's session ends nothing.
func TestEndNeverEndsAnotherPersonsSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	theirs := f.create("bob@example.com")
	list, _ := f.s.ListForEmail(ctx, "bob@example.com", theirs)
	for _, id := range []string{list[0].ID, "zz", ""} {
		if ok, err := f.s.End(ctx, "ann@example.com", id); err != nil || ok {
			t.Errorf("End(%q) = %v, %v; want false, nil", id, ok, err)
		}
	}
	if _, err := f.lookup(theirs); err != nil {
		t.Errorf("bob's session was ended: %v", err)
	}
}

func TestEndOthersKeepsOnlyTheCurrentSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cur := f.create("ann@example.com")
	other1, other2 := f.create("ann@example.com"), f.create("ann@example.com")
	bob := f.create("bob@example.com")

	n, err := f.s.EndOthers(ctx, "ann@example.com", cur)
	if err != nil || n != 2 {
		t.Fatalf("EndOthers = %d, %v; want 2", n, err)
	}
	for _, tok := range []string{other1, other2} {
		if _, err := f.lookup(tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("another session still live: %v", err)
		}
	}
	for _, tok := range []string{cur, bob} {
		if _, err := f.lookup(tok); err != nil {
			t.Errorf("a session that should stay was ended: %v", err)
		}
	}
	if _, err := f.s.EndOthers(ctx, "ann@example.com", ""); err == nil {
		t.Error("EndOthers without the current token must be refused")
	}
}

func TestClientOf(t *testing.T) {
	cases := []struct {
		name, remote, cf, ua string
		want                 Client
	}{
		{"tunnel header wins", "172.18.0.2:5000", "203.0.113.9", "UA", Client{"UA", "203.0.113.9"}},
		{"v6 header", "172.18.0.2:5000", "2001:db8::1", "", Client{"", "2001:db8::1"}},
		{"junk header falls back", "172.18.0.2:5000", "not-an-ip<script>", "UA", Client{"UA", "172.18.0.2"}},
		{"no header", "192.0.2.1:1234", "", "UA", Client{"UA", "192.0.2.1"}},
		{"v6 remote", "[2001:db8::2]:80", "", "", Client{"", "2001:db8::2"}},
		{"unreadable remote", "garbage", "", "", Client{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("CF-Connecting-IP", tc.cf)
			r.Header.Set("User-Agent", tc.ua)
			if got := ClientOf(r); got != tc.want {
				t.Errorf("ClientOf = %+v, want %+v", got, tc.want)
			}
		})
	}
	long := httptest.NewRequest(http.MethodGet, "/", nil)
	long.Header.Set("User-Agent", repeat("a", 5000))
	if got := len(ClientOf(long).UserAgent); got != maxUserAgent {
		t.Errorf("stored user agent is %d bytes, want %d", got, maxUserAgent)
	}
}

func repeat(s string, n int) string {
	b := make([]byte, 0, n)
	for range n {
		b = append(b, s...)
	}
	return string(b)
}

func TestParseUserAgent(t *testing.T) {
	cases := []struct{ ua, want string }{
		{"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0", "Firefox on Linux"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.0.0", "Edge on Windows"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "Safari on macOS"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/129.0.0.0 Mobile/15E148 Safari/604.1", "Chrome on iOS"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36", "Chrome on Android"},
		{"curl/8.5.0", "curl"},
		{"", "Unknown browser"},
	}
	for _, tc := range cases {
		if got := ParseUserAgent(tc.ua).String(); got != tc.want {
			t.Errorf("ParseUserAgent(%q) = %q, want %q", tc.ua, got, tc.want)
		}
	}
}
