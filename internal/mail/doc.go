// Package mail sends gobbler's email: invitations, over SMTP submission
// (ADR-00055). The server, port, user, from address and password are the
// "mail" integration settings on the Deployment page (ADR-00057), read on every
// send so a change takes effect without a restart.
//
// Every connection is encrypted before anything else is said: implicit TLS on
// port 465, STARTTLS that the client insists on on any other port. The
// server's certificate is verified against the configured host. Nothing turns
// either off.
//
// The package knows nothing of households or HTTP; the household service
// (internal/api/households) decides what to send and records the outcome.
package mail
