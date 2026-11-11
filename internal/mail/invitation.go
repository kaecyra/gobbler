package mail

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// expiresLayout is how the expiry is written in the email; UTC and spelled
// out, so it reads the same wherever the message is opened.
const expiresLayout = "2 January 2006, 15:04 MST"

// Invitation is the content of an invitation email (ADR-00055).
type Invitation struct {
	// To is the invited address.
	To string
	// InvitedBy is the verified email of the person who invited.
	InvitedBy string
	// Household names the household a join invitation leads to; empty for an
	// operator's invitation to start one.
	Household string
	// Link is the absolute address the invitee opens.
	Link    string
	Expires time.Time
}

func (i Invitation) what() string {
	if i.Household == "" {
		return "start a household on gobbler"
	}
	return "join " + oneLine(i.Household) + " on gobbler"
}

// Subject is the subject line.
func (i Invitation) Subject() string {
	if i.Household == "" {
		return "You are invited to start a household on gobbler"
	}
	return "You are invited to join " + oneLine(i.Household) + " on gobbler"
}

// Text is the plain-text part.
func (i Invitation) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s invited you to %s.\n\n", oneLine(i.InvitedBy), i.what())
	fmt.Fprintf(&b, "Open this link while signed in as %s to accept:\n%s\n\n", i.To, i.Link)
	fmt.Fprintf(&b, "The invitation expires %s.\n", i.Expires.UTC().Format(expiresLayout))
	return b.String()
}

// The page's tokens (internal/web/static/app.css) as literals, because mail
// clients do not read custom properties: paper, card, ink, ink-2, line,
// wattle-ink and on-accent. Styles are inline and nothing is loaded by URL.
var htmlTemplate = template.Must(template.New("invitation").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:24px 12px;background:#fcfaf5;color:#2b3634;font-family:system-ui,-apple-system,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:520px;margin:0 auto;"><tr><td style="background:#ffffff;border:1px solid #ebe6dc;border-radius:6px;padding:28px;">
<p style="margin:0 0 4px;font-size:15px;font-weight:650;letter-spacing:0.04em;color:#a64835;">gobbler</p>
<h1 style="margin:0 0 16px;font-size:22px;line-height:1.3;color:#2b3634;">{{.Heading}}</h1>
<p style="margin:0 0 24px;font-size:16px;line-height:1.5;color:#525d5a;">{{.InvitedBy}} invited you to {{.What}}.</p>
<p style="margin:0 0 24px;"><a href="{{.Link}}" style="display:inline-block;background:#a64835;color:#ffffff;text-decoration:none;font-weight:600;font-size:16px;padding:12px 24px;border-radius:6px;">Open the invitation</a></p>
<p style="margin:0 0 8px;font-size:14px;line-height:1.5;color:#525d5a;">Sign in as {{.To}} to accept. The invitation expires {{.Expires}}.</p>
<p style="margin:0;font-size:13px;line-height:1.5;color:#525d5a;word-break:break-all;">If the button does not work, open this address: {{.Link}}</p>
</td></tr></table>
</body>
</html>
`))

// HTML is the HTML part: layout, type and colour inline, one link button, no
// image and nothing loaded from an address.
func (i Invitation) HTML() (string, error) {
	heading := "Join " + oneLine(i.Household)
	if i.Household == "" {
		heading = "Start a household"
	}
	var b bytes.Buffer
	err := htmlTemplate.Execute(&b, map[string]string{
		"Subject": i.Subject(), "Heading": heading, "InvitedBy": oneLine(i.InvitedBy), "What": i.what(),
		"Link": i.Link, "To": i.To, "Expires": i.Expires.UTC().Format(expiresLayout),
	})
	if err != nil {
		return "", fmt.Errorf("render invitation email: %w", err)
	}
	return b.String(), nil
}

// Message builds the email from this content, sent from the address from.
func (i Invitation) Message(from string, now time.Time) (Message, error) {
	html, err := i.HTML()
	if err != nil {
		return Message{}, err
	}
	return Message{From: from, To: i.To, Subject: i.Subject(), Text: i.Text(), HTML: html, Date: now}, nil
}

// StatusLine says what became of an invitation's email, for the household and
// operator pages: sent is the time it last went out, already formatted, and
// empty when it never has; errText is the latest send error. failed is true
// when the latest attempt failed.
func StatusLine(sent, errText string) (line string, failed bool) {
	switch {
	case errText != "" && sent != "":
		return "Email failed: " + errText + " (last sent " + sent + ")", true
	case errText != "":
		return "Email failed: " + errText, true
	case sent != "":
		return "Emailed " + sent, false
	}
	return "Not emailed", false
}
