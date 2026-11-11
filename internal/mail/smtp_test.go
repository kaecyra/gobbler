package mail

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	nmail "net/mail"
	"strings"
	"testing"
	"time"
)

func testSettings(s *fakeServer, host string) Settings {
	return Settings{Host: host, Port: s.port(), User: "u", Password: "p", From: "gobbler <hello@example.com>", Timeout: 5 * time.Second}
}

func testMessage(t *testing.T) []byte {
	t.Helper()
	data, err := Invitation{
		To: "cy@example.com", InvitedBy: "ann@example.com", Household: "The Smiths",
		Link: "https://gobbler.example.com/invitations/AbCd1234", Expires: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC),
	}.mustMessage(t).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (i Invitation) mustMessage(t *testing.T) Message {
	t.Helper()
	m, err := i.Message("gobbler <hello@example.com>", time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

var loopback = []net.IP{net.ParseIP("127.0.0.1")}

func TestSTARTTLSSendsOnlyAfterEncryptionAndSignsIn(t *testing.T) {
	cert, roots := pki(t, nil, loopback)
	s := startServer(t, &fakeServer{starttls: true, cert: cert})
	if err := deliver(context.Background(), testSettings(s, "127.0.0.1"), false, roots, "cy@example.com", testMessage(t)); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range s.log() {
		order = append(order, strings.ToUpper(strings.SplitN(c, " ", 2)[0]))
	}
	idx := func(cmd string) int {
		for i, c := range order {
			if c == cmd {
				return i
			}
		}
		return -1
	}
	if idx("STARTTLS") < 0 || idx("AUTH") < idx("STARTTLS") || idx("MAIL") < idx("AUTH") {
		t.Errorf("commands = %v; want STARTTLS, then AUTH, then MAIL", s.log())
	}
	if !s.saw(plainAuth("u", "p")) {
		t.Errorf("credentials not presented as configured: %v", s.log())
	}
	if !s.saw("RCPT TO:<CY@EXAMPLE.COM>") || !s.saw("MAIL FROM:<HELLO@EXAMPLE.COM>") {
		t.Errorf("envelope wrong: %v", s.log())
	}
	if len(s.received()) == 0 {
		t.Error("no message data reached the server")
	}
}

func TestImplicitTLSSends(t *testing.T) {
	cert, roots := pki(t, nil, loopback)
	s := startServer(t, &fakeServer{implicit: true, cert: cert})
	if err := deliver(context.Background(), testSettings(s, "127.0.0.1"), true, roots, "cy@example.com", testMessage(t)); err != nil {
		t.Fatal(err)
	}
	if !s.saw(plainAuth("u", "p")) || len(s.received()) == 0 {
		t.Errorf("implicit TLS send incomplete: %v", s.log())
	}
}

func TestPortDecidesTheMode(t *testing.T) {
	for port, want := range map[int]bool{465: true, 587: false, 25: false, 2525: false, 4650: false} {
		if got := (Settings{Port: port}).ImplicitTLS(); got != want {
			t.Errorf("port %d: ImplicitTLS = %v, want %v", port, got, want)
		}
	}
}

// ADR-00055 obligation 1.
func TestServerWithoutSTARTTLSGetsNoCredentialsAndNoMessage(t *testing.T) {
	cert, roots := pki(t, nil, loopback)
	s := startServer(t, &fakeServer{starttls: false, cert: cert})
	err := deliver(context.Background(), testSettings(s, "127.0.0.1"), false, roots, "cy@example.com", testMessage(t))
	if !errors.Is(err, ErrNoSTARTTLS) {
		t.Fatalf("err = %v, want ErrNoSTARTTLS", err)
	}
	for _, cmd := range []string{"AUTH", "MAIL FROM", "RCPT TO", "DATA"} {
		if s.saw(cmd) {
			t.Errorf("server was sent %s: %v", cmd, s.log())
		}
	}
}

// ADR-00055 obligation 2.
func TestCertificateForAnotherHostFailsTheSend(t *testing.T) {
	cert, roots := pki(t, []string{"mail.other.example"}, nil)
	for _, implicit := range []bool{false, true} {
		s := startServer(t, &fakeServer{starttls: true, implicit: implicit, cert: cert})
		err := deliver(context.Background(), testSettings(s, "127.0.0.1"), implicit, roots, "cy@example.com", testMessage(t))
		if err == nil || !strings.Contains(err.Error(), "certificate") {
			t.Errorf("implicit=%v: err = %v, want a certificate verification failure", implicit, err)
		}
		for _, cmd := range []string{"AUTH", "MAIL FROM"} {
			if s.saw(cmd) {
				t.Errorf("implicit=%v: server was sent %s after a bad certificate", implicit, cmd)
			}
		}
	}
}

func TestCertificateFromAnUntrustedAuthorityFailsTheSend(t *testing.T) {
	cert, _ := pki(t, nil, loopback)
	_, otherRoots := pki(t, nil, loopback)
	s := startServer(t, &fakeServer{starttls: true, cert: cert})
	err := deliver(context.Background(), testSettings(s, "127.0.0.1"), false, otherRoots, "cy@example.com", testMessage(t))
	if err == nil || s.saw("AUTH") {
		t.Errorf("err = %v, AUTH seen = %v; want a failure before AUTH", err, s.saw("AUTH"))
	}
}

func TestSilentServerTimesOut(t *testing.T) {
	cert, roots := pki(t, nil, loopback)
	s := startServer(t, &fakeServer{silent: true, cert: cert})
	cfg := testSettings(s, "127.0.0.1")
	cfg.Timeout = 200 * time.Millisecond
	start := time.Now()
	err := deliver(context.Background(), cfg, false, roots, "cy@example.com", testMessage(t))
	if err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("err = %v after %s; want a timeout error well inside 3s", err, time.Since(start))
	}
}

func TestUnreachableServerFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	cfg := Settings{Host: "127.0.0.1", Port: port, From: "hello@example.com", Timeout: time.Second}
	if err := Send(context.Background(), cfg, "cy@example.com", testMessage(t)); err == nil {
		t.Error("send to a closed port succeeded")
	}
}

