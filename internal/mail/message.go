package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Message is one email: a plain-text part and an HTML part carrying the same
// content (ADR-00055), nothing else.
type Message struct {
	// From is the sender, as the owner configured it.
	From string
	// To is the recipient's address.
	To      string
	Subject string
	Text    string
	HTML    string
	Date    time.Time
}

// Bytes renders the message as a multipart/alternative RFC 5322 message.
func (m Message) Bytes() ([]byte, error) {
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return nil, fmt.Errorf("from address %q: %w", m.From, err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return nil, fmt.Errorf("recipient %q: %w", m.To, err)
	}
	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	domain := from.Address[strings.LastIndex(from.Address, "@")+1:]

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, p := range []struct{ typ, content string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.typ + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, fmt.Errorf("start %s part: %w", p.typ, err)
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := io.WriteString(qp, p.content); err != nil {
			return nil, fmt.Errorf("write %s part: %w", p.typ, err)
		}
		if err := qp.Close(); err != nil {
			return nil, fmt.Errorf("finish %s part: %w", p.typ, err)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("finish message body: %w", err)
	}

	var out bytes.Buffer
	for _, h := range [][2]string{
		{"From", from.String()},
		{"To", to.Address},
		{"Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject))},
		{"Date", m.Date.UTC().Format(time.RFC1123Z)},
		{"Message-ID", "<" + id + "@" + domain + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", `multipart/alternative; boundary="` + mw.Boundary() + `"`},
	} {
		out.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// oneLine removes line breaks, so text from a person can never start a new
// header.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("draw message id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
