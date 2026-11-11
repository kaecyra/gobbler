package settings

import "github.com/kaecyra/gobbler/internal/mail"

// MailSection is the mail integration's part of the page (ADR-00055): the SMTP
// server's host, port, user and from address, the password as a secret, and
// the send timeout. Wiring registers it on Integrations.Sections.
//
// The page refuses a host without the rest and says which field is missing
// (mail.Validate). Clearing the host turns mail off.
func MailSection() Section {
	return Section{
		Integration: mail.Integration,
		Title:       "Mail",
		Fields: []PlainField{
			{Name: mail.FieldHost, Label: "SMTP host", Placeholder: "smtp.example.com", Hint: "Leave blank to turn mail off."},
			{Name: mail.FieldPort, Label: "Port", Placeholder: "587", Hint: "465 connects with TLS. Any other port must offer STARTTLS."},
			{Name: mail.FieldUser, Label: "User"},
			{Name: mail.FieldFrom, Label: "From address", Placeholder: "Gobbler <invites@example.com>"},
			{Name: mail.FieldTimeout, Label: "Timeout", Placeholder: mail.DefaultTimeout.String(), Hint: "How long one send may take. Optional."},
		},
		Secrets: []SecretField{{Name: mail.FieldPassword, Label: "Password"}},
		Validate: func(values map[string]string, secretSet map[string]bool) string {
			return mail.Validate(values, secretSet[mail.FieldPassword])
		},
	}
}