// ADR-00055 obligation 3.
func TestSentMessageIsPlainTextAndHTMLAndNothingElse(t *testing.T) {
	cert, roots := pki(t, nil, loopback)
	s := startServer(t, &fakeServer{starttls: true, cert: cert})
	if err := deliver(context.Background(), testSettings(s, "127.0.0.1"), false, roots, "cy@example.com", testMessage(t)); err != nil {
		t.Fatal(err)
	}
	msg, err := nmail.ReadMessage(strings.NewReader(string(s.received())))
	if err != nil {
		t.Fatal(err)
	}
	typ, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || typ != "multipart/alternative" {
		t.Fatalf("content type = %q, %v; want multipart/alternative", typ, err)
	}
	if got := msg.Header.Get("To"); got != "cy@example.com" {
		t.Errorf("To = %q", got)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types []string
	var html, text string
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		pt, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		types = append(types, pt)
		body, err := io.ReadAll(p) // multipart decodes quoted-printable itself
		if err != nil {
			t.Fatal(err)
		}
		switch pt {
		case "text/html":
			html = string(body)
		case "text/plain":
			text = string(body)
		}
	}
	if strings.Join(types, ",") != "text/plain,text/html" {
		t.Fatalf("parts = %v, want exactly text/plain then text/html", types)
	}
	for _, banned := range []string{"<img", "src=", "url(", "@import", "<link", "<script"} {
		if strings.Contains(strings.ToLower(html), banned) {
			t.Errorf("HTML contains %q", banned)
		}
	}
	for _, part := range map[string]string{"text": text, "html": html} {
		for _, want := range []string{"ann@example.com", "The Smiths", "https://gobbler.example.com/invitations/AbCd1234", "17 November 2026"} {
			if !strings.Contains(part, want) {
				t.Errorf("%q missing from a part:\n%s", want, part)
			}
		}
	}
}
